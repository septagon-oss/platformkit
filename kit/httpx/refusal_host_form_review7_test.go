package httpx_test

// A reviewer's case, T-0111 review round 7 (2026-09-30).
//
// `refusal_tenant_test.go` (this round's own) pins that a refusal rendered as a page is handed
// the tenant of its address. It asks with `httptest.NewRequest(..., "http://"+host+"/...")`, so
// the request's Host arrives already spelled the way the loader's key is spelled: lower-cased,
// no port, no trailing dot.
//
// The line that resolves the refusal's host is `a.resolve(ctx, HostOnly(r.Host))`, and
// `HostOnly` is documented as the one normalisation every TenantLoader and every caller shares
// ("two normalisations that drift is a domain that resolves for nobody"). A refusal that skipped
// it would not fail loudly: it would resolve nothing, leave the request as it arrived, and answer
// the person from the deployment's whole catalogue — the exact answer `kit/httpx/README.md` calls
// the case of "a host nobody serves". An address served to a tenant that declared one language
// would then be worded by whoever brought the header, and every case in this package would stay
// green because every one of them spells the host the easy way already.
//
// So this asks for the same 404 with the address spelled the way a client's Host header often
// arrives — upper case, port, trailing dot — and insists the renderer is handed the tenant anyway.
// It reaches the assertion through the verdict and the media type (a page, 404), which is what the
// correct behaviour prints whatever the language it ends up negotiating.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestARefusalNormalisesItsHostBeforeAskingWhoseTenantItIs — the same lookup the request
// middleware makes, or the refusal is negotiated from the header alone.
func TestARefusalNormalisesItsHostBeforeAskingWhoseTenantItIs(t *testing.T) {
	for _, spelled := range []struct{ name, header string }{
		{"with a port", "ACME.TEST:8443"},
		{"with a trailing dot", "acme.test."},
		{"in capitals", "ACME.TEST"},
	} {
		t.Run(spelled.name, func(t *testing.T) {
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
			req.Host = spelled.header
			req.Header.Set("Accept", browserAccept)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Fatalf("%s: an address nobody mounted answered %d, so no refusal reached the "+
					"renderer at all: %s", spelled.name, w.Code, w.Body.String())
			}
			if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("%s: a navigating caller was answered %q, so no page was rendered: %s",
					spelled.name, ct, w.Body.String())
			}
			if !resolved {
				t.Errorf("%s: the renderer of a refusal was handed no tenant, so its language comes "+
					"from Accept-Language alone and the address's own tenant is not consulted — the "+
					"loader's key is HostOnly(host), and %q is that host spelled as a client spelled it",
					spelled.name, spelled.header)
			}
			if resolved && askedFor.Slug != "acme" {
				t.Errorf("%s: the refusal was handed the tenant %q, not the one this host resolves to",
					spelled.name, askedFor.Slug)
			}
		})
	}
}
