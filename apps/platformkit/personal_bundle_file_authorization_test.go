package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestReadingOrdinaryFilesDoesNotGrantAnotherPersonsExport(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport, options.Log = memory.New(), quiet()
	start(t, cfg, c.modules, options)
	email := "private.bundle@acme.localhost"
	provisionAs(t, cfg, c, acmeTenant(t, cfg), email, "member")
	person := signIn(t, cfg, acmeHost, email, adminPass)
	code, body := do(t, cfg, person, http.MethodPost, acmeHost, exportsPath, `{}`)
	if code != http.StatusAccepted {
		t.Fatalf("request export: %d %s", code, body)
	}
	request := field(t, body, "id")
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	runExportWalk(t, c, conn)
	code, body = do(t, cfg, person, http.MethodGet, acmeHost, exportsPath+"/"+request, "")
	if code != http.StatusOK || field(t, body, "state") != "ready" {
		t.Fatalf("export did not become ready: %d %s", code, body)
	}
	bundle := field(t, body, "bundle")

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/auth/tokens",
		`{"name":"Read ordinary files","scopes":["file:read"]}`)
	if code != http.StatusCreated {
		t.Fatalf("mint file-only credential: %d %s", code, body)
	}
	key := field(t, body, "token")
	if code, body := doKey(t, cfg, key, http.MethodPost, acmeHost, exportsPath+"/for/"+request+"/link", ""); code != http.StatusForbidden {
		t.Errorf("file-only credential at the subject export door: %d %s", code, body)
	}
	code, body = doKey(t, cfg, key, http.MethodGet, acmeHost, "/api/v1/file/files", "")
	if code != http.StatusOK {
		t.Fatalf("file-only credential cannot list ordinary files: %d %s", code, body)
	}
	t.Logf("file-only credential can discover the bundle in the file list: %v", strings.Contains(body, bundle))
	// Knowing a file ID is not authorization, even if a future file list hides it.
	code, body = doKey(t, cfg, key, http.MethodGet, acmeHost, "/api/v1/file/files/"+bundle+"/content", "")
	if code != http.StatusForbidden && code != http.StatusNotFound {
		t.Errorf("file:read alone downloaded another person's export: status=%d contains private email=%v; want 403 or 404",
			code, strings.Contains(body, email))
	}
}
