package internal_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

// mailWiring is one installation's answer about its carrier, handed to both
// implementations of the auth contract at once: what a composition Put for
// contracts.Mailer and what it Put for contracts.MailDelivery. The two ports are
// one object in every wired installation and different objects in a hand-built
// Deps, and it is the hand-built Deps where an implementation that asks the wrong
// field drifts.
type mailWiring struct {
	name   string
	sender func() contracts.Mailer
	port   func() contracts.MailDelivery
	// refused is whether an installation wired this way may promise a person an
	// email. It is the deployment's fact, so both implementations answer it alike.
	refused bool
}

// TestTheFakeAndTheServiceAnswerOneMailWiringAlike asks the forgotten-password
// door of the fake and of the service the same wiring, four wirings, and requires
// one answer from the pair.
//
// The pair is the point. The fake stands in for auth in every consumer module's
// tests, so a fake that admits a request the service refuses — or refuses one the
// service admits — makes those tests pass over a promise the real installation
// would not keep, and the conformance suite only sees the wirings it is run with.
// The wiring that separates the two rules is a sender that is wired while nobody
// wired the port that answers for it: a recorder asked about itself says it
// delivers, so a fake that asks the sender opens the door, and the service, which
// asks the port, must refuse. A port nobody wired is read as "nothing leaves", the
// same fail-closed answer as a sender nobody wired.
func TestTheFakeAndTheServiceAnswerOneMailWiringAlike(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	seed(t, conn, acme)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	const address = "someone@acme.localhost"

	for _, wiring := range []mailWiring{
		{name: "no sender and no port", sender: func() contracts.Mailer { return nil },
			port: func() contracts.MailDelivery { return nil }, refused: true},
		{name: "a sender with no port answering for it", sender: func() contracts.Mailer { return &authtest.Mailbox{} },
			port: func() contracts.MailDelivery { return nil }, refused: true},
		{name: "the recorder in both ports, saying nothing leaves",
			sender: func() contracts.Mailer { return &authtest.Mailbox{Undeliverable: true} },
			port:   func() contracts.MailDelivery { return authtest.Undelivers }, refused: true},
		{name: "the recorder in both ports, saying it dials",
			sender: func() contracts.Mailer { return &authtest.Mailbox{} },
			port:   func() contracts.MailDelivery { return authtest.Delivers }, refused: false},
	} {
		t.Run(wiring.name, func(t *testing.T) {
			// The service, reached the way a composition reaches it: through the
			// module's own Deps, so the answer is also the answer the wiring carries.
			users := realUsers()
			service, _ := auth.New(auth.Deps{
				Users: users, Notify: &authtest.Notices{}, Mailer: wiring.sender(),
				MailDelivery: wiring.port(), Hosts: authtest.Host(host), PublicHost: host,
				EmailRegistration: &contracts.EmailRegistration{
					Users: users.(contracts.EmailRegistrar), Roles: []string{"member"},
				},
			})
			var serviceErr, fakeErr error
			err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				serviceErr = service.Forget(ctx, tx, address)
				return nil
			})
			if err != nil {
				t.Fatalf("the service's transaction: %v", err)
			}

			fake := authtest.NewFake(usertest.NewFake())
			fake.Mailer, fake.MailDelivery = wiring.sender(), wiring.port()
			fakeErr = fake.Forget(ctx, db.Tx[db.Tenant]{}, address)

			promise := "delivers, so a door that promises a link is open"
			if wiring.refused {
				promise = "delivers nothing, so a door that promises a link refuses"
			}
			if got := refusedForNoMail(serviceErr); got != wiring.refused {
				t.Errorf("the service stands in the installation whose carrier %s and answered %q with %v",
					promise, wiring.name, serviceErr)
			}
			if got := refusedForNoMail(fakeErr); got != wiring.refused {
				t.Errorf("the fake stands in the installation whose carrier %s and answered %q with %v, where the service answered %v",
					promise, wiring.name, fakeErr, serviceErr)
			}
			if !wiring.refused {
				return
			}
			// A refused mutation writes nothing: not the token, and not the event
			// whose acknowledgment the door withheld with it.
			var held int64
			err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return tx.DB().Table("platformkit_outbox").
					Where("name = ?", contracts.EventResetRequested).Count(&held).Error
			})
			if err != nil {
				t.Fatalf("read the outbox: %v", err)
			}
			if held != 0 {
				t.Errorf("%s published %d %s rows, want none beside a refused door", wiring.name, held,
					contracts.EventResetRequested)
			}
		})
	}
}

// refusedForNoMail is whether this error is the deployment's own unavailable
// refusal — the sentence and status every account-email door answers with where
// nothing is delivered — rather than some other failure of the same call.
func refusedForNoMail(err error) bool {
	var refused *problem.Problem
	return errors.As(err, &refused) && refused.GetStatus() == http.StatusServiceUnavailable &&
		refused.Detail == authtest.NoMailDetail
}
