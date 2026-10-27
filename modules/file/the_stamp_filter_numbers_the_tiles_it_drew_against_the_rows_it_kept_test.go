package file_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheStampFilterNumbersTheTilesItDrewAgainstTheRowsItKept asks the grid, narrowed
// to "only what nothing shows", what it numbers its tiles against and what pages it
// offers: the stamped match or the whole library. The count rides the same filtered
// read as the rows, so a count taken before the stamp is applied prints the library's
// total above a shorter grid and links a page the filter cannot fill. Twenty-one
// measured pictures are filed, one record's field names all of them and then names
// one, so twenty carry the ledger's stamp and one is still shown.
func TestTheStampFilterNumbersTheTilesItDrewAgainstTheRowsItKept(t *testing.T) {
	router, admin, conn, svc, _ := libraryFixture(t)
	frame := wideFrame(t)
	filled := contracts.DefaultLibraryPageSize + 1
	hidden := make([]string, 0, filled)
	ids := make(map[string]uuid.UUID, filled)
	for i := range filled - 1 {
		name := fmt.Sprintf("counted-hidden-%02d.jpg", i)
		code, out := upload(t, router, files, name, "image/jpeg", string(frame))
		if code != http.StatusCreated {
			t.Fatalf("filing %s beside the use ledger = %d %s", name, code, out)
		}
		hidden = append(hidden, name)
		ids[name] = uuid.MustParse(field(t, out, "id"))
	}
	shownName := "counted-shown.jpg"
	code, out := upload(t, router, files, shownName, "image/jpeg", string(frame))
	if code != http.StatusCreated {
		t.Fatalf("filing %s beside the use ledger = %d %s", shownName, code, out)
	}
	shown := uuid.MustParse(field(t, out, "id"))

	writer := tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), uuid.New())
	identities := func(list []string) []uuid.UUID {
		got := make([]uuid.UUID, 0, len(list))
		for _, n := range list {
			got = append(got, ids[n])
		}
		return got
	}
	use := contracts.Use{Module: "content", Entity: "page", Record: uuid.New(), Field: "body", Locale: "en"}
	all := append(identities(hidden), shown)
	if err := db.Run(writer, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.SetUses(ctx, tx, use, all)
		return err
	}); err != nil {
		t.Fatalf("one record's field filing its use of every picture: %v", err)
	}
	if err := db.Run(writer, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.SetUses(ctx, tx, use, []uuid.UUID{shown})
		return err
	}); err != nil {
		t.Fatalf("the same field edited to name one picture: %v", err)
	}
	// The probe reads the ledger's own answer before the grid is asked anything: this
	// library holds one screenful of stamped pictures and one picture still shown.
	var stamped int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM files WHERE unused_since IS NOT NULL`).Scan(&stamped); err != nil {
		t.Fatalf("how many pictures the ledger stamped: %v", err)
	}
	if stamped != contracts.DefaultLibraryPageSize {
		t.Fatalf("the use ledger stamped %d of the %d pictures filed, want the twenty nothing shows",
			stamped, filled)
	}
	whole := libraryPage(t, router, libraryAddress)
	if !strings.Contains(whole, fmt.Sprintf("— file %d of %d", contracts.DefaultLibraryPageSize, filled)) {
		t.Fatalf("the unfiltered library did not number its last tile against the %d pictures this case filed", filled)
	}

	filtered := libraryPage(t, router, libraryAddress+"?unused=1")
	if drawn := named(hidden, filtered); len(drawn) != contracts.DefaultLibraryPageSize {
		t.Errorf("the stamp filter drew %d of the %d pictures the ledger stamped, want the whole match on one screenful",
			len(drawn), contracts.DefaultLibraryPageSize)
	}
	if strings.Contains(filtered, shownName) {
		t.Errorf("the grid narrowed to what nothing shows drew %q, which one record still shows", shownName)
	}
	if want := fmt.Sprintf("— file %d of %d", contracts.DefaultLibraryPageSize, contracts.DefaultLibraryPageSize); !strings.Contains(filtered, want) {
		t.Errorf("the stamp filter numbered its tiles against something larger than its own match, want the last of them reading %q", want)
	}
	if against := fmt.Sprintf("— file %d of %d", contracts.DefaultLibraryPageSize, filled); strings.Contains(filtered, against) {
		t.Errorf("the stamp filter numbered its tiles %q, against the whole library rather than the %d pictures it answers with", against, contracts.DefaultLibraryPageSize)
	}
	if strings.Contains(filtered, `data-page="2"`) {
		t.Errorf("the stamp filter offered a link to a second page, but its whole match is one screenful of %d", contracts.DefaultLibraryPageSize)
	}
}
