package main

// The workspace home's primary action, in the language of the person who reads the
// page. An action's words belong to the composition that declared it, so the
// reference application holds them in its own catalogue and the shell asks for them
// under the screen the action leads to. Both halves are asserted here: the action
// answers in the reader's language, and the destination beside it keeps its own word
// — the home's action and the sidebar's link are keyed apart, not shared.

import (
	"regexp"
	"strings"
	"testing"
)

// homeAnchor is one rendered anchor, inspected for where it leads and what it says.
var homeAnchor = regexp.MustCompile(`(?s)<a\b[^>]*>.*?</a>`)

var anchorText = regexp.MustCompile(`(?s)<[^>]*>`)

// homeWords answers the text of the home's one primary action that leads to href, and
// of the sidebar item for that same destination. Either is "" when the page draws no
// such element, which the caller reports as the missing label it is.
func homeWords(t *testing.T, html, href string) (action, destination string) {
	t.Helper()
	for _, tag := range homeAnchor.FindAllString(html, -1) {
		if !strings.Contains(tag, `href="`+href+`"`) {
			continue
		}
		text := strings.TrimSpace(anchorText.ReplaceAllString(tag, " "))
		text = strings.TrimSpace(strings.ReplaceAll(text, "<!--pk-text:label-->", ""))
		switch {
		case strings.Contains(tag, `data-variant="primary"`) && strings.Contains(tag, `data-component="button"`):
			if action != "" {
				t.Errorf("the home draws a second primary action for %s: %q", href, text)
			}
			action = text
		case strings.Contains(tag, `data-sidebar-item-label="true"`):
			if destination == "" {
				destination = text
			}
		}
	}
	return action, destination
}

// TestTheHomeActionSpeaksTheReadersLanguage: the home's primary action is the
// composition's own copy, so a tenant served in Portuguese reads that action in
// Portuguese while the sidebar keeps the destination's word, and a reader who asks in
// English reads the label the composition declared.
func TestTheHomeActionSpeaksTheReadersLanguage(t *testing.T) {
	cfg, admin := languageApp(t, `{"default":"pt-PT","supported":["en"]}`)

	for _, leg := range []struct {
		ranking, lang, action, destination string
	}{
		{"pt-PT,pt;q=0.9", "pt-PT", "Ir para as tarefas", "Tarefas"},
		{"en", "en", "Go to tasks", "Tasks"},
	} {
		html := dashboard(t, cfg, admin, leg.ranking)
		if got := pageLang(html); got != leg.lang {
			t.Fatalf("asking %q answered the page lang %q, want %q", leg.ranking, got, leg.lang)
		}
		action, destination := homeWords(t, html, "/app/task/tasks")
		if action != leg.action {
			t.Errorf("%s: the home's primary action reads %q, want %q", leg.ranking, action, leg.action)
		}
		if destination != leg.destination {
			t.Errorf("%s: the sidebar names that destination %q, want %q", leg.ranking, destination, leg.destination)
		}
	}
}
