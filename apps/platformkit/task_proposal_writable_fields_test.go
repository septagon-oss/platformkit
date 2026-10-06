package main

import (
	"net/http"
	"testing"
)

// A task proposal is a diff the apply will write through taskcontracts.Writer, and
// that writer moves only the task's proposable fields. A diff naming anything else —
// the status a command owns, the revision the server owns, or a name that is not a
// field of a task at all — is a proposal no apply could ever carry out as reviewed,
// and Propose says it refuses those "here rather than at the review". Each of these is
// refused at the propose door, and no proposal row is left behind.
func TestATaskProposalNoApplyCouldWriteIsRefusedWhenProposed(t *testing.T) {
	cfg, _, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	id := newTask(t, cfg, admin, "Inspect the cooling tower", "normal")

	for name, diff := range map[string]string{
		"a command-owned field": `{"status":"resolved"}`,
		"the server's revision": `{"revision":7}`,
		"a column, not a field": `{"sla_deadline":"2030-06-01T00:00:00Z"}`,
		"no field of a task":    `{"deadline":"2030-06-01T00:00:00Z"}`,
	} {
		code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
			`{"subjectModule":"task","subjectEntity":"task","subjectId":"`+id+`",`+
				`"diff":`+diff+`,"summary":"`+name+`"}`)
		if code != http.StatusUnprocessableEntity && code != http.StatusConflict {
			t.Errorf("a proposal over %s (%s) = %d %s, want 422 or 409 at the propose door", name, diff, code, body)
		}
	}
}
