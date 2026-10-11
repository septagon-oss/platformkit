package main

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
)

// A writer's default answers a field the file leaves out and nothing else. These
// are the two ways a field can be left out — not written at all, or written with no
// value — and the three ways it cannot: empty text where a closed set holds two
// answers, and a date YAML read as a date. The first two arrive as the owner's own
// default; the others refuse at the record's own line, because an answer somebody
// wrote is not the same sentence as nobody writing one.
func TestSeedKeepsAnAbsentFieldApartFromADeclaredValue(t *testing.T) {
	asset, err := fs.ReadFile(seedFiles, "seed/demo/assets/welcome.png")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		// stored is what the row behind the record holds afterwards, which for a
		// refused record is no row at all and so reads as 0.
		name, file, document, query, stored, asks string
		refuses                                   bool
	}{
		{
			name: "no priority written is the module's ordinary one", file: "seed/starter/tasks.yaml",
			document: "resource: tasks\nrecords:\n  - key: quiet-priority\n    fields: {title: Quiet priority}\n",
			query:    "SELECT priority FROM tasks WHERE title = 'Quiet priority'", stored: "normal",
		},
		{
			name: "a priority written with no value is a field left out", file: "seed/starter/tasks.yaml",
			document: "resource: tasks\nrecords:\n  - key: blank-priority\n    fields: {title: Blank priority, priority: }\n",
			query:    "SELECT priority FROM tasks WHERE title = 'Blank priority'", stored: "normal",
		},
		{
			name: "an empty visibility is a declared value, and is not one of the two", file: "seed/starter/files.yaml",
			document: "resource: files\nrecords:\n  - key: welcome.png\n    asset: assets/welcome.png\n    fields: {visibility: \"\"}\n",
			query:    "SELECT count(*) FROM files WHERE name = 'welcome.png'", stored: "0", refuses: true, asks: `visibility ""`,
		},
		{
			name: "an empty kind is a declared value, and is not one of the two", file: "seed/starter/contents.yaml",
			document: "resource: contents\nrecords:\n  - key: kindless\n    fields: {title: Kindless, body: Body, kind: \"\"}\n",
			query:    "SELECT count(*) FROM contents WHERE title = 'Kindless'", stored: "0", refuses: true, asks: `kind ""`,
		},
		{
			name: "an absolute due date refuses with the grammar's own words", file: "seed/starter/tasks.yaml",
			document: "resource: tasks\nrecords:\n  - key: fixed-day\n    fields: {title: Fixed day, dueAt: 2026-10-10}\n",
			query:    "SELECT count(*) FROM tasks WHERE title = 'Fixed day'", stored: "0", refuses: true, asks: "+3d",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, cfg := configure(t)
			install(t, path)
			c := compose(cfg)
			conn, err := db.Open(t.Context(), cfg.Database.URL)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			files := fstest.MapFS{
				tc.file:                           {Data: []byte("apiVersion: platformkit.seed/v1\n" + tc.document)},
				"seed/starter/assets/welcome.png": {Data: asset},
			}
			service, err := seed.New(seed.Deps{Files: files, Root: "seed", Clock: seedClock{},
				Writers: []seed.Writer{&contentSeeder{svc: c.contents}, &userSeeder{users: c.users},
					taskSeeder{svc: c.tasks}, fileSeeder{svc: c.files}},
				Authorize: seedGrants{auth: c.auth}})
			if err != nil {
				t.Fatal(err)
			}
			var plan seed.Plan
			applyErr := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
				tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
				if err != nil {
					return err
				}
				return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					ctx, err = seedActor(ctx, c.users, tx, adminEmail)
					if err != nil {
						return err
					}
					plan, err = service.Apply(ctx, tx, seed.Selection{})
					return err
				})
			})
			switch {
			case tc.refuses && applyErr == nil:
				t.Fatalf("seed accepted %s and committed: %s; want a refusal at %s", tc.name, plan, tc.file)
			case tc.refuses:
				if !strings.Contains(applyErr.Error(), tc.file+":") {
					t.Errorf("refusal = %v; want it to name %s and the line the value is on", applyErr, tc.file)
				}
				if !strings.Contains(applyErr.Error(), tc.asks) {
					t.Errorf("refusal = %v; want it to name %s", applyErr, tc.asks)
				}
			case applyErr != nil:
				t.Fatalf("seed refused %s: %v", tc.name, applyErr)
			}
			// The row says what the run really wrote: a refusal that left one behind
			// wrote something, and a default that arrived as the empty string wrote an
			// answer the owner never gave.
			var got any
			if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
				tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
				if err != nil {
					return err
				}
				return db.InTenant(ctx, system, tenant, func(_ context.Context, tx db.Tx[db.Tenant]) error {
					return tx.DB().Raw(tc.query).Row().Scan(&got)
				})
			}); err != nil {
				t.Fatal(err)
			}
			if have := fmt.Sprint(got); have != tc.stored {
				t.Errorf("%s left the row holding %v; want %s", tc.name, have, tc.stored)
			}
		})
	}
}
