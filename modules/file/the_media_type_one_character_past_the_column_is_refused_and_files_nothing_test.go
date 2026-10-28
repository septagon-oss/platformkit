package file_test

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheMediaTypeOneCharacterPastTheColumnIsRefusedAndFilesNothing asks the
// upload route the two questions one column width decides.
//
// files.content_type is varchar(120), and contracts.MaxContentType refuses what
// is longer so the caller reads a 422 that names the media type rather than a
// constraint violation it reads as a 500. The width separates those two answers
// at exactly one value, so this asks at it and one past: a declaration of
// MaxContentType characters is filed whole — the column holds the declaration
// down to the last character it is able to hold — and one character more is
// refused, files no row and emits nothing. The accepted upload beside the
// refusal is the witness that this route does write a row when it accepts, so
// the counts below are a comparison and not an empty table.
func TestTheMediaTypeOneCharacterPastTheColumnIsRefusedAndFilesNothing(t *testing.T) {
	router, admin, _ := mountedOn(t, map[string]tenancy.Tenant{host: acme})

	before, _ := rowsAndEventsFiled(t, admin)
	fits := "text/plain" + strings.Repeat("x", contracts.MaxContentType-len("text/plain"))
	code, out := declare(t, router, "at-the-width.txt", fits, "a line of text\r\n")
	if code != http.StatusCreated {
		t.Fatalf("a declaration of %d characters answered %d: %s", len(fits), code, out)
	}
	var held string
	if err := admin.QueryRowContext(t.Context(),
		`SELECT content_type FROM files WHERE name = $1`, "at-the-width.txt").Scan(&held); err != nil {
		t.Fatalf("reading the row at the column's width: %v", err)
	}
	if got := len([]rune(held)); got != contracts.MaxContentType || held != fits {
		t.Errorf("a declaration of %d characters is filed as %d characters (%q): the row holds what the form declared",
			contracts.MaxContentType, got, held)
	}

	filed, emitted := rowsAndEventsFiled(t, admin)
	if filed != before+1 {
		t.Fatalf("the accepted upload left %d rows beside the %d already there, want one more", filed, before)
	}

	over := fits + "x"
	code, out = declare(t, router, "past-the-width.txt", over, "a line of text\r\n")
	if code != http.StatusUnprocessableEntity {
		t.Errorf("a declaration of %d characters answered %d: %s", len(over), code, out)
	}
	if !strings.Contains(out, "media type") {
		t.Errorf("the refusal does not name what is wrong with the declaration: %s", out)
	}
	if afterRows, afterEvents := rowsAndEventsFiled(t, admin); afterRows != filed || afterEvents != emitted {
		t.Errorf("the refused upload left %d rows and %d events where the accepted one left %d and %d",
			afterRows, afterEvents, filed, emitted)
	}
}

// rowsAndEventsFiled counts the two tables this module's upload writes to, over
// every tenant. The counts are compared with each other, never read as totals.
func rowsAndEventsFiled(t *testing.T, admin *sql.DB) (rows, events int) {
	t.Helper()
	if err := admin.QueryRowContext(t.Context(),
		`SELECT (SELECT count(*) FROM files), (SELECT count(*) FROM platformkit_outbox)`,
	).Scan(&rows, &events); err != nil {
		t.Fatalf("counting the filed rows and the emitted events: %v", err)
	}
	return rows, events
}
