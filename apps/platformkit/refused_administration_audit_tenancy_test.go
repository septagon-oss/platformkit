package main

// The refusal record is written on a detached transaction, outside the caller's. What
// keeps it at the caller's tenant is that the detached connection still carries the
// request's tenant, so the row lands under acme's RLS and nowhere else. This case asks
// the trail from the neighbour's side: Globex's administrator, holding the same plan
// feature, reads the trail for the refusal acme just recorded and finds nothing.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestARefusedAdministrationWriteIsNotInTheNeighboursTrail(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, plansWrite,
		`{"code":"pro","name":"Pro","priceCents":2900,"currency":"EUR","interval":"month","active":true,"features":["audit-trail"]}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", plansWrite, code, body)
	}
	planID := field(t, body, "id")
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, subPath+"/subscribe", `{"planId":"`+planID+`"}`); code != http.StatusOK {
		t.Fatalf("subscribe = %d %s, want 200", code, body)
	}

	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tenantPath, code, body)
	}
	globexID := uuid.MustParse(field(t, body, "id"))
	provision(t, cfg, globexID, "root@globex.localhost")
	other := signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)
	if code, body = do(t, cfg, other, http.MethodPost, globexHost, subPath+"/subscribe", `{"planId":"`+planID+`"}`); code != http.StatusOK {
		t.Fatalf("globex subscribe = %d %s, want 200", code, body)
	}

	me := field(t, whoami(t, cfg, admin), "userId")
	refusedWrite(t, cfg, admin, http.MethodPost, usersPath+"/"+me+"/roles", `{"roles":["member"]}`)
	// Once acme's trail holds the record, the relay and the subscriber have run.
	waitForAudit(t, cfg, admin, usercontracts.EventAdministrationRefused)

	code, body = do(t, cfg, other, http.MethodGet, globexHost, auditPath+"?name="+usercontracts.EventAdministrationRefused, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s as globex = %d %s, want 200 to an administrator holding the feature", auditPath, code, body)
	}
	var out struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("read the trail from %s: %v", body, err)
	}
	if len(out.Items) != 0 {
		t.Errorf("globex's trail answers acme's refusal: %v", out.Items)
	}
}
