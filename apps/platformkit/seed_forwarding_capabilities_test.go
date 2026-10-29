package main

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/content"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/site"
	"github.com/septagon-oss/platformkit/modules/task"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
	"github.com/septagon-oss/platformkit/pkit"
)

type refusingSeedWriter struct {
	usercontracts.SeedWriter
	err   error
	calls *int
}

func (w refusingSeedWriter) Invite(context.Context, db.Tx[db.Tenant], string, string) (*usercontracts.User, error) {
	*w.calls++
	return nil, w.err
}

func TestProvisioningForwardsOnlyTheSeedWriterAndPermissionReader(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, content.Migrations, file.Migrations, site.Migrations, task.Migrations)
	c := composeReference(referenceCompositionConfig(t), pkit.Development)
	calls, permissionReads := 0, 0
	stop := errors.New("seed writer refused")
	provision := seedProvisioner{
		users:    refusingSeedWriter{SeedWriter: usertest.NewFake(), err: stop, calls: &calls},
		contents: c.contents, sites: c.sites, tasks: c.tasks,
		files: c.files,
		auth: identityPermissions(func(context.Context, db.Tx[db.Tenant], []string) ([]string, error) {
			permissionReads++
			return nil, nil
		}),
	}
	tenant := &tenantcontracts.Tenant{ID: uuid.New(), Slug: "new", Name: "New", Demo: true}
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if err := tx.DB().Create(tenant).Error; err != nil {
			return err
		}
		return provision.OnTenantCreate(ctx, tx, tenant)
	})
	if !errors.Is(err, stop) || calls != 1 || permissionReads != 0 {
		t.Fatalf("provisioning = %v; account calls=%d permission reads=%d", err, calls, permissionReads)
	}
}

func TestSeedPermissionReaderReceivesCurrentActorRoles(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations)
	users, _ := user.New(user.Deps{
		Administration: &usercontracts.AdministrationFunc{Ask: func(context.Context, db.Tx[db.Tenant]) ([]string, error) { return []string{"admin"}, nil }},
		Granting:       &usercontracts.GrantingFunc{Ask: func(context.Context, db.Tx[db.Tenant]) (bool, error) { return true, nil }},
	})
	tenant := tenancy.Tenant{ID: uuid.New()}
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		person, err := users.Invite(ctx, tx, "actor@example.test", "Actor")
		if err != nil {
			return err
		}
		if err := users.SetPassword(ctx, tx, person.ID, "a sufficiently long password"); err != nil {
			return err
		}
		if _, err := users.SetRoles(ctx, tx, person.ID, []string{"current"}); err != nil {
			return err
		}
		reads := 0
		grants := seedGrants{auth: identityPermissions(func(_ context.Context, got db.Tx[db.Tenant], roles []string) ([]string, error) {
			reads++
			if got.DB() != tx.DB() || len(roles) != 1 || roles[0] != "current" {
				t.Fatal("seed authority used a stale actor or another transaction")
			}
			return []string{"task:manage"}, nil
		})}
		actor := tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: person.ID, Roles: []string{"stale"}})
		if err := grants.Check(actor, tx, seed.Resource{Module: "task", Entity: "task", WriteGrant: "task:manage"}, ""); err != nil {
			return err
		}
		if reads != 1 {
			t.Fatalf("permission reads = %d", reads)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
