package page_test

import (
	"bytes"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"testing"
)

func TestLoggedOutFormJourneysExpectUnauthorized(t *testing.T) {
	source, err := os.ReadFile("../../e2e/session-recovery.spec.ts")
	if err != nil {
		t.Fatal(err)
	}
	// Track sign-in and logout across repeated submits so a later validation
	// refusal is kept separate from a submit that still has no session.
	actions := regexp.MustCompile(`await page\.request\.post\('/api/v1/auth/logout'\)|await signIn\([^)]+\)|await submit\((\d+)\)`)
	anonymous, submits := false, 0
	for _, action := range actions.FindAllSubmatch(source, -1) {
		switch {
		case bytes.HasPrefix(action[0], []byte("await page.request.post(")):
			anonymous = true
		case bytes.HasPrefix(action[0], []byte("await signIn(")):
			anonymous = false
		case anonymous:
			status, err := strconv.Atoi(string(action[1]))
			if err != nil {
				t.Fatal(err)
			}
			submits++
			if status != http.StatusUnauthorized {
				t.Errorf("a logged-out form journey expects HTTP %d; the protected door requires HTTP %d", status, http.StatusUnauthorized)
			}
		}
	}
	if submits == 0 {
		t.Fatal("no logged-out form submits found in the recovery journey")
	}
}
