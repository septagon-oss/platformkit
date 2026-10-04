package main

// The task is change control's second subject, and this file is the brief's first
// consumer asked of the running reference application: a protected task field cannot
// be written directly while the installation asks for a second pair of eyes, the
// proposal that goes through the other door writes the row exactly once at the
// revision the diff was made against, and both halves of that write reach the trail
// with the actor who asked for it.
//
// Each case names the acceptance criterion it answers at the top of the function,
// because the criteria are the deliverable and a reviewer should be able to check
// them off one by one against a command output.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// taskChangeFixture is the reference composition with the task switch set by hand.
// The site switch is left off on purpose: a case that turned both on could pass with
// either one broken, and the two doors are wired by different lines.
func taskChangeFixture(t *testing.T, gateOn bool) (config.Config, composition, app.Options, string) {
	t.Helper()
	path, cfg := configure(t)
	cfg.Flags = &config.Flags{Values: map[string]bool{taskSLAFlag: gateOn}}
	c := compose(cfg)
	opts := appOptions(cfg, c, app.All)
	opts.Transport, opts.Log = memory.New(), quiet()
	install(t, path)
	start(t, cfg, c.modules, opts)
	return cfg, c, opts, path
}

// newTask makes a task through the door a person uses, and returns its id.
func newTask(t *testing.T, cfg config.Config, admin *http.Client, title, priority string) string {
	t.Helper()
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tasksPath,
		`{"title":"`+title+`","priority":"`+priority+`","slaDeadline":"2027-01-01T00:00:00Z"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want the task created", tasksPath, code, body)
	}
	if got := fieldNumber(t, body, "revision"); got != 1 {
		t.Fatalf("a new task reports revision %v, want 1: %s", got, body)
	}
	return field(t, body, "id")
}

// decider makes one account that may decide, the way the installation makes people:
// a role with the three change grants, an invitation, the mailed link, a sign-in.
func decider(t *testing.T, cfg config.Config, admin *http.Client, box *notification.Mailbox, key string) *http.Client {
	t.Helper()
	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, rolesPath+"/decider",
		`{"permissions":["change:read","change:propose","change:decide","task:read","task:update"]}`); code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("PUT %s/decider = %d %s, want the role", rolesPath, code, body)
	}
	email := key + "@acme.localhost"
	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, invitesPath,
		`{"email":"`+email+`","displayName":"`+key+`","roles":["decider"]}`); code != http.StatusCreated {
		t.Fatalf("POST %s for %s = %d %s, want 201", invitesPath, key, code, body)
	}
	var link string
	eventually(t, key+"'s invitation to be mailed", func() bool {
		for _, sent := range box.Sent() {
			if sent.To == email {
				link = sent.Body
				return true
			}
		}
		return false
	})
	pass := "a chosen passphrase for " + key
	if code, body := do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/auth/password/reset",
		`{"token":"`+tokenIn(t, link)+`","new":"`+pass+`"}`); code != http.StatusOK {
		t.Fatalf("the reset for %s = %d %s, want 200", key, code, body)
	}
	return signIn(t, cfg, acmeHost, email, pass)
}

// Criterion 6 (direct REST cannot bypass the policy) with criterion 1's second
// sentence (the live record remains unchanged) and the refusal's shape: it names the
// field, because "which one" is the question a person acting on it has to answer.
func TestAProtectedTaskFieldIsNotWrittenDirectlyAndTheRefusalNamesIt(t *testing.T) {
	cfg, _, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	id := newTask(t, cfg, admin, "Retire the old pump room", "normal")

	code, body := do(t, cfg, admin, http.MethodPatch, acmeHost, tasksPath+"/"+id,
		`{"slaDeadline":"2030-06-01T00:00:00Z"}`)
	if code != http.StatusConflict {
		t.Fatalf("PATCH of a protected field = %d %s, want 409", code, body)
	}
	for _, want := range []string{"slaDeadline", proposalsPath, "change:propose"} {
		if !strings.Contains(body, want) {
			t.Errorf("the refusal does not name %q, so the caller cannot act on it: %s", want, body)
		}
	}

	// Nothing moved: not the value, and not the number a later diff would quote. The
	// second is the one that catches a gate bolted on after the write — a refusal
	// that arrives after the UPDATE has run has refused nothing.
	code, body = do(t, cfg, admin, http.MethodGet, acmeHost, tasksPath+"/"+id, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", tasksPath, code, body)
	}
	if got := fieldNumber(t, body, "revision"); got != 1 {
		t.Errorf("the refused write left the task on revision %v, want 1: it wrote and then refused", got)
	}
	if strings.Contains(body, "2030-06-01") {
		t.Errorf("the refused deadline reached the row: %s", body)
	}

	// The same door, the same tenant, an unprotected field: the answer is a write.
	// A gate that read the switch before asking which fields had moved would refuse
	// this, and every task in the tenant would stop being editable over a flag about
	// deadlines.
	if code, body = do(t, cfg, admin, http.MethodPatch, acmeHost, tasksPath+"/"+id,
		`{"description":"the pump room closes in june"}`); code != http.StatusOK {
		t.Fatalf("PATCH of an unprotected field = %d %s, want 200", code, body)
	}

	// A body that resends the current deadline is not a write of it, and a rule that
	// read the request's keys instead of the entity would refuse the screen that
	// redraws the whole row.
	if code, body = do(t, cfg, admin, http.MethodPatch, acmeHost, tasksPath+"/"+id,
		`{"slaDeadline":"2027-01-01T00:00:00Z"}`); code != http.StatusOK {
		t.Fatalf("PATCH resending the current deadline = %d %s, want 200 and no change", code, body)
	}

	// Criterion 6's DELETE: a row with a protected value is the largest write there
	// is, and there is no proposal door for a delete, so the door refuses and says so
	// rather than quietly allowing the one way past the policy.
	if code, body = do(t, cfg, admin, http.MethodDelete, acmeHost, tasksPath+"/"+id, ""); code != http.StatusConflict {
		t.Errorf("DELETE of a protected task = %d %s, want 409", code, body)
	}
}

// Criteria 1, 2, 3 and 4 along one path: propose (the live row untouched), self-review
// refused, another account decides, and the apply writes the reviewed change once at
// the revision the diff was made against — with the trail holding both halves.
func TestATaskChangeGoesThroughTheProposalDoorAndWritesOnce(t *testing.T) {
	cfg, c, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the composition wired %T as its mailer, want the mailbox", c.mail)
	}
	grace := decider(t, cfg, admin, box, "grace")
	id := newTask(t, cfg, admin, "Re-plan the chiller upgrade", "high")

	// Criterion 1: target, explicit change and a reason in; the live record and its
	// revision out, unchanged. The body names no proposer and no base revision —
	// there is no field for either, and the number is read under the row lock.
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
		`{"subjectModule":"task","subjectEntity":"task","subjectId":"`+id+`",`+
			`"diff":{"slaDeadline":"2030-06-01T00:00:00Z"},"summary":"move the chiller SLA out a year"}`)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want the proposal recorded", proposalsPath, code, body)
	}
	pid := field(t, body, "id")
	if got := fieldNumber(t, body, "baseRevision"); got != 1 {
		t.Errorf("the proposal records base revision %v, want the 1 the task was on", got)
	}
	if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, tasksPath+"/"+id, ""); code != http.StatusOK ||
		strings.Contains(body, "2030-06-01") {
		t.Fatalf("GET %s after the propose = %d %s, want the task untouched", tasksPath, code, body)
	}

	// Criterion 2's actor half and criterion 3's recorded decision: the proposer
	// cannot decide, and the account that can, can.
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath+"/"+pid+"/review",
		`{"verdict":"approved","expectedRevision":1}`); code != http.StatusConflict {
		t.Fatalf("self-review = %d %s, want 409", code, body)
	}
	if code, body = do(t, cfg, grace, http.MethodPost, acmeHost, proposalsPath+"/"+pid+"/review",
		`{"verdict":"approved","comment":"the plant review moved","expectedRevision":1}`); code != http.StatusOK {
		t.Fatalf("the second account's verdict = %d %s, want 200", code, body)
	}
	if reviewer := field(t, body, "reviewer"); reviewer == "" {
		t.Errorf("the decision records no reviewer: %s", body)
	}

	// Criterion 4: the apply re-checks the subject under its row lock, writes the
	// merged document through the task module, and reports the revision it caused.
	if code, body = do(t, cfg, grace, http.MethodPost, acmeHost, proposalsPath+"/"+pid+"/apply",
		`{"expectedRevision":2}`); code != http.StatusOK {
		t.Fatalf("apply = %d %s, want 200", code, body)
	}
	if got := field(t, body, "state"); got != "applied" {
		t.Fatalf("the apply left the proposal %q, want applied", got)
	}
	applied := fieldNumber(t, body, "appliedRevision")
	code, body = do(t, cfg, admin, http.MethodGet, acmeHost, tasksPath+"/"+id, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", tasksPath, code, body)
	}
	if !strings.Contains(body, "2030-06-01") {
		t.Errorf("the reviewed deadline never reached the row: %s", body)
	}
	if got := fieldNumber(t, body, "revision"); got != applied {
		t.Errorf("the task is on revision %v and the proposal claims %v; one write, one number", got, applied)
	}

	// Criterion 4's "applied once": the same account retries and gets the row back,
	// and the row it changed does not change again.
	if code, body = do(t, cfg, grace, http.MethodPost, acmeHost, proposalsPath+"/"+pid+"/apply",
		`{"expectedRevision":3}`); code != http.StatusOK {
		t.Fatalf("a retry of a finished apply = %d %s, want the row it already produced", code, body)
	}
	if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, tasksPath+"/"+id, ""); code != http.StatusOK ||
		fieldNumber(t, body, "revision") != applied {
		t.Errorf("the retried apply moved the task again: %s", body)
	}

	// Criterion 5's observable half: the apply wrote the row *and* said so, and both
	// rows are in the trail with the account that asked for the apply. The task
	// module's own event is the one that would be missing if the apply bypassed the
	// module's write, and it is the one an audit reader would otherwise never see.
	trailIncluded(t, cfg, admin)
	for _, name := range []string{"change.proposal_applied", "task.task.updated"} {
		if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, auditPath+"?name="+name, ""); code != http.StatusOK {
			t.Fatalf("GET %s?name=%s = %d %s", auditPath, name, code, body)
		}
		row := waitForAudit(t, cfg, admin, name)
		if row["actor"] == nil || row["actor"] == "" {
			t.Errorf("the trail row for %s names no actor: %v", name, row)
		}
	}
}

// Criterion 6's commands half: the switch is about the fields a person edits, and it
// does not stop a tenant resolving its own tasks. A gate that protected a command's
// own fields would leave every open task open for as long as the flag was on.
func TestTheTaskSwitchNeverRefusesACommand(t *testing.T) {
	cfg, _, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	id := newTask(t, cfg, admin, "Clear the backlog", "low")

	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tasksPath+"/"+id+"/resolve",
		`{"resolution":"duplicate of 4112"}`); code != http.StatusOK {
		t.Fatalf("POST resolve with the switch on = %d %s, want 200: the flag protects fields, not the lifecycle", code, body)
	}
	// The command moved the row's number as much as a patch would have, which is what
	// keeps a later proposal's stale-base refusal explainable.
	code, body := do(t, cfg, admin, http.MethodGet, acmeHost, tasksPath+"/"+id, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", tasksPath, code, body)
	}
	if got := fieldNumber(t, body, "revision"); got != 2 {
		t.Errorf("the resolve left the task on revision %v, want 2: a command that writes the row moves its number", got)
	}
}

// Criterion 9's second tenant, at the door this delivery added: Globex's own
// administrator, holding every grant Globex has, proposes a change to a task of
// Acme's, and the subject's row is not there for the asking. The proposal is refused
// by the subject binding's read under row-level security, and nothing Acme can see
// changes.
func TestATaskProposalAcrossTenantsReachesNothing(t *testing.T) {
	cfg, _, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	task := newTask(t, cfg, admin, "A task of acme's", "normal")

	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tenantPath, code, body)
	}
	provision(t, cfg, uuid.MustParse(field(t, body, "id")), "root@globex.localhost")
	globex := signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)

	code, body = do(t, cfg, globex, http.MethodPost, globexHost, proposalsPath,
		`{"subjectModule":"task","subjectEntity":"task","subjectId":"`+task+`",`+
			`"diff":{"priority":"critical"},"summary":"another tenant's task"}`)
	if code == http.StatusOK || code == http.StatusCreated {
		t.Fatalf("a proposal over another tenant's task was accepted: %d %s", code, body)
	}
	// And the row it pointed at says nothing about the attempt: still normal, still
	// on revision 1, still acme's to read.
	if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, tasksPath+"/"+task, ""); code != http.StatusOK ||
		!strings.Contains(body, `"priority":"normal"`) || fieldNumber(t, body, "revision") != 1 {
		t.Errorf("the first tenant's task changed after a second tenant's attempt: %d %s", code, body)
	}
	// Globex may not even see the row it tried to change.
	if code, _ = do(t, cfg, globex, http.MethodGet, globexHost, tasksPath+"/"+task, ""); code != http.StatusNotFound {
		t.Errorf("globex GET acme's task = %d, want 404", code)
	}
}
