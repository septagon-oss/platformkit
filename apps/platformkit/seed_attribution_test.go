package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// A seed write is an ordinary owner event with the seed's provenance attached.
// The outbox is the durable input to audit, so every field audit must later
// report has to be present on this committed event.
func TestSeededWriteCarriesSourceActorAndTrace(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	if err := seedCommand([]string{"--config", path, "--tenant", "acme", "--as", adminEmail}); err != nil {
		t.Fatalf("seed through the application command: %v", err)
	}
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			var body []byte
			if err := tx.DB().Raw(`SELECT to_jsonb(o) FROM platformkit_outbox o
				WHERE name = 'content.content.created' AND payload->>'slug' = 'home'
				ORDER BY created_at LIMIT 1`).Row().Scan(&body); err != nil {
				return err
			}
			var event map[string]any
			if err := json.Unmarshal(body, &event); err != nil {
				return err
			}
			if event["actor_kind"] != "seed" {
				t.Errorf("created home event actor kind = %v; want seed", event["actor_kind"])
			}
			if event["actor"] != nil {
				t.Errorf("created home event actor = %v; a seed is a system actor", event["actor"])
			}
			if event["source_file"] != "seed/starter/contents.yaml" {
				t.Errorf("created home event source file = %v; want its seed file", event["source_file"])
			}
			if line, ok := event["source_line"].(float64); !ok || line <= 0 {
				t.Errorf("created home event source line = %v; want its positive line", event["source_line"])
			}
			if event["traceparent"] == nil || event["traceparent"] == "" {
				t.Error("created home event has no traceparent to join it to the seed run")
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
