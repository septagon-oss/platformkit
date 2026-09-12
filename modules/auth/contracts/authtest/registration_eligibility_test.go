package authtest_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

func TestFakeRecoveryCannotOfferPasswordSetupToEitherRegistrationGate(t *testing.T) {
	users := usertest.NewFake()
	auth := authtest.NewFake(users)
	box := &authtest.Mailbox{}
	auth.Mailer = box
	ctx, tx := t.Context(), db.Tx[db.Tenant]{}
	for i, register := range []func() (*user.User, error){
		func() (*user.User, error) {
			return users.RegisterPending(ctx, tx, user.PasswordRegistration{Email: "approval@example.com", Password: authtest.Password})
		},
		func() (*user.User, error) {
			return users.RegisterUnverified(ctx, tx, user.PasswordRegistration{Email: "verification@example.com", Password: authtest.Password})
		},
	} {
		u, err := register()
		if err != nil {
			t.Fatal(err)
		}
		if err := auth.Reissue(ctx, tx, u.Email); err != nil {
			t.Fatal(err)
		}
		if err := auth.Offer(ctx, tx, u.ID); err != nil {
			t.Fatal(err)
		}
		if len(box.Sent()) != 0 {
			t.Fatalf("registration gate %d received a password setup link", i)
		}
	}
}
