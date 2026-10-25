package internal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestInvitationRoleAssignmentRechecksActorAndBearerAuthority(t *testing.T) {
	f := invitationSQL(t)
	f.clock.Advance(db.Now().Sub(f.clock.Now()))
	u := authtest.InvitationPerson(t, f.InvitationFixture)
	actor, _ := tenancy.PrincipalFrom(f.ctx)
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		roles := map[string][]string{"admin": {"*"}, "manager": {"user:manage", "user:read"}, "editor": {"content:read"}, "privileged": {"content:write"}}
		for name, grants := range roles {
			if err := tx.DB().Exec("INSERT INTO roles (tenant_id,name,permissions) VALUES (?,?,?)", db.TenantOf(tx).ID, name, pq.StringArray(grants)).Error; err != nil {
				return err
			}
		}
		return tx.DB().Exec("UPDATE users SET roles = ARRAY['manager'] WHERE id = ?", actor.UserID).Error
	}); err != nil {
		t.Fatal(err)
	}
	f.authority.Ask = f.service.InspectInvitationAuthority
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		got, err := f.Commands.SetInvitationRoles(ctx, tx, u.ID, 1, []string{"privileged"})
		if got != nil {
			t.Fatal("overgrant returned row")
		}
		return err
	}); !errors.Is(err, user.ErrInvitationForbidden) {
		t.Fatalf("overgrant = %v", err)
	}
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := f.Commands.SetInvitationRoles(ctx, tx, u.ID, 1, nil)
		return err
	}); err != nil {
		t.Fatalf("removing role required regrant: %v", err)
	}
	var issued *contracts.IssuedToken
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		issued, err = f.service.IssueToken(ctx, tx, actor.UserID, contracts.TokenIntent{Name: "read only", Scopes: []string{"user:read"}, ExpiresAt: db.Now().Add(time.Hour)}, []tenancy.Grant{{Permission: "user:read"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	actor.TokenID = issued.ID
	actor.Permissions = []string{"*"}
	ctx := tenancy.WithPrincipal(f.ctx, actor)
	if err := db.Run(ctx, f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := f.Commands.RevokeInvitation(ctx, tx, u.ID, 2)
		return err
	}); !errors.Is(err, user.ErrInvitationForbidden) {
		t.Fatalf("read-only bearer wrote: %v", err)
	}
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return f.service.RevokeToken(ctx, tx, actor.UserID, issued.ID)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := f.service.GetInvitationReading(ctx, tx, u.ID)
		return err
	}); !errors.Is(err, user.ErrInvitationUnauthenticated) {
		t.Fatalf("revoked bearer read: %v", err)
	}
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("UPDATE users SET status = 'inactive' WHERE id = ?", actor.UserID).Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := f.Commands.RevokeInvitation(ctx, tx, u.ID, 2)
		return err
	}); !errors.Is(err, user.ErrInvitationForbidden) {
		t.Fatalf("inactive actor wrote: %v", err)
	}
}

func TestInvitationCannotGrantAnOperatorPermissionInACustomerTenant(t *testing.T) {
	f := invitationSQL(t)
	f.service.Declare([]tenancy.Grant{{Permission: "tenant:manage", Operator: true}, {Permission: "user:read"}, {Permission: "user:manage"}, {Permission: "role:manage"}})
	if err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for name, grants := range map[string][]string{"admin": {"*", "tenant:manage"}, "operator_role": {"tenant:manage"}} {
			if err := tx.DB().Exec("INSERT INTO roles (tenant_id,name,permissions) VALUES (?,?,?)", db.TenantOf(tx).ID, name, pq.StringArray(grants)).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.authority.Ask = f.service.InspectInvitationAuthority
	err := f.Step(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		got, err := f.Commands.CreateInvitation(ctx, tx, user.InvitationIntent{Email: "operator_role@example.com", Roles: []string{"operator_role"}})
		if got != nil {
			t.Error("out-of-scope operator assignment returned a person")
		}
		return err
	})
	if !errors.Is(err, user.ErrInvitationForbidden) {
		t.Fatalf("operator assignment outside operator tenant = %v", err)
	}
}
