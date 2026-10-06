package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// The task's revision is the server's count of its writes. The generated edit form
// draws the fields a person types into, and the revision is not one of them: a form
// that posts back the number it rendered moves the row to whatever that number was.
func TestTheTaskEditFormOffersNoRevisionToType(t *testing.T) {
	cfg, _, _, _ := taskChangeFixture(t, false)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	id := newTask(t, cfg, admin, "Paint the stairwell", "normal")

	status, _, page := askNavigate(t, cfg, admin, acmeHost, "/app/task/tasks/"+id+"/edit")
	if status != http.StatusOK || !strings.Contains(page, `name="title"`) {
		t.Fatalf("GET the task's edit form = %d, want the form with its title field", status)
	}
	locked := regexp.MustCompile(`\s(readonly|disabled)(\s|=|/?>)`)
	for _, input := range regexp.MustCompile(`<input[^>]*name="revision"[^>]*>`).FindAllString(page, -1) {
		if !locked.MatchString(input) {
			t.Errorf("the edit form offers the revision as a field to type and post back: %s", input)
		}
	}
}
