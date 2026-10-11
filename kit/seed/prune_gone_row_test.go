package seed

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

// TestPruneOfARowAlreadyGoneForgetsItsKey: a file with prune: true dropped a
// record the seed owns, and somebody already deleted the owner's row through
// the product. Pruning it is a delete that finds none, which house rule 8 says
// is never refused: the run applies the rest of the file and forgets the key.
func TestPruneOfARowAlreadyGoneForgetsItsKey(t *testing.T) {
	file := "seed/starter/contents.yaml"
	declared := "apiVersion: platformkit.seed/v1\nresource: contents\nprune: true\nrecords:\n  - key: home\n    fields: {title: Home}\n"
	writer := newFakeWriter("contents", "content", "content", true)
	seedTenant(t, false, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		service, err := New(Deps{Files: fstest.MapFS{file: {Data: []byte(declared)}},
			Root: "seed", Clock: seedAt, Writers: []Writer{writer}, Authorize: &fakeGrant{}})
		if err != nil {
			t.Fatal(err)
		}
		if err := putKey(tx, writer.Resource(), "retired", "starter", uuid.New()); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Apply(ctx, tx, Selection{}); err != nil {
			t.Fatalf("Apply refused a prune whose row was already gone: %v", err)
		}
		if _, ok := writer.rows["home"]; !ok {
			t.Error("the declared record was not created")
		}
		keys, err := ownedKeys(tx, writer.Resource(), "starter")
		if err != nil {
			t.Fatal(err)
		}
		if len(keys) != 1 || keys[0].Value != "home" {
			t.Errorf("provenance after the run = %+v; want only home", keys)
		}
		return nil
	})
}
