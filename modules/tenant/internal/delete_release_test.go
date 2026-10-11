package internal_test

// T-0115 review round 1, finding 1 — the author's own case for the behaviour the
// review demanded.
//
// `review_r1_a_retired_tenants_host_test.go` asks for exactly this and cannot run:
// its helper is `fail := func(what string, err error) { t.Fatalf("%s: %v", what, err) }`,
// with no `if err != nil` around the Fatal, so it fails at its first call whatever
// the code does, printing the error it was handed as `<nil>`. That file belongs to
// the review and decision 0008 keeps my hands off it, so the behaviour it demands
// is asserted here — over the same three facts it names (the retired tenant's host
// is servable by another customer, `ByHost` resolves that name to the new tenant,
// and the retired row survives with `deleted_at` set so the cure cannot be bought
// by erasing the customer), plus the one claim the cure adds beyond them: the
// `tenant.deleted` row names the names it released, because after this write the
// routing table no longer says which customer answered at them.

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/trace"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestADeleteReleasesAndNamesTheHostsItHeld(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	svc := internal.NewService(nil, nil, "")
	installed(t, conn, svc)
	ctx := trace.With(t.Context(), trace.New())

	var retired, successor uuid.UUID
	var released []string
	deleted := struct {
		TenantID uuid.UUID
		Hosts    []string
	}{}
	if err := dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		created, err := svc.Create(ctx, tx, contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"})
		if err != nil {
			return err
		}
		retired = created.ID
		if _, err := svc.AddHost(ctx, tx, retired, "www.acme.example.com", false); err != nil {
			return err
		}
		got, err := svc.Delete(ctx, tx, retired, contracts.Delete{Confirm: "acme"})
		if err != nil {
			return err
		}
		// The list the verb returns is the list it released: the route invalidates
		// exactly these cached resolutions, and nothing reads it afterwards,
		// because a retired tenant is not found by any read.
		released = got.Hosts
		var payload string
		if err := tx.DB().Table("platformkit_outbox").Select("payload::text").
			Where("name = ?", contracts.EventDeleted).Scan(&payload).Error; err != nil {
			return err
		}
		return json.Unmarshal([]byte(payload), &deleted)
	}); err != nil {
		t.Fatalf("retire a tenant that answered at two names: %v", err)
	}

	if want := []string{"acme.example.com", "www.acme.example.com"}; !slices.Equal(released, want) {
		t.Errorf("the delete released %v, want both names it answered at %v", released, want)
	}
	if !slices.Equal(deleted.Hosts, released) {
		t.Errorf("the %s row names %v, want the released hosts %v: the routing table stops saying whose name this was, so the trail has to",
			contracts.EventDeleted, deleted.Hosts, released)
	}

	// The customer that arrives next, at the name the retired one answered at — the
	// primary one, which `RemoveHost` could never have reached on a retired tenant.
	if err := dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		created, err := svc.Create(ctx, tx, contracts.NewTenant{
			Slug: "second", Name: "Second Customer", Host: "acme.example.com",
		})
		if err != nil {
			return err
		}
		successor = created.ID
		resolved, err := svc.ByHost(ctx, tx, "acme.example.com")
		if err != nil {
			return err
		}
		if resolved.ID != successor {
			t.Errorf("acme.example.com resolves to %s, want the tenant just hosted there (%s)", resolved.ID, successor)
		}
		var held int64
		if err := tx.DB().Table("tenant_hosts").Where("tenant_id = ?", retired).Count(&held).Error; err != nil {
			return err
		}
		if held != 0 {
			t.Errorf("the retired tenant still holds %d host rows, want none: the name would be reserved by a customer nobody can see", held)
		}
		return nil
	}); err != nil {
		t.Fatalf("host a new tenant at a retired tenant's name: %v", err)
	}

	// And the retired customer is still there, with its row retired rather than
	// erased: this is the half the release must not cost.
	var kept int64
	if err := dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Table("tenants").Where("id = ? AND deleted_at IS NOT NULL", retired).Count(&kept).Error
	}); err != nil {
		t.Fatalf("count the retired tenant: %v", err)
	}
	if kept != 1 {
		t.Errorf("the retired customer's row is %d, want it kept with deleted_at set", kept)
	}
}
