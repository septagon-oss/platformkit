package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/modules/user/internal"
)

// TestErasingTheLastAdministratorIsRefused erases the only person who can administer
// the tenant: the erasure is refused with the floor's reason, and the row still names
// the person, because a refused erasure writes nothing.
func TestErasingTheLastAdministratorIsRefused(t *testing.T) {
	conn, ctx, _ := tenantWatched(t)
	svc := newService()
	ada := administratorIn(t, ctx, conn, svc, "ada@acme.example.com")
	subject := personaldata.Subject{ID: ada, Email: "ada@acme.example.com"}

	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData(svc).Data.Erase(ctx, tx, subject)
	})
	if !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("erasing the last administrator = %v, want ErrInvalid", err)
	}
	if got := administratorsIn(t, ctx, conn); len(got) != 1 || got[0] != "ada@acme.example.com" {
		t.Errorf("after a refused erasure the administrators are %v", got)
	}
}

// TestErasingAndStandingDownQueueBehindEachOther is the floor's race through the
// erasure's door: one administrator stands down and holds the transaction open while
// the other is erased. The erasure waits on the tenant's administration lock and is
// refused once the stand-down commits.
func TestErasingAndStandingDownQueueBehindEachOther(t *testing.T) {
	conn, ctx, admin := tenantWatched(t)
	svc := newService()
	ada := administratorIn(t, ctx, conn, svc, "ada@acme.example.com")
	grace := administratorIn(t, ctx, conn, svc, "grace@acme.example.com")

	written, release := make(chan struct{}), make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if _, err := svc.SetRoles(ctx, tx, ada, nil); err != nil {
				return err
			}
			close(written)
			<-release
			return nil
		})
	}()
	<-written

	second := make(chan error, 1)
	go func() {
		second <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return internal.PersonalData(svc).Data.Erase(ctx, tx,
				personaldata.Subject{ID: grace, Email: "grace@acme.example.com"})
		})
	}()

	blockedOnAdministration(t, admin, acme.ID)
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("the stand-down = %v, want it allowed", err)
	}
	if err := <-second; !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("the erasure = %v, want ErrInvalid", err)
	}
	if got := administratorsIn(t, ctx, conn); len(got) == 0 {
		t.Error("nobody can administer this tenant any more; the stand-down and the erasure were both allowed")
	}
}
