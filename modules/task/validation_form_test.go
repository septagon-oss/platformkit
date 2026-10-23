package task_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestTaskFormShowsEachViolationOnlyUnderItsOwnField(t *testing.T) {
	api, router := mounted(t)
	res := api.Resources()[0]
	shell := page.Shell{
		Chrome: page.Chrome{Brand: "Tasks", SignIn: "/app/admin/login"},
		Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node {
			return h.Div(g.Group(body))
		},
	}
	screens.Mount(api.Surfaces("task").App, shell, screens.Options{Workspace: "/app"}, res)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://"+host+res.Screen,
		strings.NewReader(url.Values{"title": {" "}, "priority": {"unknown"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Origin", "http://"+host)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("refused form: %d %s", w.Code, w.Body.String())
	}
	doc, err := html.Parse(strings.NewReader(w.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	controls, byID := map[string]*html.Node{}, map[string]*html.Node{}
	for node := range doc.Descendants() {
		if node.Type != html.ElementNode {
			continue
		}
		byID[attr(node, "id")] = node
		if slices.Contains([]string{"input", "select", "textarea"}, node.Data) {
			controls[attr(node, "name")] = node
		}
	}
	for name, want := range map[string]string{
		"title": "a task needs a title", "priority": `priority "unknown" is not a priority`,
	} {
		control := controls[name]
		if control == nil {
			t.Errorf("refused form has no %s control", name)
			continue
		}
		errorID := attr(control, "id") + "-error"
		message := byID[errorID]
		if attr(control, "aria-invalid") != "true" || !slices.Contains(strings.Fields(attr(control, "aria-describedby")), errorID) || message == nil {
			t.Errorf("%s has no associated field refusal", name)
			continue
		}
		if got := strings.TrimSpace(text(message)); got != want {
			t.Errorf("%s feedback = %q; want only %q", name, got, want)
		}
	}
}
