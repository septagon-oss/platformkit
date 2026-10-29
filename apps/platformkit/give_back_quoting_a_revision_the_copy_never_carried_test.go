package main

// A kept copy is written once and given back once, so it carries one revision for its
// whole life. A give-back quoting any other number — one a page showed before it was
// rewritten, one nobody ever showed, or the field left out of the body — is a write
// over a copy the caller was never shown, so each is refused with a reason, writes
// nothing and emits nothing. A form that arrives without the field is the case that
// matters most: it is what a page that stopped quoting the number would send, and a
// command that read a missing revision as "any" would delete whatever the address
// named. The number is checked inside the service's own transaction; this asks it at
// the address a browser posts to, where the answer is a status, a redirect and a
// rendered list, and ends on what every refusal has to leave true: the copy is still
// there, and still gives itself back at the number the list shows.

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// TestAGiveBackQuotingARevisionTheCopyNeverCarriedKeepsTheCopy posts one keeper's
// drawn give-back three ways: quoting the next revision the copy will never reach,
// quoting none at all, and quoting the revision the list shows. The first two are
// refused and leave the copy; the third is the keeper's own answer and takes it.
func TestAGiveBackQuotingARevisionTheCopyNeverCarriedKeepsTheCopy(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	startSaved(t, cfg, c)
	keeper := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	title := "Reletter the returns shelf"
	id := newTask(t, cfg, keeper, title, "normal")
	record := servedPage(t, cfg, keeper, acmeHost, "/app/task/tasks/"+id)
	if code, _, fault := askForm(t, cfg, keeper, acmeHost, drawnAddress(t, record, "/saved/save"),
		saveForm(t, record, id)); code != http.StatusSeeOther {
		t.Fatalf("keep a copy: %d (%s)", code, fault)
	}
	// The address, the number beside it and the pair behind both come from that page:
	// a give-back is asked where the person was sent, not where this file guessed.
	kept := drawnAddress(t, record, "/saved?module=")
	list := servedPage(t, cfg, keeper, acmeHost, kept)
	drawn := quotedRevision(t, list)
	giveBack := drawnAddress(t, list, "/remove")
	reached, err := strconv.ParseInt(drawn, 10, 64)
	if err != nil {
		t.Fatalf("the revision the kept list quotes is %q, which is no number: %v", drawn, err)
	}

	for _, body := range []string{
		"expectedRevision=" + strconv.FormatInt(reached+1, 10) + "&module=task&kind=task",
		"module=task&kind=task",
	} {
		code, location, fault := askForm(t, cfg, keeper, acmeHost, giveBack, body)
		if code < http.StatusBadRequest || location != "" {
			t.Errorf("POST %s of the drawn give-back = %d (Location %q, %s); want a refusal that claims nothing",
				body, code, location, fault)
		}
	}

	after := servedPage(t, cfg, keeper, acmeHost, kept)
	if !strings.Contains(after, title) {
		t.Fatalf("the copy is gone from its keeper's list after two posts that quoted a revision it never carried:\n%s", after)
	}
	if code, location, fault := askForm(t, cfg, keeper, acmeHost, giveBack,
		"expectedRevision="+quotedRevision(t, after)+"&module=task&kind=task"); code != http.StatusSeeOther {
		t.Errorf("the keeper's own give-back after those refusals = %d (Location %q, %s); want the 303 to the grid",
			code, location, fault)
	}
	if again := servedPage(t, cfg, keeper, acmeHost, kept); strings.Contains(again, title) {
		t.Errorf("the copy outlived a give-back at the revision its list quoted (drawn %s):\n%s", drawn, again)
	}
}
