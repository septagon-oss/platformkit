package file_test

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestANameInFourByteRunesIsFiledWholeAndOneCharacterMoreFilesNothing asks that
// the width this module refuses a file name at is counted in characters at the
// widest runes a person can type, and that the refusal above it files nothing.
//
// files.name is varchar(255) and counts characters; a rune outside the Basic
// Multilingual Plane takes four bytes, so a name of the full width built from
// them arrives as 1020 bytes at a column that keeps 255 characters. Counted in
// bytes the write reads as four times too long and is refused; counted in
// characters it is the longest name the table has room for. The limit is asked
// for as a 422 and not merely a 4xx because the entity's own count is what stops
// the database's "value too long" arriving as a 500, and it is asked beside a
// count of the rows because a refusal that filed its row leaves a library
// holding a file nobody wrote.
func TestANameInFourByteRunesIsFiledWholeAndOneCharacterMoreFilesNothing(t *testing.T) {
	router, admin, _ := mountedOn(t, map[string]tenancy.Tenant{host: acme})
	const character = "🙂" // one rune per character, four bytes each

	atTheWidth := strings.Repeat(character, contracts.MaxName)
	code, out := upload(t, router, files, atTheWidth, "text/plain", "a line of text\n")
	if code != http.StatusCreated {
		t.Fatalf("a name of %d four-byte characters answered %d, want 201 for a name files.name has room for: %s",
			contracts.MaxName, code, out)
	}
	var held string
	if err := admin.QueryRowContext(t.Context(), `SELECT name FROM files WHERE id = $1`,
		field(t, out, "id")).Scan(&held); err != nil {
		t.Fatalf("reading the row a name of %d four-byte characters filed: %v", contracts.MaxName, err)
	}
	if held != atTheWidth {
		t.Errorf("the row holds %q, which is not the %d-character name the form declared", held, contracts.MaxName)
	}

	pastTheWidth := atTheWidth + character
	if code, out = upload(t, router, files, pastTheWidth, "text/plain", "a line of text\n"); code != http.StatusUnprocessableEntity {
		t.Fatalf("a name of %d characters answered %d, want the 422 that names the limit rather than the database's own error: %s",
			contracts.MaxName+1, code, out)
	}
	// The count is asked of the name that was kept first, so that the zero below
	// says the refusal filed nothing rather than that this query sees no rows.
	if written := countRowsNamed(t, admin, atTheWidth); written != 1 {
		t.Fatalf("the name the form was allowed to file appears %d times, want the one row it wrote", written)
	}
	if filed := countRowsNamed(t, admin, pastTheWidth); filed != 0 {
		t.Errorf("a name refused for its length filed %d row(s), and a refusal files nothing", filed)
	}
}

// countRowsNamed answers how many rows of the files table carry one name.
func countRowsNamed(t *testing.T, admin *sql.DB, name string) int {
	t.Helper()
	var n int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM files WHERE name = $1`, name).Scan(&n); err != nil {
		t.Fatalf("counting the rows by a name of %d characters: %v", len(name), err)
	}
	return n
}
