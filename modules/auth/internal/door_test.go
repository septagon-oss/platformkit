package internal_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// TestAnUnknownAddressAnInvitedPersonAndAnUnverifiedAccountGetTheSameSentence is
// rule 2's two halves in one case: the answer stays the single answer it always
// was — three states, one body, so a stranger with a form learns nothing about
// which addresses this tenant has — and that one answer now carries the next
// step, because a person whose only sentence was "those credentials are not
// right" had nowhere to go.
//
// The unverified account is the one that could have leaked: it has a password, so
// a body that distinguished it would be an oracle. The byte-for-byte comparison is
// the whole test; the sentence check is what says the refusal is now useful.
func TestAnUnknownAddressAnInvitedPersonAndAnUnverifiedAccountGetTheSameSentence(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	somebodyWhoCannotSignInYet(t, conn, "invited@acme.localhost", false)
	somebodyWhoCannotSignInYet(t, conn, "unverified@acme.localhost", true)

	bodies := map[string]string{}
	for _, email := range []string{"nobody@acme.localhost", "invited@acme.localhost", "unverified@acme.localhost"} {
		res := call(t, router, http.MethodPost, "/api/v1/auth/login",
			`{"email":"`+email+`","password":"`+authtest.Password+`"}`)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("%s: sign-in = %d %s, want 401", email, res.Code, res.Body.String())
		}
		if cookie := sessionCookie(res); cookie != "" {
			t.Errorf("%s: a refused sign-in set a session cookie %s", email, cookie)
		}
		bodies[email] = withoutInstance(res.Body.String())
	}
	if bodies["nobody@acme.localhost"] != bodies["invited@acme.localhost"] ||
		bodies["invited@acme.localhost"] != bodies["unverified@acme.localhost"] {
		t.Errorf("the three refusals differ, which is an enumeration oracle:\n  unknown:    %s\n  invited:    %s\n  unverified: %s",
			bodies["nobody@acme.localhost"], bodies["invited@acme.localhost"], bodies["unverified@acme.localhost"])
	}
	// The refusal names what to do next, and names it by what the link is rather
	// than by its label: the label is translated and this sentence is not, so a
	// quoted label is a phrase the person may not be able to find on the page.
	for _, want := range []string{"are not right", "forgotten-password link under this form"} {
		if !strings.Contains(bodies["nobody@acme.localhost"], want) {
			t.Errorf("the one sentence omits %q; a refusal at the door names what to do next:\n  %s",
				want, bodies["nobody@acme.localhost"])
		}
	}
}

// somebodyWhoCannotSignInYet writes an account that a password login must refuse:
// invited (no password at all) or unverified (a password, an address nobody
// confirmed). Both are ordinary states on the way in, and both are reached the way
// the modules reach them rather than by updating the row behind their back.
func somebodyWhoCannotSignInYet(t *testing.T, conn *db.Conn, email string, unverified bool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		users := realUsers()
		var u *usercontracts.User
		var err error
		if unverified {
			u, err = users.RegisterUnverified(ctx, tx, usercontracts.PasswordRegistration{
				Email: email, DisplayName: "Waiting", Password: authtest.Password,
			})
		} else {
			u, err = users.Invite(ctx, tx, email, "Waiting")
		}
		if err != nil {
			return err
		}
		id = u.ID
		return nil
	})
	if err != nil {
		t.Fatalf("write %s: %v", email, err)
	}
	return id
}
