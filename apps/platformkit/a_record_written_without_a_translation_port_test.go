package main

// a_record_written_without_a_translation_port_test.go pins the in-process write doors
// of a Spec that declares translatable fields and was never handed the port. The
// reference application's seed writer stands at exactly that door: it reaches content
// through the package-level `content.Spec`, which is the declaration as the module
// wrote it, before any composition assigned the translation port its own copy carries.
// `Spec.check` refuses that pairing at mount, so nothing routed is served this way;
// what it would have done to a seed run is the question, and the answer must not be a
// nil dereference two statements after the row was already written.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	content "github.com/septagon-oss/platformkit/modules/content"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
)

func TestARecordWrittenThroughASpecWithNoPortIsWrittenAndRemoved(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.testTenants().ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			// The seeder's own context: no person's grant stands behind a write made
			// by a command, and the doors below are the ones it stands at.
			ctx = tenancy.WithActor(ctx, uuid.Nil)
			row, err := content.Spec.CreateRow(ctx, tx, &contentcontracts.Content{
				Slug: "written-without-a-port", Title: "Written without a port",
				Body: "The seed writer's own text.", Kind: "page",
			})
			if err != nil {
				return err
			}
			if _, err := content.Spec.UpdateRow(ctx, tx, row.ID,
				map[string]any{"title": "Rewritten by the seed writer"}); err != nil {
				return err
			}
			stored, err := crud.Get[*contentcontracts.Content](tx, row.ID)
			if err != nil {
				return err
			}
			if stored.Title != "Rewritten by the seed writer" {
				t.Errorf("title after the update = %q, want the text the write carried", stored.Title)
			}
			if _, err := content.Spec.DeleteRow(ctx, tx, row.ID); err != nil {
				return err
			}
			if _, err := crud.Get[*contentcontracts.Content](tx, row.ID); err == nil {
				t.Error("the record is still readable after DeleteRow")
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
