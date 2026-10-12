package filetest

// The library half of the fake: the grid's read, the meter and the caption. The
// decisions are contracts' — the family a row belongs to, the six orders, what a
// caption may claim — and this file calls them rather than repeating them, which
// is the whole reason they are functions in contracts and clauses in a query: a
// fake that guessed at a filter would pass its own suite and disagree with
// production.
//
// What this fake cannot do is pretend at a second tenant or at a second width:
// the rows are keyed by id alone (the limit stated at the top of fake.go), and it
// encodes no variants at all. The meter's `Variants` is therefore always zero
// here, and the claim that a picture and its other widths are one tenant's disk
// is answered once, in internal, where the encode actually runs.

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

var _ contracts.Library = (*Fake)(nil)

// Search is the grid, over the map. The order is the SQL service's order — the
// chosen field, then the row id descending as the tie-break — because a paginated
// read whose order a second implementation is free to choose is a read that
// shows a person the same row twice and misses another.
func (f *Fake) Search(ctx context.Context, _ db.Tx[db.Tenant], q contracts.LibraryQuery) ([]*contracts.File, int64, error) {
	if _, err := contracts.ScopeOf(ctx); err != nil {
		return nil, 0, err
	}
	q, err := q.Normalize()
	if err != nil {
		return nil, 0, err
	}
	field, desc, err := contracts.LibrarySort(q.Sort)
	if err != nil {
		return nil, 0, err
	}
	needle := strings.ToLower(q.Name)

	f.mu.Lock()
	defer f.mu.Unlock()
	var rows []*contracts.File
	for _, row := range f.rows {
		if needle != "" && !strings.Contains(strings.ToLower(row.Name), needle) {
			continue
		}
		if q.Family != "" && contracts.MediaFamily(row.ContentType, row.Width) != q.Family {
			continue
		}
		if q.Unused && row.UnusedSince == nil {
			continue
		}
		rows = append(rows, &row)
	}
	slices.SortFunc(rows, func(a, b *contracts.File) int {
		var ord int
		switch field {
		case "Name":
			ord = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case "Size":
			ord = cmp.Compare(a.Size, b.Size)
		default:
			ord = a.CreatedAt.Compare(b.CreatedAt)
		}
		if ord == 0 {
			ord = strings.Compare(a.ID.String(), b.ID.String())
		}
		if desc {
			return -ord
		}
		return ord
	})
	total := int64(len(rows))
	start := min(q.Offset, len(rows))
	rows = rows[start:min(start+q.Limit, len(rows))]
	return rows, total, nil
}

// Quota is the meter over the rows this fake holds. See contracts.Library for
// what the number is and is not; the fake never held the lock and never will.
func (f *Fake) Quota(context.Context, db.Tx[db.Tenant]) (*contracts.Quota, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var used int64
	for _, row := range f.rows {
		used += row.Size
	}
	return &contracts.Quota{Used: used, Limit: f.quota, Unlimited: f.quota <= 0}, nil
}

// SetAlt is the caption, including the refusal that makes it a revision guard
// rather than a last-write-wins merge: the fake has no row to lock, so the clock
// it checks is the one it wrote itself, and a caller carrying an instant the row
// has passed is refused exactly as Postgres refuses the real one.
func (f *Fake) SetAlt(ctx context.Context, _ db.Tx[db.Tenant], id uuid.UUID, alt string, decorative bool, expected time.Time) (*contracts.File, error) {
	if _, err := contracts.ScopeOf(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[id]
	if !ok {
		return nil, crud.ErrNotFound
	}
	if expected.IsZero() {
		return nil, fmt.Errorf("%w: %s: an alt text is written against the file you were shown", crud.ErrInvalid, id)
	}
	if !row.UpdatedAt.Equal(expected) {
		return nil, fmt.Errorf("%w: this file changed while you were looking — it is at %s, and you were shown %s",
			crud.ErrConflict, row.UpdatedAt.UTC().Format(time.RFC3339Nano), expected.UTC().Format(time.RFC3339Nano))
	}
	if err := contracts.AltClaim(alt, decorative); err != nil {
		return nil, err
	}
	row.Alt, row.Decorative = strings.TrimSpace(alt), decorative
	row.UpdatedAt = db.Now()
	f.rows[id] = row
	f.published = append(f.published, contracts.EventAltered)
	return &row, nil
}
