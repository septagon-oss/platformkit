package file_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheWidthANameIsRefusedAtCountsTheCharactersTheColumnCounts asks that the
// length this module refuses a file name at is counted in characters, which is
// how files.name counts them, and not in bytes.
//
// contracts.MaxName equals the column's width, and varchar(n) keeps n characters
// of any script, so the two figures agree only while the entity counts the same
// thing the column does. A count of bytes refuses a name the table has room for
// — a name of MaxName characters in three-byte runes is three times that many
// bytes — and the caller is answered a 422 for a write the installation would
// have kept. Both directions are asked here: a name of exactly the width is
// filed and reads back character for character, and one character more is the
// refusal the limit exists to be.
func TestTheWidthANameIsRefusedAtCountsTheCharactersTheColumnCounts(t *testing.T) {
	router, admin, _ := mountedOn(t, map[string]tenancy.Tenant{host: acme})
	// One rune per character, three bytes each: counted in bytes, this is three
	// times the width the column keeps and the refusal below arrives early.
	const character = "é"

	exact := strings.Repeat(character, contracts.MaxName)
	code, out := upload(t, router, files, exact, "text/plain", "a line of text\n")
	if code != http.StatusCreated {
		t.Fatalf("a name of %d characters answered %d, want 201 for a name files.name has room for: %s",
			contracts.MaxName, code, out)
	}
	var held string
	if err := admin.QueryRowContext(t.Context(), `SELECT name FROM files WHERE id = $1`,
		field(t, out, "id")).Scan(&held); err != nil {
		t.Fatalf("reading the row a name of %d characters filed: %v", contracts.MaxName, err)
	}
	if held != exact {
		t.Errorf("the row holds %q, which is not the %d-character name the form declared", held, contracts.MaxName)
	}

	if code, out = upload(t, router, files, exact+character, "text/plain", "a line of text\n"); code != http.StatusUnprocessableEntity {
		t.Fatalf("a name of %d characters answered %d, want 422 naming the limit: %s",
			contracts.MaxName+1, code, out)
	}
}
