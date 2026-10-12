package rest_test

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/rest"
)

type LimitedArticle struct {
	entity.Base
	Title string `json:"title" maxLength:"12" i18n:"translatable"`
	Body  string `json:"body,omitempty"`
}

func (LimitedArticle) TableName() string { return "rest_articles" }

func TestPlainTranslationObeysTheFieldsLengthLimit(t *testing.T) {
	port := &recordedPort{Translations: &stubPort{}}
	_, router, admin := mountAs(t, rest.Spec[*LimitedArticle]{
		Module: "articles", Entity: "article", Path: "/article",
		Read: "article:read", Write: "article:write", Translations: port,
	}, spokeCaller{})
	if _, err := admin.ExecContext(t.Context(), articleDDL); err != nil {
		t.Fatal(err)
	}
	code, body := call(t, router, http.MethodPost, "/api/v1/articles/article", `{"title":"About us."}`)
	if code != http.StatusCreated {
		t.Fatalf("create source = %d %s", code, body)
	}
	path := "/api/v1/articles/article/" + id(t, body)
	const overLimit = `{"title":"Este título é demasiado longo"}`
	code, body = call(t, router, http.MethodPost, "/api/v1/articles/article", overLimit)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("source field limit = %d %s, want 422", code, body)
	}
	code, body = call(t, router, http.MethodPost, path+"/translate",
		`{"lang":"pt-PT","values":`+overLimit+`}`)
	if code != http.StatusUnprocessableEntity {
		t.Errorf("translated field limit = %d %s, want 422", code, body)
	}
	if len(port.saved) != 0 {
		t.Errorf("invalid translation reached Save: %+v", port.saved)
	}
}
