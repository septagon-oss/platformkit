package page_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
)

func TestRequestNoticeExamplesRetainTheRenderedRecoveryContract(t *testing.T) {
	t.Parallel()
	c := chrome()
	c.Scripts = append(c.Scripts, "htmx-config.js")
	examples := page.RequestNoticeExamples(c.SignIn)
	if len(examples) != 4 {
		t.Fatalf("request notices = %d, want 4", len(examples))
	}
	runtime := render(t, page.Document(c, page.Request{}, page.View{}, h.Main()))
	for index, want := range []struct {
		kind, title, message string
		signin               bool
	}{
		{"anonymous", "Sign-in required", "Keep this page open to retain your input. Sign in in another tab, then return and try again. Nothing is retried automatically.", true},
		{"denied", "Permission denied", "You do not have permission for this action. Keep this page open to retain your input and contact an administrator if you need access.", false},
		{"changed", "Account changed", "Sign in with the account that opened this page before submitting again. Keep this page open to retain your input.", true},
		{"uncertain", "Check the result", "The request outcome is unknown. Keep this page open and check whether the action completed before trying again.", false},
	} {
		example := examples[index]
		if example.ID != "pk-auth-"+want.kind || example.Name != want.title || example.ComponentID != "pk-ui.component.stack" {
			t.Fatalf("wrong recovery identity or component contract: %+v", example.ExampleInfo)
		}
		body := []g.Node{components.Alert(components.AlertProps{Tone: "danger", Title: want.title, Message: want.message, Bordered: true})}
		if want.signin {
			body = append(body, components.Link(components.LinkProps{Label: "Sign in (opens a new tab)", Href: c.SignIn, External: true}))
		}
		content := components.Stack(components.StackProps{Gap: "3"}, body...)
		description, err := example.Describe()
		if err != nil || description.HTML != render(t, content) {
			t.Fatalf("%s changed the existing visible recovery content: %v", want.kind, err)
		}
		wrapper := render(t, h.Div(h.ID(example.ID), h.Hidden(""), h.Lang("en"), g.Attr("data-request-notice", ""), content))
		if !strings.Contains(runtime, wrapper) || strings.Count(runtime, `id="`+example.ID+`"`) != 1 {
			t.Fatalf("%s runtime lost its hidden, English, controller-owned wrapper", want.kind)
		}
		if !description.PropsEditable || len(description.OpaqueSlots) != 0 || len(description.Children) != len(body) {
			t.Fatalf("%s does not expose its directly owned composition", want.kind)
		}
		for index, child := range description.Children {
			id, component := "message", "pk-ui.component.alert"
			if index == 1 {
				id, component = "sign-in", "pk-ui.component.link"
			}
			if child.Description.ID != id || child.Description.ComponentID != component || !child.Description.PropsEditable ||
				child.Slot != "children" || child.Span == nil || description.HTML[child.Span.Start:child.Span.End] != child.Description.HTML {
				t.Fatalf("%s child %d lacks the existing typed contract or exact observed ownership", want.kind, index)
			}
		}
	}
	if _, err := ui.Export(design.Default(), append(components.Gallery(), examples...)); err != nil {
		t.Fatalf("notices must compose with Core's canonical interfaces: %v", err)
	}
	c.Scripts = nil
	if strings.Contains(render(t, page.Document(c, page.Request{}, page.View{}, h.Main())), "data-request-notice") {
		t.Fatal("a shell without the request controller must not render notices")
	}
}

func TestRequestNoticeExamplesKeepSignInLocalAndSeparateFromWriteRecovery(t *testing.T) {
	t.Parallel()
	for _, signin := range []string{"", "https://elsewhere.test/login", "//elsewhere.test/login", `/\elsewhere.test/login`, "javascript:alert(1)", "login"} {
		for _, example := range page.RequestNoticeExamples(signin) {
			description, err := example.Describe()
			if err != nil || len(description.Children) != 1 || strings.Contains(description.HTML, "<a ") {
				t.Fatalf("sign-in %q produced a recovery link: %v", signin, err)
			}
		}
	}
	for _, example := range page.RequestNoticeExamples("/auth/sign-in?next=%2Fadmin") {
		body := render(t, example.Node)
		if example.ID == "pk-auth-anonymous" || example.ID == "pk-auth-changed" {
			for _, want := range []string{`href="/auth/sign-in?next=%2Fadmin"`, `target="_blank"`, `rel="noopener noreferrer"`, "Sign in (opens a new tab)"} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s lost safe navigation or its visible name: %s", example.ID, want)
				}
			}
		} else if strings.Contains(body, "<a ") {
			t.Fatalf("%s must not misrepresent sign-in as write recovery", example.ID)
		}
		if strings.Contains(body, "<form") || strings.Contains(body, "<button") || strings.Contains(body, "hx-") {
			t.Fatal("a recovery notice must not replay a write or collect input")
		}
	}
}

func TestRequestNoticeSourceEditsAreDetachedAndUseSharedReplacementContracts(t *testing.T) {
	t.Parallel()
	theme := design.Default()
	source := page.RequestNoticeExamples("/admin/login")
	base, err := ui.Export(theme, source)
	if err != nil {
		t.Fatal(err)
	}
	path := []string{"pk-auth-uncertain", "message"}
	proposal := ui.PropsProposal{BaseSHA256: base.SHA256, Path: path, Props: json.RawMessage(`{"message":"Keep this page open while checking the stored result."}`)}
	_, edited, err := ui.ProjectProps(theme, source, proposal)
	if err != nil || edited.SHA256 == base.SHA256 {
		t.Fatalf("the nested recovery text did not produce a source candidate: %v", err)
	}
	for index, before := range base.Examples {
		after := edited.Examples[index]
		if before.ID == path[0] {
			if !strings.Contains(after.HTML, "Keep this page open while checking the stored result.") {
				t.Fatal("the selected message was not recomposed")
			}
		} else if !reflect.DeepEqual(before, after) {
			t.Fatalf("editing recovery copy changed sibling %s", before.ID)
		}
	}
	_, replaced, err := ui.ProjectReplacement(theme, source, ui.ReplacementProposal{
		BaseSHA256: base.SHA256, Path: path, ReplacementPath: []string{"pk-auth-denied", "message"}})
	if err != nil {
		t.Fatalf("same-interface Alert replacement failed: %v", err)
	}
	index := slices.IndexFunc(replaced.Examples, func(e components.ExampleDescription) bool { return e.ID == path[0] })
	if index < 0 || !strings.Contains(replaced.Examples[index].HTML, "Permission denied") {
		t.Fatal("Alert replacement did not retain its destination or source copy")
	}
	if _, _, err := ui.ProjectReplacement(theme, source, ui.ReplacementProposal{
		BaseSHA256: base.SHA256, Path: path, ReplacementPath: []string{"pk-auth-anonymous", "sign-in"}}); err == nil {
		t.Fatal("a Link must not replace an Alert's different interface")
	}
	changed := page.RequestNoticeExamples("/account/sign-in")
	if _, _, err := ui.ProjectProps(theme, changed, proposal); !errors.Is(err, ui.ErrStaleExport) {
		t.Fatalf("changed recovery navigation accepted a stale source proposal: %v", err)
	}
	fresh, err := ui.Export(theme, page.RequestNoticeExamples("/admin/login"))
	if err != nil || !reflect.DeepEqual(fresh, base) {
		t.Fatalf("projection or replacement mutated the runtime notice source: %v", err)
	}
}
