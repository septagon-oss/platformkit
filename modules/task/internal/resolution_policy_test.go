package internal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/internal"
)

type resolutionPolicy struct {
	err   error
	calls int
}

func (p *resolutionPolicy) Decide(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
	p.calls++
	return tenancy.PolicyDecision{}, p.err
}

func TestResolutionPolicyPrecedesRetriesAndConflicts(t *testing.T) {
	_, conn := dbtest.Schema(t)
	for _, status := range []string{contracts.StatusResolved, contracts.StatusClosed} {
		for _, access := range []struct {
			name string
			err  error
			want error
		}{
			{name: "denied", want: tenancy.ErrPolicyDenied},
			{name: "unavailable", err: errors.New("provider unavailable"), want: tenancy.ErrPolicyUnavailable},
		} {
			t.Run(status+"/"+access.name, func(t *testing.T) {
				policy := &resolutionPolicy{err: access.err}
				svc := internal.NewService()
				svc.Policy = policy
				ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), acme), tenancy.Principal{UserID: uuid.New()})
				if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					at := time.Now().UTC().Truncate(time.Microsecond)
					task := &contracts.Task{Title: "Completed", Status: status, Resolution: " done ", ResolvedAt: &at}
					if err := crud.Create(ctx, tx, task); err != nil {
						return err
					}
					for _, text := range []string{"", "done", "different"} {
						if got, err := svc.Resolve(ctx, tx, task.ID, text); got != nil || !errors.Is(err, access.want) {
							t.Fatalf("Resolve(%q) = %v, %v; want policy refusal %v", text, got, err, access.want)
						}
					}
					stored, err := crud.Get[*contracts.Task](tx, task.ID)
					if err != nil {
						return err
					}
					if policy.calls != 3 || stored.Status != status || stored.Resolution != " done " || stored.ResolvedAt == nil || !stored.ResolvedAt.Equal(at) {
						t.Fatalf("policy calls=%d; stored=%+v", policy.calls, stored)
					}
					var count int64
					if err := tx.DB().Table(outbox).Where("tenant_id = ?", acme.ID).Count(&count).Error; err != nil {
						return err
					}
					if count != 0 {
						t.Fatalf("denied resolution published %d events", count)
					}
					return errRollback
				}); !errors.Is(err, errRollback) {
					t.Fatal(err)
				}
			})
		}
	}
}
