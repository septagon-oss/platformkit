package file_test

import (
	"context"
	"html"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheStampFilterDrawsTheRowWhoseLastUseEndedAndHidesTheOneStillShown asks the
// library's stamp filter to name the row it keeps, not only the rows it hides. The
// cases that ask this page for its way out of "only what nothing shows" reach that
// question through an empty grid: the stamp belongs to the use ledger, which no page
// write touches, so nothing they file is ever stamped. Here the ledger stamps through
// the module's own port — one record's field names two pictures, an edit names one,
// and the last use of the other ends with its stamp set in the same transaction — and
// both sides of the grid are asserted. A filter that selected nothing and one that
// selected everything would each pass a case that reads only an empty grid; the way
// out is then followed from a page that does hide a row, so the control that clears
// the stamp is answered by the library holding both pictures.
func TestTheStampFilterDrawsTheRowWhoseLastUseEndedAndHidesTheOneStillShown(t *testing.T) {
	router, admin, conn, svc, _ := libraryFixture(t)
	frame := wideFrame(t)
	filed := func(name string) uuid.UUID {
		t.Helper()
		code, out := upload(t, router, files, name, "image/jpeg", string(frame))
		if code != http.StatusCreated {
			t.Fatalf("filing %s beside the use ledger = %d %s", name, code, out)
		}
		return uuid.MustParse(field(t, out, "id"))
	}
	released, kept := filed("ledger-released.jpg"), filed("ledger-kept.jpg")

	writer := tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), uuid.New())
	showing := contracts.Use{Module: "content", Entity: "page", Record: uuid.New(), Field: "body", Locale: "en"}
	showBoth := func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.SetUses(ctx, tx, showing, []uuid.UUID{released, kept})
		return err
	}
	if err := db.Run(writer, conn, showBoth); err != nil {
		t.Fatalf("one record's field filing its use of both pictures: %v", err)
	}
	var ended []uuid.UUID
	if err := db.Run(writer, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		ended, err = svc.SetUses(ctx, tx, showing, []uuid.UUID{kept})
		return err
	}); err != nil {
		t.Fatalf("the same field edited to name one picture: %v", err)
	}
	if len(ended) != 1 || ended[0] != released {
		t.Fatalf("ending that field's use of two pictures returned %v, want exactly %s", ended, released)
	}
	// The probe reads the ledger's own answer, before this case asks the page
	// anything: the picture the record dropped carries the stamp, the one it kept does
	// not. A row the ledger never stamped is what every unstamped tile on this page is.
	stamped := func(id uuid.UUID) bool {
		var set bool
		if err := admin.QueryRowContext(t.Context(),
			`SELECT unused_since IS NOT NULL FROM files WHERE id = $1`, id).Scan(&set); err != nil {
			t.Fatalf("whether the ledger stamped %s: %v", id, err)
		}
		return set
	}
	if releasedStamped, keptStamped := stamped(released), stamped(kept); !releasedStamped || keptStamped {
		t.Fatalf("unused_since set on the picture whose last use ended = %t and on the one still shown = %t, want the stamp on the released picture alone",
			releasedStamped, keptStamped)
	}

	grid := libraryPage(t, router, libraryAddress+"?unused=1")
	names := []string{"ledger-released.jpg", "ledger-kept.jpg"}
	if drawn := named(names, grid); len(drawn) != 1 || drawn[0] != names[0] {
		t.Errorf("the grid asked for only what nothing shows and drew %v of %v, want the released picture alone", drawn, names)
	}
	way := ghostLink.FindStringSubmatch(grid)
	if len(way) != 2 {
		t.Fatalf("a grid narrowed to the one picture nothing shows offered no way out of the filter")
	}
	at := html.UnescapeString(way[1])
	if at != libraryAddress {
		t.Errorf("the way out offered beside a standing stamp is %q, want the whole library at %q", at, libraryAddress)
	}
	if back := named(names, libraryPage(t, router, at)); len(back) != len(names) {
		t.Errorf("following the way out of the stamp drew %v of %v, so the filter outlived the control that clears it", back, names)
	}
}
