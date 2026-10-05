package seed

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
)

func TestPrunePreservesARecordMovedFromStarterToDemo(t *testing.T) {
	const record = "  - key: tour\n    fields: {title: Tour}\n"
	const header = "apiVersion: platformkit.seed/v1\nresource: pages\nprune: true\nrecords:\n"
	files := fstest.MapFS{
		"seed/starter/pages.yaml": {Data: []byte(header + record)},
		"seed/demo/pages.yaml":    {Data: []byte(header + "  []\n")},
	}
	writer := newFakeWriter("pages", "content", "content", true)
	service, err := New(Deps{Files: files, Root: "seed", Clock: seedAt, Writers: []Writer{writer}, Authorize: &fakeGrant{}})
	if err != nil {
		t.Fatal(err)
	}
	seedTenant(t, true, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := service.Apply(ctx, tx, Selection{Demo: true}); err != nil {
			return err
		}
		original := writer.rows["tour"].ID
		files["seed/starter/pages.yaml"].Data = []byte(header + "  []\n")
		files["seed/demo/pages.yaml"].Data = []byte(header + record)
		plan, err := service.Apply(ctx, tx, Selection{Demo: true})
		if err != nil {
			return err
		}
		row, present := writer.rows["tour"]
		if !present || row.ID != original {
			t.Errorf("still-declared tour was removed or replaced: present=%v, plan=%s", present, plan)
		}
		for _, item := range plan.Items {
			if item.Action == Prune && item.Key == "tour" {
				t.Errorf("pruned a record still declared in the selected demo file: %s", plan)
			}
		}
		return nil
	})
}
