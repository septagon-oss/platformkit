package internal_test

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
)

// TestATenantThatTurnedSingleSignOnOffIsNotServedTheDoor is the other half of
// the same dropped field, and the half that is a refusal rather than a feature.
//
// migrations/000030 says `disabled` "is a tenant saying 'single sign-on is not
// what we use here', which the module answers with 404 before it dials anything";
// internal/oidc.go repeats it at both legs ("so 'disabled' is not a page that
// discovers an issuer it will not use"); contracts/oidc_provider.go calls the
// value one of the three answers a tenant gives for itself; the control-plane
// route documents it to an operator. Every one of those sentences is about a
// branch selected by `cfg.mode()`, and `mode()` reads `OIDC.Registration`.
//
// The case drives the door of a tenant whose row says `disabled` and asks two
// things of it: the answer is 404, and the identity provider is never asked for
// anything. Both are assertions about the refusal the tenant chose — neither is
// reachable through what the door answers today, so the case says nothing about
// the shape of the wrong answer and will pass, unchanged, on any fix that makes
// the row mean what the module says it means.
func TestATenantThatTurnedSingleSignOnOffIsNotServedTheDoor(t *testing.T) {
	idp := authtest.NewIssuer(t)
	dials := 0
	original := idp.Server.Config.Handler
	idp.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dials++
		original.ServeHTTP(w, r)
	})

	router, _ := mountTwo(t, mapProviders{
		acme.ID: {Issuer: idp.URL, ClientID: "platformkit", SecretRef: "ACME_SECRET",
			RedirectPath: "/api/v1/auth/oidc/callback", Registration: contracts.RegistrationDisabled},
	}, mapSecrets{"ACME_SECRET": "acme-secret"})

	res := get(t, router, host, "/api/v1/auth/oidc/start")
	if res.Code != http.StatusNotFound {
		t.Errorf("a tenant that turned single sign-on off was served %d at its start door, want 404: %s",
			res.Code, res.Body.String())
	}
	if res.Code == http.StatusSeeOther {
		t.Error("a tenant that turned single sign-on off was redirected to its identity provider")
	}

	// And the callback: the same row, the second leg. A door that is closed on
	// the way in is closed on the way back, or it is a way in.
	res = get(t, router, host, "/api/v1/auth/oidc/callback?code=anything&state=anything")
	if res.Code == http.StatusSeeOther {
		t.Error("the callback opened a session for a tenant that turned single sign-on off")
	}

	if dials != 0 {
		t.Errorf("the refusal asked the identity provider for %d things; a tenant that says it does not sign in here should not cause a discovery request", dials)
	}
}
