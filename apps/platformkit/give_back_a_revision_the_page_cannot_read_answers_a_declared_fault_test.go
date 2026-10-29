package main

// The kept list's give-back reads its one number with a plain conversion, so a body
// carrying text that is no number arrives as the same request as a body carrying no
// number at all: no revision quoted, which the service answers as a write over a copy
// the caller was never shown. Two things follow, and neither is asked anywhere else.
//
// The answer is a fault of the request — one of the two client faults the route
// declares — and never a server fault: a page that turned its own unreadable form
// field into a 5xx is a page that cannot read the form it drew, and a refusal is a
// reason given to a person.
//
// And a refusal writes nothing, which a person can see: the revision printed beside
// their copy is the row's own number, and it is the same number after four refusals
// as before them. The copy then gives itself back at that number, so what the refusals
// left behind is a row still whole, not one half-consumed.

import (
	"net/http"
	"strings"
	"testing"
)

func TestAGiveBackQuotingTextThatIsNoNumberAnswersAFaultTheRouteDeclares(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	startSaved(t, cfg, c)
	keeper := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	kept, giveBack, title, revision := oneKeptCopy(t, cfg, keeper, "Reletter the returns shelf")

	for _, text := range []string{"not-a-number", "1.5", "", "9223372036854775808"} {
		code, location, fault := askForm(t, cfg, keeper, acmeHost, giveBack,
			"expectedRevision="+text+"&module=task&kind=task")
		if code != http.StatusConflict && code != http.StatusUnprocessableEntity {
			t.Errorf("POST of the drawn give-back with expectedRevision=%q = %d (Location %q, %s); want the 409 or 422 the route declares, because %q is a body the page cannot read and not a fault of the server",
				text, code, location, fault, text)
		}
		if location != "" {
			t.Errorf("POST of the drawn give-back with expectedRevision=%q redirected to %q; a refusal claims it moved the person nowhere", text, location)
		}
		still := servedPage(t, cfg, keeper, acmeHost, kept)
		if !strings.Contains(still, title) {
			t.Fatalf("the copy is gone from its keeper's list after a give-back quoting %q:\n%s", text, still)
		}
		if moved := quotedRevision(t, still); moved != revision {
			t.Errorf("the revision beside the copy moved from %s to %s across the refusal of expectedRevision=%q; a refused write leaves the row it refused at the number it had",
				revision, moved, text)
		}
	}

	if code, location, fault := askForm(t, cfg, keeper, acmeHost, giveBack,
		"expectedRevision="+revision+"&module=task&kind=task"); code != http.StatusSeeOther {
		t.Errorf("the keeper's own give-back after four refusals = %d (Location %q, %s); want the 303 to the grid",
			code, location, fault)
	}
}
