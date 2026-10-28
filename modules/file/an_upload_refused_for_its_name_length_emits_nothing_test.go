package file_test

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestAnUploadRefusedForItsNameLengthEmitsNothing asks that the refusal above the
// name's width leaves the delivery table as it found it.
//
// file.uploaded is published in the transaction that writes the row, so a name the
// entity refuses can be expected to emit nothing; what keeps this asked rather than
// assumed is that no case reads that table at the length refusal. The conformance
// harness counts the event a kept upload leaves, and the refusals this package reads
// the table at are the cross-tenant and permission ones. The kept upload is asked
// for first, so the silence below is a refusal's and not a count that sees no event
// at all.
func TestAnUploadRefusedForItsNameLengthEmitsNothing(t *testing.T) {
	router, admin, _ := mountedOn(t, map[string]tenancy.Tenant{host: acme})
	const character = "🙂"

	atTheWidth := strings.Repeat(character, contracts.MaxName)
	if code, out := upload(t, router, files, atTheWidth, "text/plain", "a line of text\n"); code != http.StatusCreated {
		t.Fatalf("a name of %d characters answered %d, want 201 for the longest name files.name keeps: %s",
			contracts.MaxName, code, out)
	}
	kept := uploadedEvents(t, admin)
	if kept != 1 {
		t.Fatalf("the upload the module kept left %d %q row(s), want the one it owes", kept, contracts.EventUploaded)
	}

	pastTheWidth := atTheWidth + character
	code, out := upload(t, router, files, pastTheWidth, "text/plain", "a line of text\n")
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("a name of %d characters answered %d, want the 422 the contract's own width gives: %s",
			contracts.MaxName+1, code, out)
	}
	if after := uploadedEvents(t, admin); after != kept {
		t.Errorf("the refused upload left the table holding %d %q rows where the kept one had left %d, and a refusal emits nothing",
			after, contracts.EventUploaded, kept)
	}
}

// uploadedEvents counts the rows of one event name the delivery table holds.
func uploadedEvents(t *testing.T, admin *sql.DB) int {
	t.Helper()
	var n int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM `+outbox+` WHERE name = $1`,
		contracts.EventUploaded).Scan(&n); err != nil {
		t.Fatalf("counting the %q rows the delivery table holds: %v", contracts.EventUploaded, err)
	}
	return n
}
