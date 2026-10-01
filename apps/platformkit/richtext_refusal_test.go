package main

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestRefusedRichTextPatchLeavesRowAndOutboxUntouched(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	opts := appOptions(cfg, c, app.All)
	opts.Transport = memory.New()
	opts.Log = quiet()
	start(t, cfg, c.modules, opts)
	who := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, response := do(t, cfg, who, http.MethodPost, acmeHost, contentPath,
		`{"slug":"refusal-proof","title":"Refusal proof","body":"## Original"}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, response)
	}
	id := field(t, response, "id")
	admin := dbtest.Open(t, cfg.Database.MigrateURL)
	var beforeBody string
	var beforeTime time.Time
	if err := admin.QueryRowContext(t.Context(), "SELECT body, updated_at FROM contents WHERE id = $1", id).
		Scan(&beforeBody, &beforeTime); err != nil {
		t.Fatal(err)
	}

	code, response = do(t, cfg, who, http.MethodPatch, acmeHost, contentPath+"/"+id,
		`{"body":"<script>alert(1)</script>\n![x](https://example.test/x.png)"}`)
	if code != http.StatusUnprocessableEntity || !strings.Contains(response, "raw HTML") ||
		!strings.Contains(response, "image source") || !strings.Contains(response, "line 1") ||
		!strings.Contains(response, "line 2") {
		t.Fatalf("refusal = %d %s", code, response)
	}
	if strings.Contains(response, `"id":"`+id+`"`) || strings.Contains(response, `"body":"`+beforeBody+`"`) {
		t.Fatalf("refusal returned a stale row: %s", response)
	}
	var afterBody string
	var afterTime time.Time
	if err := admin.QueryRowContext(t.Context(), "SELECT body, updated_at FROM contents WHERE id = $1", id).
		Scan(&afterBody, &afterTime); err != nil {
		t.Fatal(err)
	}
	if afterBody != beforeBody || !afterTime.Equal(beforeTime) {
		t.Fatalf("refusal committed a row change: body %q -> %q, time %s -> %s", beforeBody, afterBody, beforeTime, afterTime)
	}
	var updatedEvents int
	if err := admin.QueryRowContext(t.Context(),
		"SELECT count(*) FROM platformkit_outbox WHERE name = 'content.content.updated'").Scan(&updatedEvents); err != nil {
		t.Fatal(err)
	}
	if updatedEvents != 0 {
		t.Fatalf("refusal emitted %d update events", updatedEvents)
	}
}
