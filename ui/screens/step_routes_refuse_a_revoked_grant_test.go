package screens_test

// A stepped form is five routes, not one: the page that opens it, the step that
// carries the answers forward, the submit that writes, and the restart that hands
// them back. Each one reads the record or writes it, so each one owes the same
// answer the moment the actor's grant goes away mid-form — including the step page,
// which prefills itself from the stored row. Nothing is written while the form is
// being answered, so a refusal that only landed on the submit would still have
// shown a person the record's contents through a grant they no longer hold.

import (
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestEveryStepRouteRefusesAGrantThatWentAway(t *testing.T) {
	var fail atomic.Bool
	h, admin, access := sqlStepServer(t, &fail)
	at := "/app/note/notes"
	first := continuation(t, stepRequest(h, at+"/new", nil), http.StatusOK, 0)
	access.denied.Store(true)
	if out := stepRequest(h, at+"/new", nil); out.Code != http.StatusForbidden {
		t.Errorf("the create page after the grant went away = %d, want 403", out.Code)
	}
	if out := postStep(h, at+"/new/steps", first, "next", url.Values{"title": {"Later"}}); out.Code != http.StatusForbidden {
		t.Errorf("the first step after the grant went away = %d, want 403: %s", out.Code, out.Body)
	}
	access.denied.Store(false)
	item := savedSteppedNote(t, h)
	answers := readyNote(t, h, item+"/edit", "Edited")
	access.denied.Store(true)
	stored := stepRequest(h, item+"/edit", nil)
	if stored.Code != http.StatusForbidden {
		t.Errorf("the edit page after the grant went away = %d, want 403", stored.Code)
	}
	if strings.Contains(stored.Body.String(), `value="Edited"`) {
		t.Errorf("the refusal repeats the stored answer: %s", stored.Body)
	}
	if out := postStep(h, item+"/edit/steps", answers, "next", url.Values{"title": {"Edited"}}); out.Code != http.StatusForbidden {
		t.Errorf("an update step after the grant went away = %d, want 403", out.Code)
	}
	if out := stepRequest(h, item+"/edit/steps", nil); out.Code != http.StatusForbidden {
		t.Errorf("the restart after the grant went away = %d, want 403", out.Code)
	}
	access.denied.Store(false)
	var revision int64
	if err := admin.QueryRowContext(t.Context(), `SELECT revision FROM notes`).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if revision != 1 {
		t.Fatalf("a refused step wrote the row to revision %d, want the one write it came from", revision)
	}
}
