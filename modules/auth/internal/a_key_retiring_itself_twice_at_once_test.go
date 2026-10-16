package internal_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
)

// TestAKeyRetiringItselfTwiceAtOnceEndsOnceAndSaysSoOnce: two transactions spend
// one key at the same moment. Exactly one ends it and publishes the event; the
// other answers "gone" and commits no event, whichever of them reads first.
func TestAKeyRetiringItselfTwiceAtOnceEndsOnceAndSaysSoOnce(t *testing.T) {
	svc, conn, ctx := roleService(t)
	var caller tenancy.Principal
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		users := realUsers()
		ada, err := users.Invite(ctx, tx, "ada@acme.localhost", "")
		if err != nil {
			return err
		}
		if err := users.SetPassword(ctx, tx, ada.ID, authtest.Password); err != nil {
			return err
		}
		if _, err := users.SetRoles(ctx, tx, ada.ID, []string{contracts.RoleAdmin}); err != nil {
			return err
		}
		issued, err := svc.IssueToken(ctx, tx, ada.ID, contracts.TokenIntent{
			Name: "Phone", Scopes: []string{contracts.PermissionRoleManage},
		}, declared)
		if err != nil {
			return err
		}
		r := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/v1/auth/tokens/revoke-self", nil)
		r.Header.Set("Authorization", "Bearer "+issued.Token)
		var ok bool
		caller, ok, err = svc.Authenticate(ctx, tx, r)
		if err == nil && !ok {
			err = errors.New("a live key was not recognised")
		}
		return err
	})
	if err != nil {
		t.Fatalf("issue the key: %v", err)
	}

	start := make(chan struct{})
	results := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i] = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return svc.RevokeOwnToken(ctx, tx, caller)
			})
		}()
	}
	close(start)
	wg.Wait()

	var ended, gone int
	for _, err := range results {
		switch {
		case err == nil:
			ended++
		case errors.Is(err, crud.ErrNotFound):
			gone++
		default:
			t.Fatalf("a concurrent self-revoke failed: %v", err)
		}
	}
	if ended != 1 || gone != 1 {
		t.Errorf("two self-revokes of one key: %d ended it and %d answered gone, want 1 and 1", ended, gone)
	}
	var events int64
	err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventAPITokenRevoked).Count(&events).Error
	})
	if err != nil || events != 1 {
		t.Errorf("the outbox holds %d revocation events (%v), want 1", events, err)
	}
}
