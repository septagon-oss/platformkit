package internal_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// TestTheGridCountsEveryMatchAndShowsTheTilesOfOnePage asks the library grid the
// two questions a page asks it at once: how many there are, and which of them go
// on this screen. The count is the whole match and the rows are one screenful of
// it, so a search is answerable past the first page: the grid's pagination is
// derived from the count, so a count taken from the rows shown says "one page"
// and hides every tile behind the screenful.
//
// The names are the ceiling exactly — contracts.MaxName characters, longer than
// contracts.MaxQuery lets anybody type — so the only question a person can ask
// about them is a fragment, and the count is the answer to that fragment rather
// than the answer to the tenant's whole library. A removed file is no match:
// the grid loses its tile and the count loses it too, because a tile whose
// download no longer answers is a page that lost a picture somebody deleted.
func TestTheGridCountsEveryMatchAndShowsTheTilesOfOnePage(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	svc := internal.NewService(internal.NewLocal(t.TempDir()), 16<<20, 0, 0)

	// Every one of these is MaxName characters and shares the fragment; the last
	// is the same length and does not contain it, so a count of everything would
	// disagree with a count of the matches.
	fragment := strings.Repeat("é", 20)
	nameOf := func(tail int) string {
		return fragment + strings.Repeat("x", contracts.MaxName-len(fragment)-tail) + strings.Repeat("y", tail)
	}
	frames := map[string]string{}
	for _, tail := range []int{0, 1, 2} {
		frames[nameOf(tail)] = "frame"
	}
	frames[strings.Repeat("z", contracts.MaxName)] = "another picture"

	filed := map[uuid.UUID]string{}
	ctx := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for name, body := range frames {
			row, err := svc.Upload(ctx, held(tx), contracts.Upload{
				Name: name, ContentType: "text/plain",
				Declared: int64(len(body)), Body: strings.NewReader(body),
			})
			if err != nil {
				return err
			}
			filed[row.ID] = row.Name
		}
		return nil
	}); err != nil {
		t.Fatalf("filing %d names of %d characters = %v", len(frames), contracts.MaxName, err)
	}

	// One read of the grid: the rows it puts on the screen and the count it
	// prints above them, asked together the way the page asks them.
	type answer struct {
		rows  []*contracts.File
		total int64
	}
	ask := func(limit, offset int, name string) answer {
		var got answer
		err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			rows, total, err := svc.Search(ctx, tx, contracts.LibraryQuery{
				Name: name, Limit: limit, Offset: offset,
			})
			got.rows, got.total = rows, total
			return err
		})
		if err != nil {
			t.Fatalf("searching a %d-character fragment at offset %d = %v", len(fragment), offset, err)
		}
		return got
	}
	seen := func(a answer) map[uuid.UUID]bool {
		out := map[uuid.UUID]bool{}
		for _, row := range a.rows {
			if row.Name != filed[row.ID] {
				t.Errorf("the grid names the tile %s %q, want the whole %d-character name it was filed under",
					row.ID, row.Name, contracts.MaxName)
			}
			out[row.ID] = true
		}
		return out
	}

	first := ask(2, 0, fragment)
	if first.total != 3 || len(first.rows) != 2 {
		t.Fatalf("the first screenful of a fragment three files carry answered %d of %d, want 2 tiles of 3 matches",
			len(first.rows), first.total)
	}
	onFirst := seen(first)

	second := ask(2, 2, fragment)
	if second.total != 3 || len(second.rows) != 1 {
		t.Fatalf("the second screenful answered %d of %d, want the third tile of the same 3 matches",
			len(second.rows), second.total)
	}
	reached := map[uuid.UUID]bool{}
	for id := range onFirst {
		reached[id] = true
	}
	for id := range seen(second) {
		if onFirst[id] {
			t.Errorf("the tile %s is answered on both screenfuls, so paging repeats a file", id)
		}
		reached[id] = true
	}
	if len(reached) != 3 {
		t.Errorf("two screenfuls of three matches reached %d distinct files, want all three", len(reached))
	}

	// Everything the fragment does not match stays out of the count: a fragment of
	// the one name nobody else shares is answered with that name alone.
	whole := ask(contracts.DefaultLibraryPageSize, 0, strings.Repeat("z", 20))
	if whole.total != 1 || len(whole.rows) != 1 {
		t.Errorf("the name nothing else shares was answered %d of %d, want its one tile of one match",
			len(whole.rows), whole.total)
	}

	// A deleted file leaves both answers. The row survives for its audit, so only
	// the grid's own filter can answer this, and the count is the half of it that
	// a person reads first: the number of pictures they have.
	var gone uuid.UUID
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		gone = first.rows[0].ID
		_, err := svc.Delete(ctx, tx, gone)
		return err
	}); err != nil {
		t.Fatalf("deleting one of the three = %v", err)
	}
	after := ask(contracts.DefaultLibraryPageSize, 0, fragment)
	if after.total != 2 || len(after.rows) != 2 {
		t.Errorf("the grid answered %d of %d after one of the three was deleted, want 2 tiles of 2 matches",
			len(after.rows), after.total)
	}
	for _, row := range after.rows {
		if row.ID == gone {
			t.Errorf("the grid still shows the tile %s whose file was deleted", gone)
		}
	}
}
