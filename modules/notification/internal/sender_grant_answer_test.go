package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// grants is a composition's answer to "does this caller hold sender:manage".
type grants struct {
	held bool
	err  error
}

func (g grants) Holds(_ context.Context, _ db.Tx[db.Tenant], permission string) (bool, error) {
	if permission != contracts.PermissionSenderManage {
		return false, nil
	}
	return g.held, g.err
}

// TestSenderCommandsFollowTheGrantCheckersAnswer: a signed-in caller's Put is
// decided by the composition's GrantChecker — held writes the row and publishes
// sender_set; not held, or a checker that fails, writes nothing and publishes
// nothing; and a session whose actor is somebody else is refused before the
// checker is asked.
func TestSenderCommandsFollowTheGrantCheckersAnswer(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	person := uuid.New()
	signedIn := tenancy.WithPrincipal(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), person), tenancy.Principal{UserID: person})
	cases := []struct {
		name    string
		ctx     context.Context
		checker grants
		allowed bool
	}{
		{"held", signedIn, grants{held: true}, true},
		{"not held", signedIn, grants{}, false},
		{"checker failed", signedIn, grants{held: true, err: errors.New("roles store unavailable")}, false},
		{"actor is not the session", tenancy.WithActor(signedIn, uuid.New()), grants{held: true}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := senders()
			store.Grants = c.checker
			err := db.Run(c.ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				before := len(outbox(t, tx))
				row, err := store.Put(ctx, tx, sender())
				saved, readErr := store.For(ctx, tx)
				if readErr != nil {
					return readErr
				}
				published := len(outbox(t, tx)) - before
				if c.allowed {
					if err != nil || row == nil || saved == nil || published != 1 {
						t.Errorf("granted Put: err %v, row %v, saved %v, %d events; want the row and one sender_set", err, row != nil, saved != nil, published)
					}
				} else {
					if err == nil || row != nil || saved != nil || published != 0 {
						t.Errorf("refused Put: err %v, row %v, saved %v, %d events; want an error and nothing written", err, row != nil, saved != nil, published)
					}
					if c.checker.err == nil && !errors.Is(err, tenancy.ErrPolicyDenied) {
						t.Errorf("refused Put: got %v, want policy denial", err)
					}
				}
				return errRollback
			})
			if !errors.Is(err, errRollback) {
				t.Fatal(err)
			}
		})
	}
}
