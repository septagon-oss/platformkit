package authtest_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

// TestFakeConforms runs the suite against the fake, over the user module's own
// fake. Two fakes, both of them conforming to their own suite, is what lets a
// consumer test a signed-in flow without a database.
func TestFakeConforms(t *testing.T) {
	authtest.RunService(t, func(t *testing.T, run func(authtest.Fixture)) {
		users := usertest.NewFake()
		fake := authtest.NewFake(users)
		notices, box := &authtest.Notices{}, &authtest.Mailbox{}
		fake.Notify, fake.Mailer = notices, box
		ctx, tx := t.Context(), db.Tx[db.Tenant]{}
		fake.Grant(contracts.RoleAdmin, contracts.Wildcard)
		fake.Grant(contracts.RoleMember)
		run(authtest.Fixture{
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
		})
	})
}

// TestTheFakeKeepsTheLockout is what makes it worth having in contracts rather
// than in internal: the limit is part of what Login promises, so a consumer
// testing against the fake meets the same refusal a deployment would.
func TestTheFakeKeepsTheLockout(t *testing.T) {
	users := usertest.NewFake()
	fake := authtest.NewFake(users)
	ctx, tx := t.Context(), db.Tx[db.Tenant]{}
	u, err := users.Invite(ctx, tx, "ada@acme.example.com", "")
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if err := users.SetPassword(ctx, tx, u.ID, authtest.Password); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	for range contracts.MaxAttempts {
		_, _, _ = fake.Login(ctx, tx, "ada@acme.example.com", authtest.Wrong, contracts.Client{})
	}
	if _, _, err := fake.Login(ctx, tx, "ada@acme.example.com", authtest.Password, contracts.Client{}); err != contracts.ErrTooManyAttempts {
		t.Errorf("the fake let a locked-out address in: %v", err)
	}
	var _ usercontracts.Service = users
}
