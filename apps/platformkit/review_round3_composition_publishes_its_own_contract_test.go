package main

// Review 3's pin over the one production line this delivery changed.
//
// apps/platformkit/fault.go's WorkspaceCatalog literal is what makes the published
// document name the fields a native shell parses — the commit body of 6c41681 calls
// it "typed where a device reads it", and openapi_contract_test.go's
// TestTheCatalogOperationDescribesWhatAShellParses says "this case is red for a
// composition that loses the type again". Neither half of the gate can see that
// literal: the contract gate boots deviceComposition, which spells its own
// app.Options and so arrives with WorkspaceCatalog nil, and start() then hands it a
// renderer written a second time inside app_test.go. The checked-in document is the
// fixture's, not the composition's.
//
// Reproduction of the hole, at 5fe47ff, with fault.go's renderer rewritten to hand
// back *any (the type erased — the state the case above promises to refuse):
//
//	go test ./apps/platformkit -count=1        -> ok
//
// The binary this repository ships would then publish a catalog whose body it
// describes as an empty object, and every gate here would stay green while a shell
// generated from the contract read nothing.
//
// This case closes the distance by asking the question of the composition itself: it
// boots through appOptions — the function run, serve and bootstrap share, the one
// persona_test.go, review_surfaces_test.go and app_fault_test.go already use — and
// reads the document that process answers with. The assertion is reached through what
// the correct behaviour prints: the response schema names catalogVersion, resources
// and the Entry keys. A composition that erases the type prints a schema with no
// properties, and the case fails naming the key that went missing. No assertion here
// depends on the wording of a refusal, a redirect or an unreachable branch.

import (
	"net/http"
	"os"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestThePublishedCatalogContractComesFromTheCompositionItself boots the reference
// application the way its entry points do and reads the contract off that process.
func TestThePublishedCatalogContractComesFromTheCompositionItself(t *testing.T) {
	path, cfg := configure(t)
	// The same host the golden is rendered at, so the byte comparison below means
	// "this composition published this contract" and not "these two fixtures differ
	// in where they say they live".
	cfg.Server.PublicHost = contractHost
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	// The two fields a test process must name for itself, exactly as
	// persona_test.go names them: an in-memory transport and a quiet log. Neither
	// reaches the published document.
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	code, body := do(t, cfg, nil, http.MethodGet, acmeHost, "/openapi.json", "")
	if code != http.StatusOK {
		t.Fatalf("GET /openapi.json from the reference composition = %d %s, want 200", code, body)
	}
	served := []byte(body)

	doc := wireDocument(t, served)
	route, ok := wireRoute(doc, "app-resources")
	if !ok {
		t.Fatal("the composition apps/platformkit/fault.go wires publishes no app-resources operation")
	}
	schema := wireResolveRef(doc, jsonResponseSchema(route))
	if schema == nil {
		t.Fatal("app-resources has no application/json response schema at the address a device is generated from")
	}
	// The JSON tags of ui/screens.Catalog, which is what fault.go's renderer returns.
	// A renderer handed back as `any` prints none of them: the body is still valid
	// JSON on the wire, and the contract that describes it says nothing — which is
	// the failure a generated shell cannot recover from at runtime.
	// Read as key presence, not through wireProperty: that helper returns the empty
	// map rather than nil for a name that is not there (wire_compatibility_test.go's
	// wireMap never returns nil), so `wireProperty(...) == nil` is a condition that
	// cannot hold and the assertion would be decoration.
	for _, name := range []string{"catalogVersion", "resources"} {
		if _, ok := wireMap(schema["properties"])[name]; !ok {
			t.Errorf("the composition's own document has no %q on the catalog body: the type a shell parses did not reach the document", name)
		}
	}
	entry := wireItemsOf(doc, schema, "resources")
	if entry == nil {
		t.Fatal("the composition's catalog body has no array of entries")
	}
	for _, name := range []string{"module", "entity", "path", "writable", "screen", "commands"} {
		if _, ok := wireMap(entry["properties"])[name]; !ok {
			t.Errorf("a catalog entry in the composition's own document has no %q", name)
		}
	}

	// And the artefact a device is generated from is this document, byte for byte.
	golden, err := os.ReadFile(openapiGolden)
	if err != nil {
		t.Fatalf("read %s: %v", openapiGolden, err)
	}
	if string(golden) != string(served) {
		t.Errorf("%s is not the document the reference composition serves: first difference at byte %d (%d bytes checked in, %d served). The golden is compared against deviceComposition, which supplies its own catalog renderer, so a change to apps/platformkit/fault.go moves the document this binary publishes and leaves this file — and every shell generated from it — where it was.",
			openapiGolden, firstDifference(golden, served), len(golden), len(served))
	}
}
