package rest_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func TestAReadOnlySignedInResourceCannotBeMutatedThroughTranslation(t *testing.T) {
	port := &recordedPort{Translations: &stubPort{}}
	_, router, admin := mountAs(t, rest.Spec[*Article]{
		Module: "articles", Entity: "article", Path: "/article",
		ReadAuth: httpx.SignedIn(), WriteAuth: httpx.SignedIn(),
		Operations: []httpx.CRUD{rest.List, rest.Read}, Translations: port,
	}, spokeCaller{})
	if _, err := admin.ExecContext(t.Context(), articleDDL); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if _, err := admin.ExecContext(t.Context(),
		`INSERT INTO rest_articles (id, tenant_id, title) VALUES ($1, $2, 'About us.')`, id, spokeTenant.ID); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/articles/article/" + id.String()
	code, body := call(t, router, http.MethodGet, path, "")
	if code != http.StatusOK {
		t.Fatalf("read declared resource = %d %s", code, body)
	}
	code, body = call(t, router, http.MethodPatch, path, `{"title":"Changed"}`)
	if code < 400 {
		t.Fatalf("read-only PATCH unexpectedly accepted: %d %s", code, body)
	}
	code, body = call(t, router, http.MethodPost, path+"/translate", `{"lang":"pt-PT","values":{"title":"Alterado"}}`)
	if code < 400 || len(port.saved) != 0 {
		t.Errorf("signed-in caller mutated read-only resource: status %d, Save calls %d, body %s", code, len(port.saved), body)
	}
}
