package user

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

type lookupOnly func(context.Context, db.Tx[db.Tenant], uuid.UUID) (*contracts.User, error)

func (f lookupOnly) Get(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.User, error) {
	return f(ctx, tx, id)
}

type provisionOnly func(context.Context, db.Tx[db.System], uuid.UUID, string, string, string, []string) (*contracts.User, error)

func (f provisionOnly) Provision(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, email, name, password string, roles []string) (*contracts.User, error) {
	return f(ctx, tx, id, email, name, password, roles)
}

type invitationOnly struct {
	invite func(context.Context, db.Tx[db.Tenant], string, string) (*contracts.User, error)
	roles  func(context.Context, db.Tx[db.Tenant], uuid.UUID, []string) (*contracts.User, error)
}

func (f invitationOnly) Invite(ctx context.Context, tx db.Tx[db.Tenant], email, name string) (*contracts.User, error) {
	return f.invite(ctx, tx, email, name)
}
func (f invitationOnly) SetRoles(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, roles []string) (*contracts.User, error) {
	return f.roles(ctx, tx, id, roles)
}

func TestRecipientNeedsOnlyLookup(t *testing.T) {
	id := uuid.New()
	want := errors.New("lookup failed")
	for _, err := range []error{nil, want} {
		calls := 0
		r := recipients{users: lookupOnly(func(_ context.Context, _ db.Tx[db.Tenant], got uuid.UUID) (*contracts.User, error) {
			calls++
			if got != id {
				t.Fatal("recipient identity changed")
			}
			return &contracts.User{Email: "ada@example.test"}, err
		})}
		address, got := r.Email(t.Context(), db.Tx[db.Tenant]{}, id)
		if !errors.Is(got, err) || calls != 1 || err == nil && address != "ada@example.test" {
			t.Fatalf("recipient = %q, %v; calls=%d", address, got, calls)
		}
	}
}

func TestFirstAdministratorNeedsOnlySystemProvisioning(t *testing.T) {
	id := uuid.New()
	calls := 0
	a := firstAdmin{users: provisionOnly(func(_ context.Context, _ db.Tx[db.System], tenant uuid.UUID, email, name, password string, roles []string) (*contracts.User, error) {
		calls++
		if tenant != id || email != "ada@example.test" || name != "Ada" || password != "" || !slices.Equal(roles, []string{authcontracts.RoleAdmin}) {
			t.Fatal("first administrator inputs changed")
		}
		return &contracts.User{}, nil
	})}
	if err := a.Invite(t.Context(), db.Tx[db.System]{}, id, "ada@example.test", "Ada"); err != nil || calls != 1 {
		t.Fatalf("first administrator: %v, calls=%d", err, calls)
	}
}

func TestSSOProvisioningFailureRollsBackTheInvitation(t *testing.T) {
	for _, failRoles := range []bool{false, true} {
		t.Run(map[bool]string{true: "role assignment", false: "later authentication"}[failRoles], func(t *testing.T) {
			_, conn := dbtest.Schema(t, Migrations, auth.Migrations)
			ctx := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New()})
			svc, _ := New(Deps{
				Administration: &contracts.AdministrationFunc{Ask: func(context.Context, db.Tx[db.Tenant]) ([]string, error) { return nil, nil }},
				Granting:       &contracts.GrantingFunc{Ask: func(context.Context, db.Tx[db.Tenant]) (bool, error) { return true, nil }},
			})
			want := errors.New("provisioning interrupted")
			adapter := provisioner{users: invitationOnly{invite: svc.Invite, roles: func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, roles []string) (*contracts.User, error) {
				if failRoles {
					return nil, want
				}
				return svc.SetRoles(ctx, tx, id, roles)
			}}}
			err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				id, err := adapter.Provision(ctx, tx, "ada@example.test", "Ada", []string{"member"})
				if failRoles {
					if id != uuid.Nil || !errors.Is(err, want) {
						t.Fatalf("failed provisioning = %v, %v", id, err)
					}
					return err
				}
				if err != nil || id == uuid.Nil {
					t.Fatalf("provisioning = %v, %v", id, err)
				}
				return want
			})
			if !errors.Is(err, want) {
				t.Fatal(err)
			}
			if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				for _, table := range []string{"users", "sessions", "platformkit_outbox"} {
					var count int64
					if err := tx.DB().Table(table).Count(&count).Error; err != nil {
						return err
					}
					if count != 0 {
						t.Fatalf("failed provisioning left %d rows in %s", count, table)
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUserRejectsEmptyGrantAdapters(t *testing.T) {
	for _, g := range []*contracts.GrantingFunc{nil, {}} {
		t.Run("empty adapter", func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("empty granting was accepted")
				}
			}()
			New(Deps{Administration: &contracts.AdministrationFunc{Ask: auth.AdministeringRoles}, Granting: g})
		})
	}
}
