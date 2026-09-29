package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
)

// TestAnAssignedTaskIsResolvedByItsAssigneeAtTheReferenceApplication is decision 0011's
// object-scope question answered at the shipped composition, with the rule this
// application writes in policy/task.rego and evaluates with embedded OPA.
//
// The administrator holds task:resolve through the wildcard, so the grant is not what is
// asked here — which task is. A task assigned to somebody else is refused with
// POLICY_DENIED, and that refusal is in the audit trail as security.denied naming the
// action, the rule's reason and the policy's revision, as a missing grant would be. The
// same person resolves the same task once it is theirs: what separates the two answers
// is the assignee alone.
func TestAnAssignedTaskIsResolvedByItsAssigneeAtTheReferenceApplication(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	me := field(t, whoami(t, cfg, admin), "userId")

	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tasksPath, `{"title":"chiller-2 supply temp","priority":"high"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tasksPath, code, body)
	}
	task := tasksPath + "/" + field(t, body, "id")

	somebodyElse := uuid.NewString()
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, task+"/assign", `{"assigneeId":"`+somebodyElse+`"}`); code != http.StatusOK {
		t.Fatalf("assign to somebody else = %d %s, want 200: the policy allows assignment", code, body)
	}
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, task+"/resolve", `{"resolution":"done"}`)
	if code != http.StatusForbidden || !strings.Contains(body, "POLICY_DENIED") {
		t.Fatalf("resolve somebody else's task = %d %s, want 403 POLICY_DENIED", code, body)
	}

	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, task+"/assign", `{"assigneeId":"`+me+`"}`); code != http.StatusOK {
		t.Fatalf("assign to self = %d %s, want 200", code, body)
	}
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, task+"/resolve", `{"resolution":"done"}`); code != http.StatusOK ||
		!strings.Contains(body, `"`+taskcontracts.StatusResolved+`"`) {
		t.Fatalf("the assignee resolving their own task = %d %s, want 200 and a resolved task", code, body)
	}

	// The trail is a plan feature here, so the tenant buys it before it is read.
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, plansWrite,
		`{"code":"pro","name":"Pro","priceCents":2900,"currency":"EUR","interval":"month","active":true,"features":["audit-trail"]}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", plansWrite, code, body)
	}
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, subPath+"/subscribe", `{"planId":"`+field(t, body, "id")+`"}`); code != http.StatusOK {
		t.Fatalf("subscribe = %d %s, want 200", code, body)
	}
	denied := waitForAudit(t, cfg, admin, "security.denied")
	if denied["actor"] != me {
		t.Errorf("the trail credits the refusal to %v, want the administrator %s", denied["actor"], me)
	}
	payload, _ := denied["payload"].(map[string]any)
	detail, _ := payload["detail"].(string)
	if payload["code"] != "POLICY_DENIED" || payload["status"] != float64(http.StatusForbidden) ||
		!strings.Contains(detail, "task:resolve") || !strings.Contains(detail, "resolved by its assignee") ||
		!strings.Contains(detail, taskPolicy.Revision()) {
		t.Errorf("the recorded refusal is %v; want POLICY_DENIED 403 naming task:resolve, the rule's reason and policy %s",
			payload, taskPolicy.Revision())
	}
}
