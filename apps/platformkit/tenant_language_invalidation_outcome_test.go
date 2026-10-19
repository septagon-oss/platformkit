package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestTenantLanguageWriteReportsAnUnfinishedInvalidation(t *testing.T) {
	for _, surface := range []string{"api", "page"} {
		t.Run(surface, func(t *testing.T) {
			path, cfg := configure(t)
			install(t, path)
			cfg.Cache = config.Cache{Adapter: "valkey", App: "tenant-language-outcome", URL: "valkey://127.0.0.1:6379"}
			backend := cache.MemoryBackend()
			t.Cleanup(func() { _ = backend.Close() })
			store := &storeThatRefusesTheFirstClose{Cache: cache.MemoryNamed(cfg.Cache.App, backend)}
			c := compose(cfg)
			options := appOptions(cfg, c, app.All)
			options.Transport = memory.New()
			options.Log = quiet()
			options.Caches = app.Caches{Valkey: func(context.Context, config.Cache) (cache.Cache, error) { return store, nil }}
			start(t, cfg, c.modules, options)
			admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
			code, held := do(t, cfg, admin, http.MethodGet, acmeHost, "/api/v1/tenant/languages", "")
			if code != http.StatusOK {
				t.Fatalf("read the current declaration = %d: %s", code, held)
			}
			var current struct {
				Revision *int64 `json:"revision"`
			}
			if err := json.Unmarshal([]byte(held), &current); err != nil || current.Revision == nil {
				t.Fatalf("read the declaration revision: %v: %s", err, held)
			}
			revision := fmt.Sprint(*current.Revision)
			code, _, _, _ = r7navigate(t, cfg, admin, acmeHost, "/app", "en")
			if code != http.StatusOK {
				t.Fatalf("warm the tenant's dashboard = %d", code)
			}
			store.refused.Store(true)
			write := func() (int, string) {
				if surface == "page" {
					return postLanguages(t, cfg, admin, revision)
				}
				return do(t, cfg, admin, http.MethodPut, acmeHost, "/api/v1/tenant/languages",
					fmt.Sprintf(`{"default":"pt-PT","supported":["en"],"expectedRevision":%s}`, revision))
			}
			code, body := write()
			if code != http.StatusServiceUnavailable {
				t.Errorf("tenant language %s reports %d while the store refused invalidation; want 503: %s", surface, code, firstLineOf(body))
			}
			code, held = do(t, cfg, admin, http.MethodGet, acmeHost, "/api/v1/tenant/languages", "")
			if code != http.StatusOK || !strings.Contains(held, `"default":"pt-PT"`) {
				t.Fatalf("the committed declaration was lost = %d: %s", code, held)
			}
			beforeRetry, _, language, html := r7navigate(t, cfg, admin, acmeHost, "/app", "de")
			t.Logf("committed default is pt-PT; next dashboard before retry: status=%d Content-Language=%q lang=%q", beforeRetry, language, pageLang(html))
			code, body = write()
			want := http.StatusOK
			if surface == "page" {
				want = http.StatusSeeOther
			}
			if code != want {
				t.Fatalf("retry after the store recovers = %d, want %d: %s", code, want, firstLineOf(body))
			}
			code, _, declared, body := r7navigate(t, cfg, admin, acmeHost, "/app", "pt-PT")
			if code != http.StatusOK || declared != "pt-PT" || pageLang(body) != "pt-PT" {
				t.Errorf("retry did not finish the language change: status=%d header=%q lang=%q", code, declared, pageLang(body))
			}
		})
	}
}
