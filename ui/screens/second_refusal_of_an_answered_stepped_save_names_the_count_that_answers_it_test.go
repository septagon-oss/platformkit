package screens_test

// A refusal after the person's own save still hands back the count that answers it.
//
// A stepped save refused over a write count the row has passed is answered with the
// count the row counts itself at. That answer is a page's worth of state: the same
// continuation can be posted again after it was taken — a back button over a saved
// form, a submit pressed twice — and the second post is refused, because the row its
// answer moved is one write past the count it quotes. What decides whether the person
// is still in the form is that refusal: it has to name a count a save can take, and
// the save answered with it has to go through with its one event. This refuses a save,
// has its answer accepted, posts that answer again, and answers the refusal it is
// handed with the count the row counts itself at, then asks the row whose answer, what
// count and how many events it ends on.

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestTheSecondRefusalOfAnAnsweredSteppedSaveStillNamesTheCountThatAnswersIt(t *testing.T) {
	h, admin := countedNoteServer(t)
	item := savedSteppedNote(t, h)
	answers := readyNote(t, h, item+"/edit", "Mine, answered once")
	if _, err := admin.ExecContext(t.Context(),
		`UPDATE notes SET revision = revision + 1, title = 'Saved by somebody else'`); err != nil {
		t.Fatal(err)
	}
	refused := postStep(h, item, answers, "submit", nil)
	if refused.Code != http.StatusPreconditionFailed {
		t.Fatalf("saving over the row that moved while the last step was open = %d, want the refusal the person can answer: %s",
			refused.Code, refused.Body)
	}
	held := continuation(t, refused, http.StatusPreconditionFailed, 2)
	held.Revision = strconv.Itoa(savedRowRevision(t, admin))
	if saved := postStep(h, item, held, "submit", nil); saved.Code != http.StatusSeeOther {
		t.Fatalf("answering the refusal with revision %s = %d, want the save accepted: %s",
			held.Revision, saved.Code, saved.Body)
	}
	var titleOnce string
	var revisionOnce, eventsOnce int
	if err := admin.QueryRowContext(t.Context(), `SELECT title, revision FROM notes`).Scan(&titleOnce, &revisionOnce); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&eventsOnce); err != nil {
		t.Fatal(err)
	}
	second := postStep(h, item, held, "submit", nil)
	if second.Code != http.StatusPreconditionFailed {
		t.Fatalf("posting the answered refusal a second time = %d, want the refusal of a write over the row at %d: %s",
			second.Code, revisionOnce, second.Body)
	}
	hands := strconv.Itoa(savedRowRevision(t, admin))
	if notice := refusalNotice(second); !strings.Contains(notice, hands) {
		t.Fatalf("the second refusal holds %q while the row counts itself %s, so the page leaves the person no count to send back: %s",
			notice, hands, second.Body)
	}
	retry := continuation(t, second, http.StatusPreconditionFailed, 2)
	retry.Revision = hands
	retry.Answers["title"] = "Mine, answered a second time"
	if saved := postStep(h, item, retry, "submit", nil); saved.Code != http.StatusSeeOther {
		t.Fatalf("answering the second refusal with the count the row is on (%s) = %d, want the person's own save accepted: %s",
			hands, saved.Code, saved.Body)
	}
	var titleTwice string
	var revisionTwice, eventsTwice int
	if err := admin.QueryRowContext(t.Context(), `SELECT title, revision FROM notes`).Scan(&titleTwice, &revisionTwice); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&eventsTwice); err != nil {
		t.Fatal(err)
	}
	if titleTwice != "Mine, answered a second time" || revisionTwice != revisionOnce+1 || eventsTwice != eventsOnce+1 {
		t.Fatalf("the answer to the second refusal left title %q at revision %d with %d events, want %q at revision %d with %d events",
			titleTwice, revisionTwice, eventsTwice, "Mine, answered a second time", revisionOnce+1, eventsOnce+1)
	}
}
