package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestDeactivatingAnAlreadyWithdrawnPersonChangesNothing checks the published
// promise of the deactivate command — "Deactivating them again changes nothing" —
// on the one inactive person this module creates: somebody whose invitation was
// withdrawn. The command's published body carries no fields, so the answer comes
// from the row's state alone: the same inactive person, unchanged, and no second
// fact about it.
func TestDeactivatingAnAlreadyWithdrawnPersonChangesNothing(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	code, invited := do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/user/invitations",
		`{"email":"withdrawn@acme.localhost","displayName":"Withdrawn","roles":["member"]}`)
	if code != http.StatusCreated {
		t.Fatalf("invite: %d %s", code, invited)
	}
	id := field(t, invited, "id")
	withdraw := "/api/v1/user/invitations/" + id + "/revoke"
	if code, body := do(t, cfg, admin, http.MethodPost, acmeHost, withdraw, `{"expectedRevision":1}`); code != http.StatusOK {
		t.Fatalf("withdraw the invitation: %d %s", code, body)
	}
	reading := usersPath + "/" + id
	code, before := do(t, cfg, admin, http.MethodGet, acmeHost, reading, "")
	if code != http.StatusOK || !strings.Contains(before, `"status":"inactive"`) {
		t.Fatalf("withdrawn person: %d %s", code, before)
	}
	deactivate := usersPath + "/" + id + "/deactivate"
	if code, refusal := do(t, cfg, admin, http.MethodPost, acmeHost, deactivate, `{}`); code != http.StatusOK {
		t.Fatalf("deactivating a person who is already inactive answered %d %s; the published command takes no body and promises that deactivating again changes nothing",
			code, refusal)
	}
	if code, after := do(t, cfg, admin, http.MethodGet, acmeHost, reading, ""); code != http.StatusOK || after != before {
		t.Fatalf("the second deactivation moved the row: %d %s, was %s", code, after, before)
	}
}
