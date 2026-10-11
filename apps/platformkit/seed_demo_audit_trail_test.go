package main

// The brief's acceptance: every seeded write has an audit record naming the seed
// and the seed file. The demo half adds two owners — task, through its Spec write
// core, and file, through Upload — so the trail of a demo tenant's creation names
// seed/demo/tasks.yaml on task.task.created and seed/demo/files.yaml on
// file.uploaded, the same way the starter's page names its file.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

func TestSeededWorkAndFilesAreAuditedWithTheirSource(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	var tenantID string
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		created, err := c.tenants.Create(ctx, system, tenantcontracts.NewTenant{
			Slug: "demo-audit", Name: "Demo", Host: "demo-audit.localhost", Demo: true,
		})
		if err != nil {
			return err
		}
		tenantID = created.ID.String()
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	for _, want := range []struct{ event, file string }{
		{taskcontracts.EventCreated, "seed/demo/tasks.yaml"},
		{filecontracts.EventUploaded, "seed/demo/files.yaml"},
	} {
		var row []byte
		eventually(t, want.event+" of the demo tenant reaches the audit trail", func() bool {
			return owner.QueryRowContext(t.Context(),
				`SELECT to_jsonb(a) - 'payload' FROM audit_events a
				 WHERE name = $1 AND tenant_id = $2 ORDER BY occurred_at LIMIT 1`,
				want.event, tenantID).Scan(&row) == nil
		})
		var trail map[string]any
		if err := json.Unmarshal(row, &trail); err != nil {
			t.Fatal(err)
		}
		var seed, file bool
		var visit func(map[string]any)
		visit = func(m map[string]any) {
			for _, v := range m {
				switch v := v.(type) {
				case string:
					seed = seed || v == "seed"
					file = file || v == want.file
				case map[string]any:
					visit(v)
				}
			}
		}
		visit(trail)
		if !seed || !file {
			t.Errorf("the audit row for %s names seed=%t and %s=%t: %s", want.event, seed, want.file, file, row)
		}
	}
}
