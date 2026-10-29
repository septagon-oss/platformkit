package internal_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/saved"
	"github.com/septagon-oss/platformkit/modules/saved/contracts"
	"github.com/septagon-oss/platformkit/modules/saved/contracts/savedtest"
	"github.com/septagon-oss/platformkit/modules/saved/internal"
)

// wren is a person a copy names without being about who kept it, and namedSource
// is that copy's source: a title and a projection written by another module about
// somebody who is not the member pressing save. A retained copy is written to
// survive its source's own later rewrites, including the scrub the source's owner
// runs for this person, so the erasure is the only statement that reaches these
// words once they are copied.
func wren() personaldata.Subject {
	return personaldata.Subject{ID: uuid.New(), Email: "wren@example.com", DisplayName: "Wren Quill"}
}

func namedSource() *savedtest.Source {
	return &savedtest.Source{Value: contracts.Capture{
		Title: "Fix Wren Quill's invoice", Locale: "en-GB", Version: "v1",
		Values: json.RawMessage(`{"assignee":"Wren Quill","note":"wren@example.com asked first"}`)}}
}

func saveNamed(t *testing.T, ctx context.Context, conn *db.Conn, service *internal.Service) contracts.Item {
	t.Helper()
	var held contracts.Item
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		held, err = service.Save(ctx, tx, contracts.Save{Source: oneSource, ExpectedSourceVersion: "v1"})
		return err
	}); err != nil {
		t.Fatalf("save a copy that names the subject: %v", err)
	}
	return held
}

// TestAnErasureTakesTheWordsAboutTheSubjectAndKeepsTheCopy answers what the module
// owes the two people one copy involves. The subject's own copies are theirs and
// disappear; the copy somebody else kept of a source that named them does not — it
// is their keeper's, and what the erasure takes from it is the sentence about the
// subject, not the copy. The source the copy points at, its identity and the
// revision every unsave quotes all survive the rewrite, because the copy is a right
// to a source and not a paragraph.
func TestAnErasureTakesTheWordsAboutTheSubjectAndKeepsTheCopy(t *testing.T) {
	_, conn := dbtest.Schema(t, saved.Migrations)
	grants := &desk{held: map[uuid.UUID][]string{}}
	keeper, subject := uuid.New(), wren()
	grants.grant(keeper, contracts.PermissionRead, contracts.PermissionWrite)
	grants.grant(subject.ID, contracts.PermissionRead, contracts.PermissionWrite)
	home := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	ownCtx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), home), tenancy.Principal{UserID: keeper})
	theirs := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), home), tenancy.Principal{UserID: subject.ID})
	service := internal.NewService(grants, []contracts.Savable{{Module: "example", Kind: "thing", Source: namedSource()}})
	held := saveNamed(t, ownCtx, conn, service)
	theirOwn := saveNamed(t, theirs, conn, service)

	if err := db.Run(theirs, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, subject)
	}); err != nil {
		t.Fatalf("erase the person the copy names: %v", err)
	}

	rows := witness(t, conn, ownCtx)
	if strings.Contains(rows, theirOwn.ID.String()) {
		t.Errorf("an erasure left the copy its own subject kept: %s", rows)
	}
	if !strings.Contains(rows, held.ID.String()) || !strings.Contains(rows, oneSource.Key) {
		t.Errorf("an erasure took a copy that was not its subject's: %s", rows)
	}
	for _, term := range subject.Identifiers() {
		if strings.Contains(rows, term) {
			t.Errorf("an erasure left %q standing in the table: %s", term, rows)
		}
	}

	// The surviving copy is still readable as the same copy of the same source, and
	// it says so at the surface a caller quotes a revision against.
	var back contracts.View
	err := db.Run(ownCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		back, err = service.Find(ctx, tx, oneSource)
		return err
	})
	if err != nil {
		t.Fatalf("read the copy the erasure rewrote: %v", err)
	}
	if back.ID != held.ID || back.Revision != 1 || back.Source != oneSource || back.Availability != "available" {
		t.Errorf("the scrubbed copy reads %+v, want the same copy of %v", back.Item, oneSource)
	}
	if !back.UpdatedAt.After(held.UpdatedAt) {
		t.Errorf("a copy whose words an erasure rewrote still says it last changed at %s", back.UpdatedAt)
	}
	// The erasure is not a removal the keeper asked for, so it announces nothing.
	if got := published(t, conn, ownCtx, home); len(got) != 2 || got[0] != contracts.EventSaved || got[1] != contracts.EventSaved {
		t.Errorf("events around an erasure: %v, want the two saves and nothing else", got)
	}
}

// TestAnErasureScrubsOnlyTheCopiesOfTheTenantThatAsked bounds the new write the way
// TestAnErasureLeavesTheCopyAnotherTenantHoldsUnderTheSamePersonID bounds the old
// one: the scrub carries no tenant predicate of its own, so the tenant boundary
// under it is row-level security, and an erasure one tenant asked cannot rewrite
// the words beside it. What the asking tenant sees is clean, and what the other
// holds is untouched.
func TestAnErasureScrubsOnlyTheCopiesOfTheTenantThatAsked(t *testing.T) {
	_, conn := dbtest.Schema(t, saved.Migrations)
	grants := &desk{held: map[uuid.UUID][]string{}}
	keeper, subject := uuid.New(), wren()
	grants.grant(keeper, contracts.PermissionRead, contracts.PermissionWrite)
	home, abroad := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}, tenancy.Tenant{ID: uuid.New(), Slug: "globex"}
	signedIn := func(tenant tenancy.Tenant) context.Context {
		return tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), tenant), tenancy.Principal{UserID: keeper})
	}
	homeCtx, abroadCtx := signedIn(home), signedIn(abroad)
	service := internal.NewService(grants, []contracts.Savable{{Module: "example", Kind: "thing", Source: namedSource()}})
	saveNamed(t, homeCtx, conn, service)
	saveNamed(t, abroadCtx, conn, service)

	if err := db.Run(homeCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, subject)
	}); err != nil {
		t.Fatalf("erase the person two tenants' copies name: %v", err)
	}
	if rows := witness(t, conn, homeCtx); strings.Contains(rows, subject.DisplayName) {
		t.Errorf("the tenant that asked was left holding the name it asked about: %s", rows)
	}
	if rows := witness(t, conn, abroadCtx); !strings.Contains(rows, subject.DisplayName) {
		t.Errorf("an erasure in one tenant rewrote the copy another tenant holds: %s", rows)
	}
}
