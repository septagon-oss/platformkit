// Package translationtest is the conformance suite for the translation port,
// and a fake that passes it.
//
// The suite is the specification written as executable cases, and it is the
// reason the fake exists at all: the real service runs the same list against a
// real Postgres, so "the fake behaves like the real thing" is a test result and
// not a hope (AGENTS.md rule 8). A consumer that wants to test what it does with
// a Translations port takes a fake instead of a database; whoever changes the
// rules changes the list, and both implementations have to still pass it.
//
// Two things the fake enforces rather than skips, because skipping them makes a
// caller's test pass for no reason: the identity, so a second write of one
// field in one locale updates the row rather than accumulating a second one,
// and the tenant the transaction names, so a wrong-tenant read answers
// not-found here exactly as row-level security answers it there.
//
// What it cannot do is a rollback. Nothing here commits, so no case can check
// that a refused write left nothing behind *in the database*; the service's own
// test runs the same list inside a transaction it then throws away, and says so
// in its own words.
package translationtest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/translation/contracts"
)

// Row is one stored translation: the fake's map entry, holding the same facts
// the table holds. Exported because the case that pins "every row's digest
// matches its own source copy" has to read them, and a fake that hid its rows
// could not be asked that question at all.
type Row struct {
	TenantID     uuid.UUID
	Module       string
	Entity       string
	RecordID     uuid.UUID
	Field        string
	Locale       string
	Value        string
	SourceText   string
	SourceHash   string
	Status       string
	Origin       string
	ReviewedAt   *time.Time
	TranslatorID uuid.UUID
	Revision     int64
}

// Fake is contracts.Service over a slice: the same rules, no database, no
// transaction.
type Fake struct {
	mu      sync.Mutex
	rows    []*Row
	events  []contracts.Updated
	now     func() time.Time
	sources map[string]rest.TranslationSource
	// translator is the machine, and nil is the default: an installation whose
	// operator named no provider has no machine, and Suggest says so.
	translator locale.Translator
}

// NewFake returns an empty store over the same entity sources the real service
// is built over — the same list, the same type — so a case asking the fake what
// the overview looks like is asking the same question of the same declaration of
// which entities exist and which of their fields are translatable.
func NewFake(translator locale.Translator, sources ...rest.TranslationSource) *Fake {
	f := &Fake{now: db.Now, sources: map[string]rest.TranslationSource{}, translator: translator}
	for _, s := range sources {
		f.sources[s.Module()+"\x00"+s.Entity()] = s
	}
	return f
}

var _ contracts.Service = (*Fake)(nil)

// WithTranslator gives the fake a machine; with none, Suggest refuses, which is
// what every installation with no configured provider does.
func (f *Fake) WithTranslator(fn locale.Translator) *Fake {
	f.translator = fn
	return f
}

// WithClock pins "when", for the cases that name a date.
func (f *Fake) WithClock(now func() time.Time) *Fake {
	if now != nil {
		f.now = now
	}
	return f
}

// Published is the events the fake would have emitted, one name per event, in
// order. A case that wants to check silence checks this.
func (f *Fake) Published() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Repeat([]string{contracts.EventUpdated}, len(f.events))
}

// Payloads is what those events carried, which is the only way a case can check
// that translation.updated named the field that went stale rather than merely
// that something happened somewhere.
func (f *Fake) Payloads() []contracts.Updated {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.events)
}

// Rows is every stored row of the transaction's tenant, which is what makes the
// same accessor meaningful for the fake and for the SQL service: the service's
// version of this reads the table under the same policy.
func (f *Fake) Rows(_ context.Context, tx db.Tx[db.Tenant]) []Row {
	f.mu.Lock()
	defer f.mu.Unlock()
	tenantID := db.TenantOf(tx).ID
	var out []Row
	for _, r := range f.rows {
		if r.TenantID == tenantID {
			out = append(out, *r)
		}
	}
	return out
}

func (f *Fake) find(tenantID uuid.UUID, module, entity, locale string, recordID uuid.UUID, field string) *Row {
	for _, r := range f.rows {
		if r.TenantID == tenantID && r.Module == module && r.Entity == entity &&
			r.Locale == locale && r.RecordID == recordID && r.Field == field {
			return r
		}
	}
	return nil
}

// Translated mirrors internal.Service.Translated line for line, the withheld
// draft included: a machine translation nobody reviewed is not the tenant's
// Portuguese, and never reaches a public reader.
func (f *Fake) Translated(ctx context.Context, tx db.Tx[db.Tenant], q rest.TranslatedQuery) ([]rest.TranslatedRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if q.Locale == "" || len(q.RecordIDs) == 0 {
		return nil, nil
	}
	tenantID := db.TenantOf(tx).ID
	out := make([]rest.TranslatedRecord, 0, len(q.RecordIDs))
	for _, id := range q.RecordIDs {
		rec := rest.TranslatedRecord{ID: id, Fields: map[string]rest.TranslatedField{}}
		for _, field := range sortedKeys(q.Sources[id]) {
			r := f.find(tenantID, q.Module, q.Entity, q.Locale, id, field)
			if r == nil {
				continue
			}
			fld := rest.TranslatedField{Value: r.Value, Origin: r.Origin, ReviewedAt: r.ReviewedAt,
				Revision: r.Revision, SourceText: r.SourceText, SourceHash: r.SourceHash}
			if r.Origin == rest.OriginMachine && r.ReviewedAt == nil {
				// The review question first: an unreviewed draft whose source
				// moved is still unreviewed, and "outdated" does not make it the
				// tenant's Portuguese. See internal.Service.Translated.
				if q.Public {
					fld = rest.TranslatedField{Status: rest.FallbackWithheld, Revision: r.Revision, Origin: r.Origin}
				} else {
					fld.Status = rest.FallbackMachine
				}
			} else if current, ok := q.Sources[id][field]; ok {
				if hash, err := contracts.Hash(current, q.RichText[field]); err == nil && hash != r.SourceHash {
					fld.Status = rest.FallbackOutdated
				}
			}
			rec.Fields[field] = fld
		}
		out = append(out, rec)
	}
	return out, nil
}

// Save mirrors internal.Service.Save, order included: every field's revision is
// checked before any field is written, so a save that loses on one of six
// writes none of them and emits nothing.
func (f *Fake) Save(ctx context.Context, tx db.Tx[db.Tenant], q rest.SaveQuery) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if q.Locale == "" {
		return fmt.Errorf("%w: a translation needs the language it is written in", crud.ErrInvalid)
	}
	tenantID := db.TenantOf(tx).ID
	fields := sortedKeys(q.Values)
	for _, field := range fields {
		r := f.find(tenantID, q.Module, q.Entity, q.Locale, q.RecordID, field)
		if r != nil && r.Revision != q.Expected[field] {
			return fmt.Errorf("%w: the %s translation of %s changed since you read it (revision %d, you sent %d)",
				crud.ErrConflict, q.Locale, field, r.Revision, q.Expected[field])
		}
		if strings.TrimSpace(q.Values[field]) == "" {
			return fmt.Errorf("%w: a translation of %s cannot be empty; delete it instead of blanking it",
				crud.ErrInvalid, field)
		}
		if _, ok := q.Source[field]; !ok {
			return fmt.Errorf("%w: %s has no source text to translate from", crud.ErrInvalid, field)
		}
	}
	actor, _ := tenancy.ActorFrom(ctx)
	for _, field := range fields {
		r := f.find(tenantID, q.Module, q.Entity, q.Locale, q.RecordID, field)
		value := strings.TrimSpace(q.Values[field])
		hash, err := contracts.Hash(q.Source[field], q.RichText[field])
		if err != nil {
			return fmt.Errorf("%w: %s could not be measured against its source: %v", crud.ErrInvalid, field, err)
		}
		// The two facts and their two rules, as internal.Service.Save states them:
		// provenance is never erased, and a person typing over a draft is the
		// case where origin and review come apart.
		writer := q.Origin
		if writer == "" {
			writer = rest.OriginHuman
		}
		origin := writer
		if r != nil && r.Origin == rest.OriginMachine {
			origin = rest.OriginMachine
		}
		stamp := origin == rest.OriginMachine && writer == rest.OriginHuman
		unchanged := r != nil && r.Value == value && r.SourceHash == hash
		if unchanged {
			stamp = r.ReviewedAt != nil
		}
		if r != nil && unchanged && r.Origin == origin && (r.ReviewedAt != nil) == stamp {
			continue // nothing moved: no row, no revision, no event
		}
		next := Row{TenantID: tenantID, Module: q.Module, Entity: q.Entity, RecordID: q.RecordID,
			Field: field, Locale: q.Locale, Value: value, SourceText: q.Source[field],
			SourceHash: hash, Origin: origin, TranslatorID: actor,
			Status: contracts.StatusOf(origin, stamp, false), Revision: 1}
		if stamp {
			at := f.now()
			next.ReviewedAt = &at
		}
		if r == nil {
			f.rows = append(f.rows, &next)
		} else {
			r.Value, r.SourceText, r.SourceHash = next.Value, next.SourceText, next.SourceHash
			r.Origin, r.Status, r.Revision = origin, next.Status, r.Revision+1
			r.ReviewedAt, r.TranslatorID = next.ReviewedAt, actor
			next = *r
		}
		f.events = append(f.events, payload(field, &next))
	}
	return nil
}

// Review mirrors internal.Service.Review, refusal included: a translation whose
// source has moved cannot be marked reviewed, because the completeness badge
// would then be a claim nobody checked.
func (f *Fake) Review(ctx context.Context, tx db.Tx[db.Tenant], q rest.ReviewQuery) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	tenantID := db.TenantOf(tx).ID
	rows := f.holding(tenantID, q.Module, q.Entity, q.Locale, q.RecordID, q.Fields)
	if len(rows) == 0 {
		return fmt.Errorf("%w: there is no %s translation of this %s to review", crud.ErrNotFound, q.Locale, q.Entity)
	}
	for _, r := range rows {
		if q.Expected != nil && r.Revision != q.Expected[r.Field] {
			return fmt.Errorf("%w: the %s translation of %s changed since you read it (revision %d, you sent %d)",
				crud.ErrConflict, q.Locale, r.Field, r.Revision, q.Expected[r.Field])
		}
		source, ok := q.Source[r.Field]
		if !ok {
			return fmt.Errorf("%w: the %s translation of %s cannot be reviewed: its current source was not supplied to check it against",
				crud.ErrInvalid, q.Locale, r.Field)
		}
		hash, err := contracts.Hash(source, q.RichText[r.Field])
		if err != nil {
			return fmt.Errorf("%w: the %s translation of %s could not be measured against its source: %v",
				crud.ErrInvalid, q.Locale, r.Field, err)
		}
		if hash != r.SourceHash {
			return fmt.Errorf("%w: the source changed after this %s translation of %s was made; save the corrected text before marking it reviewed",
				crud.ErrInvalid, q.Locale, r.Field)
		}
	}
	actor, _ := tenancy.ActorFrom(ctx)
	for _, r := range rows {
		// Only a machine draft has a review to record; a human row is already
		// somebody's answer, and asking twice writes nothing and says nothing.
		if r.Origin != rest.OriginMachine || r.ReviewedAt != nil {
			continue
		}
		at := f.now()
		r.ReviewedAt, r.Status, r.Revision = &at, contracts.StatusOf(r.Origin, true, false), r.Revision+1
		r.TranslatorID = actor
		f.events = append(f.events, payload(r.Field, r))
	}
	return nil
}

// Suggest mirrors internal.Service.Suggest: no translator is a refusal that
// writes nothing, and a provider that hands back the text it was given is not a
// translation — it is English saved as Portuguese, which is the failure mode
// this call would otherwise never notice.
func (f *Fake) Suggest(ctx context.Context, tx db.Tx[db.Tenant], q rest.SuggestQuery) error {
	if f.translator == nil {
		return contracts.ErrNoMachine
	}
	values := map[string]string{}
	for _, field := range q.Fields {
		text := strings.TrimSpace(q.Source[field])
		if text == "" {
			continue
		}
		out, err := f.translator.Translate(ctx, text, q.From, q.Locale)
		if errors.Is(err, locale.ErrNoProvider) {
			return contracts.ErrNoMachine
		}
		if err != nil {
			return fmt.Errorf("%w: %s", crud.ErrConflict, err)
		}
		if strings.TrimSpace(out) == "" || strings.TrimSpace(out) == text {
			return fmt.Errorf("%w: %s returned the %s text unchanged, which is not a translation",
				crud.ErrConflict, q.Locale, q.From)
		}
		values[field] = out
	}
	if len(values) == 0 {
		return fmt.Errorf("%w: there is nothing in this record to translate", crud.ErrInvalid)
	}
	return f.Save(ctx, tx, rest.SaveQuery{
		Module: q.Module, Entity: q.Entity, Locale: q.Locale, RecordID: q.RecordID,
		Values: values, Expected: q.Expected, Source: q.Source,
		Origin: rest.OriginMachine, RichText: f.rich(q.Module, q.Entity),
	})
}

// Untranslate mirrors internal.Service.Untranslate: a write, so it is audited —
// the event it publishes is the only record that a Portuguese existed at all.
func (f *Fake) Untranslate(ctx context.Context, tx db.Tx[db.Tenant], q rest.ReviewQuery) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	tenantID := db.TenantOf(tx).ID
	for _, r := range f.holding(tenantID, q.Module, q.Entity, q.Locale, q.RecordID, q.Fields) {
		removed := *r
		removed.Status = rest.FallbackRemoved
		removed.Revision = r.Revision + 1
		f.rows = slices.DeleteFunc(f.rows, func(x *Row) bool { return x == r })
		f.events = append(f.events, payload(r.Field, &removed))
	}
	return nil
}

// ForgetRecord mirrors internal.Service.ForgetRecord: every locale at once,
// which is what a record's delete owes and what no foreign key can do, the row
// it points at living in another module's schema.
func (f *Fake) ForgetRecord(ctx context.Context, tx db.Tx[db.Tenant], module, entity string, recordID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	tenantID := db.TenantOf(tx).ID
	var gone []*Row
	for _, r := range f.rows {
		if r.TenantID == tenantID && r.Module == module && r.Entity == entity && r.RecordID == recordID {
			gone = append(gone, r)
		}
	}
	for _, r := range gone {
		removed := *r
		removed.Status = rest.FallbackRemoved
		removed.Revision = r.Revision + 1
		f.rows = slices.DeleteFunc(f.rows, func(x *Row) bool { return x == r })
		f.events = append(f.events, payload(r.Field, &removed))
	}
	return nil
}

// Overview mirrors internal.Service.Overview, and needs the source port for the
// same reason: a record with no row in this locale is one of the three answers
// being asked for, and a query over translations alone cannot see it.
func (f *Fake) Overview(ctx context.Context, tx db.Tx[db.Tenant], q rest.OverviewQuery) ([]rest.OverviewRow, rest.OverviewCounts, int64, error) {
	src, ok := f.sources[q.Module+"\x00"+q.Entity]
	if !ok {
		return nil, rest.OverviewCounts{}, 0, fmt.Errorf(
			"%w: nothing in this installation declares %s.%s as translatable", crud.ErrInvalid, q.Module, q.Entity)
	}
	speaks := len(q.Languages) == 0 || slices.Contains(q.Languages, q.Locale)
	if !speaks && !q.IncludeRemoved {
		return nil, rest.OverviewCounts{}, 0, nil
	}
	limit := q.Limit
	if limit <= 0 || limit > crud.MaxLimit {
		limit = crud.MaxLimit
	}
	total := int64(0)
	records, pageTotal, err := src.Page(ctx, tx, limit, q.Offset)
	if err != nil {
		return nil, rest.OverviewCounts{}, 0, err
	}
	if q.State != "" {
		// A filter over a derived state cannot be pushed into the entity's query,
		// so the whole set is judged and the page cut out of the survivors —
		// which is the only way the total can be the total after the filter.
		records = nil
		for offset := 0; ; offset += crud.MaxLimit {
			page, _, err := src.Page(ctx, tx, crud.MaxLimit, offset)
			if err != nil {
				return nil, rest.OverviewCounts{}, 0, err
			}
			records = append(records, page...)
			if len(page) < crud.MaxLimit {
				break
			}
		}
	} else {
		total = pageTotal
	}
	ids := make([]uuid.UUID, 0, len(records))
	for _, rec := range records {
		ids = append(ids, rec.ID)
	}
	live, err := src.Rows(ctx, tx, ids)
	if err != nil {
		return nil, rest.OverviewCounts{}, 0, err
	}
	current := make(map[uuid.UUID]map[string]string, len(live))
	for _, rec := range live {
		current[rec.ID] = rec.Values
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	tenantID := db.TenantOf(tx).ID
	fields, rich := src.Fields(), src.RichText()
	var counts rest.OverviewCounts
	out := make([]rest.OverviewRow, 0, len(records))
	for _, rec := range records {
		row := rest.OverviewRow{ID: rec.ID, UpdatedAt: rec.UpdatedAt, States: map[string]string{}}
		for _, field := range fields {
			stored := f.find(tenantID, q.Module, q.Entity, q.Locale, rec.ID, field)
			state := rest.StateMissing
			if stored != nil && !speaks {
				state = rest.FallbackRemoved
			} else {
				hash, err := contracts.Hash(current[rec.ID][field], rich[field])
				state = contracts.StateOf(stored != nil,
					stored != nil && stored.Origin == rest.OriginMachine && stored.ReviewedAt == nil,
					stored != nil && err == nil && hash == stored.SourceHash)
			}
			row.States[field] = state
			switch state {
			case rest.StateMissing:
				row.Missing++
				counts.Missing++
			case rest.StateOutdated:
				row.Outdated++
				counts.Outdated++
			case rest.StateMachine:
				row.Machine++
				counts.Machine++
			case rest.StateComplete:
				row.Complete++
				counts.Complete++
			}
		}
		if q.State != "" {
			matches := false
			for _, state := range row.States {
				if state == q.State {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}
		}
		out = append(out, row)
	}
	if q.State != "" {
		total = int64(len(out))
		start := min(q.Offset, len(out))
		out = out[start:min(start+limit, len(out))]
	}
	return out, counts, total, nil
}

// holding is the rows one record has in one locale, named fields only — or all
// of them when none is named, which is rest.ReviewQuery's "every field of this
// record", in field order so both implementations publish in one order.
func (f *Fake) holding(tenantID uuid.UUID, module, entity, locale string, recordID uuid.UUID, fields []string) []*Row {
	var out []*Row
	for _, r := range f.rows {
		if r.TenantID != tenantID || r.Module != module || r.Entity != entity ||
			r.Locale != locale || r.RecordID != recordID {
			continue
		}
		if len(fields) > 0 && !slices.Contains(fields, r.Field) {
			continue
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b *Row) int { return strings.Compare(a.Field, b.Field) })
	return out
}

// MarkOutdated mirrors internal.Service.MarkOutdated: the source write's half of
// the rule, touching status and nothing else — source_text and source_hash stay
// as the translator started from them, because they are the evidence the reviewer
// is working against.
func (f *Fake) MarkOutdated(_ context.Context, tx db.Tx[db.Tenant], module, entity, field string, recordID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	tenantID := db.TenantOf(tx).ID
	for _, r := range f.rows {
		if r.TenantID == tenantID && r.Module == module && r.Entity == entity &&
			r.RecordID == recordID && r.Field == field {
			r.Status = rest.FallbackOutdated
		}
	}
	return nil
}

func (f *Fake) rich(module, entity string) map[string]bool {
	if src, ok := f.sources[module+"\x00"+entity]; ok {
		return src.RichText()
	}
	return nil
}

func payload(field string, r *Row) contracts.Updated {
	return contracts.Updated{Module: r.Module, Entity: r.Entity, RecordID: r.RecordID,
		Field: field, Locale: r.Locale, Value: r.Value, Status: r.Status, Origin: r.Origin,
		SourceHash: r.SourceHash, Revision: r.Revision, Translator: r.TranslatorID, ReviewedAt: r.ReviewedAt}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
