package main

// The composition journey 0088 layer 3 names: a tenant whose plan does not open
// a door is refused at that door, and the refusal is about the door and not
// about the tenant. Two tenants are booted in one installation for that claim.
// Acme and Globex both work — each writes a task at its own host — and neither
// has bought the audit trail this product prices (modules/audit/module.go's
// Deps.Feature; apps/platformkit is the one that names it). Both are answered
// 402 with the feature named, and the refusal takes nothing away: each tenant's
// own rows stay readable at its own host, and one tenant's rows never surface
// at the other's. Then Acme buys the plan and its door opens — while Globex,
// which bought nothing, is still refused and still working.
//
// What existed before this file: apps/platformkit/app_test.go proves the 402
// for one tenant with no subscription, and cross_tenant_proposal_test.go proves
// row isolation inside a served tenant. Nothing walked the refusal and the
// working side of the same door in one installation with two tenants, which is
// the pair that says a plan gate closes a door rather than a tenant.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
)

const planRefusalTenantAdmin = "root@globex.localhost"

func TestATenantThePlanDoesNotOpenIsRefusedAtThatDoorAndKeepsTheDoorsItOwns(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	acme := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	// A second tenant, made through the control plane and given its own
	// administrator: the tenant the plan refuses has to be a real tenant, or the
	// refusal proves nothing about a tenant.
	code, body := do(t, cfg, acme, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tenantPath, code, body)
	}
	globexID := uuid.MustParse(field(t, body, "id"))
	provision(t, cfg, globexID, planRefusalTenantAdmin)
	globex := signIn(t, cfg, globexHost, planRefusalTenantAdmin, adminPass)

	// Both tenants work. Two titles, so that every assertion below can say whose
	// row it found.
	for _, at := range []struct {
		client *http.Client
		host   string
		title  string
	}{{acme, acmeHost, "acme-chiller-temp"}, {globex, globexHost, "globex-chiller-temp"}} {
		if code, body := do(t, cfg, at.client, http.MethodPost, at.host, tasksPath,
			`{"title":"`+at.title+`","priority":"high"}`); code != http.StatusCreated {
			t.Fatalf("POST %s at %s = %d %s, want 201", tasksPath, at.host, code, body)
		}
	}

	// Neither tenant has bought the trail: the door is closed at both hosts, with
	// the plan's own sentence and the feature named, not a silence.
	for _, host := range []string{acmeHost, globexHost} {
		client := acme
		if host == globexHost {
			client = globex
		}
		if code, body := do(t, cfg, client, http.MethodGet, host, auditPath, ""); code != http.StatusPaymentRequired ||
			!strings.Contains(body, httpx.CodePlanExcludes) || !strings.Contains(body, "audit-trail") {
			t.Errorf("GET %s at %s with no subscription = %d %s, want 402, %s and the feature named",
				auditPath, host, code, body, httpx.CodePlanExcludes)
		}
	}

	// The closed door took nothing away: each tenant still reads its own work,
	// and reads only its own. This is the half a 402 alone cannot say — a refusal
	// that also switched the tenant's own doors off would pass the assertions
	// above.
	for _, at := range []struct {
		client *http.Client
		host   string
		mine   string
		theirs string
	}{{acme, acmeHost, "acme-chiller-temp", "globex-chiller-temp"},
		{globex, globexHost, "globex-chiller-temp", "acme-chiller-temp"}} {
		code, body := do(t, cfg, at.client, http.MethodGet, at.host, tasksPath, "")
		if code != http.StatusOK || !strings.Contains(body, at.mine) {
			t.Fatalf("GET %s at %s = %d %s, want the tenant's own task", tasksPath, at.host, code, body)
		}
		if strings.Contains(body, at.theirs) {
			t.Errorf("GET %s at %s shows %s: one tenant's row is at the other's host: %s",
				tasksPath, at.host, at.theirs, body)
		}
	}

	// Acme buys the plan that includes the trail. Nothing about Globex changes.
	trailIncluded(t, cfg, acme)
	if code, body := do(t, cfg, acme, http.MethodGet, acmeHost, auditPath, ""); code != http.StatusOK {
		t.Fatalf("GET %s after the plan that opens it = %d %s, want 200", auditPath, code, body)
	}
	if row := waitForAudit(t, cfg, acme, taskcontracts.EventCreated); row["name"] != taskcontracts.EventCreated {
		t.Errorf("the trail acme can now read holds %v, want the created task event", row)
	}
	// Acme's own trail may speak of Globex where acme acted on it — the lifecycle
	// record of creating that tenant is acme's own act — but it holds no event
	// Globex's own work published.
	if code, body := do(t, cfg, acme, http.MethodGet, acmeHost, auditPath, ""); strings.Contains(body, "globex-chiller-temp") {
		t.Errorf("the trail acme reads after its own upgrade holds the event Globex's own task published: %d %s", code, body)
	}
	if code, body := do(t, cfg, globex, http.MethodGet, globexHost, auditPath, ""); code != http.StatusPaymentRequired ||
		!strings.Contains(body, httpx.CodePlanExcludes) {
		t.Errorf("GET %s at %s after the neighbouring tenant upgraded = %d %s, want the 402 to stand",
			auditPath, globexHost, code, body)
	}
}
