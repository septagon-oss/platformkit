package httpx_test

// Which language a refusal is *shown* in is the tenant's declaration and not the
// browser's, and the half of that which lives here is that the request handed to the
// presentation layer names a tenant. ui/page can only intersect a preference list with a
// set it was given: a refusal answered ahead of routing arrives with no tenant on its
// context unless this package resolves the address's host for it.
//
// The application-level case (apps/platformkit/review_r6_refusal_locale_test.go) asks the
// composed binary the whole question — two tenants, one header. This pins the kernel half
// where it is owned, so the promise survives however the composition is hung together: the
// renderer of a page is handed a tenant, and a problem document is not why a host gets
// looked up.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestARefusalRenderedForAPersonIsHandedTheTenantOfItsAddress asks chi's own answer — an
// address nobody mounted, which reaches no handler, no operation middleware and no
// transaction — of a navigating client, and reads the tenant off the request the renderer
// was given. Without it the page is negotiated from `Accept-Language` alone, and a tenant
// that declared one language is refused in whichever the browser happened to bring.
func TestARefusalRenderedForAPersonIsHandedTheTenantOfItsAddress(t *testing.T) {
	var (
		askedFor tenancy.Tenant
		resolved bool
	)
	remembers := func(w http.ResponseWriter, r *http.Request, p *problem.Problem) bool {
		askedFor, resolved = tenancy.FromContext(r.Context())
		return documentFault(w, r, p)
	}
	_, router := setupFault(t, remembers)

	req := httptest.NewRequest(http.MethodGet, "http://"+host+"/nothing_is_mounted_here", nil)
	req.Header.Set("Accept", browserAccept)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("an address nobody mounted answered %d, so this is not the refusal the case is about: %s",
			w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("a navigating caller was answered %q, so no page was rendered and the tenant on the "+
			"request means nothing: %s", ct, w.Body.String())
	}
	if !resolved {
		t.Errorf("the renderer was handed a request that names no tenant: a refusal is a page of the "+
			"tenant behind its address, and the shell that words it can only intersect a preference list "+
			"with a set it was given — the fixture's tenant is served at %q", host)
	}
	if resolved && askedFor.Slug != "acme" {
		t.Errorf("the refusal was handed the tenant %q, not the one this host resolves to", askedFor.Slug)
	}
}

// TestARefusalAnsweredToAProgramAsksNothingOfTheTenant is the half that keeps the cure from
// becoming a query in front of every answer. A machine's refusal is a code — the same in
// every language — so the lookup belongs to the branch that renders a page and nowhere
// else. The renderer not being reached at all is what says so here.
func TestARefusalAnsweredToAProgramAsksNothingOfTheTenant(t *testing.T) {
	var sawDocument bool
	remembers := func(w http.ResponseWriter, r *http.Request, p *problem.Problem) bool {
		sawDocument = true
		return documentFault(w, r, p)
	}
	api, router := setupFault(t, remembers)

	got := postFrom(t, router, at(api, "/widgets"), "application/json", "http://elsewhere.test", false)

	if sawDocument {
		t.Errorf("a client that asked for a value was answered with a document, the shape this package "+
			"refuses for a reason, and with it the host lookup a page alone needs: %s", got.Body.String())
	}
	if got.Code != http.StatusForbidden {
		t.Errorf("the verdict changed while the shape was being decided: %d", got.Code)
	}
}
