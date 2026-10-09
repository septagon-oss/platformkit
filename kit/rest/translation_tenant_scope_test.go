package rest_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func TestTranslationRoutesCannotReachAnotherTenantsRecord(t *testing.T) {
	port := &recordedPort{Translations: &stubPort{}}
	_, router, admin := mountAs(t, rest.Spec[*Article]{
		Module: "articles", Entity: "article", Path: "/article",
		Read: "article:read", Write: "article:write", Translations: port,
	}, spokeCaller{})
	if _, err := admin.ExecContext(t.Context(), articleDDL); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if _, err := admin.ExecContext(t.Context(),
		`INSERT INTO rest_articles (id, tenant_id, title) VALUES ($1, $2, 'Private source')`, id, uuid.New()); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/articles/article/" + id.String()
	if code, body := call(t, router, http.MethodGet, path+"?lang=pt-PT", ""); code != http.StatusNotFound {
		t.Errorf("foreign translated read = %d %s", code, body)
	}
	for _, verb := range []string{"translate", "review-translation", "suggest-translation", "untranslate"} {
		input := `{"lang":"pt-PT","fields":["title"]}`
		if verb == "translate" {
			input = `{"lang":"pt-PT","values":{"title":"Outra"}}`
		}
		code, body := call(t, router, http.MethodPost, path+"/"+verb, input)
		if code != http.StatusNotFound {
			t.Errorf("foreign %s = %d %s", verb, code, body)
		}
	}
	if len(port.saved)+len(port.reviewed)+len(port.suggested)+len(port.removed) != 0 {
		t.Error("a foreign record reached the translation mutation port")
	}
}
