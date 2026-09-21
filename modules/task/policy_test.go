package task_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task"
)

type taskPolicy struct {
	owner uuid.UUID
	err   error
	seen  []tenancy.PolicyRequest
}

func (p *taskPolicy) Decide(_ context.Context, request tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
	p.seen = append(p.seen, request)
	return tenancy.PolicyDecision{Allowed: request.Actor.ID == p.owner.String(), Revision: "reviewed-v1"}, p.err
}

func TestTaskPolicyGuardsTheLockedLifecycleAndPreservesFailureOutcomes(t *testing.T) {
	admin, conn := dbtest.Schema(t, task.Migrations)
	owner, other := uuid.New(), uuid.New()
	actor := other
	policy := &taskPolicy{owner: owner}
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: actor}, true, nil
		}, Log: slog.New(slog.DiscardHandler),
	})
	task.Module(task.Deps{Policy: policy}).Routes(surfacesOf(api))
	code, body := call(t, router, http.MethodPost, path, `{"title":"Scoped work"}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	id := id(t, body)
	at := path + "/" + id
	assign := `{"assigneeId":"` + owner.String() + `"}`
	if code, body := call(t, router, http.MethodPost, at+"/assign", assign); code != http.StatusForbidden {
		t.Fatalf("unassigned actor: %d %s", code, body)
	}
	var changes int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name = 'task.assigned'").Scan(&changes); err != nil || changes != 0 {
		t.Fatalf("denied assignment published work: %d %v", changes, err)
	}
	if len(policy.seen) != 1 || policy.seen[0].Resource.ID != id || policy.seen[0].Tenant.ID != acme.ID ||
		policy.seen[0].Resource.TenantID != acme.ID || policy.seen[0].Resource.Attributes["status"] != "open" ||
		policy.seen[0].Resource.Attributes["requested_assignee_id"] != owner.String() {
		t.Fatalf("policy did not receive the server-owned resource and proposed assignment: %+v", policy.seen)
	}
	actor = owner
	if code, body := call(t, router, http.MethodPost, at+"/assign", assign); code != http.StatusOK {
		t.Fatalf("permitted assignment: %d %s", code, body)
	}
	// An idempotent retry still rechecks policy; revoking access must not turn
	// an already completed command into an unguarded read of private state.
	actor = other
	if code, body := call(t, router, http.MethodPost, at+"/assign", assign); code != http.StatusForbidden {
		t.Fatalf("revoked retry: %d %s", code, body)
	}
	actor = owner
	policy.err = errors.Join(tenancy.ErrPolicyDenied, errors.New("private provider diagnostic"))
	if code, body := call(t, router, http.MethodPost, at+"/resolve", `{"resolution":"done"}`); code != http.StatusServiceUnavailable || strings.Contains(body, "private provider") {
		t.Fatalf("provider outage must be distinct and sanitized: %d %s", code, body)
	}
	if code, body := call(t, router, http.MethodGet, at, ""); code != http.StatusOK || !strings.Contains(body, `"status":"acknowledged"`) {
		t.Fatalf("outage changed task state: %d %s", code, body)
	}
	policy.err = nil
	if code, body := call(t, router, http.MethodPost, at+"/resolve", `{"resolution":"done"}`); code != http.StatusOK {
		t.Fatalf("permitted resolution: %d %s", code, body)
	}
	// A policy allow still cannot revive a task whose domain state is resolved.
	if code, body := call(t, router, http.MethodPost, at+"/assign", `{"assigneeId":"`+other.String()+`"}`); code != http.StatusConflict {
		t.Fatalf("policy bypassed task lifecycle: %d %s", code, body)
	}
	last := policy.seen[len(policy.seen)-1]
	if last.Resource.Attributes["status"] != "resolved" || last.Resource.Attributes["assignee_id"] != owner.String() {
		t.Fatalf("policy saw stale task facts: %+v", last)
	}
}
