package internal_test

import (
	"context"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
)

type allowedLocaleManager struct{}

func (allowedLocaleManager) MaySetOwnLocale(context.Context, db.Tx[db.Tenant]) (bool, error) {
	return true, nil
}

func TestConcurrentTenantLanguageDoorsAccountForEveryAcceptedRevision(t *testing.T) {
	owner, conn := dbtest.Schema(t)
	svc := internal.NewServiceWithLocaleManager(nil, []string{"en", "pt-PT"}, allowedLocaleManager{})
	var tenant *contracts.Tenant
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		tenant, err = svc.Create(ctx, tx, contracts.NewTenant{
			Slug: "acme", Name: "Acme", Host: "acme.example.com",
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	ready := make(chan error, 1)
	selfDone := make(chan error, 1)
	go func() {
		selfDone <- db.Run(tenancy.WithTenant(ctx, tenant.Tenancy()), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := svc.SetOwnLocale(ctx, tx, 0, contracts.SetLocale{Default: "pt-PT"})
				ready <- err
				if err != nil {
					return err
				}
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatalf("tenant's declaration: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("tenant's declaration did not reach its held transaction: %v", ctx.Err())
	}

	operatorDone := make(chan error, 1)
	go func() {
		operatorDone <- dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			if err := tx.DB().Exec("SET LOCAL application_name TO 'locale_revision_conflict'").Error; err != nil {
				return err
			}
			_, err := svc.SetLocale(ctx, tx, tenant.ID,
				contracts.SetLocale{Default: "en", Supported: []string{"pt-PT"}})
			return err
		})
	}()
	blocked := false
	tick := time.Tick(10 * time.Millisecond)
	for !blocked {
		var waiting int
		if err := owner.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE application_name = 'locale_revision_conflict' AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		blocked = waiting > 0
		if blocked {
			break
		}
		select {
		case err := <-operatorDone:
			t.Fatalf("the operator did not queue behind the tenant's row lock: %v", err)
		case <-tick:
		case <-ctx.Done():
			t.Fatalf("the operator never queued behind the tenant's row lock: %v", ctx.Err())
		}
	}
	close(release)
	if err := <-selfDone; err != nil {
		t.Fatalf("tenant commit: %v", err)
	}
	operatorErr := <-operatorDone
	var revision, events int64
	if err := owner.QueryRowContext(ctx,
		`SELECT locale_revision FROM tenants WHERE id = $1`, tenant.ID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(ctx,
		`SELECT count(*) FROM platformkit_outbox WHERE name = 'tenant.locale_set' AND tenant_id = $1`, tenant.ID).
		Scan(&events); err != nil {
		t.Fatal(err)
	}
	want := int64(1)
	if operatorErr == nil {
		want = 2
	}
	if revision != want || events != want {
		t.Errorf("the two doors left revision %d and %d locale_set events (operator error %v), want %d accepted declarations accounted for",
			revision, events, operatorErr, want)
	}
}
