package components_test

import (
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/document"
)

func TestEmptyStateCanonicalCopyAndSingleAction(t *testing.T) {
	for _, text := range []string{"Create the first item.", "Crie o primeiro item."} {
		p := c.EmptyStateProps{Title: "Items", Description: "legacy", Text: text,
			Action: new(c.ButtonProps{Label: "Create", Href: "/new"})}
		out := html(t, c.EmptyState(p))
		if !strings.Contains(out, text) || strings.Contains(out, "legacy") || strings.Count(out, `href="/new"`) != 1 {
			t.Fatalf("canonical copy/action lost: %s", out)
		}
		p.Text = ""
		if !strings.Contains(html(t, c.EmptyState(p)), "legacy") {
			t.Fatal("Description compatibility lost")
		}
		_, err := document.Render(c.EmptyStateWithSlots(p, c.EmptyStateSlots{Actions: []g.Node{g.Text("another action")}}))
		if err == nil {
			t.Fatal("P1: two primary action sources must be refused")
		}
	}
}

func TestNoticeAnnouncementContract(t *testing.T) {
	for _, tc := range []struct{ live, role, expected string }{
		{"", "status", "polite"}, {"polite", "status", "polite"},
		{"assertive", "alert", "assertive"}, {"off", "note", "off"},
	} {
		out := html(t, c.Notice(c.NoticeProps{Text: "Falhou", Live: tc.live}))
		if strings.Count(out, `aria-live=`) != 1 || !strings.Contains(out, `role="`+tc.role+`"`) || !strings.Contains(out, `aria-live="`+tc.expected+`"`) {
			t.Fatalf("P3: one correctly matched announcement for %q: %s", tc.live, out)
		}
	}
	out := html(t, c.Notice(c.NoticeProps{Text: "Guardado", Tone: "ok"}))
	if !strings.Contains(out, `data-alert-tone="success"`) {
		t.Fatalf("mobile ok must reuse Alert success: %s", out)
	}
	legacy := html(t, c.Alert(c.AlertProps{Message: "Legacy", Tone: "danger"}))
	if !strings.Contains(legacy, `aria-live="assertive"`) {
		t.Fatal("existing Alert announcement default changed")
	}
}

func TestNoticeRefusesInvalidCompositionWithoutBytesOrCapture(t *testing.T) {
	for name, p := range map[string]c.NoticeProps{
		"missing copy": {}, "blank copy": {Text: " "},
		"unknown tone":            {Text: "secret", Tone: "surprise"},
		"unknown live":            {Text: "secret", Live: "loud"},
		"missing dismissal label": {Text: "secret", Dismissible: true},
		"unnamed action":          {Text: "secret", Action: new(c.ButtonProps{Href: "/secret"})},
	} {
		t.Run(name, func(t *testing.T) {
			if p.Validate() == nil {
				t.Fatal("P1: Validate accepted malformed composition")
			}
			var raw strings.Builder
			if err := c.Notice(p).Render(&raw); err == nil || raw.Len() != 0 {
				t.Fatalf("P1: raw renderer emitted partial bytes: %q, %v", raw.String(), err)
			}
			if out, err := document.Render(c.Notice(p)); err == nil || out != nil {
				t.Fatalf("P1: document exposed invalid state: %s, %v", out, err)
			}
			example := examples.ExampleOf(examples.ExampleInfo{ID: "invalid", ComponentID: "pk-ui.component.notice"}, p, c.Notice)
			if _, err := example.Describe(); err == nil {
				t.Fatal("P1: invalid state became an exported capture")
			}
		})
	}
	p := c.NoticeProps{Text: "Read failed", Action: new(c.ButtonProps{Label: "Retry", Href: "/retry"})}
	if out, err := document.Render(c.NoticeWithSlots(p, c.NoticeSlots{Actions: []g.Node{g.Text("duplicate")}})); err == nil || out != nil {
		t.Fatalf("P1: duplicate action emitted bytes: %s, %v", out, err)
	}
}

func TestRichStateActionsRetainNativeFormsAndDoNotMutateInputs(t *testing.T) {
	action := c.ButtonProps{Label: "Tentar novamente", Type: "submit",
		ComponentProps: c.ComponentProps{Attrs: map[string]string{"form": "recovery"}}}
	p := c.NoticeProps{Text: "Não foi possível guardar.", Action: &action}
	out := html(t, c.Notice(p))
	for _, want := range []string{`type="submit"`, `form="recovery"`, "Tentar novamente"} {
		if !strings.Contains(out, want) {
			t.Fatalf("native recovery form lost %s: %s", want, out)
		}
	}
	p.Disabled = true
	if !strings.Contains(html(t, c.Notice(p)), "disabled") || action.Disabled {
		t.Fatal("disabled state must reach the action without mutating the caller")
	}
	p.Disabled, action.Loading = false, true
	out = html(t, c.Notice(p))
	if !strings.Contains(out, `aria-busy="true"`) || !strings.Contains(out, "disabled") || action.Disabled {
		t.Fatalf("pending retry must prevent a second submission: %s", out)
	}
}

func TestRichStateCopyIsEscapedAndRequestLocal(t *testing.T) {
	for _, text := range []string{"Tenant A <script>private</script>", "Tenant B & reservado"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			p := c.NoticeProps{Text: text, Title: text, Tone: "info"}
			out := html(t, c.Notice(p))
			if strings.Contains(out, "<script>") || strings.Count(out, "Tenant ") != 2 {
				t.Fatalf("P28: copy was not escaped/isolated: %s", out)
			}
			other := "Tenant A"
			if strings.HasPrefix(text, other) {
				other = "Tenant B"
			}
			if strings.Contains(out, other) {
				t.Fatal("P28: another request's notice was rendered")
			}
		})
	}
}
