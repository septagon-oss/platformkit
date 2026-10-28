package internal_test

import (
	"context"
	"encoding/json"
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

// TestTheListFilterSeparatesTwoModulesThatShareOneKind asks List what its contract
// promises and nothing else measures: a pair of filters selects exactly, before
// counting and paging. A kind is one word and two modules may both contribute it,
// so the module half is what decides whose copies a caller is shown.
//
// Three copies of one source are kept — one kind under two modules, and a second
// kind under one of them — and every query a caller can form from a pair is asked:
// each complete pair, which must draw one copy alone; a module that kept nothing,
// which must answer empty rather than with another module's rows; and each half of
// the pair left unset, whose answer is every copy that half covers. A filter this
// installation cannot name is refused by PageBounds before any of this, so the
// queries here are pairs and the only thing in question is which rows they select.
func TestTheListFilterSeparatesTwoModulesThatShareOneKind(t *testing.T) {
	_, conn := dbtest.Schema(t, saved.Migrations)
	source := &savedtest.Source{
		Value:       contracts.Capture{Title: "One kind, kept twice", Locale: "en", Version: "v1", Values: json.RawMessage(`{}`)},
		Destination: "/app/example/things/one",
	}
	grants := &desk{held: map[uuid.UUID][]string{}}
	keeper := uuid.New()
	grants.grant(keeper, contracts.PermissionRead, contracts.PermissionWrite)
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New(), Slug: "acme"}),
		tenancy.Principal{UserID: keeper})
	service := internal.NewService(grants, []contracts.Savable{
		{Module: "example", Kind: "thing", Words: words("Save thing", "Saved to things"), Source: source},
		{Module: "example", Kind: "plan", Words: words("Save plan", "Saved to plans"), Source: source},
		{Module: "archive", Kind: "thing", Words: words("Save thing", "Saved to the archive"), Source: source},
	})

	for _, kept := range []contracts.Ref{
		{Module: "example", Kind: "thing", Key: "one"},
		{Module: "example", Kind: "plan", Key: "one"},
		{Module: "archive", Kind: "thing", Key: "one"},
	} {
		err := db.Run(ctx, conn, func(cx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := service.Save(cx, tx, contracts.Save{Source: kept, ExpectedSourceVersion: "v1"})
			return err
		})
		if err != nil {
			t.Fatalf("keep a copy under %s/%s: %v", kept.Module, kept.Kind, err)
		}
	}

	for _, ask := range []struct {
		module, kind string
		want         []string
	}{
		{"example", "thing", []string{"example/thing"}},
		{"example", "plan", []string{"example/plan"}},
		{"archive", "thing", []string{"archive/thing"}},
		{"nowhere", "thing", nil},
		{"example", "", []string{"example/plan", "example/thing"}},
		{"", "thing", []string{"archive/thing", "example/thing"}},
		{"", "", []string{"archive/thing", "example/plan", "example/thing"}},
	} {
		var page contracts.Page
		err := db.Run(ctx, conn, func(cx context.Context, tx db.Tx[db.Tenant]) error {
			var err error
			page, err = service.List(cx, tx, contracts.Query{Module: ask.module, Kind: ask.kind})
			return err
		})
		if err != nil {
			t.Fatalf("list module=%q kind=%q: %v", ask.module, ask.kind, err)
		}
		// Rows come back newest capture first, an order of three saves this case set
		// no clock for, so the answer is read as the set of pairs it drew.
		want := slices.Clone(ask.want)
		slices.Sort(want)
		drawn := make([]string, 0, len(page.Items))
		for _, item := range page.Items {
			drawn = append(drawn, item.Source.Module+"/"+item.Source.Kind)
		}
		slices.Sort(drawn)
		if !slices.Equal(drawn, want) || page.Total != int64(len(want)) {
			t.Errorf("list module=%q kind=%q drew %v of %d rows, want %v", ask.module, ask.kind, drawn, page.Total, want)
		}
	}
}
