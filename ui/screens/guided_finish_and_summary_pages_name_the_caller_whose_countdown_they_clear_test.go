package screens_test

// Every screen of a guided walk hands the countdown script one address to keep a person's
// timers at: the `scope` field of the config the page writes, which the script concatenates
// with its own prefix and its session to reach every stored countdown. The script does not
// only read there — on loading any guided page it discards every stored entry that is not
// under the address that page named, and on a finished page it clears that address outright.
// So a guided screen that named the walk without the caller would not merely read the wrong
// entries: its one page load would delete the countdowns every other person of that browser
// had left running, and no markup, number or word would move to show it.
// The step page's address is pinned beside one companion case and the rest page's beside
// another. The two ends of the walk — the page a person finishes from, and the summary a
// finish returns — were read by nothing. This case asks both to the same caller, asks each to
// name that caller, and asks the two answers to be one address.

import (
	"testing"

	"github.com/google/uuid"
)

// TestTheGuidedFinishAndSummaryPagesNameTheCallerWhoseCountdownTheyClear asks one caller for
// the finish page of a walk still under them and for the summary page of the same walk ended,
// reads the countdown scope each document hands the script, and asks that both name that
// caller and resource and that the two answers be one string. A screen that named the walk
// without the caller hands the script an address no person owns, and its load discards the
// timers that address does not cover — the other people of this browser, and the next page of
// this very walk.
func TestTheGuidedFinishAndSummaryPagesNameTheCallerWhoseCountdownTheyClear(t *testing.T) {
	caller := uuid.MustParse("55555555-5555-4555-8555-555555555555")
	res := resource()
	want := formTenant + "/" + caller.String() + "/" + res.Module + "/" + res.Entity

	underWay := onePairRecorded(false)
	at := "/app/note/notes/" + underWay.ID.String()
	finish, finishBody := guidedScopeAnswered(t, scopeServer(t, underWay), at+"/finish", caller)
	if finish != want {
		t.Errorf("the finish page answered to %s hands its countdown store %q, want %q — the address is what "+
			"the script clears and keeps a person's timers by, so it has to name the caller it answered to: %s",
			caller, finish, want, firstWords(finishBody))
	}

	ended := onePairRecorded(true)
	at = "/app/note/notes/" + ended.ID.String()
	summary, summaryBody := guidedScopeAnswered(t, scopeServer(t, ended), at+"/summary", caller)
	if summary != want {
		t.Errorf("the summary page answered to %s hands its countdown store %q, want %q — this is the page that "+
			"empties the store, so an address of its own is one other person's countdowns cleared: %s",
			caller, summary, want, firstWords(summaryBody))
	}
	if finish != summary {
		t.Errorf("one person's walk ends at %q while it finishes at %q, so the store the summary page empties is "+
			"not the one the walk stored under: %s", finish, summary, firstWords(summaryBody))
	}
}
