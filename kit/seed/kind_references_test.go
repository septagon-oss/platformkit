package seed

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
)

func TestReferencesWorkWhenAResourceHasStarterAndDemoRecords(t *testing.T) {
	writer := newFakeWriter("pages", "content", "content", true)
	writer.resource.References = []Reference{{Resource: "pages", Path: "fields/parent", Target: "pages"}}
	files := fstest.MapFS{
		"seed/starter/pages.yaml": {Data: []byte("apiVersion: platformkit.seed/v1\nresource: pages\nrecords:\n  - key: home\n    fields: {title: Home}\n")},
		"seed/demo/pages.yaml":    {Data: []byte("apiVersion: platformkit.seed/v1\nresource: pages\nrecords:\n  - key: tour\n    fields: {title: Tour, parent: pages/home}\n")},
	}
	service, err := New(Deps{Files: files, Root: "seed", Clock: seedAt, Writers: []Writer{writer}, Authorize: &fakeGrant{}})
	if err != nil {
		t.Fatal(err)
	}
	seedTenant(t, true, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		plan, err := service.Plan(ctx, tx, Selection{Demo: true})
		if err != nil {
			t.Fatalf("valid starter/demo reference graph refused: %v", err)
		}
		if len(plan.Items) != 2 || plan.Items[0].Key != "home" || plan.Items[1].Key != "tour" {
			t.Fatalf("plan = %+v; want home before tour", plan.Items)
		}
		if _, err := service.Apply(ctx, tx, Selection{Demo: true}); err != nil {
			return err
		}
		if len(writer.rows) != 2 {
			t.Errorf("applied rows = %d; want both kinds", len(writer.rows))
		}
		return nil
	})
}
