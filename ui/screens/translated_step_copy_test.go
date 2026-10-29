package screens_test

// A stepped form's step labels are copy keys, so the words a person sees on the
// button arrive from the product's catalogues in the request's language. The step
// rules refuse a label that cannot interpolate an answer — a second {answer} token,
// a stray brace, an empty value — and refuse it when it is written in the
// declaration; a catalogue carries the same words for every language the product
// serves, and nothing checks them at mount. Copy a translator cannot have meant
// must cost the person that one label, not the form: the step the person is
// answering stays, with the label the author declared.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// unreadableStepCopy is a product's catalogue in one language whose step label the
// step rules cannot honour.
type unreadableStepCopy struct{}

func (unreadableStepCopy) Select(...string) locale.Locale {
	return locale.Locale{Language: "pt-PT", Formatter: unreadableStepCopy{}}
}

func (unreadableStepCopy) Text(_, fallback string, _ ...any) string {
	if strings.Contains(fallback, "{answer}") {
		return "Continuar {answer} {answer}"
	}
	return fallback
}

// translatedStepServer mounts the stepped note behind a shell that answers in that
// language, the way a product's composition does.
func translatedStepServer(t *testing.T, res httpx.Resource, messages page.Messages) http.Handler {
	t.Helper()
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: formHost, Tenants: formDirectory{}, Authorize: formDirectory{},
		Conn: conn, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.MustParse("22222222-2222-4222-8222-222222222222")}, true, nil
		},
	})
	shell := formShell()
	shell.Messages = messages
	screens.Mount(api.Surfaces("note").App, shell, opts, res)
	return router
}

func TestASteppedFormKeepsItsStepsWhenTheTranslatedLabelCannotBeHonoured(t *testing.T) {
	res := steppedNote()
	res.Create = func(context.Context, map[string]any) (map[string]any, error) {
		t.Error("the first step page wrote")
		return map[string]any{"id": uuid.NewString()}, nil
	}
	h := translatedStepServer(t, res, unreadableStepCopy{})

	req := httptest.NewRequest(http.MethodGet, "http://"+formHost+res.Screen+"/new", nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "pt-PT")
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("a stepped form whose translated step label cannot be honoured = %d (%s), want the first step of the form",
			w.Code, body)
	}
	for _, want := range []string{`name="__pkit_form"`, `aria-current="step"`, `name="title"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the stepped page omits %q: %s", want, body)
		}
	}
	if !strings.Contains(body, `data-step-heading`) {
		t.Errorf("the stepped page shows no step heading: %s", body)
	}
}
