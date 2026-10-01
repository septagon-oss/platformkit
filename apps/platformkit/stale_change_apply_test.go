package main

// A proposal is a decision about one exact revision of somebody else's row, so the
// number that carries that promise has to be the row's own count and not a guess
// about it. modules/site gained the column for exactly that: site_settings.revision
// counts its writes from 1, and a tenant with no row yet sits at 0 with the
// defaults. That gap between "no row" (0) and "the row a migration created"
// (DEFAULT 1) is where an off-by-one between the revision a diff was made against
// and the revision the apply reads back would live, and every case that crosses it
// today is the fake subject in modules/change/internal, whose revision is a field on
// a struct somebody increments by hand.
//
// So this case runs one call shape twice against the composed subject. First with
// nobody touching the settings between the review and the apply, which has to write;
// then with a write made outside the proposal in between — allowed here, because the
// switch that would refuse that door is off — which has to refuse, leave the
// proposal approved rather than applied, and leave the title the hand wrote. The
// second half is only evidence because the first one passes at the same expected
// revisions: a 409 earned by anything other than the moved row would appear in both
// halves. The third half asks for the same change again against the row as it is now
// and expects it to land, which is what separates a refusal from a broken row.

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/notification"
)

const settingsHandWriteEmail = "rechecker@acme.localhost"

// changeDeciderRole is the grant set that makes a second person a decider and
// nothing else: read, propose, decide, the three keys modules/change declares.
const changeDeciderRole = `{"permissions":["change:read","change:propose","change:decide"]}`

func TestAWriteMadeOutsideTheProposalRefusesTheApply(t *testing.T) {
	cfg, c, _, _ := changeFixture(t, false)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, rolesPath+"/decider",
		changeDeciderRole); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("PUT %s/decider = %d %s, want the role", rolesPath, code, body)
	}
	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, invitesPath,
		`{"email":"`+settingsHandWriteEmail+`","displayName":"Rechecker","roles":["decider"]}`); code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", invitesPath, code, body)
	}
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the composition wired %T as its mailer, want the mailbox", c.mail)
	}
	var link string
	eventually(t, "the decider's invitation to be mailed", func() bool {
		for _, sent := range box.Sent() {
			if sent.To == settingsHandWriteEmail {
				link = sent.Body
				return true
			}
		}
		return false
	})
	if code, body := do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/auth/password/reset",
		`{"token":"`+tokenIn(t, link)+`","new":"a chosen passphrase for the decider"}`); code != http.StatusOK {
		t.Fatalf("the reset = %d %s, want 200", code, body)
	}
	decider := signIn(t, cfg, acmeHost, settingsHandWriteEmail, "a chosen passphrase for the decider")

	// propose is the door a change enters by, with the nil subject id the
	// one-row-per-tenant entity answers with.
	propose := func(title string) string {
		t.Helper()
		code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
			`{"subjectModule":"site","subjectEntity":"settings",`+
				`"subjectId":"00000000-0000-0000-0000-000000000000",`+
				`"diff":{"title":"`+title+`"},"summary":"one line the reviewer reads"}`)
		if code != http.StatusOK && code != http.StatusCreated {
			t.Fatalf("POST %s for %q = %d %s, want the proposal recorded", proposalsPath, title, code, body)
		}
		return field(t, body, "id")
	}
	review := func(id string, revision int) (int, string) {
		t.Helper()
		return do(t, cfg, decider, http.MethodPost, acmeHost, proposalsPath+"/"+id+"/review",
			`{"verdict":"approved","expectedRevision":`+strconv.Itoa(revision)+`}`)
	}
	apply := func(id string, revision int) (int, string) {
		t.Helper()
		return do(t, cfg, decider, http.MethodPost, acmeHost, proposalsPath+"/"+id+"/apply",
			`{"expectedRevision":`+strconv.Itoa(revision)+`}`)
	}
	state := func(id string) string {
		t.Helper()
		code, body := do(t, cfg, admin, http.MethodGet, acmeHost, proposalsPath+"/"+id, "")
		if code != http.StatusOK {
			t.Fatalf("GET %s/%s = %d %s", proposalsPath, id, code, body)
		}
		return field(t, body, "state")
	}
	title := func() string {
		t.Helper()
		code, body := do(t, cfg, admin, http.MethodGet, acmeHost, settingsPath, "")
		if code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", settingsPath, code, body)
		}
		return body
	}

	// First half: the reviewed write lands. Same grants, same doors, same expected
	// revisions as the half below, and no hand write between the review and the
	// apply. If this half does not pass, the refusal below proves nothing.
	first := propose("Acme, reviewed and applied")
	if code, body := review(first, 1); code != http.StatusOK {
		t.Fatalf("the second account reviews = %d %s, want 200", code, body)
	}
	code, body := apply(first, 2)
	if code != http.StatusOK || !strings.Contains(body, `"state":"applied"`) {
		t.Fatalf("the second account applies = %d %s, want the change written", code, body)
	}
	if got := title(); !strings.Contains(got, `"title":"Acme, reviewed and applied"`) {
		t.Fatalf("GET %s = %s, want the applied title on the tenant's site", settingsPath, got)
	}

	// Second half: between the review and the apply somebody writes the same row
	// through the open door. The switch is off, so nothing refuses them — and the
	// proposal that was decided against the revision before that write is now a
	// decision about a row that no longer exists.
	second := propose("Acme, applied over a hand write")
	if code, body := review(second, 1); code != http.StatusOK {
		t.Fatalf("the second account reviews = %d %s, want 200", code, body)
	}
	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, settingsPath,
		`{"title":"Acme, written by hand","theme":"system"}`); code != http.StatusOK {
		t.Fatalf("PUT %s = %d %s, want the hand write to land while the switch is off", settingsPath, code, body)
	}
	code, body = apply(second, 2)
	if code == http.StatusOK || strings.Contains(body, `"state":"applied"`) {
		t.Fatalf("applying over a row that moved = %d %s, want a refusal that writes nothing", code, body)
	}
	if got := state(second); got != "approved" {
		t.Errorf("the apply that moved a row nobody reviewed was made about left the proposal %q, want approved", got)
	}
	if got := title(); !strings.Contains(got, `"title":"Acme, written by hand"`) ||
		strings.Contains(got, "Acme, applied over a hand write") {
		t.Errorf("the refused apply wrote the subject anyway, GET %s = %s", settingsPath, got)
	}

	// Third half: the refusal cost nothing but the stale proposal. The same change,
	// put forward again against the row as it stands now, still goes through the
	// same two accounts and lands.
	third := propose("Acme, put forward again")
	if code, body := review(third, 1); code != http.StatusOK {
		t.Fatalf("the second account reviews the fresh proposal = %d %s, want 200", code, body)
	}
	if code, body = apply(third, 2); code != http.StatusOK || !strings.Contains(body, `"state":"applied"`) {
		t.Fatalf("the same change proposed against the current revision = %d %s, want 200 and applied", code, body)
	}
	if got := title(); !strings.Contains(got, `"title":"Acme, put forward again"`) {
		t.Errorf("GET %s = %s, want the second proposal's title", settingsPath, got)
	}
}
