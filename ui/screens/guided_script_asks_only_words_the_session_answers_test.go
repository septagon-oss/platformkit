package screens_test

// The countdown script and the mount are joined by one map and by nothing else:
// ui/resource writes the resolved session presentation into the page as its words,
// and ui/assets/js/guided.js asks that map for a line by the last part of the
// catalogue key it was translated under. One go/ast gate joins the Go source that
// raises a `screens.` key to the catalogues that answer it, and the guided length
// cases beside this one read the script for one number. Nothing joins the script's
// *vocabulary* to the words a mount answers: rename a key in ui/screens/catalog.go
// and `cfg.labels[name]` is undefined, the script's own words helper throws, and the
// whole enhancement — the countdown, the pause, the sound — is dead with no case in
// the tree to say so. Nor does anything ask that an answered line carry only the
// placeholders the script can fill: a `{duration}` added to a line the script passes
// no duration for is printed to a person as its own key. So this case reads the calls
// out of the script and requires each key to be a word the mount answers, and each
// placeholder of that word to be one the very call asks with.

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// guidedCall is one `words(…)` of the countdown script: the key it asks the page for,
// and the values it holds to fill that line with.
type guidedCall struct {
	asked string
	fills map[string]bool
}

// scriptCalls answers every `words('key')` and `words('key', {slot: …})` call in the
// source the browser runs, not a paraphrase of it. A script this case can read no
// words out of is a failure rather than a skip: the script and the mount have stopped
// being one vocabulary in a shape this case cannot see, which is the drift it exists
// to notice.
func scriptCalls(t *testing.T) []guidedCall {
	t.Helper()
	source, err := os.ReadFile("../assets/js/guided.js")
	if err != nil {
		t.Fatalf("no countdown script to read: %v", err)
	}
	asked := regexp.MustCompile(`words\('([a-z_0-9]+)'(?:,\s*\{([^{}]*)\})?\)`).FindAllStringSubmatch(string(source), -1)
	if len(asked) == 0 {
		t.Fatal("the countdown script asks for no word this case can read, so it and the mount are no longer one vocabulary")
	}
	calls := make([]guidedCall, 0, len(asked))
	for _, one := range asked {
		fills := map[string]bool{}
		for _, slot := range strings.Split(one[2], ",") {
			name, _, _ := strings.Cut(slot, ":")
			if name = strings.TrimSpace(name); name != "" {
				fills[name] = true
			}
		}
		calls = append(calls, guidedCall{asked: one[1], fills: fills})
	}
	return calls
}

// answeredWords is the map one mount hands its guided screens: the resolved session
// presentation of a resource that declares a guided read and both its commands, which
// is the same map ui/resource writes into the page the script reads.
func answeredWords(t *testing.T) map[string]string {
	t.Helper()
	r := resource()
	r.Guided = func(context.Context, uuid.UUID) (entity.GuidedSession, error) { return entity.GuidedSession{}, nil }
	r.GuidedCommands = [2]bool{true, true}
	r.Present = entity.EntryHints{Session: true}
	resolved := screens.Describe1(r, true).Resolved
	if resolved == nil || resolved.Session == nil || len(resolved.Session.Labels) == 0 {
		t.Fatal("a session read resolves no words, so a guided screen would answer its own script with nothing")
	}
	return resolved.Session.Labels
}

// TestTheGuidedScriptAsksOnlyWordsTheSessionAnswered requires every word the countdown
// script asks the page for to be a word a mount answers, and every placeholder that
// answered word carries to be one the very call fills. An unanswered key is a script
// that throws where a person expected a countdown; an unfilled placeholder is a line
// printed to a person as its own brace-word.
func TestTheGuidedScriptAsksOnlyWordsTheSessionAnswered(t *testing.T) {
	words := answeredWords(t)
	for _, call := range scriptCalls(t) {
		said, answered := words[call.asked]
		if !answered || strings.TrimSpace(said) == "" {
			t.Errorf("the countdown script asks the page for %q and the mount answers nothing, so the guided enhancement dies on that word", call.asked)
			continue
		}
		for _, slot := range regexp.MustCompile(`\{([a-z_0-9]+)\}`).FindAllStringSubmatch(said, -1) {
			if !call.fills[slot[1]] {
				t.Errorf("%q says {%s}, which no call for it fills, so a person reads %q with its placeholder whole", call.asked, slot[1], said)
			}
		}
	}
}
