package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/net/html"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestLocalizedCompositionKeepsSurfaceBoundaries(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	opts := appOptions(cfg, c, app.All)
	opts.Transport, opts.Log = memory.New(), quiet()
	start(t, cfg, c.modules, opts)

	code, _, body := get(t, cfg, nil, "/app")
	if code != http.StatusOK || !strings.Contains(string(body), "data-login-form") {
		t.Fatalf("anonymous /app did not lead to the sign-in page: %d %s", code, body)
	}
	t.Log("anonymous /app answered the sign-in page")
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+cfg.Server.Addr+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = acmeHost
	req.Header.Set("Accept-Language", "pt-PT")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || len(res.Header.Values("Set-Cookie")) != 0 {
		t.Errorf("public home: status %d, cookies %v", res.StatusCode, res.Header.Values("Set-Cookie"))
	}
	t.Logf("public home: status %d, Set-Cookie %v", res.StatusCode, res.Header.Values("Set-Cookie"))
	linkedWorkspace := false
	tokens := html.NewTokenizer(res.Body)
	for tokens.Next() != html.ErrorToken {
		for _, attr := range tokens.Token().Attr {
			if attr.Key != "href" {
				continue
			}
			linkedWorkspace = linkedWorkspace || attr.Val == "/app"
			if strings.HasPrefix(attr.Val, "/app/") {
				t.Errorf("public frame links deeper than the workspace root: %s", attr.Val)
			}
		}
	}
	if err := tokens.Err(); err != io.EOF {
		t.Fatal(err)
	}
	if !linkedWorkspace {
		t.Error("public frame does not link /app")
	}

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, answer := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("create client tenant: %d %s", code, answer)
	}
	provision(t, cfg, uuid.MustParse(field(t, answer, "id")), "root@globex.localhost")
	client := signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)
	for _, at := range []string{"/ops", "/api/v1/ops/tenant/tenants"} {
		if code, body := do(t, cfg, client, http.MethodGet, globexHost, at, ""); code != http.StatusNotFound {
			t.Errorf("client host reached %s: %d %s", at, code, body)
		} else {
			t.Logf("client host %s answered 404", at)
		}
	}
}
