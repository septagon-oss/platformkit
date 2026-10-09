package main

// The tenant's own mapping door, answered by a session rather than a key.
//
// `MapGroup` re-checks who is asking inside the transaction that writes the binding,
// and the reference composition answers that question over two rows it does not own:
// the person's roles, which are modules/user's column, and what those roles grant,
// which is the auth module's table. A case in `modules/scim` can only ask that
// question of a harness's own answer to it. The adapter this application wired —
// `holderGrants` in `apps/platformkit/scim.go` — has never been asked anything unless
// a running composition asks it, and the failure it would introduce is not a security
// hole but the opposite one: an administrator's own session refused at the tenant's
// own screen, because the command-level recheck could not resolve a wildcard.
//
// So this is the composition's case in both directions: a session whose person signs
// in with the tenant's administrator role gets the binding written and reads it back,
// and a credential carrying its own narrower scope — one that names `user:read`, and
// so is a caller the mapping is not for — does not.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

const scimMappingDoor = "/api/v1/scim/group-mappings"

func TestTheTenantBindsAGroupToARoleFromItsOwnSession(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	// The collection arrives the only way a collection arrives: through the
	// provider's door, with the protocol's credential.
	key := scimKey(t, cfg, admin, "scim:provision")
	code, _, body := scimAsk(t, cfg, http.MethodPost, acmeHost, "/scim/v2/Groups",
		`{"displayName":"Night Crew"}`, key, "application/scim+json")
	if code != http.StatusCreated {
		t.Fatalf("declaring a group through the SCIM door = %d %s, want 201", code, body)
	}

	// The binding is a session's act. An admin cookie carries no list of its own
	// permissions — auth hands a session its person and its roles, and the
	// authorizer works the grants out per request — so this is the shape the
	// command asks its Authority about.
	code, body = do(t, cfg, admin, http.MethodPut, acmeHost, scimMappingDoor,
		`{"group":"Night Crew","role":"admin"}`)
	if code != http.StatusOK {
		t.Fatalf("an administrator session binding the group = %d %s; want the write accepted — the recheck must resolve this person's role into the grant its route already honoured", code, body)
	}
	if !strings.Contains(body, "Night Crew") || !strings.Contains(body, "admin") {
		t.Errorf("the binding reads back %s", body)
	}
	if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, scimMappingDoor, ""); code != http.StatusOK ||
		!strings.Contains(body, "Night Crew") {
		t.Errorf("GET %s = %d %s, want the table the session just wrote", scimMappingDoor, code, body)
	}

	// The other shape, through the same address: a credential that carries its own
	// scope, which names the read and not the write.
	narrow := scimKey(t, cfg, admin, "user:read")
	if code, _, body = scimAsk(t, cfg, http.MethodPut, acmeHost, scimMappingDoor,
		`{"group":"Night Crew","role":"member"}`, narrow, "application/json"); code == http.StatusOK {
		t.Errorf("a key scoped to user:read bound a group to a role: %s", body)
	}
	if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, scimMappingDoor, ""); code != http.StatusOK ||
		!strings.Contains(body, `"role":"admin"`) {
		t.Errorf("GET %s = %d %s, want the one binding the administrator made", scimMappingDoor, code, body)
	}
}
