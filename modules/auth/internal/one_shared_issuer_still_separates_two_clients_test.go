package internal_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
)

// TestOneSharedIssuerStillSeparatesTwoClients is the multi-tenant shape the
// two-issuer case cannot reach: one identity provider for the whole installation —
// a single Keycloak realm, the arrangement most of these deployments actually
// run — with each tenant registered as its own client, so the discovery cache is
// shared by construction.
//
// Keying that cache by issuer is right for the document; it is only safe if the
// exchange and the id-token verification use *this* tenant's client. If anything
// in that path is taken from the cached provider rather than from the row the
// request resolved, a person who is registered at the shared door arrives in
// whichever tenant's host they were sent to, and the tenant boundary in the
// sign-in is one string in a redirect. The two-issuer test cannot see this: nothing
// is shared there, so a wrong cache key is the only failure it can catch, and the
// one failure it catches.
//
// Both people exist, in their own tenants, before any of this: the control leg
// completing proves the door works for the tenant whose client minted the token,
// and it is what makes the refusal below a refusal of the *audience* rather than
// of an address nobody has. The case then asks for the status and for the absence
// of a session cookie — the state and the code, never the sentence the refusal
// prints.
func TestOneSharedIssuerStillSeparatesTwoClients(t *testing.T) {
	idp := authtest.NewIssuer(t)
	router, conn := mountTwo(t, mapProviders{
		acme.ID: {Issuer: idp.URL, ClientID: "acme-portal", SecretRef: "SHARED_SECRET",
			RedirectPath: "/api/v1/auth/oidc/callback"},
		globex.ID: {Issuer: idp.URL, ClientID: "globex-portal", SecretRef: "SHARED_SECRET",
			RedirectPath: "/api/v1/auth/oidc/callback"},
	}, mapSecrets{"SHARED_SECRET": "one-secret"})

	person(t, conn, "ada@acme.example.com", contracts.RoleMember)
	// globex's person, activated: a password of their own, the status that lets a
	// session open. This case is about which client a token was minted for; the
	// separate question of whether an *invited* person — the shape a `provision`
	// tenant makes, and the shape a single-sign-on-only person is — may sign in at
	// all is reported on its own, and this case must not depend on its answer.
	err := db.Run(tenancy.WithTenant(t.Context(), globex), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			users := realUsers()
			u, err := users.Invite(ctx, tx, "ada@globex.example.com", "Ada L.")
			if err != nil {
				return err
			}
			return users.SetPassword(ctx, tx, u.ID, authtest.Password)
		})
	if err != nil {
		t.Fatalf("give globex a person: %v", err)
	}

	_, acmeState, acmeCookie, acmeStart := start(t, router, host)
	if acmeStart != http.StatusSeeOther {
		t.Fatalf("acme's start = %d, want 303", acmeStart)
	}
	_, globexState, globexCookie, globexStart := start(t, router, secondHost)
	if globexStart != http.StatusSeeOther {
		t.Fatalf("globex's start = %d, want 303", globexStart)
	}

	// The control: globex's own person, globex's own client, globex's host, at
	// the issuer both tenants name. If this is refused, the refusal below proves
	// nothing, so it is a Fatalf.
	idp.Issue("home", "ada@globex.example.com", true, "globex-portal", nonce(globexCookie))
	if res := callback(t, router, secondHost, "home", globexState, globexCookie); res.Code != http.StatusSeeOther {
		t.Fatalf("a tenant's own client at the shared issuer = %d %s, want 303", res.Code, res.Body.String())
	}

	// The same door, the same address, the other tenant's client in the
	// audience. A session here would mean one client's token opened another
	// tenant's account.
	idp.Issue("cross", "ada@globex.example.com", true, "acme-portal", nonce(globexCookie))
	res := callback(t, router, secondHost, "cross", globexState, globexCookie)
	if res.Code == http.StatusSeeOther {
		t.Errorf("a token minted for acme-portal completed a session at globex through the shared issuer (Location %s)",
			res.Header().Get("Location"))
	}
	if res.Code != http.StatusForbidden {
		t.Errorf("an id token bearing another client's audience = %d %s, want 403", res.Code, res.Body.String())
	}
	if cookie := sessionCookie(res); cookie != "" {
		t.Error("the refusal set a session cookie")
	}

	// And acme's own leg at the very same shared issuer still works, so the
	// refusal above is not one tenant having poisoned a cache everybody reads.
	idp.Issue("acme-home", "ada@acme.example.com", true, "acme-portal", nonce(acmeCookie))
	if res := callback(t, router, host, "acme-home", acmeState, acmeCookie); res.Code != http.StatusSeeOther {
		t.Errorf("acme's own client at the shared issuer = %d %s, want 303", res.Code, res.Body.String())
	}
}
