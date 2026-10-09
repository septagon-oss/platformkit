package rest_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/translation/contracts/translationtest"
)

// A machine draft is held to the field's own rules exactly as a person's text is:
// a provider that answers a title longer than the field allows is a 422 that saves
// no draft, publishes nothing, and leaves nothing for a reviewer to approve.
func TestAMachineDraftObeysTheFieldsLengthLimit(t *testing.T) {
	spec := rest.Spec[*LimitedArticle]{
		Module: "articles", Entity: "article", Path: "/article",
		Read: "article:read", Write: "article:write",
	}
	machine := &translationtest.Machine{Answer: func(context.Context, string, string, string) (string, error) {
		return "Este título é demasiado longo", nil
	}}
	fake := translationtest.NewFake(machine, rest.TranslationSourceOf(spec))
	spec.Translations = fake
	_, router, admin := mountAs(t, spec, spokeCaller{})
	if _, err := admin.ExecContext(t.Context(), articleDDL); err != nil {
		t.Fatal(err)
	}
	code, body := call(t, router, http.MethodPost, "/api/v1/articles/article", `{"title":"About us."}`)
	if code != http.StatusCreated {
		t.Fatalf("create source = %d %s", code, body)
	}
	path := "/api/v1/articles/article/" + id(t, body)
	code, body = call(t, router, http.MethodPost, path+"/suggest-translation", `{"lang":"pt-PT","fields":["title"]}`)
	if code != http.StatusUnprocessableEntity {
		t.Errorf("machine draft over the field limit = %d %s, want 422", code, body)
	}
	if got := fake.Payloads(); len(got) != 0 {
		t.Errorf("a machine draft over the field limit was saved and announced: %+v", got)
	}
}
