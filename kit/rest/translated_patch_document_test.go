package rest_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/rest"
)

func TestTranslatedPatchDocumentationPreservesTheLocaleWriteBoundary(t *testing.T) {
	docAPI, docRouter := patchDocumentAPI()
	s := rest.Spec[*CountedArticle]{Name: rest.Name{One: "Counted", Other: "Counteds"},
		Module: "articles", Entity: "counted", Path: "/counted",
		Read: "article:read", Write: "article:write", Translations: &stubPort{},
	}
	s.Mount(docAPI.Surfaces(s.Module))
	patch := patchComponents(t, docRouter)["CountedArticlePatch"]
	if patch == nil || len(patch.Properties) != 1 || patch.Properties["title"] == nil || patch.Properties["title"].Type != "string" {
		t.Fatalf("translated PATCH must document only its writable title: %+v", patch)
	}
	op := recorded(t, docAPI, "articles-counted-update")
	if got := op.RequestBody.Content["application/json"].Schema.Ref; got != "#/components/schemas/CountedArticlePatch" {
		t.Fatalf("translated PATCH schema = %q", got)
	}
	for _, name := range []string{"lang", "If-Match"} {
		found := false
		for _, parameter := range op.Parameters {
			if parameter.Name == name {
				found = true
			}
		}
		if !found {
			t.Errorf("typed PATCH document lost parameter %s", name)
		}
	}

	port := &recordedPort{Translations: &stubPort{}}
	router, rowID := mountedCountedArticle(t, port)
	path := "/api/v1/articles/counted/" + rowID
	code, before, _ := quoted(t, router, http.MethodGet, path+"?lang=en", "", "")
	if code != http.StatusOK {
		t.Fatalf("source read = %d %s", code, before)
	}
	for _, body := range []string{`{"title":null}`, `{"revision":2}`, `{"unknown":"text"}`} {
		code, refused, _ := quoted(t, router, http.MethodPatch, path+"?lang=pt-PT", body, `"1"`)
		if code != http.StatusUnprocessableEntity || strings.Contains(refused, `"id":`) {
			t.Errorf("invalid locale PATCH %s = %d %s, want 422 with no row", body, code, refused)
		}
	}
	if len(port.saved) != 0 {
		t.Fatalf("invalid locale PATCH reached Save: %+v", port.saved)
	}
	if code, after, _ := quoted(t, router, http.MethodGet, path+"?lang=en", "", ""); code != http.StatusOK || after != before {
		t.Fatalf("invalid locale writes changed source: %d %s; before %s", code, after, before)
	}
	code, body, head := quoted(t, router, http.MethodPatch, path+"?lang=pt-PT", `{"title":"Sobre nós."}`, `"1"`)
	if code != http.StatusOK || head.Get("ETag") != `"1"` {
		t.Fatalf("valid locale PATCH = %d %s, ETag %q", code, body, head.Get("ETag"))
	}
	if len(port.saved) != 1 || port.saved[0].Values["title"] != "Sobre nós." || port.saved[0].Locale != "pt-PT" {
		t.Fatalf("valid locale PATCH did not save the Portuguese title: %+v", port.saved)
	}
	if code, after, _ := quoted(t, router, http.MethodGet, path+"?lang=en", "", ""); code != http.StatusOK || after != before {
		t.Errorf("valid locale write changed source: %d %s; before %s", code, after, before)
	}
}
