package main

import (
	"net/http"
	"testing"
)

// There is no proposal for a delete, so the change module's README names the way a
// task is deleted while the task switch is on: empty the protected field first. A task
// that never had a deadline and carries the priority every task defaults to has
// nothing left to empty, and the delete goes through — otherwise no task is deletable
// at all while the switch is on, through any door, which is a wall with a flag on it.
func TestATaskWithNothingProtectedLeftToEmptyCanBeDeleted(t *testing.T) {
	cfg, _, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tasksPath, `{"title":"A note filed by mistake"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s", tasksPath, code, body)
	}
	id := field(t, body, "id")
	if code, body = do(t, cfg, admin, http.MethodDelete, acmeHost, tasksPath+"/"+id, ""); code != http.StatusNoContent && code != http.StatusOK {
		t.Errorf("DELETE of a task with no deadline and the default priority = %d %s, want it deleted", code, body)
	}
}
