package screens_test

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func steppedNote() httpx.Resource {
	r := resource()
	r.Present.Sections = []entity.EntitySection{{Key: "details", Label: "Description"}, {Key: "options", Label: "Options"}}
	r.Present.Forms = &entity.FormsHints{Create: &entity.FormHints{Layout: "steps", Steps: []entity.FormStepHints{
		{PrimaryLabel: "Next · {answer}", AnswerField: "title", EmptyLabel: "Enter a title"},
		{Section: "details", PrimaryLabel: "Next"}, {Section: "options", PrimaryLabel: "Save note"},
	}}}
	for i := range r.Schema.Fields {
		switch r.Schema.Fields[i].Name {
		case "body", "rank":
			r.Schema.Fields[i].Presentation.Section = "details"
		case "pinned", "tags":
			r.Schema.Fields[i].Presentation.Section = "options"
		}
	}
	return r
}

func stepRequest(h http.Handler, path string, values url.Values) *httptest.ResponseRecorder {
	method, body := "GET", ""
	if values != nil {
		method, body = "POST", values.Encode()
	}
	r := httptest.NewRequest(method, "http://"+formHost+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "text/html")
	r.Header.Set("Origin", "http://"+formHost)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func continuation(t *testing.T, response *httptest.ResponseRecorder, status, step int) rest.FormState {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status %d, want %d: %s", response.Code, status, response.Body)
	}
	match := regexp.MustCompile(`name="__pkit_form" value="([^"]*)"`).FindStringSubmatch(response.Body.String())
	if len(match) != 2 {
		t.Fatalf("no continuation: %s", response.Body)
	}
	var state rest.FormState
	if err := json.Unmarshal([]byte(html.UnescapeString(match[1])), &state); err != nil || state.Step != step {
		t.Fatalf("state: %+v %v", state, err)
	}
	if strings.Contains(response.Body.String(), "hx-post=") {
		t.Fatal("steps must navigate whole documents")
	}
	if !strings.Contains(response.Header().Get("Cache-Control"), "no-store") || response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("answers were cacheable: %v", response.Header())
	}
	return state
}

func postStep(h http.Handler, path string, state rest.FormState, intent string, values url.Values) *httptest.ResponseRecorder {
	if values == nil {
		values = url.Values{}
	}
	encoded, _ := json.Marshal(state)
	values.Set("__pkit_form", string(encoded))
	values.Set("__pkit_intent", intent)
	return stepRequest(h, path, values)
}

func TestGeneratedStepsCaptureBackAndRouteFinalErrors(t *testing.T) {
	r := steppedNote()
	calls := 0
	r.Create = func(_ context.Context, values map[string]any) (map[string]any, error) {
		calls++
		if values["title"] == "reserved" {
			return nil, &problem.Problem{Status: 422, Detail: "Choose another title", Violations: []fault.Violation{{Location: "tags", Message: "Choose a tag"}, {Location: "title", Message: "Already used"}}}
		}
		if values["body"] != "edited" || values["rank"] != int64(7) || values["pinned"] != false {
			t.Fatalf("incomplete final payload: %+v", values)
		}
		return map[string]any{"id": uuid.NewString()}, nil
	}
	h := formResourceServer(t, r)
	at, nav := r.Screen, r.Screen+"/new/steps"
	s := continuation(t, stepRequest(h, at+"/new", nil), 200, 0)
	s = continuation(t, postStep(h, nav, s, "next", url.Values{"title": {"reserved"}}), 200, 1)
	bad := postStep(h, nav, s, "next", url.Values{"body": {"draft"}, "rank": {"not-an-integer"}})
	s = continuation(t, bad, 422, 1)
	if !strings.Contains(bad.Body.String(), `value="not-an-integer"`) || !strings.Contains(bad.Body.String(), `autofocus="autofocus"`) {
		t.Fatal("invalid raw answer or focus lost")
	}
	s = continuation(t, postStep(h, nav, s, "next", url.Values{"body": {"edited"}, "rank": {"7"}}), 200, 2)
	s = continuation(t, postStep(h, nav, s, "back", url.Values{"tags": {"a, b"}}), 200, 1)
	if s.Answers["tags"] != "a, b" || s.Answers["pinned"] != "false" || calls != 0 {
		t.Fatalf("back changed answers or wrote: %+v %d", s, calls)
	}
	s = continuation(t, postStep(h, nav, s, "next", url.Values{"body": {"edited"}, "rank": {"7"}}), 200, 2)
	s = continuation(t, postStep(h, at, s, "submit", url.Values{"tags": {"a, b"}}), 422, 0)
	if calls != 1 || s.Answers["body"] != "edited" {
		t.Fatalf("domain refusal: %+v calls=%d", s, calls)
	}
	s = continuation(t, postStep(h, nav, s, "next", url.Values{"title": {"Notebook"}}), 200, 1)
	s = continuation(t, postStep(h, nav, s, "next", url.Values{"body": {"edited"}, "rank": {"7"}}), 200, 2)
	if got := postStep(h, at, s, "submit", nil); got.Code != 303 || calls != 2 {
		t.Fatalf("final: %d %d %s", got.Code, calls, got.Body)
	}
}

func TestSteppedFormScopeMismatchDoesNotEchoAnswers(t *testing.T) {
	r := steppedNote()
	r.Create = func(context.Context, map[string]any) (map[string]any, error) {
		t.Fatal("scope mismatch invoked write")
		return nil, nil
	}
	h := formResourceServer(t, r)
	s := continuation(t, stepRequest(h, r.Screen+"/new", nil), 200, 0)
	s.Tenant, s.Answers["title"] = uuid.NewString(), "private-canary"
	got := postStep(h, r.Screen+"/new/steps", s, "next", url.Values{"title": {"private-canary"}})
	if got.Code != 404 || strings.Contains(got.Body.String(), "private-canary") {
		t.Fatalf("scope echo: %d %s", got.Code, got.Body)
	}
}

func TestLocalizedStepPolicyHasStableIdentityAndDigest(t *testing.T) {
	r := steppedNote()
	english := screens.Describe1(r, true)
	translated := screens.Describe1(screens.Localise(r, func(_, fallback string) string { return "Traduzido " + fallback }), true)
	first, second := english.Resolved, translated.Resolved
	if first.Layout.Forms.Create.Steps[0].PrimaryLabel == second.Layout.Forms.Create.Steps[0].PrimaryLabel {
		t.Fatal("copy was not localized")
	}
	if rest.FormDigest("create", r.Schema.Fields, *first, *first.Layout.Forms.Create) != rest.FormDigest("create", r.Schema.Fields, *second, *second.Layout.Forms.Create) {
		t.Fatal("copy invalidated the continuation")
	}
	if r.Present.Forms.Create.Steps[0].PrimaryLabel != "Next · {answer}" {
		t.Fatal("shared hints mutated")
	}
}
