package main

// The two things this delivery promises a person, asked of the running reference
// application rather than of the module's own package:
//
//   - the proposal doors answer, and one account cannot both put a change forward
//     and decide it (the invariant, over HTTP);
//   - `change.control.site-settings` decides which door a settings write comes
//     through, off leaving the form alone and on refusing it with the proposal
//     address in the answer (the switch, and the refusal that names the way).
//
// The trail is read back at the end because that is the promise the pillar contract
// makes about a state change — every transition this module introduces is audited —
// and because the request id on the row is written by kit/httpx's middleware and
// read by nothing else in the tree: a case that builds its own context proves the
// columns can hold a value, and only a request proves the middleware puts one
// there.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	changecontracts "github.com/septagon-oss/platformkit/modules/change/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
)

const (
	rolesPath   = "/api/v1/auth/roles"
	invitesPath = "/api/v1/user/invitations"
)

const (
	proposalsPath = "/api/v1/change/proposals"
	settingsPath  = "/api/v1/site/settings"
)

// changeFixture is the reference composition with the one thing the composition
// file cannot decide for itself set by hand: whether the switch is on.
func changeFixture(t *testing.T, gateOn bool) (config.Config, composition, app.Options, string) {
	t.Helper()
	path, cfg := configure(t)
	cfg.Flags = &config.Flags{Values: map[string]bool{siteSettingsFlag: gateOn}}
	c := compose(cfg)
	opts := appOptions(cfg, c, app.All)
	opts.Transport, opts.Log = memory.New(), quiet()
	install(t, path)
	start(t, cfg, c.modules, opts)
	return cfg, c, opts, path
}

func TestAProposedChangeIsDecidedBySomebodyElseAndTheTrailSaysWhichCall(t *testing.T) {
	cfg, _, _, _ := changeFixture(t, false)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	trailIncluded(t, cfg, admin)

	// The proposal: one field of one tenant's settings, against the revision the
	// settings row is on. The body carries no proposer — there is no field for it.
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
		`{"subjectModule":"site","subjectEntity":"settings",`+
			`"subjectId":"00000000-0000-0000-0000-000000000000",`+
			`"diff":{"title":"Acme, proposed"},"summary":"rename the site"}`)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want the proposal recorded", proposalsPath, code, body)
	}
	id := field(t, body, "id")
	if digest := field(t, body, "diffDigest"); !strings.HasPrefix(digest, "sha256:") {
		t.Errorf("the proposal carries %q as its digest; a verdict is about that string", digest)
	}

	// One account cannot decide what it put forward. This is the same refusal
	// modules/change/internal answers with, reached through the mounted door with
	// the credentials of the person who asked.
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath+"/"+id+"/review",
		`{"verdict":"approved","expectedRevision":1}`)
	if code != http.StatusConflict || !strings.Contains(body, "cannot be the one who decides") {
		t.Fatalf("review by the proposer = %d %s, want 409 and the sentence that names the cure", code, body)
	}

	// The refusal wrote nothing to the subject: the tenant's site still says what
	// it said, not what the proposal would have said.
	if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, settingsPath, ""); code != http.StatusOK ||
		strings.Contains(body, "Acme, proposed") {
		t.Fatalf("GET %s = %d %s, want the settings untouched by a refused review", settingsPath, code, body)
	}

	// And the transition that did happen is in the trail, with the id the caller of
	// that request was answered with. Nothing in this case wrote to the trail: the
	// propose published change.proposal_proposed, the relay carried it, and the
	// columns behind the last two assertions are filled by kit/httpx's middleware on
	// the way through — which no other test in the tree touches.
	code, body = do(t, cfg, admin, http.MethodGet, acmeHost, auditPath+"?name=change.proposal_proposed", "")
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d %s, want the proposal in the trail", auditPath, code, body)
	}
	row := waitForAudit(t, cfg, admin, changecontracts.EventProposed)
	if row["requestId"] == nil || row["requestId"] == "" {
		t.Errorf("the trail row for the propose carries no request id, and kit/httpx/request_id.go is the only writer of that key in this tree: %v", row)
	}
	if row["actor"] == nil || row["actor"] == "" {
		t.Errorf("the trail row for the propose names no actor: %v", row)
	}
}

func TestTheSiteSettingsSwitchDecidesWhichDoorAWriteComesThrough(t *testing.T) {
	t.Run("off, the form writes", func(t *testing.T) {
		cfg, _, _, _ := changeFixture(t, false)
		admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
		code, body := do(t, cfg, admin, http.MethodPut, acmeHost, settingsPath,
			`{"title":"Acme","theme":"system"}`)
		if code != http.StatusOK {
			t.Fatalf("PUT %s = %d %s, want the settings written", settingsPath, code, body)
		}
		if !strings.Contains(body, `"revision":1`) {
			t.Errorf("the first save of a tenant's settings did not report revision 1: %s", body)
		}
	})

	// On, the same credentials at the same address are answered with the door to go
	// through — and the settings row is untouched, which is the half that makes this
	// a refusal rather than a redirect somebody could ignore.
	t.Run("on, the form is refused with the proposal address", func(t *testing.T) {
		cfg, _, _, _ := changeFixture(t, true)
		admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
		code, body := do(t, cfg, admin, http.MethodPut, acmeHost, settingsPath,
			`{"title":"Acme by hand","theme":"system"}`)
		if code != http.StatusConflict {
			t.Fatalf("PUT %s with the switch on = %d %s, want 409", settingsPath, code, body)
		}
		for _, want := range []string{proposalsPath, "change:propose"} {
			if !strings.Contains(body, want) {
				t.Errorf("the refusal does not name %q, so it is a wall and not a door: %s", want, body)
			}
		}
		if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, settingsPath, ""); code != http.StatusOK ||
			strings.Contains(body, "Acme by hand") {
			t.Fatalf("GET %s = %d %s, want the settings the refusal refused to write", settingsPath, code, body)
		}
		// The object is still reachable with the switch on: what the flag moves is
		// which door a settings write comes through, not whether a proposal exists.
		if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, proposalsPath, ""); code != http.StatusOK ||
			!strings.Contains(body, `"total":0`) {
			t.Errorf("GET %s with the switch on = %d %s, want the empty queue", proposalsPath, code, body)
		}
	})
}

// TestTheSecondAccountIsTheOneWhoWritesTheChange is the invariant end to end, with
// two people in it.
//
// The module's own suite proves the state machine over one Postgres with two actor
// ids in a context, and the case above proves the doors answer and refuse the one
// account they must refuse. What neither could prove is the configuration the rule
// exists for and that only a running installation has: one tenant in which a person
// holds both change:propose and change:decide — which is the ordinary grant of a
// small team, not an edge case — who therefore cannot complete their own change, and
// a second person who can. If the four-eyes rule were only a check inside the
// service, this case would be redundant; the point of it being in the service is that
// these two HTTP calls, with these two sessions and no shared secret between them,
// settle it.
//
// The switch is on, so the settings door is shut to both of them: the only way this
// title reaches the tenant's site is the proposal.
func TestTheSecondAccountIsTheOneWhoWritesTheChange(t *testing.T) {
	cfg, c, _, _ := changeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	trailIncluded(t, cfg, admin)

	// A role with the whole of change control, so the second person is not decided
	// by an absence. It is written through the installation's own role door rather
	// than seeded, because the three grants are this module's and no seed names them.
	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, rolesPath+"/decider",
		`{"permissions":["change:read","change:propose","change:decide"]}`); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("PUT %s/decider = %d %s, want the role", rolesPath, code, body)
	}

	// Grace is invited with that role, given the passphrase the link carries, and
	// signs in: a second account in the same tenant, holding the same grants as the
	// first, which is exactly the pair the rule is about.
	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, invitesPath,
		`{"email":"grace@acme.localhost","displayName":"Grace","roles":["decider"]}`); code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", invitesPath, code, body)
	}
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the composition wired %T as its mailer, want the mailbox", c.mail)
	}
	var link string
	eventually(t, "grace's invitation to be mailed", func() bool {
		for _, sent := range box.Sent() {
			if sent.To == "grace@acme.localhost" {
				link = sent.Body
				return true
			}
		}
		return false
	})
	redeemMailedLink(t, cfg, nil, acmeHost, "/api/v1/auth/password/reset",
		`{"token":"`+tokenIn(t, link)+`","new":"a chosen passphrase for grace"}`)
	grace := signIn(t, cfg, acmeHost, "grace@acme.localhost", "a chosen passphrase for grace")

	// The proposer: the administrator, who holds both grants and is still refused
	// the decision, because the rule reads the row and not the grant list.
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
		`{"subjectModule":"site","subjectEntity":"settings",`+
			`"subjectId":"00000000-0000-0000-0000-000000000000",`+
			`"diff":{"title":"Acme, approved by Grace"},"summary":"rename the site"}`)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want the proposal recorded", proposalsPath, code, body)
	}
	id := field(t, body, "id")
	if state := field(t, body, "state"); state != "proposed" {
		t.Errorf("a new proposal is %q, want proposed", state)
	}
	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath+"/"+id+"/review",
		`{"verdict":"approved","expectedRevision":1}`); code != http.StatusConflict {
		t.Fatalf("the proposer reviews their own change = %d %s, want 409", code, body)
	}

	// The decider: Grace, who did not put it forward. She may also not take it back
	// — that belongs to the proposer alone — and the two refusals together are why
	// neither of them holds the whole lifecycle.
	if code, body = do(t, cfg, grace, http.MethodPost, acmeHost, proposalsPath+"/"+id+"/withdraw",
		`{"expectedRevision":1}`); code != http.StatusConflict {
		t.Fatalf("somebody else withdraws the proposal = %d %s, want 409", code, body)
	}
	if code, body = do(t, cfg, grace, http.MethodPost, acmeHost, proposalsPath+"/"+id+"/review",
		`{"verdict":"approved","expectedRevision":1}`); code != http.StatusOK {
		t.Fatalf("the second account reviews = %d %s, want 200", code, body)
	}
	if state := field(t, body, "state"); state != "approved" {
		t.Errorf("the review left the proposal %q, want approved", state)
	}
	if reviewer := field(t, body, "reviewer"); reviewer == field(t, mustGet(t, cfg, admin, proposalsPath+"/"+id), "proposer") {
		t.Errorf("the row credits the decision to the proposer: %s", body)
	}

	// And the write happens by her hand, once. Asking again returns the same row and
	// does not move the subject a second time, which is the idempotency half of the
	// same promise.
	if code, body = do(t, cfg, grace, http.MethodPost, acmeHost, proposalsPath+"/"+id+"/apply",
		`{"expectedRevision":2}`); code != http.StatusOK {
		t.Fatalf("the second account applies = %d %s, want 200", code, body)
	}
	if state := field(t, body, "state"); state != "applied" {
		t.Errorf("the apply left the proposal %q, want applied", state)
	}
	// Asking again, at the revision the first apply produced, is the same row and no
	// second write: appliedRevision says the number the first one wrote. (A retry
	// that still carried 2 is a different thing — a decision about a revision the
	// row is no longer on, which every command refuses before it looks at state.)
	first := fieldNumber(t, body, "appliedRevision")
	code, body = do(t, cfg, grace, http.MethodPost, acmeHost, proposalsPath+"/"+id+"/apply",
		`{"expectedRevision":3}`)
	if code != http.StatusOK || !strings.Contains(body, `"state":"applied"`) {
		t.Fatalf("the retry of an applied proposal = %d %s, want the same row", code, body)
	}
	if again := fieldNumber(t, body, "appliedRevision"); again != first {
		t.Errorf("the retry wrote the subject again: applied revision %v became %v", first, again)
	}

	// The subject moved, and it moved to what the reviewed bytes said. This is the
	// whole object justified in one read: a person asked for a title, a different
	// person said yes, and the tenant's site now says that and nothing else.
	if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, settingsPath, ""); code != http.StatusOK ||
		!strings.Contains(body, `"title":"Acme, approved by Grace"`) {
		t.Fatalf("GET %s = %d %s, want the applied title", settingsPath, code, body)
	}

	// Both transitions are in the trail, each with the request that caused it.
	applied := waitForAudit(t, cfg, admin, changecontracts.EventApplied)
	if applied["requestId"] == nil || applied["requestId"] == "" {
		t.Errorf("the trail row for the apply carries no request id: %v", applied)
	}
	if applied["actor"] == "" || applied["actor"] == field(t, mustGet(t, cfg, admin, proposalsPath+"/"+id), "proposer") {
		t.Errorf("the trail credits the apply to %v, want the decider", applied["actor"])
	}
}

// mustGet reads one resource as this client and answers with its body. It exists for
// the assertions below that compare two fields of the same row against each other.
func mustGet(t *testing.T, cfg config.Config, client *http.Client, path string) string {
	t.Helper()
	code, body := do(t, cfg, client, http.MethodGet, acmeHost, path, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d %s, want 200", path, code, body)
	}
	return body
}

// trailIncluded puts the tenant on the plan this product sells the audit trail on,
// because apps/platformkit prices it (modules.go names the feature) and a read
// without it is a 402 about a subscription rather than a read of the trail.
func trailIncluded(t *testing.T, cfg config.Config, admin *http.Client) {
	t.Helper()
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, plansWrite,
		`{"code":"pro","name":"Pro","priceCents":2900,"currency":"EUR","interval":"month","active":true,"features":["audit-trail"]}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", plansWrite, code, body)
	}
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, subPath+"/subscribe",
		`{"planId":"`+field(t, body, "id")+`"}`); code != http.StatusOK {
		t.Fatalf("subscribe = %d %s, want 200", code, body)
	}
}

// fieldNumber reads one numeric field out of a JSON body.
func fieldNumber(t *testing.T, body, name string) float64 {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("read %s from %s: %v", name, body, err)
	}
	n, ok := out[name].(float64)
	if !ok {
		t.Fatalf("no %s in %s", name, body)
	}
	return n
}
