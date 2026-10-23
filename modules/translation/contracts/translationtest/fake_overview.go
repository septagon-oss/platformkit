package translationtest

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/translation/contracts"
)

// overviewRecords reads the requested page first, then scans all records when
// a derived-state filter needs them. Provider calls stay outside the fake's lock.
func overviewRecords(ctx context.Context, tx db.Tx[db.Tenant], src rest.TranslationSource, q rest.OverviewQuery, limit int) ([]rest.SourceRow, int64, error) {
	total := int64(0)
	records, pageTotal, err := src.Page(ctx, tx, limit, q.Offset)
	if err != nil {
		return nil, 0, err
	}
	if q.State != "" {
		// A filter over a derived state cannot be pushed into the entity's query,
		// so the whole set is judged and the page cut out of the survivors —
		// which is the only way the total can be the total after the filter.
		records = nil
		for offset := 0; ; offset += crud.MaxLimit {
			page, _, err := src.Page(ctx, tx, crud.MaxLimit, offset)
			if err != nil {
				return nil, 0, err
			}
			records = append(records, page...)
			if len(page) < crud.MaxLimit {
				break
			}
		}
	} else {
		total = pageTotal
	}
	return records, total, nil
}

func overviewState(stored *Row, source string, rich, speaks bool) string {
	state := rest.StateMissing
	if stored != nil && !speaks {
		state = rest.FallbackRemoved
	} else {
		hash, err := contracts.Hash(source, rich)
		state = contracts.StateOf(stored != nil,
			stored != nil && stored.Origin == rest.OriginMachine && stored.ReviewedAt == nil,
			stored != nil && err == nil && hash == stored.SourceHash)
	}
	return state
}

func countOverviewField(row *rest.OverviewRow, counts *rest.OverviewCounts, state string) {
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
