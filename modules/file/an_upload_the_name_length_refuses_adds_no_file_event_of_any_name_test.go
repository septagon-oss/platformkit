package file_test

// The delivery row a kept upload leaves is the receipt every subscriber reads,
// so it has to name the file that was filed and say so under the name the caller
// subscribed to. The pair of requests is the width the case sits at: the upload at
// contracts.MaxName is the one the module keeps, the one past it is the one it
// refuses, and the second half of the promise is that the refusal adds nothing to
// anybody's delivery table — under any event name, not only the one an upload
// would have published. A refusal that published a file event of another name
// would deliver a receipt for a file nobody filed while the count of uploaded rows
// stayed exactly as full as it was.
//
// The receipt is looked up by the id the door answered with, and its payload is
// read back beside its name: a row of the right name naming nobody, or naming the
// file but losing the name that was filed, is a delivery a subscriber cannot act
// on and a count that cannot see it.

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

func TestAnUploadTheNameLengthRefusesAddsNoFileEventOfAnyName(t *testing.T) {
	router, admin, _ := mountedOn(t, map[string]tenancy.Tenant{host: acme})

	atTheWidth := strings.Repeat("é", contracts.MaxName)
	code, out := upload(t, router, files, atTheWidth, "text/plain", "a line of text\n")
	if code != http.StatusCreated {
		t.Fatalf("an upload of a %d-character name = %d %s, want it kept", contracts.MaxName, code, out)
	}
	id := field(t, out, "id")

	var published, named string
	err := admin.QueryRowContext(t.Context(), `SELECT name, payload->>'name' FROM `+outbox+`
		WHERE tenant_id = $1 AND payload->>'fileId' = $2`, acme.ID, id).Scan(&published, &named)
	if err == sql.ErrNoRows {
		t.Fatalf("the delivery table holds no event naming %s: the row an upload leaves has to name the file it filed", id)
	}
	if err != nil {
		t.Fatalf("read the delivery row the upload left: %v", err)
	}
	if published != contracts.EventUploaded {
		t.Errorf("the event naming the kept file is published as %q, want %q: a subscriber that asked for uploads is told about %s",
			published, contracts.EventUploaded, published)
	}
	if named != atTheWidth {
		t.Errorf("the receipt carries the name %q, want the %d characters that were filed", named, contracts.MaxName)
	}

	kept := fileEvents(t, admin)
	if code, out = upload(t, router, files, atTheWidth+"x", "text/plain", "a line of text\n"); code != http.StatusUnprocessableEntity {
		t.Fatalf("one character past the ceiling = %d %s, want 422", code, out)
	}
	if after := fileEvents(t, admin); after != kept {
		t.Errorf("the refused upload left the tenant's file events %q, want %q: a refusal emits nothing", after, kept)
	}
}

// fileEvents is every file event one tenant's delivery rows hold, in the order
// they were written, joined so a failure reads as the list it found.
func fileEvents(t *testing.T, admin *sql.DB) string {
	t.Helper()
	rows, err := admin.QueryContext(t.Context(), `SELECT name FROM `+outbox+`
		WHERE tenant_id = $1 ORDER BY created_at, name`, acme.ID)
	if err != nil {
		t.Fatalf("read the tenant's delivery rows: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("read a delivery row: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the tenant's delivery rows: %v", err)
	}
	return strings.Join(names, ",")
}
