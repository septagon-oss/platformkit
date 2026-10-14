package main

// At the reference application: the invariant the delivery states for itself,
// read back out of the trail rather than out of the outbox.
//
// The brief's measure was that "a suspension's audit row lands in the suspended
// tenant's own audit_events — invisible from the operator's side", and its fix is one
// verb writing "one row under the tenant's scope and one under the operator
// installation's scope, both carrying the trace id (0053 §4)". Everything the delivery
// pinned asserts `platformkit_outbox`: two rows, two scopes, one trace. The outbox is
// the *queue*. The audit row is what the kernel's audit module writes when the relay
// hands it an envelope, and nothing at any level asserts that the operator's half of
// this arrives as a row in `audit_events`, that it carries the actor, or that the
// mirror stays out of the customer's own trail — which is the isolation claim, on the
// table the product actually reads at `GET /api/v1/audit/events`.
//
// This case asks the running composition: an operator's POST, one traceparent header of
// the case's own, then the trail as a person reads it. Its reachability control is the
// same route answering 200 and the same tenant's trail answering at all — the assertions
// below are reached through the verdict, the tenant's id and the trace id, never through
// anything a broken answer would print.

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/trace"
)

// r1Trail is one audit row, read the way the trail's own read door returns it.
type r1Trail struct {
	TenantID    uuid.UUID `gorm:"column:tenant_id"`
	Name        string    `gorm:"column:name"`
	Verb        string    `gorm:"column:verb"`
	Traceparent string    `gorm:"column:traceparent"`
	Actor       string    `gorm:"column:actor"`
}

// r1ask sends one request with an extra header on it, which is how this case carries
// its own W3C trace context through the door.
func r1ask(t *testing.T, cfg config.Config, client *http.Client, method, host, path, body, traceparent string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, "http://"+cfg.Server.Addr+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	if traceparent != "" {
		req.Header.Set("traceparent", traceparent)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s at %s: %v", method, path, host, err)
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return res.StatusCode, string(out)
}

// TestALifecycleVerbLeavesOneActInBothTrails is the brief's Done-when sentence, run.
func TestALifecycleVerbLeavesOneActInBothTrails(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	operator := installationTenantID(t, cfg, admin, "acme")

	// The trail is a plan feature of this composition, so the installation buys it
	// before it reads it (apps/platformkit/object_scope_test.go does the same).
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, plansWrite,
		`{"code":"pro","name":"Pro","priceCents":2900,"currency":"EUR","interval":"month","active":true,"features":["audit-trail"]}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", plansWrite, code, body)
	}
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, subPath+"/subscribe",
		`{"planId":"`+field(t, body, "id")+`"}`); code != http.StatusOK {
		t.Fatalf("subscribe = %d %s, want 200", code, body)
	}

	// The control, through the fixed behaviour: the control plane is served, this
	// actor is granted, and it can make a customer to act on.
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"watched","name":"Watched Corporation","host":"watched.localhost"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tenantPath, code, body)
	}
	customer := uuid.MustParse(field(t, body, "id"))

	// The four verbs this delivery mounts are guarded like the five it inherited:
	// an anonymous caller gets no lifecycle change at any of them.
	for _, probe := range []struct{ method, at, body string }{
		{http.MethodPost, "/rename", `{}`},
		{http.MethodPost, "/reactivate", ``},
		{http.MethodDelete, "/hosts/watched.localhost", ``},
		{http.MethodPost, "/delete", `{"confirm":"watched"}`},
	} {
		if code, body := do(t, cfg, nil, probe.method, acmeHost,
			tenantPath+"/"+customer.String()+probe.at, probe.body); code == http.StatusOK || code == http.StatusCreated {
			t.Errorf("an anonymous %s %s was accepted (%d %s); the lifecycle verbs are guarded like the routes beside them",
				probe.method, tenantPath+probe.at, code, body)
		}
	}
	if _, state := do(t, cfg, admin, http.MethodGet, acmeHost, tenantPath+"/"+customer.String(), ""); state == "" {
		t.Fatalf("the control plane does not read back the customer it created: %s", state)
	}

	// One verb, in one request, carrying one trace context.
	tr := trace.New()
	code, body = r1ask(t, cfg, admin, http.MethodPost, acmeHost,
		tenantPath+"/"+customer.String()+"/suspend", "", tr.Parent())
	if code != http.StatusOK {
		t.Fatalf("POST suspend = %d %s, want 200", code, body)
	}

	// The operator's side, over the read door the operator uses: the trail of this
	// installation holds a row that names the verb and the customer it was used on.
	row := waitForAudit(t, cfg, admin, "tenant.lifecycle_recorded")
	payload, _ := row["payload"].(map[string]any)
	if payload["verb"] != "suspend" {
		t.Errorf("the operator's trail row carries verb %v in its payload, want %q", payload["verb"], "suspend")
	}
	if payload["tenantId"] != customer.String() {
		t.Errorf("the operator's trail row names tenant %v, want the customer %s", payload["tenantId"], customer)
	}
	if row["traceparent"] != tr.Parent() {
		t.Errorf("the operator's trail row carries traceparent %v, want the request's %q — 0053 §4",
			row["traceparent"], tr.Parent())
	}
	if actor, _ := row["actor"].(string); actor == "" {
		t.Error("the operator's trail row names no actor; an audit row that cannot say who is not an audit row")
	}

	// Both sides, as two rows in two tenants of one database, each naming the same
	// request and the same actor. The customer's half is the row the brief says
	// already existed; the operator's half is the row this delivery owes. One act,
	// two rows, one trace — and nothing else in the request left a row behind.
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()

	var rows []r1Trail
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Table("audit_events").
			Select("tenant_id, name, traceparent, actor, payload->>'verb' AS verb").
			Where("traceparent = ?", tr.Parent()).Order("name").Find(&rows).Error
	}); err != nil {
		t.Fatalf("read the trail as the installation: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("one lifecycle verb in one request left %s in the trail, want exactly the customer's row and the "+
			"installation's mirror: %+v", names(rows), rows)
	}
	var customerRow, operatorRow *r1Trail
	for i := range rows {
		switch {
		case rows[i].TenantID == customer && rows[i].Name == "tenant.suspended":
			customerRow = &rows[i]
		case rows[i].TenantID == operator && rows[i].Name == "tenant.lifecycle_recorded" && rows[i].Verb == "suspend":
			operatorRow = &rows[i]
		}
	}
	if customerRow == nil || operatorRow == nil {
		t.Fatalf("the request's two trail rows are %s: want tenant.suspended in the customer's trail and "+
			"tenant.lifecycle_recorded{verb:suspend} in the installation's", names(rows))
	}
	for _, r := range []*r1Trail{customerRow, operatorRow} {
		if r.Traceparent != tr.Parent() {
			t.Errorf("trail row %s carries %q, want the request's %q", r.Name, r.Traceparent, tr.Parent())
		}
		if r.Actor == "" {
			t.Errorf("trail row %s names no actor: the audit record of a state change has to say who", r.Name)
		}
	}

	// And the boundary holds on the table the product reads: the customer's own
	// transaction, asked over its trail, sees its own row and not the operator's
	// mirror of it.
	var own []r1Trail
	if err := db.Run(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: customer}), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Table("audit_events").
				Select("tenant_id, name, traceparent, actor, payload->>'verb' AS verb").
				Where("name IN ?", []string{"tenant.suspended", "tenant.lifecycle_recorded"}).
				Find(&own).Error
		}); err != nil {
		t.Fatalf("read the trail as the customer: %v", err)
	}
	for _, r := range own {
		if r.TenantID != customer {
			t.Errorf("the customer's transaction reads a trail row scoped to %s", r.TenantID)
		}
		if r.Name == "tenant.lifecycle_recorded" {
			t.Error("the customer's own trail holds the installation's mirror row")
		}
	}
	if len(own) != 1 {
		t.Errorf("the customer's trail holds %v, want its one suspension row and nothing else", names(own))
	}
}

func names(rows []r1Trail) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}
