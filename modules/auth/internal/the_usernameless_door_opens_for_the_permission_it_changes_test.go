package internal_test

// The usernameless door has one fact — a row of the tenant's own — and one way
// it may be changed: the route, under the permission that guards it. The module
// ships no migration that opens it and no default that does, so a case has to
// come at it the way an administrator does, and the two refusals that surround
// that write are the ones worth pinning: a member opening the tenant's front
// door, and an event for a write that wrote nothing.

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

const door = "/api/v1/auth/settings/passkey-sign-in"

func TestTheUsernamelessDoorOpensOnlyForThePermissionItChanges(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	person(t, conn, "bob@acme.localhost", contracts.RoleMember)
	member := signIn(t, router, "bob@acme.localhost")
	admin := signIn(t, router, "ada@acme.localhost")

	// A member holds no permission that opens it — the tenant's admin role does,
	// through the wildcard its people are seeded with, and the member's is empty.
	// The refusal is the kernel's, before the command runs, so it writes nothing:
	// the door behind it is still the one every installation starts with.
	refused := call(t, router, http.MethodPost, door, `{"enabled":true}`, withSession(member))
	if refused.Code != http.StatusForbidden {
		t.Fatalf("a member opening the usernameless door = %d %s, want 403", refused.Code, refused.Body.String())
	}
	if begun := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", ""); begun.Code != http.StatusForbidden {
		t.Errorf("the usernameless leg after a refused write = %d, want 403: the refusal wrote no row", begun.Code)
	}
	if events := doorEvents(t, conn); events != 0 {
		t.Errorf("the refused write left %d trail rows, want none: a mutation that is refused emits nothing", events)
	}

	// The administrator opens it, and the answer is the state of the row.
	opened := setDoor(t, router, admin, true)
	if opened != true {
		t.Fatalf("opening the door reported %v, want true", opened)
	}
	if begun := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", ""); begun.Code != http.StatusOK {
		t.Fatalf("the usernameless leg after the door was opened = %d %s, want 200", begun.Code, begun.Body.String())
	}
	if events := doorEvents(t, conn); events != 1 {
		t.Errorf("opening the door published %d %s events, want exactly 1", events, contracts.EventPasskeySignInSet)
	}

	// Setting what is already set is not a change: the answer is the same, and no
	// second entry says somebody opened a door that was already open.
	if again := setDoor(t, router, admin, true); again != true {
		t.Fatalf("setting the door to what it was reported %v, want true", again)
	}
	if events := doorEvents(t, conn); events != 1 {
		t.Errorf("a write that wrote nothing published %d events, want the one from the write that did", events)
	}

	// And it closes: the promise runs both ways, or the switch is a ratchet.
	if shut := setDoor(t, router, admin, false); shut != false {
		t.Fatalf("closing the door reported %v, want false", shut)
	}
	if begun := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", ""); begun.Code != http.StatusForbidden {
		t.Errorf("the usernameless leg after the door was closed = %d, want 403", begun.Code)
	}
	if events := doorEvents(t, conn); events != 2 {
		t.Errorf("open then shut published %d events, want 2", events)
	}
}

// setDoor asks the route, as a person's browser would, and reads the state back
// off the answer rather than off the row: the route is what an administrator
// has, and the row is what it is supposed to report.
func setDoor(t *testing.T, router http.Handler, session string, enabled bool) bool {
	t.Helper()
	res := call(t, router, http.MethodPost, door,
		`{"enabled":`+strconv.FormatBool(enabled)+`}`, withSession(session))
	if res.Code != http.StatusOK {
		t.Fatalf("%s %s = %d %s, want 200", http.MethodPost, door, res.Code, res.Body.String())
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("read the door's answer: %v (%s)", err, res.Body.String())
	}
	return body.Enabled
}

// doorEvents is the tenant's own trail of the door, which is where a person
// reading the record afterwards looks.
func doorEvents(t *testing.T, conn *db.Conn) int64 {
	t.Helper()
	var count int64
	err := db.Run(tenancy.WithTenant(context.Background(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventPasskeySignInSet).
			Count(&count).Error
	})
	if err != nil {
		t.Fatalf("count the door's trail: %v", err)
	}
	return count
}
