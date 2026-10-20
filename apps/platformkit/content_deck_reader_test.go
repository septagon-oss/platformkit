package main

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestAContentReaderIsServedTheDeckWithOnlyItsSkip reads the served catalogue as
// a member holding content:read and not content:manage: Content's deck stays,
// with the local skip alone, and the publish route its right direction names
// refuses that member. The administrator is served both command directions.
func TestAContentReaderIsServedTheDeckWithOnlyItsSkip(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	tenantID := acmeTenant(t, cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	err = dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec(`INSERT INTO roles (tenant_id, name, permissions) VALUES (?, 'reader', ARRAY['content:read'])`, tenantID).Error
	})
	if err != nil {
		t.Fatalf("create the reader role: %v", err)
	}
	const readerEmail = "reader@acme.localhost"
	provisionAs(t, cfg, c, tenantID, readerEmail, "reader")

	directions := func(t *testing.T, who *http.Client) map[string]map[string]any {
		t.Helper()
		code, body := do(t, cfg, who, http.MethodGet, acmeHost, "/api/v1/app/resources", "")
		if code != http.StatusOK {
			t.Fatalf("GET /api/v1/app/resources = %d %s", code, body)
		}
		var catalog struct {
			Resources []struct {
				Module, Entity string
				Presentation   *struct {
					Deck *struct {
						Directions map[string]map[string]any `json:"directions"`
					} `json:"deck"`
				} `json:"presentation"`
			} `json:"resources"`
		}
		if err := json.Unmarshal([]byte(body), &catalog); err != nil {
			t.Fatal(err)
		}
		for _, r := range catalog.Resources {
			if r.Module == "content" && r.Entity == "content" {
				if r.Presentation == nil || r.Presentation.Deck == nil {
					t.Fatalf("content is served without its deck: %s", body)
				}
				return r.Presentation.Deck.Directions
			}
		}
		t.Fatalf("content is not in the served catalogue: %s", body)
		return nil
	}

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	if got := directions(t, admin); got["right"]["command"] != "publish" || got["left"]["command"] != "archive" {
		t.Errorf("the administrator's deck directions = %v, want publish right and archive left", got)
	}

	reader := signIn(t, cfg, acmeHost, readerEmail, adminPass)
	want := map[string]map[string]any{"down": {"skip": true}}
	if got := directions(t, reader); !reflect.DeepEqual(got, want) {
		t.Errorf("the reader's deck directions = %v, want %v", got, want)
	}
	code, body := do(t, cfg, reader, http.MethodPost, acmeHost, "/api/v1/content/contents/"+uuid.NewString()+"/publish", "")
	if code != http.StatusForbidden {
		t.Errorf("the reader's publish = %d %s, want 403", code, body)
	}
}
