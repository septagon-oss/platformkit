package internal

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/translation/contracts"
)

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
func (s *Service) publish(ctx context.Context, tx db.Tx[db.Tenant], module, entity string, recordID uuid.UUID, field, locale string, before, row *Row) error {
	changes, err := rowChanges(before, row)
	if err != nil {
		return err
	}
	return events.Publish(ctx, tx, contracts.EventUpdated, &contracts.Updated{
		Module: module, Entity: entity, RecordID: recordID, Field: field, Locale: locale,
		Value: row.Value, Status: row.Status, Origin: row.Origin, SourceHash: row.SourceHash,
		Revision: row.Revision, Translator: row.TranslatorID, ReviewedAt: row.ReviewedAt,
		Changes: changes,
	})
}

// rowState is the three members of a translation row that carry its text and its
// standing, named as contracts.Updated names them — value, status, reviewedAt —
// because a diff whose names are the row's Go names is a history a reader cannot
// join to the payload beside it.
type rowState struct {
	Value      string     `json:"value"`
	Status     string     `json:"status"`
	ReviewedAt *time.Time `json:"reviewedAt,omitempty"`
}

func stateOf(row *Row) *rowState {
	if row == nil {
		return nil
	}
	// A removed row holds no text. The copy a caller hands here is the row as it
	// was a moment ago, and leaving that text in would report a paragraph
	// surviving in a row the transaction has just deleted: the diff's after half
	// is the empty string, and the status beside it says why.
	if row.Status == rest.FallbackRemoved {
		return &rowState{Status: row.Status}
	}
	return &rowState{Value: row.Value, Status: row.Status, ReviewedAt: row.ReviewedAt}
}

// rowChanges is one write's pair of halves, asked of kit/events' own diff so the
// rules over there — json equality, a create with no before half, a name the type
// does not carry refused rather than skipped — are one rule and not two.
//
// before is nil for a create, which is the only write with no halves to compare.
func rowChanges(before, after *Row) ([]events.Change, error) {
	return events.Changes(stateOf(before), stateOf(after), "value", "status", "reviewedAt")
}
