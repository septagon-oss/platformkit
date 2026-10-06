package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/richtext"
)

func TestRichTextDoesNotStoreABodyTheNextWriteRefuses(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	opts := appOptions(cfg, c, app.All)
	opts.Transport = memory.New()
	opts.Log = quiet()
	start(t, cfg, c.modules, opts)
	who := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	admin := dbtest.Open(t, cfg.Database.MigrateURL)
	var beforeEvents int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name = 'content.content.created'").Scan(&beforeEvents); err != nil {
		t.Fatal(err)
	}
	code, body := do(t, cfg, who, http.MethodPost, acmeHost, contentPath,
		`{"slug":"incomplete-tag","title":"Incomplete tag","body":"<p\t"}`)
	if code == http.StatusUnprocessableEntity {
		if !strings.Contains(body, "raw HTML") || !strings.Contains(body, "line 1") {
			t.Fatalf("first-write refusal = %s, want raw HTML on line 1", body)
		}
		var rows, events int
		if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM contents WHERE slug = 'incomplete-tag'").Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name = 'content.content.created'").Scan(&events); err != nil {
			t.Fatal(err)
		}
		if rows != 0 || events != beforeEvents {
			t.Fatalf("refusal committed %d rows and changed created events from %d to %d", rows, beforeEvents, events)
		}
		return
	}
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s, want 201 or a first-write refusal", code, body)
	}
	id := field(t, body, "id")
	code, body = do(t, cfg, who, http.MethodGet, acmeHost, contentPath+"/"+id, "")
	if code != http.StatusOK {
		t.Fatalf("stored row = %d %s", code, body)
	}
	stored := field(t, body, "body")
	if again, err := richtext.Normalise(stored); err != nil || again != stored {
		t.Errorf("stored body %q is refused or changes on the next write: %q, %v", stored, again, err)
	}
	code, detail := do(t, cfg, who, http.MethodGet, acmeHost, "/app/content/contents/"+id, "")
	if code != http.StatusOK || !strings.Contains(detail, "&lt;p") {
		t.Errorf("accepted text disappeared from the detail view: status %d, prose present %t", code,
			strings.Contains(detail, `data-component="prose"`))
	}
}
