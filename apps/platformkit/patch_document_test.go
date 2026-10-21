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
	"github.com/septagon-oss/platformkit/kit/entity"
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

// protocolPatchDocuments names the PATCH operations this composition serves that
// are not one resource's merge-patch, and the specification whose document their
// body is. A registered Resource says which fields its writer accepts, which is
// what makes the question above answerable at full strength; a module that mounts
// a machine protocol at the address that protocol names has no Resource to ask,
// because the body is the specification's own document and not a projection of one
// row. The list is what lets this gate say which operations it asked about, and it
// buys no latitude about their bodies: each one is still refused an open object, an
// untyped or undescribed property, a document that stopped serving it, and — for
// any PATCH in neither set — the refusal it already gave.
var protocolPatchDocuments = map[string]string{
	"scim-user-patch":  "RFC 7644 §3.5.2's Operations document, asked at /scim/v2/Users/{id}",
	"scim-group-patch": "RFC 7644 §3.5.2's Operations document, asked at /scim/v2/Groups/{id}",
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
	// A cover is a file this tenant owns, so the fixture uploads one through the file
	// module's own door. The content write path refuses a file id it cannot find in
	// this tenant, so an invented uuid is not a fixture for a picture key: the value
	// has to arrive the way a person's would.
	code, body = putFile(t, cfg, admin, filesPath, "cover.svg", "image/svg+xml", "<svg xmlns='http://www.w3.org/2000/svg'/>")
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", filesPath, code, body)
	}
	cover := field(t, body, "id")
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
		seed string
		// idFrom names where the create put the new row's identity: empty is the
		// top of the body, which is where every generated create answers.
		idFrom string
		values map[string]any
	}{
		"task-task-update": {seed: `{"title":"patch task"}`, values: map[string]any{
			"title": "changed task", "description": "new description", "status": "in_progress", "priority": "high", "source": "fixture", "sourceRef": "fixture-1", "dueAt": "2030-01-02T03:04:05Z", "slaDeadline": "2030-01-03T03:04:05Z",
		}},
		"content-content-update": {seed: `{"slug":"patch-content","title":"Patch content","body":"Before","kind":"page"}`, values: map[string]any{
			"slug": "changed-content", "title": "Changed content", "body": "New **body**\n", "kind": "post", "coverImageId": cover,
		}},
		"user-user-update": {seed: `{"email":"patch-person@example.test","displayName":"Patch person"}`, values: map[string]any{
			"email": "changed-person@example.test", "displayName": "Changed person",
		}},
		"billing-plan-update": {seed: `{"code":"patch-plan","name":"Patch plan","currency":"EUR","active":true}`, values: map[string]any{
			"code": "changed-plan", "name": "Changed plan", "priceCents": float64(1234), "currency": "USD", "interval": "year", "features": []any{"exports"}, "active": false,
		}},
		// The endpoint's create is no generated create: its answer is the signing
		// secret beside the row, and a row that carries a secret shown once is
		// never read back, so the row — and its id — is one level down. The one key
		// the generic PATCH may move is the label; the address and the event list
		// belong to `target`, and the lifecycle to its own commands.
		"webhook-endpoint-update": {seed: `{"name":"Patch endpoint","url":"https://hooks.example.test/ops","events":["task.task.created"]}`, idFrom: "endpoint", values: map[string]any{
			"name": "Changed endpoint",
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
				case entity.ServerOwned(f) && !f.ReadOnly:
					excluded[f.Name] = f.Name + " is this row's own write count"
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
			rowID := createdRowID(t, body, fixture.idFrom)
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
					if name == entity.RevisionField {
						if stored[name] != old.(float64)+1 {
							t.Errorf("patching %s moved revision from %v to %v, want one write", key, old, stored[name])
						}
						continue
					}
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
	for id, speaks := range protocolPatchDocuments {
		route, ok := wireRoute(doc, id)
		if !ok {
			t.Errorf("PATCH %s is listed as a protocol document that no longer answers: %s", id, speaks)
			continue
		}
		if seen[id] {
			t.Errorf("PATCH %s is a registered resource's merge-patch and no longer needs the protocol list", id)
			continue
		}
		shape := wireResolveRef(doc, patchRequestSchema(route))
		properties := wireMap(shape["properties"])
		if shape["type"] != "object" || shape["additionalProperties"] != false || len(properties) == 0 {
			t.Errorf("PATCH %s (%s) is not a closed typed body: %v", id, speaks, shape)
			continue
		}
		for key, raw := range properties {
			property, _ := raw.(map[string]any)
			if property["type"] == nil || property["description"] == nil {
				t.Errorf("PATCH %s documents %s with no type or no description: %v", id, key, property)
			}
		}
	}
	for _, item := range wireMap(doc["paths"]) {
		route := wireMap(wireMap(item)["patch"])
		id, _ := route["operationId"].(string)
		if len(route) > 0 && !seen[id] && protocolPatchDocuments[id] == "" {
			t.Errorf("PATCH %s has no registered resource conformance case and names no protocol document", id)
		}
	}
	if len(seen) != len(fixtures) {
		t.Errorf("registered PATCH count %d, fixtures %d", len(seen), len(fixtures))
	}
}

// createdRowID is the identity of the row a create just made, read out of that
// create's own answer. A generated create answers with the row, so the id sits at
// the top of the body; a create that must answer with more than the row wraps it,
// and `wrapper` names the key the row is wrapped under. Either way the row still
// has to answer at the resource's own read address afterwards, which is the
// promise this harness makes: where a create puts its id is a shape, not a waiver.
func createdRowID(t *testing.T, body, wrapper string) string {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("read the created row from %s: %v", body, err)
	}
	if wrapper != "" {
		row, ok := out[wrapper].(map[string]any)
		if !ok {
			t.Fatalf("the create answered no %q object: %s", wrapper, body)
		}
		out = row
	}
	id, _ := out["id"].(string)
	if id == "" {
		t.Fatalf("no id in %s", body)
	}
	return id
}
