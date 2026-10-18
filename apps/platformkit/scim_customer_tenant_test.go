package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// scimKeyAt mints a scoped credential on one tenant's host, as that tenant's own
// administrator would. scimKey mints on the operator's host only.
func scimKeyAt(t *testing.T, cfg config.Config, admin *http.Client, host string, scopes ...string) string {
	t.Helper()
	code, body := do(t, cfg, admin, http.MethodPost, host, "/api/v1/auth/tokens",
		`{"name":"SCIM connector","scopes":`+jsonStringList(scopes)+`}`)
	if code != http.StatusCreated {
		t.Fatalf("minting a key scoped to %v at %s = %d %s, want 201", scopes, host, code, body)
	}
	return field(t, body, "token")
}

// TestACustomerTenantsAdministratorProvisionsThroughItsOwnSCIMDoor walks the
// identity provider of a customer tenant — one the control plane created, whose
// roles the composition's seeding hook wrote — through the SCIM door on that
// tenant's own host. Its administrator mints a scim:provision key, creates a
// person and finds them by userName; a key of the same tenant without the scope
// is refused; the customer's key opens nothing on the operator's host; and the
// operator's own directory never lists the customer's person.
func TestACustomerTenantsAdministratorProvisionsThroughItsOwnSCIMDoor(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	operator := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	code, body := do(t, cfg, operator, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("create globex = %d %s", code, body)
	}
	provision(t, cfg, uuid.MustParse(field(t, body, "id")), "root@globex.localhost")
	customer := signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)

	key := scimKeyAt(t, cfg, customer, globexHost, "scim:provision")
	const person = "leaver@globex.test"
	code, _, body = scimAsk(t, cfg, http.MethodPost, globexHost, scimDoor,
		`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"`+person+`","active":true}`,
		key, "application/scim+json")
	if code != http.StatusCreated {
		t.Fatalf("the customer's connector provisioning on its own host = %d %s, want 201", code, body)
	}
	if got := field(t, body, "userName"); got != person {
		t.Errorf("the person reads back userName %q, want %q", got, person)
	}

	byName := scimDoor + "?filter=" + url.QueryEscape(`userName eq "`+person+`"`)
	code, _, body = scimAsk(t, cfg, http.MethodGet, globexHost, byName, "", key, "")
	if code != http.StatusOK || !strings.Contains(body, person) {
		t.Errorf("filtering the customer's directory by userName = %d %s, want 200 naming %s", code, body, person)
	}

	narrow := scimKeyAt(t, cfg, customer, globexHost, "user:read")
	if code, _, body = scimAsk(t, cfg, http.MethodGet, globexHost, scimDoor, "", narrow, ""); code == http.StatusOK {
		t.Errorf("a customer key scoped to user:read was served the SCIM directory: %s", body)
	}

	if code, _, body = scimAsk(t, cfg, http.MethodGet, acmeHost, scimDoor, "", key, ""); code == http.StatusOK {
		t.Errorf("the customer's key was served the operator's SCIM directory: %s", body)
	}
	own := scimKey(t, cfg, operator, "scim:provision")
	code, _, body = scimAsk(t, cfg, http.MethodGet, acmeHost, byName, "", own, "")
	if code != http.StatusOK {
		t.Fatalf("the operator filtering its own directory = %d %s, want 200", code, body)
	}
	if strings.Contains(body, person) {
		t.Errorf("the operator's directory lists the customer's person: %s", body)
	}
}
