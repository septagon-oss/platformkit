package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/saved"
	"github.com/septagon-oss/platformkit/modules/saved/contracts"
	"github.com/septagon-oss/platformkit/modules/saved/contracts/savedtest"
	"github.com/septagon-oss/platformkit/modules/saved/internal"
)

// A removal of a copy that is not the caller's answers as the state the caller
// asked for, which is only honest while nothing was written and nothing was
// announced: one event naming another person's copy would tell every subscriber
// that a removal happened while the owner's row still stands.

// library saves one copy for one member and hands back what a second member or a
// second tenant could then try against it.
func library(t *testing.T) (contracts.Service, *db.Conn, tenancy.Tenant, uuid.UUID, contracts.Item, *desk) {
	t.Helper()
	_, conn := dbtest.Schema(t, saved.Migrations)
	source := &savedtest.Source{Value: contracts.Capture{
		Title: "A copy with one owner", Locale: "en-GB", Version: "v1",
		Values: json.RawMessage(`{"size":"red"}`),
	}}
	grants := &desk{held: map[uuid.UUID][]string{}}
	owner := uuid.New()
	grants.grant(owner, contracts.PermissionRead, contracts.PermissionWrite)
	home := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), home), tenancy.Principal{UserID: owner})
	service := internal.NewService(grants, []contracts.Savable{{Module: "example", Kind: "thing", Source: source}})
	var held contracts.Item
	err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		held, err = service.Save(ctx, tx, contracts.Save{Source: oneSource, ExpectedSourceVersion: "v1"})
		return err
	})
	if err != nil {
		t.Fatalf("the owner's first save: %v", err)
	}
	return service, conn, home, owner, held, grants
}

// stranger is one member who holds saved's grants and owns no copy.
func stranger(t *testing.T, grants *desk, home tenancy.Tenant) (context.Context, contracts.Service) {
	id := uuid.New()
	grants.grant(id, contracts.PermissionRead, contracts.PermissionWrite)
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), home), tenancy.Principal{UserID: id})
	return ctx, internal.NewService(grants, []contracts.Savable{{Module: "example", Kind: "thing",
		Source: &savedtest.Source{Value: contracts.Capture{Title: "another thing", Locale: "en-GB",
			Version: "v1", Values: json.RawMessage(`{"size":"blue"}`)}}}})
}

// TestARemoteMemberRemovingAnownedCopyWritesNothingAndAnnouncesNothing asks the
// same tenant's other member to remove a copy that is not theirs.
func TestARemoteMemberRemovingAnownedCopyWritesNothingAndAnnouncesNothing(t *testing.T) {
	service, conn, home, owner, held, grants := library(t)
	ctx, other := stranger(t, grants, home)

	err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return other.Unsave(ctx, tx, contracts.Unsave{ID: held.ID.String(), ExpectedRevision: held.Revision})
	})
	if err != nil {
		t.Fatalf("an unowned removal answers as the state asked for: %v", err)
	}
	ownCtx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), home), tenancy.Principal{UserID: owner})
	if got := published(t, conn, ownCtx, home); len(got) != 1 || got[0] != contracts.EventSaved {
		t.Errorf("events after a removal that owned nothing: %v, want only %q", got, contracts.EventSaved)
	}

	rows := witness(t, conn, ownCtx)
	if !strings.Contains(rows, held.ID.String()) {
		t.Errorf("the owner's row changed under a stranger's command: %s", rows)
	}
	var back contracts.View
	err = db.Run(ownCtx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		back, err = service.Get(ownCtx, tx, held.ID.String())
		return err
	})
	if err != nil {
		t.Fatalf("the owner still reads their own copy: %v", err)
	}
	if back.ID != held.ID || back.Snapshot.Title != "A copy with one owner" {
		t.Errorf("the retained copy is not the one that was saved: %+v", back.Item)
	}
}

// TestACopyInAnotherTenantIsNotThereForAForeignMember asks a member of a second
// tenant about the same row by id: not found, an empty list, and no removal.
func TestACopyInAnotherTenantIsNotThereForAForeignMember(t *testing.T) {
	service, conn, home, owner, held, grants := library(t)
	foreign := tenancy.Tenant{ID: uuid.New(), Slug: "globex"}
	ctx, other := stranger(t, grants, foreign)

	err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := service.Get(ctx, tx, held.ID.String()); !errors.Is(err, contracts.ErrNotFound) {
			return errors.New("a foreign tenant's copy answered as readable: " + err.Error())
		}
		if page, err := service.List(ctx, tx, contracts.Query{}); err != nil || page.Total != 0 || len(page.Items) != 0 {
			return errors.New("a foreign tenant's list was not empty")
		}
		return other.Unsave(ctx, tx, contracts.Unsave{ID: held.ID.String(), ExpectedRevision: held.Revision})
	})
	if err != nil {
		t.Fatalf("a foreign member's reads and removal: %v", err)
	}
	ownCtx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), home), tenancy.Principal{UserID: owner})
	if got := published(t, conn, ownCtx, home); len(got) != 1 || got[0] != contracts.EventSaved {
		t.Errorf("the owning tenant's outbox after a foreign command: %v", got)
	}
	if names := published(t, conn, ctx, foreign); len(names) != 0 {
		t.Errorf("a foreign tenant announced events it never wrote: %v", names)
	}
}
