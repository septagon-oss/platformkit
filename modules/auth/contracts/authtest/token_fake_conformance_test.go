package authtest_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

// TestFakeConformsToTheTokenRules runs the key suite against the fake, whose
// bearer recognition is Fake.Bearer.
func TestFakeConformsToTheTokenRules(t *testing.T) {
	authtest.RunTokens(t, func(t *testing.T, run func(authtest.TokenFixture)) {
		users := usertest.NewFake()
		fake := authtest.NewFake(users)
		ctx, tx := t.Context(), db.Tx[db.Tenant]{}
		fake.Grant(contracts.RoleAdmin, contracts.Wildcard)
		fake.Grant(contracts.RoleMember)
		run(authtest.TokenFixture{
			Fixture: authtest.Fixture{
				Ctx: ctx, Tx: tx, Service: fake, Published: fake.Published, Role: fake.Grant,
				User: func(email, password string, roles ...string) uuid.UUID {
					u, err := users.Invite(ctx, tx, email, "")
					if err != nil {
						t.Fatalf("invite %s: %v", email, err)
					}
					if err := users.SetPassword(ctx, tx, u.ID, password); err != nil {
						t.Fatalf("set a password for %s: %v", email, err)
					}
					if _, err := users.SetRoles(ctx, tx, u.ID, roles); err != nil {
						t.Fatalf("grant %v to %s: %v", roles, email, err)
					}
					return u.ID
				},
			},
			Tokens: fake,
			Caller: func(token string) (tenancy.Principal, bool) {
				caller, ok, err := fake.Bearer(ctx, tx, token)
				if err != nil {
					t.Fatalf("Bearer: %v", err)
				}
				return caller, ok
			},
			Expire: fake.ExpireToken,
		})
	})
}
