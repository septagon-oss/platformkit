package main

import (
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestServedResolvedCatalogueSeparatesLanguagesAndPermissionSets(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	acmeID := installationTenantID(t, cfg, admin, "acme")
	declareLocale(t, cfg, admin, acmeID, `{"default":"en","supported":["en","pt-PT"]}`)
	tokens := map[bool]string{}
	for _, write := range []bool{false, true} {
		scopes := `["task:read"]`
		if write {
			scopes = `["task:read","task:update"]`
		}
		code, body := do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/auth/tokens",
			`{"name":"catalogue-reader","scopes":`+scopes+`}`)
		if code != http.StatusCreated {
			t.Fatalf("mint catalogue credential: status %d", code)
		}
		tokens[write] = field(t, body, "token")
	}
	type request struct {
		language string
		write    bool
	}
	bodies := map[request][32]byte{}
	for range 2 {
		for _, language := range []string{"pt-PT", "en"} {
			for _, write := range []bool{true, false} {
				key := request{language, write}
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
					"http://"+cfg.Server.Addr+catalogPath, nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Host = acmeHost
				req.Header.Set("Accept-Language", language)
				req.Header.Set("Authorization", "Bearer "+tokens[write])
				response, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil || response.StatusCode != http.StatusOK {
					t.Fatalf("catalogue %+v: status %d, read error %v", key, response.StatusCode, err)
				}
				noStore := slices.ContainsFunc(strings.Split(response.Header.Get("Cache-Control"), ","), func(directive string) bool {
					return strings.TrimSpace(directive) == "no-store"
				})
				if response.Header.Get("Vary") != "Accept-Language" || !noStore {
					t.Errorf("catalogue %+v: Vary=%q Cache-Control=%q", key, response.Header.Get("Vary"), response.Header.Get("Cache-Control"))
				}
				hash := sha256.Sum256(body)
				if before, ok := bodies[key]; ok && before != hash {
					t.Errorf("catalogue %+v changed after other callers read it", key)
				}
				for other, before := range bodies {
					if other != key && before == hash {
						t.Errorf("catalogues %+v and %+v shared one body", other, key)
					}
				}
				bodies[key] = hash
				var catalogue screens.Catalog
				if err := json.Unmarshal(body, &catalogue); err != nil {
					t.Fatal(err)
				}
				at := slices.IndexFunc(catalogue.Resources, func(e screens.Entry) bool { return e.Module == "task" && e.Entity == "task" })
				if at < 0 || catalogue.Resources[at].Resolved == nil {
					t.Fatal("the authorized task resource has no resolved policy")
				}
				entry := catalogue.Resources[at]
				if entry.Resolved.Language != language || entry.Writable != write {
					t.Errorf("catalogue %+v: resolved language %q, writable %t", key, entry.Resolved.Language, entry.Writable)
				}
				if len(entry.Commands) != len(entry.Resolved.Commands) || (len(entry.Resolved.Commands) > 0) != write {
					t.Errorf("catalogue %+v: command counts raw=%d resolved=%d", key, len(entry.Commands), len(entry.Resolved.Commands))
				}
				for _, f := range entry.Resolved.Fields {
					if f.Name == "slaBreached" {
						want := "No"
						if language == "pt-PT" {
							want = "Não"
						}
						if f.Reading.False != want {
							t.Errorf("catalogue %+v: false word %q, want %q", key, f.Reading.False, want)
						}
					}
				}
			}
		}
	}
}
