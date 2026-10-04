package main

// Review round 4 of T-0115, at the composition: a hostname the control plane lets go
// stops answering for the tenant that let go of it — in this process, today, not
// within the resolution cache's half minute.
//
// What already covers the release: `TestARetiredTenantsHostIsServableAgain` asks the
// *service* to resolve the name and reads the successor's id back, and
// `TestEachLifecycleVerbAnswersAtItsOwnRoute` shows the released slug and host can be
// handed to a new customer through the door a person knocks on. Neither is the claim
// this file makes. Between the database and a request sits `kit/httpx`'s host cache
// (hostTTL, thirty seconds, positive entries only), which holds the resolved tenant
// with its languages and its name. `Delete` and `RemoveHost` therefore have to follow
// the write with `invalidate`, and `Delete` has to invalidate the hosts it just
// released — which are exactly the names its own response body carries and no
// subsequent read of the tenant will ever carry again, because a retired tenant is
// not found.
//
// So nothing in this tree yet says what a person would notice: a name is released, a
// second customer is hosted at it, and the first request at that address is answered
// from a cache that still believes the tenant behind it is the one that was just
// retired. That is one customer's page — its language, and with the language its
// whole anonymous frame — served at another customer's address, for as long as the
// entry is believed, by a process that was told the name changed hands and heard
// nothing.
//
// Both legs below therefore reach their assertion through what the fixed behaviour
// prints: the *successor's* declared `lang` attribute on its own sign-in page. The
// control above each release proves the warm-up happened the way the cache needs it
// to — one anonymous request at the address while the predecessor still held the
// name, answered in the predecessor's declared language — so the case cannot be
// green because the host never resolved at all. Nothing here asks for an error
// sentence, a redirect or a status only a refusal would carry.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// r4Tenant is a control-plane answer, decoded for the two fields these legs need.
type r4Tenant struct {
	ID    uuid.UUID `json:"id"`
	Slug  string    `json:"slug"`
	Hosts []string  `json:"hosts"`
}

// r4create makes one tenant at the control plane and returns it.
func r4create(t *testing.T, cfg config.Config, admin *http.Client, body, what string) r4Tenant {
	t.Helper()
	code, out := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath, body)
	if code != http.StatusCreated {
		t.Fatalf("POST %s for %s = %d %s, want 201", tenantPath, what, code, out)
	}
	var created r4Tenant
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("read the tenant %s created: %v\n%s", what, err, out)
	}
	return created
}

// r4serveIn declares which languages one tenant is served in — in these legs, European
// Portuguese alone, so the page's `lang` attribute says which tenant answered.
func r4serveIn(t *testing.T, cfg config.Config, admin *http.Client, id uuid.UUID, tag string) {
	t.Helper()
	code, out := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath+"/"+id.String()+"/locale",
		`{"default":"`+tag+`","supported":[]}`)
	if code != http.StatusOK {
		t.Fatalf("POST /locale for %s = %d %s, want 200", id, code, out)
	}
}

// r4page is the anonymous sign-in page at one address: the page a person with no
// session has, and the page whose `lang` attribute is this tenant's declaration.
func r4page(t *testing.T, cfg config.Config, host string) (int, string) {
	t.Helper()
	return getLanguage(t, cfg, host, "/app/admin/login", "")
}

// r4expectPage is the assertion both legs and both controls share. The page must be
// there (200); it must carry the language of the tenant expected to answer, and that
// tenant's own display name; and it must not carry the name of the tenant that held
// this address before — a name a request can only be served from a resolution this
// process was told to forget and did not.
//
// The name, not the other tenant's `lang` attribute, is the negative: ui/document
// emits a `lang="en"` element on a page whose tenant speaks only Portuguese (the
// anonymous-session banner is a literal no catalogue carries, which the pseudo-locale
// gate (apps/platformkit/i18n_gate_test.go) reports as an untranslated string on that
// page), so a missing-`lang` assertion would be a test of that open copy item rather
// than of this resolution. A tenant's display name
// comes out of the same cached resolution the language does, and belongs to exactly
// one tenant.
func r4expectPage(t *testing.T, cfg config.Config, host, wantLang, wantName, notName, whose string) {
	t.Helper()
	code, html := r4page(t, cfg, host)
	if code != http.StatusOK {
		t.Fatalf("GET /app/admin/login at %s = %d, want 200: the sign-in page is the page a person "+
			"with no session has, and %s cannot be proven about anything but the refusal", host, code, whose)
	}
	if !strings.Contains(html, `lang="`+wantLang+`"`) {
		t.Errorf("the sign-in page at %s declares no %q while %s is served in that language alone: %s",
			host, `lang="`+wantLang+`"`, whose, firstLineOf(html))
	}
	if !strings.Contains(html, wantName) {
		t.Errorf("the sign-in page at %s carries no %q, so %s did not answer this request: %s",
			host, wantName, whose, firstLineOf(html))
	}
	if strings.Contains(html, notName) {
		t.Errorf("the sign-in page at %s carries %q, the tenant that held this address before %s did: "+
			"the request was answered from a resolution the control plane released and this process never "+
			"forgot — %s", host, notName, whose, firstLineOf(html))
	}
}

// TestAReleasedHostStopsAnsweringForTheTenantThatReleasedIt runs two releases.
func TestAReleasedHostStopsAnsweringForTheTenantThatReleasedIt(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	// Leg 1 — `Delete` releases the name the retired tenant routed on.
	//
	// A customer served in European Portuguese alone, at a name of its own, so that
	// whose language a page at that address declares says which tenant answered.
	predecessor := r4create(t, cfg, admin,
		`{"slug":"dialpad","name":"Dialpad Corporation","host":"dialpad.localhost"}`, "dialpad")
	r4serveIn(t, cfg, admin, predecessor.ID, "pt-PT")
	r4expectPage(t, cfg, "dialpad.localhost", "pt-PT", "Dialpad Corporation", "Dialpad Renewed",
		"the tenant about to be retired")

	// The retirement, asked for twice in shape: the wrong confirm, then the slug.
	if code, out := do(t, cfg, admin, http.MethodPost, acmeHost,
		tenantPath+"/"+predecessor.ID.String()+"/delete", `{"confirm":"someone-else"}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("POST /delete without the slug = %d %s, want 422: the control must refuse before this leg starts", code, out)
	}
	if code, out := do(t, cfg, admin, http.MethodPost, acmeHost,
		tenantPath+"/"+predecessor.ID.String()+"/delete", `{"confirm":"dialpad"}`); code != http.StatusOK {
		t.Fatalf("POST /delete for dialpad = %d %s, want 200", code, out)
	}

	// The name goes to the next customer — the same address, a tenant of its own, in
	// the language its copy is written in. This request is the reachability control:
	// it proves the released name is attached again, so the page below is served by
	// somebody and not by nobody.
	successor := r4create(t, cfg, admin,
		`{"slug":"dialpad-renewed","name":"Dialpad Renewed","host":"dialpad.localhost"}`, "dialpad-renewed")

	// And the page at that address belongs to the successor.
	r4expectPage(t, cfg, "dialpad.localhost", "en", "Dialpad Renewed", "Dialpad Corporation",
		"the successor hosted there now ("+successor.Slug+")")

	// Leg 2 — `RemoveHost` releases one of two names, and the name it releases is not
	// in the response body it returns.
	second := r4create(t, cfg, admin,
		`{"slug":"initech","name":"Initech","host":"initech.localhost"}`, "initech")
	r4serveIn(t, cfg, admin, second.ID, "pt-PT")
	code, out := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath+"/"+second.ID.String()+"/hosts",
		`{"host":"initech-secondary.localhost"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST /hosts for initech = %d %s, want 201", code, out)
	}
	r4expectPage(t, cfg, "initech-secondary.localhost", "pt-PT", "Initech", "Umbrella",
		"the tenant about to lose this name")

	if code, out = do(t, cfg, admin, http.MethodDelete, acmeHost,
		tenantPath+"/"+second.ID.String()+"/hosts/initech-secondary.localhost", ""); code != http.StatusOK {
		t.Fatalf("DELETE /hosts/initech-secondary.localhost = %d %s, want 200", code, out)
	}
	r4create(t, cfg, admin,
		`{"slug":"umbrella","name":"Umbrella","host":"initech-secondary.localhost"}`, "umbrella")
	r4expectPage(t, cfg, "initech-secondary.localhost", "en", "Umbrella", "Initech",
		"the tenant now hosted at the removed name")

	// And the tenant that kept its own name is unaffected by either release: it is
	// still served in Portuguese at the address it never gave up. This is the leg
	// that stops the cure being "invalidate everything, always".
	r4expectPage(t, cfg, "initech.localhost", "pt-PT", "Initech", "Umbrella",
		"Initech, which kept this name")
}
