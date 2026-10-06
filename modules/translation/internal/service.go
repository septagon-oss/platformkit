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
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/translation/contracts"
)

// actorOf is the principal of the write, which is who translator_id names and who
// translation.updated names. No actor is the nil UUID — the same answer kit/events
// gives for the same absence, so the trail and the row name the same nobody.
func actorOf(ctx context.Context) uuid.UUID {
	if id, ok := tenancy.ActorFrom(ctx); ok {
		return id
	}
	return uuid.Nil
}

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
			// The review question is asked first, and it is the order that decides
			// what a stranger may read. A machine draft nobody has looked at is not
			// the tenant's Portuguese, and its source moving does not make it one:
			// "outdated" says the translation is behind, not that anybody read it.
			// Ask staleness first and an unreviewed draft becomes servable the moment
			// the source moves, which is the exact hole this branch exists to close.
			if row.Origin == rest.OriginMachine && row.ReviewedAt == nil {
				if q.Public {
					f = rest.TranslatedField{Status: rest.FallbackWithheld, Revision: row.Revision, Origin: row.Origin}
				} else {
					f.Status = rest.FallbackMachine
				}
			} else if current, ok := q.Sources[id][field]; ok {
				// Only a source the caller actually handed over can move the goal
				// posts. Absent here means nobody gave us the field's current text,
				// and hashing "" would report a translation of a source we never saw
				// as outdated. Outdated is a claim about a comparison, not a default.
				if hash, err := contracts.Hash(current, rich[field]); err == nil && hash != row.SourceHash {
					f.Status = rest.FallbackOutdated
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
		hash, err := contracts.Hash(source, q.RichText[field])
		if err != nil {
			return fmt.Errorf("%w: %s could not be measured against its source: %v", crud.ErrInvalid, field, err)
		}
		// Who wrote it and who has read it are two separate facts, and the rule for
		// each is one line. Provenance is never erased: a machine draft stays
		// machine however many people improve it, because "machine" answers who
		// produced the first version of this text and no later keystroke changes
		// that. The review stamp is the other fact, and a person typing over a draft
		// is where the two come apart — the draft's origin, the person's review —
		// which is exactly what the public door then serves.
		writer := q.Origin
		if writer == "" {
			writer = rest.OriginHuman
		}
		origin := writer
		if row != nil && row.Origin == rest.OriginMachine {
			origin = rest.OriginMachine
		}
		stamp := origin == rest.OriginMachine && writer == rest.OriginHuman
		unchanged := row != nil && row.Value == value && row.SourceHash == hash
		if unchanged {
			// The same text keeps whatever review it carried: re-asking the machine
			// for a draft it already produced does not un-read it.
			stamp = row.ReviewedAt != nil
		}
		if row != nil && unchanged && row.Origin == origin && (row.ReviewedAt != nil) == stamp {
			continue // nothing moved: no row, no revision, no event
		}
		var reviewedAt *time.Time
		if stamp {
			at := s.now()
			reviewedAt = &at
		}
		next := Row{
			Module: q.Module, Entity: q.Entity, RecordID: q.RecordID,
			Field: field, Locale: q.Locale, Value: value,
			SourceText: source, SourceHash: hash, Origin: origin, ReviewedAt: reviewedAt,
			// The translator is the principal of this write and not of the first
			// one: origin already says who made the draft, and this column answers
			// who is responsible for the text as it now stands.
			TranslatorID: actorOf(ctx),
			// A translation write always re-bases onto the source it was made
			// from, so the pair matches by construction: only an unread machine
			// draft is not yet somebody's answer.
			Status: contracts.StatusOf(origin, stamp, false),
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
// lock on each. An empty field list is every field of that record in that
// locale — the spelling rest.ReviewQuery documents for "this record, whole" —
// so the filter is left off the query rather than being an `IN ()` that matches
// nothing, which is how "mark reviewed" came to answer 404.
//
// The critical section here spans several rows of one table for
// one record, and the source row's lock above it is what orders it against a
// source edit; within the table, FOR UPDATE on the rows themselves is enough,
// so no advisory lock is taken where a row lock already serialises the pair.
func (s *Service) lock(ctx context.Context, tx db.Tx[db.Tenant], module, entity, locale string, recordID uuid.UUID, fields []string) (map[string]*Row, error) {
	_ = ctx
	query := tx.DB().Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("module = ? AND entity = ? AND locale = ? AND record_id = ?",
			module, entity, locale, recordID)
	if len(fields) > 0 {
		query = query.Where("field IN ?", fields)
	}
	var rows []Row
	if err := query.Find(&rows).Error; err != nil {
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
//
// A field whose source nobody handed over is refused for the same reason and not
// skipped: a review is a claim about a comparison, and a review that stamps what
// it could not check fails open — the stale draft becomes publicly servable, and
// the badge says the opposite of the truth. Fields empty is every field of the
// record, which is what the record-level "Mark reviewed" button sends.
func (s *Service) Review(ctx context.Context, tx db.Tx[db.Tenant], q rest.ReviewQuery) error {
	rows, err := s.lock(ctx, tx, q.Module, q.Entity, q.Locale, q.RecordID, q.Fields)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("%w: there is no %s translation of this %s to review",
			crud.ErrNotFound, q.Locale, q.Entity)
	}
	order := reviewedOrder(rows)
	for _, field := range order {
		row := rows[field]
		if q.Expected != nil && row.Revision != q.Expected[field] {
			return fmt.Errorf("%w: the %s translation of %s changed since you read it (revision %d, you sent %d)",
				crud.ErrConflict, q.Locale, field, row.Revision, q.Expected[field])
		}
		source, ok := q.Source[field]
		if !ok {
			return fmt.Errorf("%w: the %s translation of %s cannot be reviewed: its current source was not supplied to check it against",
				crud.ErrInvalid, q.Locale, field)
		}
		hash, err := contracts.Hash(source, q.RichText[field])
		if err != nil {
			return fmt.Errorf("%w: the %s translation of %s could not be measured against its source: %v",
				crud.ErrInvalid, q.Locale, field, err)
		}
		if hash != row.SourceHash {
			return fmt.Errorf("%w: the source changed after this %s translation of %s was made; save the corrected text before marking it reviewed",
				crud.ErrInvalid, q.Locale, field)
		}
	}
	for _, field := range order {
		row := rows[field]
		// Only a machine draft has a review to record. A human row is already
		// somebody's answer — the CHECK on the table says a review stamp means
		// a machine text — so marking one reviewed is the same pair asked for
		// twice: it succeeds, writes nothing and publishes nothing.
		if row.Origin != rest.OriginMachine || row.ReviewedAt != nil {
			continue
		}
		reviewed := s.now()
		row.ReviewedAt = &reviewed
		row.Status = contracts.StatusOf(row.Origin, true, false)
		row.Revision++
		// The reviewer is the translator of record from this write onward: the
		// stamp and the column move together, so "who is answerable for this
		// text" and "who said it was good" name the same person.
		row.TranslatorID = actorOf(ctx)
		if err := s.rewrite(ctx, tx, row); err != nil {
			return err
		}
		if err := s.publish(ctx, tx, row.Module, row.Entity, row.RecordID, row.Field, row.Locale, row); err != nil {
			return err
		}
	}
	return nil
}

// reviewedOrder is the field names, sorted, so that a review of a whole record
// publishes its events in one order rather than the order the map happened to
// iterate in — a replay of the same click has to be comparable with the first.
func reviewedOrder(rows map[string]*Row) []string {
	order := make([]string, 0, len(rows))
	for field := range rows {
		order = append(order, field)
	}
	slices.Sort(order)
	return order
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

// Untranslate deletes one record's rows in one locale; an empty field list is
// every row of that record in that locale, which is what "remove Portuguese"
// means on the record's own form. It is a write, so it is audited: the event
// names the field that went away, which is the only record
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
		// The deletion says it removed the row, in the same words Untranslate
		// uses: a subscriber — the audit trail, a search index — cannot tell a
		// deletion from an update by anything else in the payload, and a record
		// whose translations vanished while its events said "complete" is an
		// index serving text that is no longer stored anywhere.
		removed := row
		removed.Status = rest.FallbackRemoved
		removed.Revision = row.Revision + 1
		if err := s.publish(ctx, tx, module, entity, recordID, row.Field, row.Locale, &removed); err != nil {
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
//
// Whether a State filter was asked for decides how much of the entity is read,
// and the reason is worth seeing: the state is derived from the entity's row and
// the translation row together, so no query over either table alone can answer
// "only the missing ones". A filtered overview therefore judges every record of
// the entity — crud.MaxLimit at a time — and answers the caller's page out of the
// survivors, which is the only way the total it returns can honestly be the total
// after the filter. An unfiltered one judges the page it was asked for and
// nothing more, and its counts are that page's.
func (s *Service) Overview(ctx context.Context, tx db.Tx[db.Tenant], q rest.OverviewQuery) ([]rest.OverviewRow, rest.OverviewCounts, int64, error) {
	src, err := s.source(q.Module, q.Entity)
	if err != nil {
		return nil, rest.OverviewCounts{}, 0, err
	}
	// A locale the tenant no longer declares is not a language this installation
	// translates into: its rows still exist and nothing serves them, which is the
	// removed state. No list shows them until somebody asks for them by name —
	// an operator who stopped speaking Portuguese does not want a column of rows
	// they cannot act on, and an operator cleaning up wants exactly that list.
	speaks := len(q.Languages) == 0 || slices.Contains(q.Languages, q.Locale)
	if !speaks && !q.IncludeRemoved {
		return nil, rest.OverviewCounts{}, 0, nil
	}
	limit := q.Limit
	if limit <= 0 || limit > crud.MaxLimit {
		limit = crud.MaxLimit
	}
	if q.State == "" {
		records, total, err := src.Page(ctx, tx, limit, q.Offset)
		if err != nil {
			return nil, rest.OverviewCounts{}, 0, err
		}
		rows, counts, err := s.judge(ctx, tx, q, src, records, speaks)
		if err != nil {
			return nil, rest.OverviewCounts{}, 0, err
		}
		return rows, counts, total, nil
	}
	var records []rest.SourceRow
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
	judged, counts, err := s.judge(ctx, tx, q, src, records, speaks)
	if err != nil {
		return nil, rest.OverviewCounts{}, 0, err
	}
	kept := make([]rest.OverviewRow, 0, len(judged))
	for _, row := range judged {
		for _, state := range row.States {
			if state == q.State {
				kept = append(kept, row)
				break
			}
		}
	}
	// The caller's page is cut out of the survivors, which is the only place it
	// can be cut from once the filter runs over a derived state — and the total
	// is then the count of survivors, which is what the port promises.
	start := min(q.Offset, len(kept))
	return kept[start:min(start+limit, len(kept))], counts, int64(len(kept)), nil
}

// judge states one page of the entity's records for one locale: the rows this
// locale holds for them, the source the entity holds now, and the rule in
// contracts.StateOf applied to the pair.
//
// A locale the tenant no longer declares (speaks is false) reports the fields
// that have a row as removed rather than by their own state: nothing serves
// them, and a screen that called one of them "outdated" would be offering to fix
// a translation nobody can publish.
func (s *Service) judge(ctx context.Context, tx db.Tx[db.Tenant], q rest.OverviewQuery, src rest.TranslationSource,
	records []rest.SourceRow, speaks bool) ([]rest.OverviewRow, rest.OverviewCounts, error) {

	_ = ctx
	ids := make([]uuid.UUID, 0, len(records))
	for _, rec := range records {
		ids = append(ids, rec.ID)
	}
	var stored []Row
	if len(ids) > 0 {
		err := tx.DB().Where("module = ? AND entity = ? AND locale = ? AND record_id IN ?",
			q.Module, q.Entity, q.Locale, ids).Find(&stored).Error
		if err != nil {
			return nil, rest.OverviewCounts{}, crud.Classify(err)
		}
	}
	byRecord := map[uuid.UUID]map[string]*Row{}
	for i := range stored {
		row := &stored[i]
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
			state := rest.StateMissing
			if stored != nil && !speaks {
				state = rest.FallbackRemoved
			} else {
				hash, err := contracts.Hash(rec.Values[field], rich[field])
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
		out = append(out, row)
	}
	return out, counts, nil
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
			module, entity, recordID, field, rest.FallbackOutdated).
		Updates(map[string]any{"status": rest.FallbackOutdated, "updated_at": s.now()})
	if result.Error != nil {
		return crud.Classify(result.Error)
	}
	return nil
}
