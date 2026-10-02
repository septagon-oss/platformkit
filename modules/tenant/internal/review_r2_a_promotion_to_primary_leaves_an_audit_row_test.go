package internal_test

// Review round 2 of T-0115, at the module: a promotion moves the routing table and leaves
// no record anywhere that it did.
//
// `AddHost` is also the platform's only way to choose a tenant's primary host: the host is
// already there, so the command takes its second branch — `promote`, an UPDATE of
// `tenant_hosts.is_primary` — and returns the tenant. The route's own OpenAPI description
// says what that branch is for: "Adding a host the tenant already answers at changes
// nothing, **unless it makes it the primary one**. The primary host is what every absolute
// URL for this tenant is built on, so a link in a mail is a link to the name its people
// know."
//
// Every other write in this module is recorded in the subject tenant's trail and in the
// installation's (`record`, and the README's "Every lifecycle verb publishes its event in
// the subject tenant's scope and `tenant.lifecycle_recorded` in the operator tenant's").
// This branch publishes nothing at all: `promote` runs and the command returns without
// `audience`, without `record` and without an event. An operator moved which name a
// customer's links are built on, and neither trail can say it happened, by whom, or when.
//
// The case reaches its assertion through the row and the outbox, not through any message:
// the promotion is confirmed by the host list the tenant answers with afterwards, and the
// record is a count of `tenant.host_added` rows taken before and after the call, so the
// case cannot be satisfied by a request that did nothing. It passes as soon as the
// promotion publishes what the same change publishes when the host arrives for the first
// time — `tenant.host_added` with `primary` true, and its mirror — and nothing else moves.
//
// Filed against this branch because the branch put the mirror behind this command for the
// other branch of the same `if`, and because its README states the rule this branch does
// not follow; the silence itself predates it (at `dc4c5a9` the branch published nothing
// either, and asked no audience question because none existed).

import (
	"context"
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

func TestAPromotionToPrimaryLeavesAnAuditRow(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	svc := internal.NewService(nil, nil)
	installed(t, conn, svc)
	ctx := trace.With(t.Context(), trace.New())

	var customer uuid.UUID
	if err := dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		created, err := svc.Create(ctx, tx, contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"})
		if err != nil {
			return err
		}
		customer = created.ID
		_, err = svc.AddHost(ctx, tx, customer, "www.acme.example.com", false)
		return err
	}); err != nil {
		t.Fatalf("create a tenant that answers at two names: %v", err)
	}

	err := dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var before int64
		if err := tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventHostAdded).
			Count(&before).Error; err != nil {
			return err
		}
		got, err := svc.AddHost(ctx, tx, customer, "www.acme.example.com", true)
		if err != nil {
			return err
		}
		if len(got.Hosts) == 0 || got.Hosts[0] != "www.acme.example.com" {
			t.Errorf("the promotion left the host list %v, want www.acme.example.com first: the case is about a "+
				"change that happened, so it is asserted as a change", got.Hosts)
		}
		var after int64
		if err := tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventHostAdded).
			Count(&after).Error; err != nil {
			return err
		}
		if after <= before {
			t.Errorf("making another host the primary one wrote no %s row: %d before, %d after. The primary host is "+
				"the name every absolute URL for this tenant is built on, and the trail says nothing about the moment "+
				"it moved, in either tenant.", contracts.EventHostAdded, before, after)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("promote the tenant's second host: %v", err)
	}
}
