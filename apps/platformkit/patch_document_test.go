package main

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestNoRequestBodyDocumentsAnOpenUntypedObject(t *testing.T) {
	doc := wireDocument(t, mustReadOpenAPIGolden(t))
	for path, item := range wireMap(doc["paths"]) {
		for _, verb := range wireVerbs {
			route := wireMap(wireMap(item)[verb])
			for media, raw := range wireMap(wireMap(route["requestBody"])["content"]) {
				schema := wireResolveRef(doc, wireMap(wireMap(raw)["schema"]))
				if schema["type"] == "object" && len(wireMap(schema["properties"])) == 0 && schema["additionalProperties"] != false {
					t.Errorf("%s %s %s has an untyped request body: %v", verb, path, media, schema)
				}
			}
		}
	}
}

func TestEveryComposedPatchDocumentsExactlyTheKeysItWrites(t *testing.T) {
	cfg, mods, fixture := deviceComposition(t)
	var mu sync.Mutex
	var resources []httpx.Resource
	catalog := fixture.opts.WorkspaceCatalog
	fixture.opts.WorkspaceCatalog = func(api *httpx.API) {
		mu.Lock()
		resources = api.Resources()
		mu.Unlock()
		catalog(api)
	}
	install(t, fixture.path)
	start(t, cfg, mods, fixture.opts)
	mu.Lock()
	registered := slices.Clone(resources)
	mu.Unlock()
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, nil, http.MethodGet, acmeHost, "/openapi.json", "")
	if code != 200 {
		t.Fatalf("document = %d %s", code, body)
	}
	doc := wireDocument(t, []byte(body))
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	tenant := tenancy.Tenant{ID: acmeTenant(t, cfg)}
	events := func(name string) int {
		t.Helper()
		var n int
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Raw(`SELECT count(*) FROM platformkit_outbox WHERE name = ?`, name).Row().Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	// Values are fixtures, never the authority for writable keys. The expectation
	// below comes from the registered Resource, and a newly writable key fails here
	// until it has a valid value and is driven through its actual route.
	fixtures := map[string]struct {
		seed   string
		values map[string]any
	}{
		"task-task-update": {`{"title":"patch task"}`, map[string]any{
			"title": "changed task", "description": "new description", "status": "in_progress", "priority": "high", "source": "fixture", "sourceRef": "fixture-1", "dueAt": "2030-01-02T03:04:05Z", "slaDeadline": "2030-01-03T03:04:05Z",
		}},
		"content-content-update": {`{"slug":"patch-content","title":"Patch content","body":"Before","kind":"page"}`, map[string]any{
			"slug": "changed-content", "title": "Changed content", "body": "New **body**\n", "kind": "post",
		}},
		"user-user-update": {`{"email":"patch-person@example.test","displayName":"Patch person"}`, map[string]any{
			"email": "changed-person@example.test", "displayName": "Changed person",
		}},
		"billing-plan-update": {`{"code":"patch-plan","name":"Patch plan","currency":"EUR","active":true}`, map[string]any{
			"code": "changed-plan", "name": "Changed plan", "priceCents": float64(1234), "currency": "USD", "interval": "year", "features": []any{"exports"}, "active": false,
		}},
	}
	encode := func(v any) string {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	seen := map[string]bool{}
	for _, res := range registered {
		if res.Singleton || !res.Offers(httpx.CRUDUpdate) {
			continue
		}
		id := res.Module + "-" + res.Entity + "-update"
		seen[id] = true
		t.Run(id, func(t *testing.T) {
			fixture, ok := fixtures[id]
			if !ok {
				t.Fatalf("new PATCH %s needs a valid seed and field values", id)
			}
			path := res.WritePath
			if path == "" {
				path = res.Schema.Path
			}
			route := wireMap(wireMap(wireMap(doc["paths"])[path+"/{id}"])["patch"])
			if route["operationId"] != id {
				t.Fatalf("registered resource PATCH is missing at %s: %v", path, route)
			}
			shape := wireResolveRef(doc, patchRequestSchema(route))
			properties := wireMap(shape["properties"])
			if shape["type"] != "object" || shape["additionalProperties"] != false || len(wireStringList(shape["required"])) != 0 {
				t.Fatalf("PATCH is not a closed optional object: %v", shape)
			}
			writable := map[string]bool{}
			excluded := map[string]string{}
			for _, f := range res.Schema.Fields {
				reserved := slices.ContainsFunc(res.Immutable, func(name string) bool { return strings.EqualFold(f.Name, name) })
				switch {
				case reserved:
					excluded[f.Name] = f.Name + " belongs to a route of its own"
				case f.ReadOnly:
					excluded[f.Name] = f.Name + " is read-only"
				default:
					writable[f.Name] = true
				}
			}
			if len(properties) != len(writable) {
				t.Errorf("document has %d keys, decoder has %d", len(properties), len(writable))
			}
			for key := range properties {
				if !writable[key] {
					t.Errorf("document advertises unwritable %s", key)
				}
			}
			code, body := do(t, cfg, admin, http.MethodPost, acmeHost, path, fixture.seed)
			if code != 201 {
				t.Fatalf("seed = %d %s", code, body)
			}
			rowID := field(t, body, "id")
			at := path + "/" + rowID
			read := func() map[string]any {
				t.Helper()
				code, body := do(t, cfg, admin, http.MethodGet, acmeHost, res.Schema.Path+"/"+rowID, "")
				if code != 200 {
					t.Fatalf("read = %d %s", code, body)
				}
				return wireDocument(t, []byte(body))
			}
			for key := range writable {
				if _, ok := properties[key]; !ok {
					t.Errorf("document omits writable %s", key)
				}
				value, ok := fixture.values[key]
				if !ok {
					t.Fatalf("writable key %s needs a valid fixture", key)
				}
				before := read()
				code, body := do(t, cfg, admin, http.MethodPatch, acmeHost, at, encode(map[string]any{key: value}))
				if code != 200 {
					t.Fatalf("PATCH %s = %d %s", key, code, body)
				}
				stored := read()
				if !reflect.DeepEqual(stored[key], value) {
					t.Errorf("%s stored %v, want %v", key, stored[key], value)
				}
				for name, old := range before {
					if name != key && name != "updatedAt" && !reflect.DeepEqual(stored[name], old) {
						t.Errorf("patching %s also changed %s: %v -> %v", key, name, old, stored[name])
					}
				}
			}
			// Exact-case lookup and the immutable precheck must still supply these
			// errors; replacing the captured map validator would change their wording.
			excluded["unknownPatchField"] = "there is no field"
			excluded["PasswordHash"] = "there is no field"
			for key := range writable {
				excluded[strings.ToUpper(key)] = "there is no field"
				break
			}
			before := read()
			eventName := res.Module + "." + res.Entity + ".updated"
			emitted := events(eventName)
			for key, detail := range excluded {
				code, body := do(t, cfg, admin, http.MethodPatch, acmeHost, at, encode(map[string]any{key: nil}))
				if code != 422 || !strings.Contains(body, detail) {
					t.Errorf("PATCH %s = %d %s, want merge error %q", key, code, body, detail)
				}
			}
			if code, body := do(t, cfg, admin, http.MethodPatch, acmeHost, at, `{}`); code != 200 {
				t.Errorf("empty PATCH = %d %s", code, body)
			}
			if after := read(); !reflect.DeepEqual(before, after) {
				t.Errorf("refused/empty patches changed row: %v -> %v", before, after)
			}
			if n := events(eventName); n != emitted {
				t.Errorf("refused/empty patches emitted %d updates", n-emitted)
			}
		})
	}
	for _, item := range wireMap(doc["paths"]) {
		route := wireMap(wireMap(item)["patch"])
		if len(route) > 0 && !seen[route["operationId"].(string)] {
			t.Errorf("PATCH %s has no registered resource conformance case", route["operationId"])
		}
	}
	if len(seen) != len(fixtures) {
		t.Errorf("registered PATCH count %d, fixtures %d", len(seen), len(fixtures))
	}
}
