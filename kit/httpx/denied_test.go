package httpx_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

// TestADenialWithSomebodyToAttributeItToIsHandedToTheComposition is the kernel's edge of it:
// the kernel's edge: a refused authorization is no longer only a log line. A signed-in
// caller in a resolved tenant who lacks the grant is refused as before, and Options.Denied
// is handed who, where, which operation, the code and the request id — which kit/app turns
// into a security.denied event and, through modules/audit, an audit row. An anonymous
// refusal is not handed over: it has no account to attribute it to, and a row per probe
// would make every stranger's request a database write.
func TestADenialWithSomebodyToAttributeItToIsHandedToTheComposition(t *testing.T) {
	api, router, f := setup(t)
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "read-billing", Method: http.MethodGet, Path: "/billing",
	}, httpx.Permission("billing:read"), ok)

	// Anonymous: refused, and nothing is handed over.
	if res := request(t, router, http.MethodGet, at(api, "/billing"), host); res.Code != http.StatusForbidden {
		t.Fatalf("an anonymous read answered %d", res.Code)
	}
	if len(f.denials) != 0 {
		t.Fatalf("an anonymous refusal was handed to the composition: %+v", f.denials)
	}

	// Signed in, without the grant: refused, and handed over once, fully described.
	f.signedIn()
	f.allow = false
	res := get(t, router, at(api, "/billing"))
	if res.Code != http.StatusForbidden {
		t.Fatalf("a read without the grant answered %d", res.Code)
	}
	if len(f.denials) != 1 {
		t.Fatalf("the refusal was handed over %d times, want once", len(f.denials))
	}
	d := f.denials[0]
	switch {
	case d.Status != http.StatusForbidden || d.Code != httpx.CodeDenied:
		t.Errorf("the denial is %d %q, want 403 %q", d.Status, d.Code, httpx.CodeDenied)
	case d.Operation != "read-billing" || d.Method != http.MethodGet || !strings.HasSuffix(d.Path, "/billing"):
		t.Errorf("the denial names %s %s %q", d.Method, d.Path, d.Operation)
	case d.Principal.UserID != f.principal.UserID || d.Tenant.ID != f.tenant.ID:
		t.Errorf("the denial is attributed to %s in %s, want %s in %s", d.Principal.UserID, d.Tenant.Slug, f.principal.UserID, f.tenant.Slug)
	case d.RequestID == "" || res.Header().Get("X-Request-Id") != d.RequestID:
		t.Errorf("the denial's request id %q is not the response's %q", d.RequestID, res.Header().Get("X-Request-Id"))
	case !strings.Contains(d.Detail, "billing:read"):
		t.Errorf("the denial's detail %q does not name the grant", d.Detail)
	}

	// Allowed: nothing is handed over.
	f.allow = true
	if res := get(t, router, at(api, "/billing")); res.Code != http.StatusOK {
		t.Fatalf("an allowed read answered %d", res.Code)
	}
	if len(f.denials) != 1 {
		t.Errorf("an allowed request was handed over as a denial: %+v", f.denials[1:])
	}
}
