package internal_test

import (
	"context"
	"encoding/json"
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

// TestTheListPagesTheModuleItFiltered asks the half of List's sentence that its
// paging depends on and nothing cuts a page from: the pair of filters selects the
// rows before they are counted and before the page is cut, never after. The order
// is proved for a person and for a kind by the shared conformance case; the module
// half is the half an installation fills by composing two modules, and no page has
// ever been cut from it.
//
// Four copies are kept — two of one kind under one module, a second kind under that
// module, and the first kind under another module. A page of one of the module's own
// kind must count every row that pair covers while answering one, and the row it
// answers must be of that pair; a page asking past the last row of the other module's
// single copy must answer none while still counting it. Answers are read as counts and
// as pairs, because this case sets no clock and so owns no ordering within a set.
func TestTheListPagesTheModuleItFiltered(t *testing.T) {
	_, conn := dbtest.Schema(t, saved.Migrations)
	source := &savedtest.Source{
		Value:       contracts.Capture{Title: "Paged under two modules", Locale: "en", Version: "v1", Values: json.RawMessage(`{}`)},
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
		{Module: "example", Kind: "thing", Key: "two"},
		{Module: "example", Kind: "plan", Key: "one"},
		{Module: "archive", Kind: "thing", Key: "one"},
	} {
		if err := db.Run(ctx, conn, func(cx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := service.Save(cx, tx, contracts.Save{Source: kept, ExpectedSourceVersion: "v1"})
			return err
		}); err != nil {
			t.Fatalf("keep a copy under %s/%s: %v", kept.Module, kept.Kind, err)
		}
	}

	for _, ask := range []struct {
		module, kind  string
		limit, offset int
		wantTotal     int64
		wantRows      int
	}{
		{"example", "thing", 1, 0, 2, 1},
		{"example", "", 1, 0, 3, 1},
		{"archive", "thing", 1, 1, 1, 0},
	} {
		var page contracts.Page
		err := db.Run(ctx, conn, func(cx context.Context, tx db.Tx[db.Tenant]) error {
			var err error
			page, err = service.List(cx, tx, contracts.Query{Module: ask.module, Kind: ask.kind, Limit: ask.limit, Offset: ask.offset})
			return err
		})
		if err != nil {
			t.Fatalf("page module=%q kind=%q limit=%d offset=%d: %v", ask.module, ask.kind, ask.limit, ask.offset, err)
		}
		if page.Total != ask.wantTotal || len(page.Items) != ask.wantRows {
			t.Errorf("page module=%q kind=%q limit=%d offset=%d answered %d of %d rows, want %d of %d",
				ask.module, ask.kind, ask.limit, ask.offset, len(page.Items), page.Total, ask.wantRows, ask.wantTotal)
		}
		for _, item := range page.Items {
			if ask.module != "" && item.Source.Module != ask.module {
				t.Errorf("page module=%q kind=%q answered a copy of module %q", ask.module, ask.kind, item.Source.Module)
			}
			if ask.kind != "" && item.Source.Kind != ask.kind {
				t.Errorf("page module=%q kind=%q answered a copy of kind %q", ask.module, ask.kind, item.Source.Kind)
			}
		}
	}
}
