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
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/module"
	changecontracts "github.com/septagon-oss/platformkit/modules/change/contracts"
)

const (
	proposalsPath = "/api/v1/change/proposals"
	settingsPath  = "/api/v1/site/settings"
)

// changeFixture is the reference composition with the one thing the composition
// file cannot decide for itself set by hand: whether the switch is on.
func changeFixture(t *testing.T, gateOn bool) (config.Config, []module.Module, app.Options, string) {
	t.Helper()
	path, cfg := configure(t)
	cfg.Flags = map[string]bool{siteSettingsFlag: gateOn}
	c := compose(cfg)
	opts := appOptions(cfg, c, app.All)
	opts.Transport, opts.Log = memory.New(), quiet()
	install(t, path)
	start(t, cfg, c.modules, opts)
	return cfg, c.modules, opts, path
}

func TestAProposedChangeIsDecidedBySomebodyElseAndTheTrailSaysWhichCall(t *testing.T) {
	cfg, _, _, _ := changeFixture(t, false)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	// The trail is this product's plan feature, so a tenant that has bought
	// nothing is answered 402 and the read at the end of this case would be a
	// read of a door that is shut. Subscribe first, the way app_test.go does.
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, plansWrite,
		`{"code":"pro","name":"Pro","priceCents":2900,"currency":"EUR","interval":"month","active":true,"features":["audit-trail"]}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", plansWrite, code, body)
	}
	planID := field(t, body, "id")
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, subPath+"/subscribe",
		`{"planId":"`+planID+`"}`); code != http.StatusOK {
		t.Fatalf("subscribe = %d %s, want 200", code, body)
	}

	// The proposal: one field of one tenant's settings, against the revision the
	// settings row is on. The body carries no proposer — there is no field for it.
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
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
