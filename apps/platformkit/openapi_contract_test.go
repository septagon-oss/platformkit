package main

// The HTTP contract a native shell is written against, checked in and gated the
// way apps/platformkit/asyncapi_test.go gates the event contract: the golden is
// the document the running composition serves at /openapi.json, rendered by the
// same code that serves it, and a stale one fails make check naming the file and
// the byte.
//
// What a golden cannot do on its own is refuse a *breaking* change: UPDATE_GOLDEN=1
// rewrites whatever it is handed, and the shape a build already installed in a
// pocket parses does not care what was convenient to regenerate. So the pair is
// diffed by the rules in kit/wire before the flag is honoured,
// and those rules refuse the regeneration, not the change.
//
// UPDATE_GOLDEN=1 go test ./apps/platformkit -run OpenAPI rewrites the document.

import (
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/module"
	wiregate "github.com/septagon-oss/platformkit/kit/wire"
)

const openapiGolden = "testdata/openapi.json"

// contractHost is the address the checked-in document is rendered at. A document's
// `servers` is the installation that published it — kit/httpx writes
// "https://" + server.public_host there — and the artefact a device is generated
// from must not carry the address of one test fixture's process, which is what
// `platformkit.localhost` in apps/platformkit/app_test.go is. It is a host from
// RFC 2606, so it names the role and no machine: a generated client takes its base
// address from whoever configured the installation (the shell has a server field on
// its sign-in form for exactly that), and a change to the fixture's own public_host
// does not turn a contract no device depends on red.
const contractHost = "platformkit.example"

// deviceContractPaths is the set this contract promises a device: every address
// named here answers at the reference composition and appears in the golden. It is
// the mirror of TestEveryAliasRowOfTheReferenceApplicationLeadsSomewhereThatAnswers
// one level up — an address in a document that nothing answers at is a redirect
// into a 404 with an application behind it that cannot be fixed, which is what
// kit/httpx/aliases.go refuses for a route.
//
// The push registration (T-0113) is not in the list because nothing answers at that
// address yet; it joins in the same commit that mounts its door. The bearer key
// (T-0117) answered from the round that mounted it, so it is in the list below.
// That is what makes this list a gate and not a wish list.
var deviceContractPaths = []struct {
	operation string // the operationId the golden publishes
	method    string
	path      string
	// anonymous says the door answers for a caller nobody has recognised; the
	// others are read with the composition's own administrator.
	anonymous bool
	// body is what the probe sends, empty for the reads. A write needs one to be a
	// probe at all: the case refuses the 404 of an address nobody mounted, and an
	// address that answers 422 to nothing at all is the same hole wearing a hat.
	body string
}{
	{operation: "auth-login", method: http.MethodPost, path: "/api/v1/auth/login", anonymous: true,
		body: `{"email":"` + adminEmail + `","password":"` + adminPass + `"}`},
	{operation: "auth-me", method: http.MethodGet, path: "/api/v1/auth/me"},
	{operation: "auth-logout", method: http.MethodPost, path: "/api/v1/auth/logout"},
	{operation: "app-resources", method: http.MethodGet, path: "/api/v1/app/resources"},
	// The workspace's face, which a shell reads *before* it can authenticate: it is
	// the one address in this list a caller reaches with no credential at all, and
	// it joined in the round that mounted it rather than the round that documented
	// it, which is what keeps the list a gate.
	{operation: "app-connection", method: http.MethodGet, path: "/api/v1/app/connection", anonymous: true},
	{operation: "audit-event-list", method: http.MethodGet, path: "/api/v1/audit/events"},
	// The bearer key a shell presents instead of a session cookie. It joined this
	// list in the round that mounted its door (T-0117's auth-token-issue): a device
	// that authenticates with a key has to mint one somewhere first, and the address
	// it mints it at is part of what a generated client needs to exist at.
	{operation: "auth-token-issue", method: http.MethodPost, path: "/api/v1/auth/tokens",
		body: `{"name":"The device","scopes":["task:read"]}`},
}

// TestTheOpenAPIDocumentIsTheCompositionServed is the golden and the gate.
func TestTheOpenAPIDocumentIsTheCompositionServed(t *testing.T) {
	cfg, mods, fixture := deviceComposition(t)
	// The contract is rendered at the reference installation's name, not at the
	// address this fixture happens to listen on; see contractHost.
	cfg.Server.PublicHost = contractHost
	install(t, fixture.path)
	start(t, cfg, mods, fixture.opts)

	code, body := do(t, cfg, nil, http.MethodGet, acmeHost, "/openapi.json", "")
	if code != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d %s, want 200: the reference composition serves its document", code, body)
	}
	wiregate.GoldenWithAllowances(t, openapiGolden, func() []byte { return []byte(body) }, wireAuthorizationAllowances())
}

// TestTheCatalogOperationDescribesWhatAShellParses is the hole this delivery
// closes. While the catalog route answered with `any`, its entry in the published
// document described none of the body — and /api/v1/app/resources is the one
// address a native shell is generated from. The type reaches the document because
// the composition names it on the mount (app.WorkspaceCatalogRoute).
//
// This case reads the checked-in artefact, so what keeps it honest is the pair of
// cases that read the running process: TestTheOpenAPIDocumentIsTheCompositionServed
// boots the composition through appOptions and compares, and
// review_round3_composition_publishes_its_own_contract_test.go reads the document
// that process answers with and names the JSON key that is missing when the type is
// erased. A composition that loses the type is red there; a case that reads only
// this file would have stayed green beside a binary publishing {"schema":{}}.
func TestTheCatalogOperationDescribesWhatAShellParses(t *testing.T) {
	t.Parallel()
	doc := wireDocument(t, mustReadOpenAPIGolden(t))
	route, ok := wireRoute(doc, "app-resources")
	if !ok {
		t.Fatalf("%s publishes no app-resources operation", openapiGolden)
	}
	// The body is a component the document points at, which is what a validator
	// follows; this case follows it too, because the claim is about the fields.
	schema := wireResolveRef(doc, jsonResponseSchema(route))
	if schema == nil {
		t.Fatalf("app-resources has no application/json response schema in %s", openapiGolden)
	}
	// Every name below is a JSON tag of ui/screens.Catalog or ui/screens.Entry. The
	// optional ones are optional exactly as their tags say; a shell written against
	// the document still reads a document that adds a key.
	for _, name := range []string{"catalogVersion", "resources"} {
		// Key presence, not wireProperty(...) == nil: wireMap returns the empty map
		// rather than nil for a name that is not there, so that condition cannot hold
		// and the assertion would be decoration. The entry loop below reads it this way.
		if _, ok := wireMap(schema["properties"])[name]; !ok {
			t.Errorf("the published catalog document has no %q: the shape a shell parses is not in the contract", name)
		}
	}
	if required := wireStringList(schema["required"]); !slices.Contains(required, "catalogVersion") || !slices.Contains(required, "resources") {
		t.Errorf("the catalog document requires %v; both keys are always written, so a validator reading this document would accept a body with neither", required)
	}
	entry := wireItemsOf(doc, schema, "resources")
	if entry == nil {
		t.Fatal("the catalog document's resources are not an array of objects")
	}
	for _, name := range []string{"module", "entity", "path", "writable", "immutable", "screen", "write_path", "commands", "singleton", "operations", "presentation"} {
		if _, ok := wireMap(entry["properties"])[name]; !ok {
			t.Errorf("a catalog entry in the published document has no %q: a generated screen would read a field the contract never mentions", name)
		}
	}
	// `presentation` is the one key of these an entry may omit, and the document
	// has to say so: a shell that required it would refuse every resource whose
	// author said nothing about how it reads, which is every resource written
	// before the key existed. TestThePresentationKeysAreNeverRequired reads the
	// same artefact for the other half — that no hint is required anywhere.
	if required := wireStringList(entry["required"]); slices.Contains(required, "presentation") {
		t.Errorf("a catalog entry requires %v; an entry nobody hinted prints no presentation block at all", required)
	}
}

// TestEveryDeviceContractAddressAnswers reads each address twice: once at the
// running application, where a 404 is the answer of an address nothing mounted, and
// once in the document, where an absent path is a promise the composition does not
// keep. Both are the same defect seen from either end of a phone.
func TestEveryDeviceContractAddressAnswers(t *testing.T) {
	cfg, mods, fixture := deviceComposition(t)
	install(t, fixture.path)
	start(t, cfg, mods, fixture.opts)
	doc := wireDocument(t, mustReadOpenAPIGolden(t))
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	for _, want := range deviceContractPaths {
		client := admin
		if want.anonymous {
			client = nil
		}
		code, answer := do(t, cfg, client, want.method, acmeHost, want.path, want.body)
		// Anything but the answer of an address nobody mounted. A 401 or a 403 is a
		// door refusing a caller, which is a door; the 404 this case refuses is a hole.
		if code == http.StatusNotFound || strings.Contains(answer, "nothing is served at this address") {
			t.Errorf("%s %s = %d: the contract names an address nothing answers at", want.method, want.path, code)
		}
		route, ok := wireRoute(doc, want.operation)
		if !ok {
			t.Errorf("%s has no operation in %s", want.operation, openapiGolden)
			continue
		}
		if _, ok := wireMap(wireMap(doc["paths"])[want.path])[strings.ToLower(want.method)]; !ok {
			t.Errorf("%s is not published as %s in %s: %v", want.path, want.method, openapiGolden, route["operationId"])
		}
	}
}

// TestTheServedDocumentCarriesNoEphemeralBytes reads the artefact for the things
// that belong to no run: a title, a build version, and no host, tenant, address or
// mailbox from one test process. The tenant-first claim beside it — that the
// document is a property of the composition, so the one thing that may differ
// between what an operator is told and what a tenant's user is told is the
// authorization declaration on each route — is proved per caller by
// TestEveryDeviceContractAddressAnswers reading each route's own
// x-platformkit-auth, which is the half of that claim this file publishes.
func TestTheServedDocumentCarriesNoEphemeralBytes(t *testing.T) {
	t.Parallel()
	body := mustReadOpenAPIGolden(t)
	doc := wireDocument(t, body)
	info := wireMap(doc["info"])
	if info["title"] != "PlatformKit" {
		t.Errorf("info.title = %v", info["title"])
	}
	// info.version is the release string of the build that served it, not a contract
	// version: ui/screens.CatalogVersion is the version of the body a shell parses,
	// and nothing here invents a second number beside it.
	if version, _ := info["version"].(string); version == "" {
		t.Error("info.version is empty; the document says nothing about who published it")
	}
	for _, ephemeral := range []string{"/tmp", "127.0.0.1:", ".localhost", acmeHost, globexHost, adminEmail} {
		if strings.Contains(string(body), ephemeral) {
			t.Errorf("%s carries %q, which is a fact about one run and not about the contract", openapiGolden, ephemeral)
		}
	}
	// The one address that is about the installation rather than the contract, said
	// once and at the host the contract is rendered at.
	servers, _ := doc["servers"].([]any)
	if len(servers) != 1 {
		t.Errorf("%s publishes %d servers; a generator needs one base address, which is contractHost", openapiGolden, len(servers))
	} else if url, _ := wireMap(servers[0])["url"].(string); url != "https://"+contractHost {
		t.Errorf("%s is published at %q, not at %q: see contractHost", openapiGolden, url, "https://"+contractHost)
	}
	for _, extension := range []string{"x-platformkit-surface", "x-platformkit-auth"} {
		if !strings.Contains(string(body), extension) {
			t.Errorf("%s names no %s: every route's surface and permission belong in the document a shell reads", openapiGolden, extension)
		}
	}
}

// TestTheWireGateRefusesEachRuleOnTheRealDocument runs each rule over the
// composition's own document with exactly one thing done to it, so the gate is
// proven against the contract it will actually be held to rather than against a toy
// document that happens to agree. Every case mutates a copy; the golden is read.
func TestTheWireGateRefusesEachRuleOnTheRealDocument(t *testing.T) {
	t.Parallel()
	golden := mustReadOpenAPIGolden(t)
	const catalog = "paths:/api/v1/app/resources:get:responses:200:content:application/json:schema"

	for _, tc := range []struct {
		name string
		rule string
		// before and after are what the two documents hold. A case whose before is
		// nil is the golden itself, so the change is one-sided by construction; the
		// narrowing case states both, because an enum that joined a request in the
		// first place is additive and only the narrowing may be refused.
		before func(t *testing.T, doc map[string]any)
		after  func(t *testing.T, doc map[string]any)
	}{
		{"the catalog address is gone", "B1", nil, func(t *testing.T, doc map[string]any) {
			delete(wireMap(doc["paths"]), "/api/v1/app/resources")
		}},
		{"the catalog address is gone, so its operation went with it", "B2", nil, func(t *testing.T, doc map[string]any) {
			delete(wireMap(doc["paths"]), "/api/v1/app/resources")
		}},
		{"the catalog address changes hands", "B2", nil, func(t *testing.T, doc map[string]any) {
			wireAt(t, doc, "paths:/api/v1/app/resources:get")["operationId"] = "app-something-else"
		}},
		{"a field of the catalog document is removed", "B3", nil, func(t *testing.T, doc map[string]any) {
			delete(wireMap(wireAt(t, doc, "components:schemas:Catalog")["properties"]), "catalogVersion")
		}},
		{"a field of the catalog document is retyped", "B3", nil, func(t *testing.T, doc map[string]any) {
			wireAt(t, doc, "components:schemas:Catalog:properties:catalogVersion")["type"] = "string"
		}},
		{"an existing write starts demanding a key", "B4", nil, func(t *testing.T, doc map[string]any) {
			task := wireAt(t, doc, "components:schemas:Task")
			task["required"] = append(wireStringList(task["required"]), "requested_by")
		}},
		{"a response's enumerated set grows past what a reader branches on", "B5", func(t *testing.T, doc map[string]any) {
			wireAt(t, doc, "components:schemas:Entry:properties:screen")["enum"] = []any{"a"}
		}, func(t *testing.T, doc map[string]any) {
			wireAt(t, doc, "components:schemas:Entry:properties:screen")["enum"] = []any{"a", "b"}
		}},
		{"a write stops accepting a value an older client sends", "B5", func(t *testing.T, doc map[string]any) {
			wireAt(t, doc, "components:schemas:Task:properties:priority")["enum"] = []any{"high", "low"}
		}, func(t *testing.T, doc map[string]any) {
			wireAt(t, doc, "components:schemas:Task:properties:priority")["enum"] = []any{"high"}
		}},
		{"a route's authorization changes", "B6", nil, func(t *testing.T, doc map[string]any) {
			wireAt(t, doc, "paths:/api/v1/app/resources:get:x-platformkit-auth")["kind"] = "public"
		}},
		{"the catalog door narrows back to a browser's credential", "B6", nil, func(t *testing.T, doc map[string]any) {
			// The widening the rule allows, run backwards: any_credential is the door
			// as mounted (httpx.AnyCredential), and signed_in is what it was before
			// T-0117 widened it. Read as a change from the golden to this, it is a door
			// that stops admitting a bearer key, which is exactly what an installed
			// client holding one was promised. B6 refuses it, and refuses it whichever
			// way the pair is read.
			wireAt(t, doc, "paths:/api/v1/app/resources:get:x-platformkit-auth")["kind"] = "signed_in"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reference := golden
			if tc.before != nil {
				reference = wireMutated(t, golden, tc.before)
			}
			problems := breakingWireChanges(t, reference, wireMutated(t, reference, tc.after))
			if len(problems) == 0 {
				t.Fatalf("the gate accepted it; %s was expected", tc.rule)
			}
			if !strings.Contains(strings.Join(problems, "\n"), tc.rule) {
				t.Errorf("the gate said %q, which does not name %s", problems, tc.rule)
			}
		})
	}

	// The changes a delivery is allowed to make on its own: a new address, a new
	// optional field on a document a shell already reads, and a door that admits
	// every credential it admitted and one more. The first two are what
	// ui/screens/catalog.go promises beside CatalogVersion — additive, optional, and
	// never a change of meaning — and the third is the one pair in
	// widenableAuthorization; a composition that grows must not fight the gate.
	// `before` is the golden as the older document, so a case can state both sides.
	for _, tc := range []struct {
		name   string
		before func(t *testing.T, doc map[string]any)
		after  func(t *testing.T, doc map[string]any)
	}{
		{"a new optional field on a catalog entry", nil, func(t *testing.T, doc map[string]any) {
			wireAt(t, doc, "components:schemas:Entry:properties")["device_label"] = map[string]any{"type": "string"}
		}},
		{"a new address answering a new operation", nil, func(t *testing.T, doc map[string]any) {
			wireMap(doc["paths"])["/api/v1/app/devices"] = map[string]any{"post": map[string]any{
				"operationId": "app-device-register",
				"responses":   map[string]any{"204": map[string]any{"description": "No Content"}},
			}}
		}},
		{"a signed-in door widens to any credential", func(t *testing.T, doc map[string]any) {
			wireAt(t, doc, "paths:/api/v1/app/resources:get:x-platformkit-auth")["kind"] = "signed_in"
		}, func(t *testing.T, doc map[string]any) {
			wireAt(t, doc, "paths:/api/v1/app/resources:get:x-platformkit-auth")["kind"] = "any_credential"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reference := golden
			if tc.before != nil {
				reference = wireMutated(t, golden, tc.before)
			}
			if problems := breakingWireChanges(t, reference, wireMutated(t, reference, tc.after)); len(problems) != 0 {
				t.Errorf("an additive change was refused: %q", problems)
			}
		})
	}
}

// wireMutated is the document with one thing done to it, rendered the way the
// golden is rendered so the pair differs only in the thing the case changed.
func wireMutated(t *testing.T, body []byte, mutate func(t *testing.T, doc map[string]any)) []byte {
	t.Helper()
	doc := wireDocument(t, body)
	mutate(t, doc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("render the mutated document: %v", err)
	}
	return out
}

// wireAt is one node of the document, addressed by its keys with ":" between them,
// which is how the cases above name a field without restating the path to it.
func wireAt(t *testing.T, doc map[string]any, address string) map[string]any {
	t.Helper()
	node := doc
	for _, key := range strings.Split(address, ":") {
		child := wireMap(node[key])
		if len(child) == 0 {
			t.Fatalf("%s has no %q", address, key)
		}
		node = child
	}
	return node
}

func mustReadOpenAPIGolden(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile(openapiGolden)
	if err != nil {
		t.Fatalf("read %s: %v (run with UPDATE_GOLDEN=1)", openapiGolden, err)
	}
	return body
}

// deviceComposition is the reference application as the product boots it, with the
// path of the configuration written for it so a case can bootstrap a tenant. Every
// case here starts the same process the README's five commands start; a case that
// spelled out its own module list would drift from apps/platformkit/modules.go the
// way a hand-copied schema drifts from the type — and so would a case that spelled
// out its own app.Options, which is why this one takes them from appOptions.
// Options written here arrive with WorkspaceCatalog nil, which start() then fills
// with the product's mount; the document such a fixture serves is the fixture's, and
// a contract gate comparing it proves nothing about what the composition wires.
type deviceFixture struct {
	path string
	opts app.Options
}

func deviceComposition(t *testing.T) (config.Config, []module.Module, deviceFixture) {
	t.Helper()
	path, cfg := configure(t)
	c := compose(cfg)
	opts := appOptions(cfg, c, app.All)
	// The two fields a test process must name for itself, exactly as persona_test.go
	// names them: an in-memory transport and a quiet log. Neither reaches the document.
	opts.Transport, opts.Log = memory.New(), quiet()
	return cfg, c.modules, deviceFixture{path: path, opts: opts}
}
