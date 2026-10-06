package main

// Review round 1 of T-0115, round 3's delivery: the lifecycle verbs are routes, and until
// this file nothing in the repository had answered one of the four new ones successfully.
//
// What existed: modules/tenant/internal drives `Rename`, `Reactivate`, `RemoveHost` and
// `Delete` through the service and the shared conformance suite drives the same verbs
// through both doubles, and review round 1's app-level case probes those four addresses
// anonymously and asserts only that none is *accepted*. A route nobody mounted answers that
// assertion too. Between the two there was no case that a person's request — right host,
// right grant, right body, one session — was routed, decoded, answered and read back: a
// `rename` mounted at the wrong path, or a `remove-host` whose `{host}` parameter never
// reached `RemoveHost`, would have shipped green, and it is the route, not the service
// method, that a person and a native shell use.
//
// So this performs one customer's whole lifecycle through the doors and reads each answer:
// the name moves without the slug moving, a second host arrives and leaves, the last and the
// primary host are refused, a suspension is reversed, and a delete is asked for twice — and
// the name it releases is handed to the next customer by the last request below, which is
// finding 1 (a retired tenant's hostname stayed unreachable forever) pinned at the door a
// person actually knocks on rather than only at the service behind it.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// r3Tenant is one tenant as the control plane's JSON answers it — the fields this case
// reads, decoded, because what a verb changed is in the body and not in the status code.
type r3Tenant struct {
	ID     uuid.UUID `json:"id"`
	Slug   string    `json:"slug"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
	Hosts  []string  `json:"hosts"`
}

// r3read decodes a body the case expected to succeed, so a 409 or a 422 names itself here
// rather than as a zero name three assertions later.
func r3read(t *testing.T, code int, body, what string) *r3Tenant {
	t.Helper()
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("%s = %d %s, want a success", what, code, body)
	}
	var out r3Tenant
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("read %s from %s: %v", what, body, err)
	}
	return &out
}

func TestEachLifecycleVerbAnswersAtItsOwnRoute(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"dialpad","name":"Dialpad Corporation","host":"dialpad.localhost"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tenantPath, code, body)
	}
	customer := uuid.MustParse(field(t, body, "id"))

	// One door, one customer. Every verb below is asked of the tenant this request made,
	// through the route the handler mounts it at.
	at := func(method, suffix, body string) (int, string) {
		t.Helper()
		return do(t, cfg, admin, method, acmeHost, tenantPath+"/"+customer.String()+suffix, body)
	}
	ask := func(method, suffix, body, what string) *r3Tenant {
		t.Helper()
		code, body := at(method, suffix, body)
		return r3read(t, code, body, what)
	}

	// rename takes the display name and nothing else: the slug is a DNS label and the
	// base of every URL this platform builds for the tenant, so a rename that could move
	// it would be a create wearing another verb's route id.
	renamed := ask(http.MethodPost, "/rename", `{"name":"Dialpad Industries"}`, "POST rename")
	if renamed.Name != "Dialpad Industries" || renamed.Slug != "dialpad" {
		t.Errorf("the rename left %q/%q, want the new name on the slug it was given",
			renamed.Name, renamed.Slug)
	}

	// A second host arrives (201: it is a row created) and, once it is not the primary
	// one, leaves again. The primary is refused: it is what every absolute URL for this
	// tenant is built on, and the last one is where a person signs in.
	added := ask(http.MethodPost, "/hosts", `{"host":"dialpad.example.com"}`, "POST add-host")
	if len(added.Hosts) != 2 || added.Hosts[0] != "dialpad.localhost" {
		t.Errorf("add-host left the tenant at %v, want dialpad.example.com beside dialpad.localhost and that one still first",
			added.Hosts)
	}
	removed := ask(http.MethodDelete, "/hosts/dialpad.example.com", "", "DELETE remove-host")
	if len(removed.Hosts) != 1 || removed.Hosts[0] != "dialpad.localhost" {
		t.Errorf("remove-host left %v, want the one host the tenant still answers at", removed.Hosts)
	}
	if code, body = at(http.MethodDelete, "/hosts/dialpad.localhost", ""); code != http.StatusConflict {
		t.Errorf("removing a tenant's primary host = %d %s, want 409", code, body)
	}

	// A suspension and its inverse, both answered, both read back through the status the
	// route returns rather than a row somebody counted.
	suspended := ask(http.MethodPost, "/suspend", "", "POST suspend")
	if suspended.Status != "suspended" {
		t.Errorf("suspend answered status %q, want suspended", suspended.Status)
	}
	live := ask(http.MethodPost, "/reactivate", "", "POST reactivate")
	if live.Status != "active" {
		t.Errorf("reactivate answered status %q, want active", live.Status)
	}

	// The delete is asked for twice, in two different shapes: the slug repeated is the
	// confirmation, and a request that ends a customer without it is refused before it
	// reaches the service.
	if code, body = at(http.MethodPost, "/delete", `{"confirm":"someone-else"}`); code != http.StatusUnprocessableEntity {
		t.Errorf("delete with the wrong confirm = %d %s, want 422", code, body)
	}
	if code, body = at(http.MethodPost, "/delete", `{"confirm":"dialpad"}`); code != http.StatusOK {
		t.Fatalf("delete with the slug repeated = %d %s, want 200", code, body)
	}
	if code, _ = at(http.MethodGet, "", ""); code != http.StatusNotFound {
		t.Errorf("GET the retired tenant = %d, want 404: a retired tenant is not found", code)
	}

	// And the released pair — slug and hostname, both — is handed to the next customer by
	// a create that would have been a conflict on the global PRIMARY KEY of
	// tenant_hosts until this verb let go of the name it routed on.
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"dialpad","name":"Dialpad Renewed","host":"dialpad.localhost"}`); code != http.StatusCreated {
		t.Errorf("hosting a new customer at the retired tenant's slug and host = %d %s, want 201: "+
			"the delete released the row but not the name", code, body)
	}
}
