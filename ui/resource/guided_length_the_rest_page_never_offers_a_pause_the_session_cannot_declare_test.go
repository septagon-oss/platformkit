package resource_test

// Where a declared pause stops is written down three times and decided once:
// kit/entity's validation of a step admits a length, ui/screens/guided.go clamps
// the seconds a person adds on top of a rest inside the same literal, and this
// package writes the resulting length into the page. A case that asks only the
// validator leaves the screen's copy free to drift: widened there, the button on
// a rest screen would offer a pause no session can declare, and the countdown a
// person starts would run past the longest length anyone ever validated. So this
// case takes the ceiling from the validator by asking it, never by repeating the
// number, and asks the pause screen what its own "+15 s" link would take a person
// to at that ceiling and with room left under it.

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
	"golang.org/x/net/html"
)

// longestDeclaredPause answers, out of kit/entity's own validation, the longest
// number of seconds one pause of a step may reach: asked at zero, which every
// session may declare, at a length no session could mean, and between them.
func longestDeclaredPause(t *testing.T, write func(step *entity.GuidedStep, seconds int32)) int32 {
	t.Helper()
	admits := func(seconds int32) bool {
		step := entity.GuidedStep{ID: uuid.New(), Title: "Step", Items: []entity.GuidedItem{}, Sets: []uuid.UUID{uuid.New(), uuid.New()}}
		write(&step, seconds)
		return entity.GuidedSession{ID: uuid.New(), Title: "Sequence", Steps: []entity.GuidedStep{step}}.Validate(false) == nil
	}
	low, high := int32(0), int32(1<<18)
	if !admits(low) || admits(high) {
		t.Fatal("the validator answers at both ends of the search, so this case cannot find where a declared pause stops")
	}
	for low+1 < high {
		middle := low + (high-low)/2
		if admits(middle) {
			low = middle
		} else {
			high = middle
		}
	}
	return low
}

// restScreen shows the pause a person owns after the set under them and answers
// the two lengths its page commits to: the seconds the countdown starts at, and
// the total the "+15 s" link it writes would take them to.
func restScreen(t *testing.T, betweenSets bool, declared int32) (shown, offered int) {
	t.Helper()
	step, sets := uuid.New(), []uuid.UUID{uuid.New(), uuid.New()}
	own := entity.GuidedStep{ID: step, Title: "Set the pace", Items: []entity.GuidedItem{}, Sets: sets}
	set, done := 0, 1
	if betweenSets {
		own.RestBetweenSeconds = declared
	} else {
		own.RestAfterSeconds = declared
		set, done = 1, 2
	}
	session := entity.GuidedSession{ID: uuid.New(), Title: "Sequence", Steps: []entity.GuidedStep{own}}
	at := time.Unix(1777000000, 0).UTC()
	session.Progress = &entity.GuidedProgress{Revision: int64(done + 1)}
	for i := range done {
		session.Progress.Completed = append(session.Progress.Completed, entity.GuidedCompletion{
			SessionID: session.ID, StepID: step, SetID: sets[i], Revision: int64(i + 2), CompletedAt: at})
	}
	view := resource.Guided(resource.GuidedPage{Session: session, Base: "/app/sessions/" + session.ID.String(), Exit: "/app", Mode: "rest", Step: 0, Set: set,
		Policy: resource.GuidedPolicy{Labels: map[string]string{"rest": "Rest", "up_next": "Up next", "add_15": "Add 15 seconds", "skip": "Skip rest", "finish": "Finish", "timers": "Timers"}}},
		resource.Options{})
	var out strings.Builder
	for _, node := range view.Body {
		if err := node.Render(&out); err != nil {
			t.Fatal(err)
		}
	}
	doc, err := html.Parse(strings.NewReader(out.String()))
	if err != nil {
		t.Fatal(err)
	}
	add, seconds := "", ""
	for n := range doc.Descendants() {
		for _, attr := range n.Attr {
			switch attr.Key {
			case "data-guided-seconds":
				seconds = attr.Val
			case "data-guided-add":
				for _, same := range n.Attr {
					if same.Key == "href" {
						add = same.Val
					}
				}
			}
		}
	}
	if seconds == "" || !strings.Contains(add, "extra=") {
		t.Fatalf("the pause screen writes no countdown length or no offer to add to it (seconds=%q add=%q)", seconds, add)
	}
	extra, err := strconv.Atoi(add[strings.Index(add, "extra=")+len("extra="):])
	if err != nil {
		t.Fatalf("the pause screen offers %q, which is no second count: %v", add, err)
	}
	shown, err = strconv.Atoi(seconds)
	if err != nil {
		t.Fatalf("the pause screen writes %q as its countdown length: %v", seconds, err)
	}
	return shown, int(declared) + extra
}

// TestTheRestPageNeverOffersAPauseTheSessionCannotDeclare requires the pause
// screen's own "+15 s" offer to stay inside the longest length kit/entity admits —
// at that ceiling, where nothing more may be added, and with room left under it,
// where the button has to add — for a rest between two sets and a rest after a
// step. A total past the ceiling is a countdown no session could declare and no
// case ever asked the countdown about; a total short of the rest on the page is a
// button that takes the person backwards.
func TestTheRestPageNeverOffersAPauseTheSessionCannotDeclare(t *testing.T) {
	for _, pause := range []struct {
		name        string
		betweenSets bool
		write       func(step *entity.GuidedStep, seconds int32)
	}{
		{"a rest between two sets", true, func(step *entity.GuidedStep, seconds int32) { step.RestBetweenSeconds = seconds }},
		{"a rest after a step", false, func(step *entity.GuidedStep, seconds int32) { step.RestAfterSeconds = seconds }},
	} {
		ceiling := longestDeclaredPause(t, pause.write)
		for _, declared := range []int32{ceiling, ceiling / 2} {
			shown, offered := restScreen(t, pause.betweenSets, declared)
			if shown != int(declared) {
				t.Errorf("%s of %d seconds: the pause screen counts from %d", pause.name, declared, shown)
			}
			if offered > int(ceiling) {
				t.Errorf("%s of %d seconds: the screen's own button offers %d seconds, past the %d a session may declare", pause.name, declared, offered, ceiling)
			}
			if offered < int(declared) {
				t.Errorf("%s of %d seconds: the screen's own button offers %d seconds, a shorter pause than the one the page shows", pause.name, declared, offered)
			}
			if declared+15 <= ceiling && offered < int(declared)+15 {
				t.Errorf("%s of %d seconds: the screen's button offers %d seconds although %d more remain declarable", pause.name, declared, offered, int(ceiling)-int(declared))
			}
		}
	}
}
