package authtest_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

// TestFakeConformsToThePasskeyRules runs the ceremony suite against the fake.
//
// The answer seam is where the fake says what it is: an authenticator's body to
// this implementation is a credential id and nothing else, because a fake that
// checked a signature would be a second WebAuthn and would then be proving its
// own arithmetic. A signature-checking implementation plugs its own answer in at
// this one seam and runs the same table; the crypto itself is pinned where the
// real code runs, in modules/auth/internal.
func TestFakeConformsToThePasskeyRules(t *testing.T) {
	authtest.RunPasskeys(t, func(t *testing.T, run func(authtest.PasskeyFixture)) {
		users := usertest.NewFake()
		fake := authtest.NewFake(users)
		notices, box := &authtest.Notices{}, &authtest.Mailbox{}
		fake.Notify, fake.Mailer = notices, box
		ctx, tx := t.Context(), db.Tx[db.Tenant]{}
		fake.Grant(contracts.RoleAdmin, contracts.Wildcard)
		fake.Grant(contracts.RoleMember)
		run(authtest.PasskeyFixture{
			Fixture: authtest.Fixture{
				Ctx: ctx, Tx: tx, Service: fake, Published: fake.Published,
				Role: fake.Grant, Sent: notices.Sent, Mailed: box.Sent, Sessions: fake.SessionsOf,
				User: func(email, password string, roles ...string) uuid.UUID {
					u, err := users.Invite(ctx, tx, email, "")
					if err != nil {
						t.Fatalf("invite %s: %v", email, err)
					}
					if password != "" {
						if err := users.SetPassword(ctx, tx, u.ID, password); err != nil {
							t.Fatalf("set a password for %s: %v", email, err)
						}
					}
					if len(roles) > 0 {
						if _, err := users.SetRoles(ctx, tx, u.ID, roles); err != nil {
							t.Fatalf("grant %v to %s: %v", roles, email, err)
						}
					}
					return u.ID
				},
			},
			Passkeys: fake, Factors: fake,
			Answer: func(_ contracts.PasskeyChallenge, credential string) json.RawMessage {
				body, _ := json.Marshal(map[string]string{"id": credential})
				return body
			},
			AddFactor:      fake.AddFactor,
			ExpireCeremony: fake.ExpireCeremony,
			// The seam drives the command, not the boolean behind it: what the
			// suite asks of an implementation is "open the door", which is the
			// caller's question, and a case that reached for the field would be
			// testing the fake's own bookkeeping.
			EnablePasskeySignIn: func(enabled bool) {
				if _, err := fake.SetPasskeySignIn(ctx, tx, enabled); err != nil {
					t.Fatalf("SetPasskeySignIn(%v): %v", enabled, err)
				}
			},
		})
	})
}
