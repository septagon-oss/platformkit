package file_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestAShownFileIsNotDeletedAndAStrangerLearnsNothing asks the delete door about a
// file one record shows: another tenant's session finds no such file — not a
// refusal that would tell it the file exists and is in use — and the owner's own
// session is refused with the count, leaving the
// row, the use and the outbox exactly as they were.
func TestAShownFileIsNotDeletedAndAStrangerLearnsNothing(t *testing.T) {
	router, admin, _ := mountedOn(t, map[string]tenancy.Tenant{host: acme, otherHost: globex})
	code, out := upload(t, router, files, "shown.png", "image/png", png)
	if code != http.StatusCreated {
		t.Fatalf("upload = %d %s", code, out)
	}
	id := field(t, out, "id")
	if _, err := admin.ExecContext(t.Context(), `INSERT INTO file_uses (tenant_id, file_id, module, entity, record, field, locale)
		VALUES ($1, $2, 'content', 'page', $3, 'body', 'en')`, acme.ID, id, uuid.New()); err != nil {
		t.Fatal(err)
	}
	counts := func() (rows, uses, deleted int) {
		t.Helper()
		if err := admin.QueryRowContext(t.Context(), `SELECT
			(SELECT count(*) FROM files WHERE id = $1),
			(SELECT count(*) FROM file_uses WHERE file_id = $1),
			(SELECT count(*) FROM `+outbox+` WHERE name = 'file.deleted')`, id).Scan(&rows, &uses, &deleted); err != nil {
			t.Fatal(err)
		}
		return rows, uses, deleted
	}

	if code, out, _ := sendOn(t, router, otherHost, http.MethodDelete, files+"/"+id, true); code != http.StatusNotFound {
		t.Errorf("another tenant's delete of a shown file = %d %s, want 404", code, out)
	}
	code, out, _ = send(t, router, http.MethodDelete, files+"/"+id, true)
	if code != http.StatusConflict || !strings.Contains(out, "1 record") {
		t.Errorf("the owner's delete of a shown file = %d %s, want 409 naming one record", code, out)
	}
	if rows, uses, deleted := counts(); rows != 1 || uses != 1 || deleted != 0 {
		t.Errorf("after two refused deletes: %d file rows, %d uses, %d deletion events; want 1, 1, 0", rows, uses, deleted)
	}
}
