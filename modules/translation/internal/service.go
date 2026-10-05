package internal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/translation/contracts"
)

// Row is one field of one record translated into one language. It is
// migrations/000043 and nothing else: no other module reads it, so it lives
// here and not in contracts, and the port hands out its derived fields rather
// than the row itself.
//
// There is no deleted_at. A translation is removed by a command or by its
// record's deletion; a soft-delete column nothing queries would be a column
// that only ever lies about how many translations there are.
type Row struct {
	// The struct carries its own id, tenant and timestamps rather than embedding
	// crud.Base, and the reason is one column. Base brings a soft delete, and a
	// translation is not a record: it is removed by a command or by its record's
	// deletion, and a deleted_at nothing queries is a column that only ever lies
	// about how many translations a tenant has. What crud.Create would have
	// bought — the tenant stamp, the classification of a unique violation, the
	// Validate call — the two writes below do by hand, in one line each.
	ID        uuid.UUID
	TenantID  uuid.UUID `gorm:"column:tenant_id;type:uuid;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Module     string `gorm:"column:module;type:text;not null"`
	Entity     string `gorm:"column:entity;type:text;not null"`
	RecordID   uuid.UUID
	Field      string `gorm:"column:field;type:text;not null"`
	Locale     string `gorm:"column:locale;type:text;not null"`
	Value      string `gorm:"column:value;type:text;not null"`
	SourceText string `gorm:"column:source_text;type:text;not null"`
	SourceHash string `gorm:"column:source_hash;type:char(64);not null"`
	Status     string `gorm:"column:status;type:text;not null"`
	Origin     string `gorm:"column:origin;type:text;not null"`
	ReviewedAt *time.Time
	// TranslatorID is the principal of the last write — the actor of the
	// request, off the context the same way kit/events reads it. It is a user
	// id with no foreign key, which is what "cross-module dependencies are Go
	// interfaces" costs at the database.
	TranslatorID uuid.UUID
	Revision     int64 `gorm:"column:revision;not null;default:1"`
}

// TableName pins the table, so the struct and migrations/000043 agree.
func (Row) TableName() string { return "translations" }

// Service is the translation store and the staleness rule.
//
// It holds no per-tenant and no per-process state — not a locale, not a
// language set, not a translator toggle. Every one of those arrives per
// request, on the transaction or on the query, which is what 0028's
// shared-instance mode needs and what makes two tenants asking at once
// impossible to confuse. The two things it does hold, the entity row sources
// and the optional machine translator, are configuration somebody wrote down at
// composition.
type Service struct {
	sources    map[string]rest.TranslationSource
	translator locale.Translator
	// now is the clock. A refusal never consults it, so a test that never sets
	// it never learns.
	now func() time.Time
}

// NewService builds the service over the entity row sources it may ask about,
// keyed by the Spec's own two names, and an optional machine translator which
// is nil in every installation whose operator named no provider.
func NewService(sources []rest.TranslationSource, translator locale.Translator) *Service {
	s := &Service{sources: map[string]rest.TranslationSource{}, translator: translator, now: db.Now}
	for _, src := range sources {
		s.sources[src.Module()+"\x00"+src.Entity()] = src
	}
	return s
}

// WithClock replaces the clock, which is how a test pins "Source changed on
// 1 Oct" to one instant.
func (s *Service) WithClock(now func() time.Time) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

var _ rest.Translations = (*Service)(nil)

// ErrNoMachine is the refusal a suggestion gets in an installation with no
// provider. It is exported because the conformance suite asserts the same
// sentence from both implementations, and a message two packages spell
// separately is two messages waiting to disagree.
var ErrNoMachine = fmt.Errorf("%w: this installation serves no machine translation", crud.ErrInvalid)

// source is the entity's row set for one mounted Spec, and the refusal when
// this installation was never told that entity exists. A translation addressed
// at an entity nobody mounted is not a translation of anything, and the honest
// answer is the one a caller can act on rather than an empty page.
func (s *Service) source(module, entity string) (rest.TranslationSource, error) {
	src, ok := s.sources[module+"\x00"+entity]
	if !ok {
		return nil, fmt.Errorf("%w: nothing in this installation declares %s.%s as translatable",
			crud.ErrInvalid, module, entity)
	}
	return src, nil
}

// Translated answers one locale's rows for a page of records.
//
// The status of each row is derived here, from the source text the caller is
// holding, rather than read out of the status column. That is not a redundancy:
// the column is what the source write maintained, and this is the one place
// both sides of the rule are in scope at once, so a disagreement between them
// is answered with the truth rather than with whichever was written first. The
// column stays, because the overview counts it and because 0069 names it — and
// a stored fact that can silently disagree with the rule that defines it is a
// fact nobody may quote.
func (s *Service) Translated(ctx context.Context, tx db.Tx[db.Tenant], q rest.TranslatedQuery) ([]rest.TranslatedRecord, error) {
	_ = ctx
	if q.Locale == "" || len(q.RecordIDs) == 0 {
		return nil, nil
	}
	var rows []Row
	err := tx.DB().Where("module = ? AND entity = ? AND locale = ? AND record_id IN ?",
		q.Module, q.Entity, q.Locale, q.RecordIDs).Find(&rows).Error
	if err != nil {
		return nil, crud.Classify(err)
	}
	rich := q.RichText
	byRecord := map[uuid.UUID]map[string]*Row{}
	for i := range rows {
		row := &rows[i]
		if byRecord[row.RecordID] == nil {
			byRecord[row.RecordID] = map[string]*Row{}
		}
		byRecord[row.RecordID][row.Field] = row
	}
	out := make([]rest.TranslatedRecord, 0, len(q.RecordIDs))
	for _, id := range q.RecordIDs {
		rec := rest.TranslatedRecord{ID: id, Fields: map[string]rest.TranslatedField{}}
		for field, row := range byRecord[id] {
			f := rest.TranslatedField{
				Value: row.Value, Origin: row.Origin, ReviewedAt: row.ReviewedAt,
				Revision: row.Revision, SourceText: row.SourceText, SourceHash: row.SourceHash,
			}
			current := q.Sources[id][field]
			if hash, err := Hash(current, rich[field]); err == nil && hash != row.SourceHash {
				f.Status = rest.FallbackOutdated
			} else if row.Origin == rest.OriginMachine && row.ReviewedAt == nil {
				// The one line that decides what a stranger may read. A machine
				// draft nobody has looked at is not the tenant's Portuguese;
				// on the public door it is not shown at all, and the value
				// served is the source.
				if q.Public {
					f = rest.TranslatedField{Status: rest.FallbackWithheld, Revision: row.Revision, Origin: row.Origin}
				} else {
					f.Status = rest.FallbackMachine
				}
			}
			rec.Fields[field] = f
		}
		out = append(out, rec)
	}
	return out, nil
}

// Save writes one record's translation of every field named.
//
// Serialisation with a concurrent edit of the *source* is not invented here: it
// is inherited from the source row's FOR UPDATE, which the caller took before
// calling. Within one locale, two translators editing the same field are
// settled by revision, which is the optimistic check a form needs and which
// refuses the loser rather than overwriting the winner.
//
// The order matters: every field is checked for its revision before any field
// is written, so a save that loses on one field of six writes none of them and
// publishes nothing — a half-applied translation is the state a reviewer cannot
// recover from by reloading.
func (s *Service) Save(ctx context.Context, tx db.Tx[db.Tenant], q rest.SaveQuery) error {
	if q.Locale == "" {
		return fmt.Errorf("%w: a translation needs the language it is written in", crud.ErrInvalid)
	}
	fields := make([]string, 0, len(q.Values))
	for field := range q.Values {
		fields = append(fields, field)
	}
	// Sorted, so that a save of three fields publishes its events in the same
	// order whichever order the map iterated in, and a replay of the same
	// request is comparable with the first one.
	slices.Sort(fields)

	existing, err := s.lock(ctx, tx, q.Module, q.Entity, q.Locale, q.RecordID, fields)
	if err != nil {
		return err
	}
	for _, field := range fields {
		row := existing[field]
		if row != nil && row.Revision != q.Expected[field] {
			return fmt.Errorf("%w: the %s translation of %s changed since you read it (revision %d, you sent %d)",
				crud.ErrConflict, q.Locale, field, row.Revision, q.Expected[field])
		}
		value := strings.TrimSpace(q.Values[field])
		if value == "" {
			return fmt.Errorf("%w: a translation of %s cannot be empty; delete it instead of blanking it",
				crud.ErrInvalid, field)
		}
	}
	for _, field := range fields {
		if _, ok := q.Source[field]; !ok {
			return fmt.Errorf("%w: %s has no source text to translate from", crud.ErrInvalid, field)
		}
	}

	for _, field := range fields {
		row := existing[field]
		value := strings.TrimSpace(q.Values[field])
		source := q.Source[field]
		hash, err := Hash(source, q.RichText[field])
		if err != nil {
			return fmt.Errorf("%w: %s could not be measured against its source: %v", crud.ErrInvalid, field, err)
		}
		origin := q.Origin
		if origin == "" {
			origin = rest.OriginHuman
		}
		if row != nil && row.Value == value && row.SourceHash == hash && row.Origin == origin &&
			(origin == rest.OriginHuman || row.ReviewedAt == nil) {
			continue // nothing moved: no row, no revision, no event
		}
		next := Row{
			Module: q.Module, Entity: q.Entity, RecordID: q.RecordID,
			Field: field, Locale: q.Locale, Value: value,
			SourceText: source, SourceHash: hash, Origin: origin,
			// A translation write always re-bases onto the source it was made
			// from, so the pair matches by construction: only a machine draft is
			// not yet somebody's answer.
			Status: StatusOf(origin, false),
		}
		if row == nil {
			next.ID = uuid.New()
			next.Revision = 1
			next.TenantID = db.TenantOf(tx).ID
			next.CreatedAt, next.UpdatedAt = s.now(), s.now()
			// The tenant comes from the transaction and never from the caller,
			// which is what crud.Create does and the one part of it a row
			// outside crud still owes.
			if err := tx.DB().Create(&next).Error; err != nil {
				return crud.Classify(err)
			}
		} else {
			next.ID, next.Revision = row.ID, row.Revision+1
			next.ReviewedAt = row.ReviewedAt
			// A person's own text is reviewed by being typed, so a human write
			// clears the stamp rather than inheriting one from a draft nobody
			// accepted. A machine write keeps whatever review stands.
			if origin == rest.OriginHuman {
				next.ReviewedAt = nil
			}
			if err := s.rewrite(ctx, tx, &next); err != nil {
				return err
			}
		}
		if err := s.publish(ctx, tx, q.Module, q.Entity, q.RecordID, field, q.Locale, &next); err != nil {
			return err
		}
	}
	return nil
}

// rewrite writes every column of one row by hand.
//
// crud.Update with a column list would be the house style, and cannot be: a
// translation row has nine columns and a write that leaves reviewed_at out of
// the list silently keeps a stale approval on text nobody has read since. The
// column list is written out so that the day a tenth column arrives, the
// omission is a compile-adjacent choice somebody made rather than an oversight
// inside a struct scan.
func (s *Service) rewrite(ctx context.Context, tx db.Tx[db.Tenant], row *Row) error {
	result := tx.DB().Model(&Row{}).Where("id = ?", row.ID).Updates(map[string]any{
		"value": row.Value, "source_text": row.SourceText, "source_hash": row.SourceHash,
		"status": row.Status, "origin": row.Origin, "reviewed_at": row.ReviewedAt,
		"translator_id": row.TranslatorID, "revision": row.Revision, "updated_at": s.now(),
	})
	if result.Error != nil {
		return crud.Classify(result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: the %s translation of %s is gone", crud.ErrNotFound, row.Locale, row.Field)
	}
	return nil
}

// lock reads the named fields' rows for one record and locale, taking a row
// lock on each. The critical section here spans several rows of one table for
// one record, and the source row's lock above it is what orders it against a
// source edit; within the table, FOR UPDATE on the rows themselves is enough,
// so no advisory lock is taken where a row lock already serialises the pair.
func (s *Service) lock(ctx context.Context, tx db.Tx[db.Tenant], module, entity, locale string, recordID uuid.UUID, fields []string) (map[string]*Row, error) {
	_ = ctx
	if len(fields) == 0 {
		return nil, nil
	}
	var rows []Row
	err := tx.DB().Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("module = ? AND entity = ? AND locale = ? AND record_id = ? AND field IN ?",
			module, entity, locale, recordID, fields).Find(&rows).Error
	if err != nil {
		return nil, crud.Classify(err)
	}
	out := make(map[string]*Row, len(rows))
	for i := range rows {
		out[rows[i].Field] = &rows[i]
	}
	return out, nil
}

// Review marks one record's fields reviewed.
//
// A field whose source has moved since the translation was made is refused, and
// the refusal is the acceptance criterion: journey 2 ends with "fix it and mark
// reviewed", and a review that could be pressed over a stale paragraph would
// make the completeness badge mean nothing. Fixing it is a write, and a write
// re-hashes.
func (s *Service) Review(ctx context.Context, tx db.Tx[db.Tenant], q rest.ReviewQuery) error {
	rows, err := s.lock(ctx, tx, q.Module, q.Entity, q.Locale, q.RecordID, q.Fields)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("%w: there is no %s translation of this %s to review",
			crud.ErrNotFound, q.Locale, q.Entity)
	}
	for _, row := range rows {
		if q.Expected != nil && row.Revision != q.Expected[row.Field] {
			return fmt.Errorf("%w: the %s translation of %s changed since you read it (revision %d, you sent %d)",
				crud.ErrConflict, q.Locale, row.Field, row.Revision, q.Expected[row.Field])
		}
		source, ok := q.Source[row.Field]
		if !ok {
			continue
		}
		hash, err := Hash(source, q.RichText[row.Field])
		if err == nil && hash != row.SourceHash {
			return fmt.Errorf("%w: the source changed after this %s translation of %s was made; save the corrected text before marking it reviewed",
				crud.ErrInvalid, q.Locale, row.Field)
		}
	}
	for _, row := range rows {
		// Only a machine draft has a review to record. A human row is already
		// somebody's answer — the CHECK on the table says a review stamp means
		// a machine text — so marking one reviewed is the same pair asked for
		// twice: it succeeds, writes nothing and publishes nothing.
		if row.Origin != rest.OriginMachine || row.ReviewedAt != nil {
			continue
		}
		reviewed := s.now()
		row.ReviewedAt = &reviewed
		row.Status = rest.StateComplete
		row.Revision++
		if err := s.rewrite(ctx, tx, row); err != nil {
			return err
		}
		if err := s.publish(ctx, tx, row.Module, row.Entity, row.RecordID, row.Field, row.Locale, row); err != nil {
			return err
		}
	}
	return nil
}

// Suggest asks the machine for a draft of each named field and saves it as
// exactly that. Nothing here is served publicly until a person reviews it, and
// nothing here is saved when the provider is absent, refuses, or hands back the
// text it was given — a provider that echoes English would otherwise be saved
// as Portuguese, which is the failure this call would never notice.
func (s *Service) Suggest(ctx context.Context, tx db.Tx[db.Tenant], q rest.SuggestQuery) error {
	if s.translator == nil {
		return ErrNoMachine
	}
	src, err := s.source(q.Module, q.Entity)
	if err != nil {
		return err
	}
	rich := src.RichText()
	values := map[string]string{}
	for _, field := range q.Fields {
		text := strings.TrimSpace(q.Source[field])
		if text == "" {
			continue
		}
		out, err := s.translator.Translate(ctx, text, q.From, q.Locale)
		if errors.Is(err, locale.ErrNoProvider) {
			return ErrNoMachine
		}
		if err != nil {
			// The provider's own words, unwrapped: whoever reads the refusal is
			// the person who can do something about the provider.
			return fmt.Errorf("%w: %s", crud.ErrConflict, err)
		}
		if strings.TrimSpace(out) == "" {
			return fmt.Errorf("%w: %s returned no translation", crud.ErrConflict, q.Locale)
		}
		if strings.TrimSpace(out) == text {
			return fmt.Errorf("%w: %s returned the %s text unchanged, which is not a translation",
				crud.ErrConflict, q.Locale, q.From)
		}
		values[field] = out
	}
	if len(values) == 0 {
		return fmt.Errorf("%w: there is nothing in this record to translate", crud.ErrInvalid)
	}
	return s.Save(ctx, tx, rest.SaveQuery{
		Module: q.Module, Entity: q.Entity, Locale: q.Locale, RecordID: q.RecordID,
		Values: values, Expected: q.Expected, Source: q.Source,
		Origin: rest.OriginMachine, RichText: rich,
	})
}

// Untranslate deletes one record's rows in one locale. It is a write, so it is
// audited: the event names the field that went away, which is the only record
// that a Portuguese translation existed and somebody removed it.
func (s *Service) Untranslate(ctx context.Context, tx db.Tx[db.Tenant], q rest.ReviewQuery) error {
	rows, err := s.lock(ctx, tx, q.Module, q.Entity, q.Locale, q.RecordID, q.Fields)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := tx.DB().Where("id = ?", row.ID).Delete(&Row{}).Error; err != nil {
			return crud.Classify(err)
		}
		removed := *row
		removed.Status = rest.FallbackRemoved
		removed.Revision = row.Revision + 1
		if err := s.publish(ctx, tx, row.Module, row.Entity, row.RecordID, row.Field, row.Locale, &removed); err != nil {
			return err
		}
	}
	return nil
}

// ForgetRecord deletes every translation of one record, in every locale. The
// record's own delete runs it inside the same transaction as the row delete,
// and it is the whole of what keeps record_id honest: no foreign key can span
// two modules' schemas, so the promise is a command and a test rather than a
// constraint.
func (s *Service) ForgetRecord(ctx context.Context, tx db.Tx[db.Tenant], module, entity string, recordID uuid.UUID) error {
	_ = ctx
	var rows []Row
	err := tx.DB().Where("module = ? AND entity = ? AND record_id = ?", module, entity, recordID).Find(&rows).Error
	if err != nil {
		return crud.Classify(err)
	}
	for _, row := range rows {
		if err := tx.DB().Where("id = ?", row.ID).Delete(&Row{}).Error; err != nil {
			return crud.Classify(err)
		}
		if err := s.publish(ctx, tx, module, entity, recordID, row.Field, row.Locale, &row); err != nil {
			return err
		}
	}
	return nil
}

// Overview answers one entity and one locale over a page of the entity's own
// rows.
//
// The page of records comes from the Source port and not from this table, which
// is the entire reason that port exists: a record with no row in this locale is
// one of the three answers being asked for, and a query over translations alone
// cannot see it.
func (s *Service) Overview(ctx context.Context, tx db.Tx[db.Tenant], q rest.OverviewQuery) ([]rest.OverviewRow, rest.OverviewCounts, int64, error) {
	src, err := s.source(q.Module, q.Entity)
	if err != nil {
		return nil, rest.OverviewCounts{}, 0, err
	}
	limit := q.Limit
	if limit <= 0 || limit > crud.MaxLimit {
		limit = crud.MaxLimit
	}
	records, total, err := src.Page(ctx, tx, limit, q.Offset)
	if err != nil {
		return nil, rest.OverviewCounts{}, 0, err
	}
	ids := make([]uuid.UUID, 0, len(records))
	for _, rec := range records {
		ids = append(ids, rec.ID)
	}
	var rows []Row
	if len(ids) > 0 {
		err := tx.DB().Where("module = ? AND entity = ? AND locale = ? AND record_id IN ?",
			q.Module, q.Entity, q.Locale, ids).Find(&rows).Error
		if err != nil {
			return nil, rest.OverviewCounts{}, 0, crud.Classify(err)
		}
	}
	byRecord := map[uuid.UUID]map[string]*Row{}
	for i := range rows {
		row := &rows[i]
		if byRecord[row.RecordID] == nil {
			byRecord[row.RecordID] = map[string]*Row{}
		}
		byRecord[row.RecordID][row.Field] = row
	}
	fields := src.Fields()
	rich := src.RichText()

	out := make([]rest.OverviewRow, 0, len(records))
	var counts rest.OverviewCounts
	for _, rec := range records {
		row := rest.OverviewRow{ID: rec.ID, UpdatedAt: rec.UpdatedAt, States: map[string]string{}}
		for _, field := range fields {
			stored := byRecord[rec.ID][field]
			hash, err := Hash(rec.Values[field], rich[field])
			state := StateOf(stored != nil,
				stored != nil && stored.Origin == rest.OriginMachine && stored.ReviewedAt == nil,
				stored != nil && err == nil && hash == stored.SourceHash)
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
		out = append(out, row)
	}
	return out, counts, total, nil
}

// StateOf is the overview's whole rule, in one expression, exported so the
// conformance fake derives a state the same way the service does rather than
// keeping a second opinion about what "outdated" means.
//
// A machine draft is counted in a column of its own and never as complete: an
// "Up-to-date" badge on text nobody has read is the exact lie this screen
// exists to avoid. And a source this module cannot even measure is a source no
// translation may be trusted against, which is outdated and not complete.
func StateOf(hasRow, unreviewedMachine, sourceStillMatches bool) string {
	switch {
	case !hasRow:
		return rest.StateMissing
	case !sourceStillMatches:
		return rest.StateOutdated
	case unreviewedMachine:
		return rest.StateMachine
	default:
		return rest.StateComplete
	}
}

// publish writes translation.updated in the caller's transaction, so the row
// and the event that describes it commit together or neither does — the
// outbox's whole reason for existing, and the reason nothing here calls the
// audit module directly: modules/audit subscribes to every declared event, so
// emitting this one is what puts a row in the trail, with the actor, the
// request id and the trace parent the envelope already carries.
func (s *Service) publish(ctx context.Context, tx db.Tx[db.Tenant], module, entity string, recordID uuid.UUID, field, locale string, row *Row) error {
	return events.Publish(ctx, tx, contracts.EventUpdated, &contracts.Updated{
		Module: module, Entity: entity, RecordID: recordID, Field: field, Locale: locale,
		Value: row.Value, Status: row.Status, Origin: row.Origin, SourceHash: row.SourceHash,
		Revision: row.Revision, Translator: row.TranslatorID, ReviewedAt: row.ReviewedAt,
	})
}

// StatusOf is the staleness rule as one expression, used by the write that
// sets a stored status and by the source write that marks it outdated. Two
// places that each decide what "outdated" means is how a badge starts lying.
func StatusOf(origin string, stale bool) string {
	if stale {
		return rest.FallbackOutdated
	}
	if origin == rest.OriginMachine {
		return rest.FallbackMachine
	}
	return rest.StateComplete
}

// MarkOutdated is the other half of the same rule, and the half a source write
// runs: a default-language write that changed a translatable field marks every
// translation of it outdated, in that write's own transaction.
//
// What it deliberately does not touch is source_text and source_hash. Two
// source edits after one translation leave the pair the translator started from
// where it was, because that pair is the evidence the reviewer is working
// against; silently re-basing it onto the newest source would delete the
// paragraph they were told to look at.
func (s *Service) MarkOutdated(ctx context.Context, tx db.Tx[db.Tenant], module, entity, field string, recordID uuid.UUID) error {
	_ = ctx
	result := tx.DB().Model(&Row{}).
		Where("module = ? AND entity = ? AND record_id = ? AND field = ? AND status <> ?",
			module, entity, recordID, field, "outdated").
		Updates(map[string]any{"status": "outdated", "updated_at": s.now()})
	if result.Error != nil {
		return crud.Classify(result.Error)
	}
	return nil
}
