package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// A seeded write reaches the tenant's audit trail through the ordinary
// subscriber, and the trail row itself says the seed caused it and which file
// asked for it. The outbox row that carries the attribution is purged after
// its retention window, so a trail that cites nothing loses the source for good.
func TestASeededWriteIsAuditedAsTheSeedWithItsSource(t *testing.T) {
	path, cfg := configure(t)
	// The bootstrap creates acme, and acme's creation applies the starter seed.
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	var row []byte
	eventually(t, "the seeded home page's creation reaches the audit trail", func() bool {
		err := owner.QueryRowContext(t.Context(),
			`SELECT to_jsonb(a) - 'payload' FROM audit_events a
			 WHERE name = 'content.content.created' AND payload->>'slug' = 'home'
			 ORDER BY occurred_at LIMIT 1`).Scan(&row)
		return err == nil
	})
	var trail map[string]any
	if err := json.Unmarshal(row, &trail); err != nil {
		t.Fatal(err)
	}
	// The attribution may be columns of the row or one document inside it; any
	// shape that keeps the cause, the file and the line answers the case.
	var seed, file, line bool
	var visit func(map[string]any)
	visit = func(m map[string]any) {
		for k, v := range m {
			switch v := v.(type) {
			case string:
				seed = seed || v == "seed"
				file = file || v == "seed/starter/contents.yaml"
			case float64:
				line = line || (v > 0 && strings.Contains(strings.ToLower(k), "line"))
			case map[string]any:
				visit(v)
			}
		}
	}
	visit(trail)
	if !seed {
		t.Errorf("the audit row for the seeded home names no seed as its cause: %s", row)
	}
	if !file {
		t.Errorf("the audit row for the seeded home cites no seed file: %s", row)
	}
	if !line {
		t.Errorf("the audit row for the seeded home cites no line of its seed file: %s", row)
	}
}
