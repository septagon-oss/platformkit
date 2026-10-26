// Two gates keep the control plane from a caller who may not open it, and kit/httpx/README.md
// promises both answers are the ordinary refusal of that host, so that the surface discloses
// nothing: the host gate in (*API).address, for a host that is not the installation's, and
// notHere in the authorization middleware, for a tenant that is not the installation's at the
// host the control plane is served at. Both run after the middleware that puts security headers
// on a response, which is what makes a refusal of the control plane a document like any other.
//
// The files beside this one compare two refusals of one host header by header. Such a comparison
// passes when both pages are bare, so it cannot hold the promise alone. This case reads the
// control plane's own refusal page on its own terms: it carries the content security policy with
// this response's nonce, the frame protection, the no-sniff, the no-store and the no-index every
// document of this host carries.
package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTheControlPlaneRefusalPageCarriesTheHostsDocumentProtection(t *testing.T) {
	const nowhere = "/nothing-is-mounted-at-this-address"
	const id = "one-request-id-for-both"

	// Reachability, read off what a working kernel prints: at the installation host the
	// operator's tenant is served at this very address, so the refusal below is a gate's
	// answer and not a route that was never mounted.
	served, opsAt, _ := controlPlaneKernel(t, true, nil)
	controlPlane := httptest.NewServer(served)
	defer controlPlane.Close()
	if got := controlPlaneAsk(t, controlPlane.URL, installationHost, opsAt("/all"), browserAccept, id); got.status != http.StatusOK {
		t.Fatalf("the operator's tenant at %s = %d; this case is about an address served at this host",
			opsAt("/all"), got.status)
	}

	router, opsAt, appAt := controlPlaneKernel(t, false, documentFault)
	server := httptest.NewServer(router)
	defer server.Close()

	// The reference is a document of this host, asked the same way: if the kernel puts no
	// policy on any page here, the assertions below would protect nothing, and this says so.
	if ordinary := controlPlaneAsk(t, server.URL, installationHost, appAt(nowhere), browserAccept, id); !strings.Contains(ordinary.header.Get("Content-Security-Policy"), "default-src") {
		t.Fatalf("an address nobody mounted at this host answers no policy, so no page of this host is protected: %v", ordinary.header)
	}

	refused := controlPlaneAsk(t, server.URL, installationHost, opsAt("/all"), browserAccept, id)
	if refused.status != http.StatusNotFound {
		t.Fatalf("a tenant that is not the installation's at %s = %d, want the 404 the gate gives", opsAt("/all"), refused.status)
	}
	if !strings.Contains(refused.header.Get("Content-Type"), "html") {
		t.Fatalf("%s answered %q, not the page a browser is shown: %v", opsAt("/all"), refused.header.Get("Content-Type"), refused.header)
	}
	policy := refused.header.Get("Content-Security-Policy")
	for guard, protection := range map[string]string{
		"Content-Security-Policy": "default-src",
		"X-Frame-Options":         "DENY",
		"X-Content-Type-Options":  "nosniff",
		"Cache-Control":           "no-store",
		"X-Robots-Tag":            "noindex",
	} {
		if got := refused.header.Get(guard); !strings.Contains(got, protection) {
			t.Errorf("the control plane's refusal page answers %s=%q, without %s; a person shown this page is at the installation host, where every document is protected (%v)",
				guard, got, protection, refused.header)
		}
	}
	if !strings.Contains(policy, "'nonce-") {
		t.Errorf("the control plane's refusal page answers a policy with no nonce for this response, so the page cannot run its own scripts: %q", policy)
	}
}
