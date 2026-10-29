package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
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

// A composition that no longer declares a source still holds that source's
// copies: the person saved them while it was there, and the whole point of a
// retained copy is that it outlives the thing it came from. Reads answer with
// the copy and the words over it, never with a refusal, so the person can still
// see and remove what they saved. This is the same answer the memory fake gives,
// which is what makes it the contract rather than one adapter's choice.

var dropped = contracts.Ref{Module: "example", Kind: "thing", Key: "dropped"}

// keepers saves one copy through a declared source and hands back everything a
// later composition would still have: the rows, the authority desk, and a
// service over the declaration set that source is gone from.
func keepers(t *testing.T) (contracts.Service, context.Context, *db.Conn, tenancy.Tenant, contracts.Item) {
	t.Helper()
	_, conn := dbtest.Schema(t, saved.Migrations)
	source := &savedtest.Source{Value: contracts.Capture{
		Title: "The thing that outlived its source", Locale: "en-GB", Version: "v1",
		Values: json.RawMessage(`{"size":"red"}`),
	}}
	grants := &desk{held: map[uuid.UUID][]string{}}
	person := uuid.New()
	grants.grant(person, contracts.PermissionRead, contracts.PermissionWrite)
	home := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), home), tenancy.Principal{UserID: person})
	before := internal.NewService(grants, []contracts.Savable{{Module: "example", Kind: "thing", Source: source}})
	var row contracts.Item
	err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		row, err = before.Save(ctx, tx, contracts.Save{Source: dropped, ExpectedSourceVersion: "v1"})
		return err
	})
	if err != nil {
		t.Fatalf("save while the source was declared: %v", err)
	}
	after := internal.NewService(grants, nil)
	return after, ctx, conn, home, row
}

// TestACopyWhoseSourceLeftTheCompositionStillReadsAsNoLongerAvailable asks the
// three reads of a composition that dropped the source module.
func TestACopyWhoseSourceLeftTheCompositionStillReadsAsNoLongerAvailable(t *testing.T) {
	service, ctx, conn, home, row := keepers(t)
	ask := func(body func(db.Tx[db.Tenant]) error) error {
		return db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error { return body(tx) })
	}

	var view contracts.View
	if err := ask(func(tx db.Tx[db.Tenant]) error {
		var err error
		view, err = service.Get(ctx, tx, row.ID.String())
		return err
	}); err != nil || view.Availability != "unavailable" || !reflect.DeepEqual(view.Item, row) {
		t.Fatalf("a dropped source refused the copy rather than marking it: %+v, %v", view, err)
	}

	var page contracts.Page
	if err := ask(func(tx db.Tx[db.Tenant]) error {
		var err error
		page, err = service.List(ctx, tx, contracts.Query{})
		return err
	}); err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Availability != "unavailable" {
		t.Fatalf("a dropped source emptied the library: %+v, %v", page, err)
	}

	var found contracts.View
	if err := ask(func(tx db.Tx[db.Tenant]) error {
		var err error
		found, err = service.Find(ctx, tx, dropped)
		return err
	}); err != nil || found.Availability != "unavailable" || found.ID != row.ID {
		t.Fatalf("a dropped source hid the pressed state: %+v, %v", found, err)
	}

	// The live action is what the dropped source takes away, and only that.
	var path string
	var err error
	if err = ask(func(tx db.Tx[db.Tenant]) error {
		var err error
		path, err = service.Open(ctx, tx, row.ID.String())
		return err
	}); path != "" || !errors.Is(err, contracts.ErrUnavailable) {
		t.Fatalf("a dropped source opened a live action: %q, %v", path, err)
	}

	if err := ask(func(tx db.Tx[db.Tenant]) error {
		return service.Unsave(ctx, tx, contracts.Unsave{ID: row.ID.String(), ExpectedRevision: row.Revision})
	}); err != nil {
		t.Fatalf("a dropped source prevented removal: %v", err)
	}
	if copies := count(t, conn, ctx, `SELECT count(*) FROM saved_items`); copies != 0 {
		t.Fatalf("the copy survived its removal: %d rows", copies)
	}
	if want := []string{contracts.EventSaved, contracts.EventUnsaved}; !slices.Equal(published(t, conn, ctx, home), want) {
		t.Fatalf("the copy's two commands published %v, want %v", published(t, conn, ctx, home), want)
	}
}

// TestAnAbsentSourceIsAnObservationNotAnAbort pins the shared decision every
// adapter answers with: a source that is not there — deleted, revoked, or no
// longer part of the composition — is a word over the copy, never a refusal to
// read it. Only an error that means nothing about the source may abort a read.
func TestAnAbsentSourceIsAnObservationNotAnAbort(t *testing.T) {
	for _, probe := range []struct {
		err   error
		said  string
		abort bool
	}{
		{err: nil, said: "available"},
		{err: contracts.ErrUnavailable, said: "unavailable"},
		{err: contracts.ErrDenied, said: "unavailable"},
		{err: contracts.ErrTemporary, said: "temporary"},
		{err: errors.New("the store is closed"), abort: true},
	} {
		said, err := internal.Availability(probe.err)
		if probe.abort {
			if err == nil {
				t.Fatalf("%v was reported as %q rather than aborting the read", probe.err, said)
			}
			continue
		}
		if err != nil || said != probe.said {
			t.Fatalf("%v: said %q, err %v, want %q", probe.err, said, err, probe.said)
		}
	}
}
