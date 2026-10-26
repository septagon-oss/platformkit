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

// The erasure is the only write in this module that carries no person predicate of
// its own beyond the subject it was handed, and saved_items has no foreign key from
// person_id to a tenant's users: the column holds whatever uuid the caller brought.
// So the boundary beneath an erasure is row-level security alone, and one person id
// written in two tenants is the case that says whether that is true. The kernel's
// census (migrations/rls_test.go) proves the table has a policy; this proves the
// erasure answers inside the tenant that asked, and leaves the same person's other
// tenant's copy readable, removable, and unannounced.
func TestAnErasureLeavesTheCopyAnotherTenantHoldsUnderTheSamePersonID(t *testing.T) {
	_, conn := dbtest.Schema(t, saved.Migrations)
	grants := &desk{held: map[uuid.UUID][]string{}}
	keeper := uuid.New()
	grants.grant(keeper, contracts.PermissionRead, contracts.PermissionWrite)
	home, abroad := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}, tenancy.Tenant{ID: uuid.New(), Slug: "globex"}
	signedIn := func(tenant tenancy.Tenant) context.Context {
		return tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), tenant), tenancy.Principal{UserID: keeper})
	}
	homeCtx, abroadCtx := signedIn(home), signedIn(abroad)

	service := internal.NewService(grants, []contracts.Savable{{Module: "example", Kind: "thing", Source: &savedtest.Source{
		Value: contracts.Capture{Title: "One copy, two tenants", Locale: "en-GB", Version: "v1",
			Values: json.RawMessage(`{"size":"red"}`)}}}})
	here := contracts.Ref{Module: "example", Kind: "thing", Key: "kept at home"}
	elsewhere := contracts.Ref{Module: "example", Kind: "thing", Key: "kept abroad"}
	for _, held := range []struct {
		ctx context.Context
		ref contracts.Ref
	}{{homeCtx, here}, {abroadCtx, elsewhere}} {
		if err := db.Run(held.ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := service.Save(ctx, tx, contracts.Save{Source: held.ref, ExpectedSourceVersion: "v1"})
			return err
		}); err != nil {
			t.Fatalf("save %q in one tenant's own transaction: %v", held.ref.Key, err)
		}
	}

	// The export a person receives is their tenant's copies: the bundle names the
	// copy the requesting tenant holds and never the copy beside it, though both
	// rows carry the same person id.
	var bundle json.RawMessage
	if err := db.Run(homeCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		bundle, err = internal.PersonalData().Data.Export(ctx, tx, personaldata.Subject{ID: keeper})
		return err
	}); err != nil {
		t.Fatalf("export the person's copies in their own tenant: %v", err)
	}
	if !strings.Contains(string(bundle), here.Key) {
		t.Errorf("the export of the tenant that holds %q did not name it: %s", here.Key, bundle)
	}
	if strings.Contains(string(bundle), elsewhere.Key) {
		t.Errorf("the export reached a copy another tenant holds under the same person id: %s", bundle)
	}

	if err := db.Run(homeCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, personaldata.Subject{ID: keeper})
	}); err != nil {
		t.Fatalf("erase the person's copies in their own tenant: %v", err)
	}
	if rows := witness(t, conn, homeCtx); strings.Contains(rows, here.Key) {
		t.Errorf("the erasure left its own tenant's copy: %s", rows)
	}
	rows := witness(t, conn, abroadCtx)
	if !strings.Contains(rows, elsewhere.Key) {
		t.Errorf("an erasure in one tenant took the copy another tenant holds under the same person id: %s", rows)
	}
	if got := published(t, conn, abroadCtx, abroad); len(got) != 1 || got[0] != contracts.EventSaved {
		t.Errorf("the other tenant's outbox after an erasure that was not theirs: %v", got)
	}

	// The copy that survived is still a copy: readable from its own snapshot and
	// removable by the person who made it.
	var back contracts.View
	if err := db.Run(abroadCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		back, err = service.Find(ctx, tx, elsewhere)
		return err
	}); err != nil {
		t.Fatalf("read the copy the erasure abroad did not reach: %v", err)
	}
	if back.Availability != "available" || back.PersonID != keeper.String() {
		t.Errorf("the surviving copy reads %+v", back.Item)
	}
}

// A retained copy is a verbatim copy of somebody else's projection: the title and
// the values arrive from the source, and the kernel's own reference adoption for
// this module is a task detail, whose title and description modules/task scrubs
// for any person they name. So a copy can name a person who is not its keeper, and
// the erasure is asked of every module for its subject: modules/subject runs each
// contributed part, and personaldata.Scrub's rule for a text column is that it is
// cleaned "over everything the tenant can see", because the person a description is
// about is not the person whose id the row carries. An erasure that only deletes
// rows whose person_id is the subject's leaves the subject's address and display
// name standing in the copy beside them — and in the export that person is handed
// their own bundle with. Nothing in this table's personal-data declaration says the
// text exists to scrub: the claim files no Text column, so personaldata.Residue has
// no column to re-read and the erasure proves itself against nothing.
//
// What correct looks like is what modules/task, modules/change, modules/content,
// modules/decide and modules/file do: file the text columns the person can be named
// in, and clean them for the subject as well as deleting the subject's own copies.
// The keeper's copy survives — an unrelated person's retained copy is not theirs to
// lose — and what is taken from it is the sentence about the subject.
func TestErasureOfAPersonNamedInsideAnotherPersonsCopyLeavesNoTraceOfThem(t *testing.T) {
	_, conn := dbtest.Schema(t, saved.Migrations)
	grants := &desk{held: map[uuid.UUID][]string{}}
	keeper, subject := uuid.New(), uuid.New()
	grants.grant(keeper, contracts.PermissionRead, contracts.PermissionWrite)
	ownCtx := tenancy.WithPrincipal(
		tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New(), Slug: "acme"}),
		tenancy.Principal{UserID: keeper})
	service := internal.NewService(grants, []contracts.Savable{{Module: "example", Kind: "thing", Source: &savedtest.Source{
		Value: contracts.Capture{Title: "Fix Wren Quill's invoice", Locale: "en-GB", Version: "v1",
			Values: json.RawMessage(`{"assignee":"Wren Quill","note":"wren@example.com asked first"}`)}}}})
	if err := db.Run(ownCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := service.Save(ctx, tx, contracts.Save{Source: oneSource, ExpectedSourceVersion: "v1"})
		return err
	}); err != nil {
		t.Fatalf("the keeper's own save: %v", err)
	}

	wren := personaldata.Subject{ID: subject, Email: "wren@example.com", DisplayName: "Wren Quill"}
	if err := db.Run(ownCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, wren)
	}); err != nil {
		t.Fatalf("the erasure asked for the person the copy names: %v", err)
	}

	// The whole table, read as text rather than through the contract's own JSON, so
	// that a field the export happens not to carry cannot hide a surviving mention.
	if rows := witness(t, conn, ownCtx); !saysNothingOf(wren, rows) {
		t.Errorf("an erasure left its subject named in a copy that is not theirs: %s", rows)
	}
	var bundle json.RawMessage
	if err := db.Run(ownCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		bundle, err = internal.PersonalData().Data.Export(ctx, tx, personaldata.Subject{ID: keeper})
		return err
	}); err != nil {
		t.Fatalf("export the keeper's copies after that erasure: %v", err)
	}
	if !saysNothingOf(wren, string(bundle)) {
		t.Errorf("the keeper's own export still says what the erasure was asked to stop saying: %s", bundle)
	}
}

// saysNothingOf is the residue question asked of a rendering: no identifier the
// subject is known by survives in it.
func saysNothingOf(s personaldata.Subject, rendered string) bool {
	for _, term := range s.Identifiers() {
		if strings.Contains(rendered, term) {
			return false
		}
	}
	return true
}
