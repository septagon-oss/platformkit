package screens_test

// A step's button and its answer prompt are copy keys, so the words a person reads
// on them come from the product's catalogues in the request's language. The step
// rules refuse a label with no words in it — `strings.TrimSpace` of the label and of
// the answer prompt both have to hold text — and refuse it when the author wrote it
// in the declaration, at mount. Copy arriving from a catalogue passes no mount, and
// copy nobody could read is still copy: a translation whose value is spaces is a
// value, so it reaches the step rules, where the whole form is refused over it and
// the page the person is answering with dies. Words no translator meant must cost
// that one label, not the form: the step, its fields, its stepper and its
// continuation stay, in whatever words the declaration has.

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
	"github.com/septagon-oss/platformkit/ui/screens"
)

// wordlessStepCopy is a product's catalogue in one language that answers one named
// key with spaces and every other key with the words the declaration holds.
type wordlessStepCopy struct{ aspect string }

func (c wordlessStepCopy) Select(...string) locale.Locale {
	return locale.Locale{Language: "pt-PT", Formatter: c}
}

func (c wordlessStepCopy) Text(key, fallback string, _ ...any) string {
	if strings.HasSuffix(key, c.aspect) {
		return "   "
	}
	return fallback
}

// wordlessStepServer mounts a stepped note behind a shell that speaks that
// catalogue, the way a composition that translated its step copy does.
func wordlessStepServer(t *testing.T, res httpx.Resource, aspect string) http.Handler {
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
	shell.Messages = wordlessStepCopy{aspect: aspect}
	screens.Mount(api.Surfaces("note").App, shell, opts, res)
	return router
}

// firstStepOfTheForm is the structure a stepped page owes whoever asked for it, in
// any language and whatever words its button ended up wearing.
func firstStepOfTheForm(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("the stepped page in the request's language = %d (%s), want its first step", w.Code, body)
	}
	for _, want := range []string{`name="__pkit_form"`, `aria-current="step"`, `name="title"`, `data-step-heading`} {
		if !strings.Contains(body, want) {
			t.Errorf("the stepped page omits %q: %s", want, body)
		}
	}
}

// unwrittenNote refuses to be created: drawing the first step must not write, and a
// stepped page that wrote would say so here.
func unwrittenNote(t *testing.T, res httpx.Resource) httpx.Resource {
	t.Helper()
	res.Create = func(context.Context, map[string]any) (map[string]any, error) {
		t.Error("the first step page wrote")
		return map[string]any{"id": uuid.NewString()}, nil
	}
	return res
}

func getFirstStep(t *testing.T, h http.Handler, res httpx.Resource) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+formHost+res.Screen+"/new", nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "pt-PT")
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// A step whose declared label is a plain word carries no answer slot, and the
// catalogue's copy for it has no slot to keep. Spaces are not that copy.
func TestASteppedFormKeepsItsStepsWhenTheTranslatedLabelHasNoWords(t *testing.T) {
	res := unwrittenNote(t, steppedNote())
	h := wordlessStepServer(t, res, "step.section:details.primaryLabel")
	firstStepOfTheForm(t, getFirstStep(t, h, res))
}

// A step whose label interpolates the current answer needs the answer prompt to say
// what the answer will be when the field is empty; a catalogue with no words for
// that prompt leaves the step nothing to say, which is a refusal of the whole form
// today.
func TestASteppedFormKeepsItsStepsWhenTheTranslatedAnswerPromptHasNoWords(t *testing.T) {
	res := unwrittenNote(t, steppedNote())
	h := wordlessStepServer(t, res, "step.unsectioned.emptyLabel")
	firstStepOfTheForm(t, getFirstStep(t, h, res))
}

// The half that already holds: a translation that cannot carry the answer slot the
// declaration declares is refused as the label it is, and the step keeps the label
// its author wrote. This case passes on the branch and is here so the two halves of
// the rule are pinned together.
func TestASteppedFormKeepsItsStepsWhenTheTranslatedLabelDropsTheAnswerSlot(t *testing.T) {
	res := unwrittenNote(t, steppedNote())
	h := wordlessStepServer(t, res, "step.unsectioned.primaryLabel")
	firstStepOfTheForm(t, getFirstStep(t, h, res))
}
