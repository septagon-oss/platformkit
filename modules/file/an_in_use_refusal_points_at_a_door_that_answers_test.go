package file_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestAnInUseRefusalPointsAtADoorThatAnswers follows the address a refused
// delete names for the records that still show the file: the 409 is only worth
// its pointer if a client that follows it, from the same origin and session,
// reaches the module's list of uses rather than a page that does not exist.
func TestAnInUseRefusalPointsAtADoorThatAnswers(t *testing.T) {
	router, admin, _ := mountedOn(t, map[string]tenancy.Tenant{host: acme})
	code, out := upload(t, router, files, "shown.png", "image/png", png)
	if code != http.StatusCreated {
		t.Fatalf("upload = %d %s", code, out)
	}
	id := field(t, out, "id")
	record := uuid.New()
	if _, err := admin.ExecContext(t.Context(), `INSERT INTO file_uses (tenant_id, file_id, module, entity, record, field, locale)
		VALUES ($1, $2, 'content', 'page', $3, 'body', 'en')`, acme.ID, id, record); err != nil {
		t.Fatal(err)
	}
	code, out, _ = send(t, router, http.MethodDelete, files+"/"+id, true)
	if code != http.StatusConflict {
		t.Fatalf("delete of a shown file = %d %s, want 409", code, out)
	}
	var refusal struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(out), &refusal); err != nil {
		t.Fatal(err)
	}
	var pointer string
	for _, word := range strings.Fields(refusal.Detail) {
		named, err := url.Parse(strings.TrimRight(word, ".,;"))
		if err == nil && strings.HasPrefix(named.Path, "/") && strings.HasSuffix(named.Path, id+"/uses") {
			pointer = named.Path
		}
	}
	if pointer == "" {
		t.Fatalf("the refusal %q names no address of the file's uses", refusal.Detail)
	}
	if code, body, _ := send(t, router, http.MethodGet, pointer, true); code != http.StatusOK || !strings.Contains(body, record.String()) {
		t.Errorf("following the refusal's address %s = %d %s, want 200 listing the record that shows it", pointer, code, body)
	}
}
