package admin_test

import (
	"net/http"
	"strings"
	"testing"
)

// The default sign-in destination should take a role holder to the first
// screen that role can use. A person sent to a particular page with ?next
// follows that explicit destination instead.
func TestASignedInPersonLandsOnTheFirstScreenTheirRoleOpens(t *testing.T) {
	router := mountAs(t, member{"role:manage": true}, withRoles(seeded()))
	const firstScreen = "/app/auth/roles"
	if code, body, _ := call(t, router, http.MethodGet, firstScreen, ""); code != http.StatusOK {
		t.Fatalf("the role's screen = %d %s, want 200", code, body)
	}

	code, page, _ := call(t, router, http.MethodGet, "/app/admin/login", "")
	if code != http.StatusOK {
		t.Fatalf("the sign-in page = %d %s", code, page)
	}
	path := attribute(page, "data-next")
	if !strings.HasPrefix(path, "/app") {
		t.Fatalf("the sign-in destination %q does not lead into the workspace", path)
	}
	for range 4 {
		if path == firstScreen {
			return
		}
		code, _, location := call(t, router, http.MethodGet, path, "")
		if code != http.StatusSeeOther && code != http.StatusFound {
			t.Fatalf("a signed-in person stops at %s with %d; their first open screen is %s",
				path, code, firstScreen)
		}
		if !strings.HasPrefix(location, "/app/") {
			t.Fatalf("the landing at %s redirects outside the workspace to %q", path, location)
		}
		path = location
	}
	t.Fatalf("the sign-in destination did not reach %s", firstScreen)
}
