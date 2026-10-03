package main

// The wait the trail tests share had a hole, and `make check` paid for it about once
// in eight runs: `waitForAudit` returned the first row carrying an event name the
// moment any such row existed, and an event name is not a row.
// `tenant.lifecycle_recorded` is the mirror name every lifecycle verb publishes, so
// the earlier case — which creates a customer, then suspends it, then reads
// the operator's trail — was answered by its own create, twenty round-trips early,
// and read `verb: create` where it meant to see `verb: suspend`. Whether it landed
// that way was decided by where the relay's one-second tick fell between the two
// writes. Nothing in the product was wrong; the wait was asking for the wrong fact.
//
// This case holds the cure open. It puts the composition into exactly the state the
// flake fell into — the create's mirror row already in the trail, the suspension's
// not yet — by waiting for the create's row before it performs the suspension. Then
// the same wait is asked a second time, and it must answer the suspension. With a
// first-row wait this fails every time, naming `create`; with the wait that stops
// until the outbox has published what is on it, it passes every time.

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestAWaitOnTheTrailAnswersTheActThatCausedIt(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	// The trail is a plan feature of this composition, so the installation buys it
	// before it reads it.
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, plansWrite,
		`{"code":"pro","name":"Pro","priceCents":2900,"currency":"EUR","interval":"month","active":true,"features":["audit-trail"]}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", plansWrite, code, body)
	}
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, subPath+"/subscribe",
		`{"planId":"`+field(t, body, "id")+`"}`); code != http.StatusOK {
		t.Fatalf("subscribe = %d %s, want 200", code, body)
	}

	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"answered","name":"Answered Corporation","host":"answered.localhost"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tenantPath, code, body)
	}
	customer := uuid.MustParse(field(t, body, "id"))

	// The earlier act, in the trail before the later one is even attempted. This is
	// the whole of the trap: an older row under the same name, already arrived.
	first := waitForAudit(t, cfg, admin, "tenant.lifecycle_recorded")
	if verb, _ := first["payload"].(map[string]any)["verb"]; verb != "create" {
		t.Fatalf("the first mirror row in the trail carries verb %v, want the create this case just performed", verb)
	}
	if id, _ := first["payload"].(map[string]any)["tenantId"]; id != customer.String() {
		t.Fatalf("the first mirror row names tenant %v, want the customer %s", id, customer)
	}

	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost,
		tenantPath+"/"+customer.String()+"/suspend", ""); code != http.StatusOK {
		t.Fatalf("POST suspend = %d %s, want 200", code, body)
	}

	row := waitForAudit(t, cfg, admin, "tenant.lifecycle_recorded")
	payload, _ := row["payload"].(map[string]any)
	if payload["verb"] != "suspend" {
		t.Errorf("the wait answered %v rather than the suspension that caused it: %+v", payload["verb"], payload)
	}
	if payload["tenantId"] != customer.String() {
		t.Errorf("the wait answered a row naming tenant %v, want the customer %s it suspended", payload["tenantId"], customer)
	}
}
