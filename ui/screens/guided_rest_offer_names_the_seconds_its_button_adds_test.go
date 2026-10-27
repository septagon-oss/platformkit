package screens_test

// Where a guided pause stops is one rule, and the cases that walk the declared
// lengths keep asking the validator where it is. How much one press of the pause's
// own button adds is a different number, and nothing asks it: it ships in four
// places — the words this package carries for the button, the words the countdown
// says once the press lands, the seconds ui/resource writes into the link behind
// the button, and the milliseconds ui/assets/js/guided.js adds to the deadline when
// the same button is pressed with a script running. Any one can be edited alone:
// widened there, the page promises one pause and takes the person to another. So
// this case reads the number out of each thing that ships it and asks for one.

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
	core "github.com/septagon-oss/platformkit/ui/resource"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// theRest is a pair between two sets of a step, which owns its pause because its
// first set is the last one the session recorded; the two expressions are where
// the pause's page writes its offer and where the countdown script moves it.
const (
	restDeclared   = 45
	restRecorded   = 1
	pauseOfferRe   = `data-guided-add=""[^>]*href="[^"]*extra=([0-9]+)"`
	deadlineMoveRe = `restValue\.deadline\)\+([0-9]+)`
)

// pauseLabels is the session vocabulary the mount hands the renderer: the shipped
// English of a resource that declares a guided read and both its commands.
func pauseLabels(t *testing.T) map[string]string {
	t.Helper()
	r := resource()
	r.Guided = func(context.Context, uuid.UUID) (entity.GuidedSession, error) { return entity.GuidedSession{}, nil }
	r.GuidedCommands = [2]bool{true, true}
	r.Present = entity.EntryHints{Session: true}
	resolved := screens.Describe1(r, true).Resolved
	if resolved == nil || resolved.Session == nil {
		t.Fatal("a session read resolves no presentation, so its screen would say nothing at all")
	}
	return resolved.Session.Labels
}

// pauseOffer shows the pause of a step that declares declared seconds, with taken
// already added on top, and answers the total its button's own link leads a person
// to. The words it says are the ones it was given, or this is no longer that button.
func pauseOffer(t *testing.T, labels map[string]string, declared, taken int) int {
	t.Helper()
	step, sets := uuid.New(), []uuid.UUID{uuid.New(), uuid.New()}
	session := entity.GuidedSession{ID: uuid.New(), Title: "Sequence",
		Steps: []entity.GuidedStep{{ID: step, Title: "Hold", Items: []entity.GuidedItem{}, Sets: sets, RestBetweenSeconds: int32(declared)}}}
	session.Progress = &entity.GuidedProgress{Revision: int64(restRecorded + 1)}
	for i := range restRecorded {
		session.Progress.Completed = append(session.Progress.Completed, entity.GuidedCompletion{
			SessionID: session.ID, StepID: step, SetID: sets[i], Revision: int64(i + 2), CompletedAt: time.Unix(1777000000, 0).UTC()})
	}
	view := core.Guided(core.GuidedPage{Session: session, Base: "/app/practice/sessions/" + session.ID.String(), Exit: "/app",
		Mode: "rest", Step: 0, Set: 0, Extra: taken, Policy: core.GuidedPolicy{Labels: labels}}, core.Options{})
	var out strings.Builder
	for _, node := range view.Body {
		if err := node.Render(&out); err != nil {
			t.Fatal(err)
		}
	}
	body := out.String()
	if said := labels["add_15"]; said == "" || !strings.Contains(body, said) {
		t.Fatalf("the pause screen offers seconds behind no %q: %s", said, body)
	}
	offer := regexp.MustCompile(pauseOfferRe).FindStringSubmatch(body)
	if offer == nil {
		t.Fatalf("the pause screen writes no number of seconds behind its button: %s", body)
	}
	add, err := strconv.Atoi(offer[1])
	if err != nil {
		t.Fatalf("the pause screen offers %q seconds: %v", offer[1], err)
	}
	return declared + add
}

// scriptAdds is the seconds one press of the same button adds in the browser, read
// off the line that moves the pause's deadline.
func scriptAdds(t *testing.T) int {
	t.Helper()
	source, err := os.ReadFile("../assets/js/guided.js")
	if err != nil {
		t.Fatalf("no countdown script to read: %v", err)
	}
	moved := regexp.MustCompile(deadlineMoveRe).FindSubmatch(source)
	if moved == nil {
		t.Fatal("the countdown script moves the pause by no number this case can read, so the button and the countdown are no longer one promise")
	}
	millis, err := strconv.Atoi(string(moved[1]))
	if err != nil || millis%1000 != 0 {
		t.Fatalf("the countdown script moves the pause by %q milliseconds, which is no whole second", moved[1])
	}
	return millis / 1000
}

// catalogCopy is one key of the pause's vocabulary in the one language catalog this
// program ships: what the button says where the screen is read in that language.
func catalogCopy(t *testing.T, key string) string {
	t.Helper()
	file, err := fs.ReadFile(core.Catalogues(), "pt-PT.json")
	if err != nil {
		t.Fatalf("no language catalog to read: %v", err)
	}
	var entries map[string]struct {
		Translation string `json:"translation"`
	}
	if err := json.Unmarshal(file, &entries); err != nil {
		t.Fatalf("the language catalog is no catalog this case can read: %v", err)
	}
	entry, found := entries["screens.session."+key]
	if !found {
		t.Fatalf("the language catalog carries no %q, so the button counts seconds in one language only", key)
	}
	return entry.Translation
}

// secondsNamed is the one number a piece of the pause's copy counts, refused when
// the words count none or more than one.
func secondsNamed(t *testing.T, where, said string) int {
	t.Helper()
	named := regexp.MustCompile(`[0-9]+`).FindAllString(said, -1)
	if len(named) != 1 {
		t.Fatalf("%s says %q, which counts %d numbers of seconds", where, said, len(named))
	}
	count, err := strconv.Atoi(named[0])
	if err != nil || count < 1 {
		t.Fatalf("%s says %q, which counts no usable pause: %v", where, said, err)
	}
	return count
}

// TestTheRestOfferNamesTheSecondsItsButtonAdds requires one number behind a pause's
// "+15 s": the seconds its words promise, the seconds its own notice counts once the
// press lands, the seconds behind the link on the page, the seconds the countdown
// script adds, and the number where it is translated. A first press and a second
// press on the pause the first left both add exactly that many, so the button keeps
// its word however often it is pressed.
func TestTheRestOfferNamesTheSecondsItsButtonAdds(t *testing.T) {
	labels := pauseLabels(t)
	promised := secondsNamed(t, "the pause's own button", labels["add_15"])
	if counted := secondsNamed(t, "the notice after the press", labels["rest_added"]); counted != promised {
		t.Errorf("the pause's button promises %d seconds and its own notice says the pause grew by %d", promised, counted)
	}
	if added := scriptAdds(t); added != promised {
		t.Errorf("the pause's button promises %d seconds and the countdown script adds %d", promised, added)
	}
	if translated := catalogCopy(t, "add_15"); secondsNamed(t, "the translated button", translated) != promised {
		t.Errorf("the pause's button promises %d seconds in English and counts %q where it is translated", promised, translated)
	}
	for _, taken := range []int{0, promised} {
		if offered := pauseOffer(t, labels, restDeclared, taken); offered != restDeclared+taken+promised {
			t.Errorf("a person %d seconds into a %d-second pause: its button leads to %d seconds, want the %d it promises on top of the %d already taken",
				taken, restDeclared, offered, promised, taken)
		}
	}
}
