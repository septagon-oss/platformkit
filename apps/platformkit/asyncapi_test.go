package main

// The AsyncAPI document is generated from the composition and checked in, the
// way ui/screens/catalog_test.go checks the screen catalog: the golden file is
// what an integrator, a schema registry or a bridge reads, and a stale one is
// worse than none. make check runs this test, so a delivery that adds an event,
// renames one or changes a payload shape arrives with a changed document or a
// failure naming the file and the way to rewrite it.
//
// UPDATE_GOLDEN=1 go test ./apps/platformkit -run AsyncAPI writes the file.
// Nothing else does, and no reader has to run a generator to understand an
// event: the manifest in modules/<name>/module.go is the source, and this is
// its rendering (decision 0034).

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
)

const asyncapiGolden = "testdata/asyncapi.json"

func TestTheAsyncAPIDocumentIsTheCompositionRendered(t *testing.T) {
	_, cfg := configure(t)
	mods := compose(cfg).modules

	got, err := app.AsyncAPI(mods)
	if err != nil {
		t.Fatalf("AsyncAPI: %v", err)
	}

	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(asyncapiGolden, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", asyncapiGolden, err)
		}
		t.Logf("rewrote %s", asyncapiGolden)
		return
	}

	want, err := os.ReadFile(asyncapiGolden)
	if err != nil {
		t.Fatalf("read %s: %v (run with UPDATE_GOLDEN=1)", asyncapiGolden, err)
	}
	if string(want) != string(got) {
		t.Fatalf("%s is stale; run with UPDATE_GOLDEN=1.\nfirst difference at byte %d",
			asyncapiGolden, firstDifference(want, got))
	}
}

// TestEveryDeclaredEventIsInTheDocumentAndCovered is event_schema_coverage, the
// register's number for this pillar: every event the reference composition
// emits carries the payload schema its module declared. An uncovered event is
// not a style problem — it is an event a subscriber has to guess at — so the
// ratio is asserted at 1.0 rather than reported.
func TestEveryDeclaredEventIsInTheDocumentAndCovered(t *testing.T) {
	_, cfg := configure(t)
	mods := compose(cfg).modules

	var declared []events.Declared
	seen := map[string]bool{}
	for _, m := range mods {
		for _, e := range m.Emits() {
			if !seen[e.Name] {
				seen[e.Name], declared = true, append(declared, e)
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("the reference composition declares no events")
	}
	var uncovered []string
	for _, d := range declared {
		if d.Schema() == nil {
			uncovered = append(uncovered, d.Name)
		}
	}
	if len(uncovered) > 0 {
		t.Errorf("event_schema_coverage = %d/%d; these emit a payload nothing declares: %s",
			len(declared)-len(uncovered), len(declared), strings.Join(uncovered, ", "))
	}

	body, err := os.ReadFile(asyncapiGolden)
	if err != nil {
		t.Fatalf("read %s: %v", asyncapiGolden, err)
	}
	var doc struct {
		AsyncAPI   string                    `json:"asyncapi"`
		Channels   map[string]map[string]any `json:"channels"`
		Operations map[string]map[string]any `json:"operations"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("%s is not JSON: %v", asyncapiGolden, err)
	}
	if doc.AsyncAPI != app.AsyncAPIVersion {
		t.Errorf("the document says asyncapi %q, which is not what this code emits (%s)", doc.AsyncAPI, app.AsyncAPIVersion)
	}
	// The kernel declares three events of its own — the record kit/events.Replay
	// writes, the refusal kit/app records and the ask that follows one — and the
	// generator adds them to whatever list it is handed: a document rendered from
	// an app's own modules still says the installation replays things and records
	// what it refused and who asked.
	kernel := []string{events.EventReplayed, app.EventDenied, app.EventAccessRequested}
	if want := len(declared) + len(kernel); len(doc.Channels) != want {
		t.Errorf("%d channels for %d declared events plus the kernel's %d", len(doc.Channels), len(declared), len(kernel))
	}
	for _, name := range kernel {
		if _, ok := doc.Channels[name]; !ok {
			t.Errorf("the document has no %s channel", name)
		}
	}
	for _, d := range declared {
		ch, ok := doc.Channels[d.Name]
		if !ok {
			t.Errorf("%s has no channel", d.Name)
			continue
		}
		if _, ok := doc.Operations[d.Name]; !ok {
			t.Errorf("%s has no send operation", d.Name)
		}
		// The address is the tenant wildcard, not the bare name: an integrator
		// reading this document is told where the tenant lives.
		if addr, _ := ch["address"].(string); !strings.HasPrefix(addr, "platformkit.*.") {
			t.Errorf("%s is published at %q, which is not a per-tenant address", d.Name, addr)
		}
		messages, _ := ch["messages"].(map[string]any)
		msg, _ := messages[d.Name].(map[string]any)
		if msg == nil {
			t.Errorf("%s has no message of its own", d.Name)
			continue
		}
		if msg["contentType"] != "application/json" {
			t.Errorf("%s's message is %v", d.Name, msg["contentType"])
		}
		payload, _ := msg["payload"].(map[string]any)
		if payload == nil {
			t.Errorf("%s's message has no payload schema an integrator can read: %v", d.Name, msg["payload"])
			continue
		}
		// The projection is the payload member itself. Wrapped in a `schema` of
		// its own, or carrying a $schema dialect the enclosing document does not
		// use, it is a contract no validator descends into.
		if _, wrapped := payload["schema"]; wrapped {
			t.Errorf("%s's payload wraps the schema in a `schema` member: %v", d.Name, payload)
		}
		if _, dialect := payload["$schema"]; dialect {
			t.Errorf("%s's payload names a $schema dialect the document does not use: %v", d.Name, payload)
		}
		if payload["type"] != "object" {
			t.Errorf("%s's payload schema is %v; every declared payload here is an object", d.Name, payload["type"])
		}
	}
}

// TestTheDocumentIsRenderedFromTheManifestsAlone: drop one module's events and
// the document loses exactly that channel. The generator reads the manifests and
// nothing else — there is no list of events beside them.
func TestTheDocumentIsRenderedFromTheManifestsAlone(t *testing.T) {
	body, err := app.AsyncAPI([]module.Module{{Name: "ledger", Declared: []events.Declared{
		{Name: "ledger.entry_posted"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["asyncapi"] != app.AsyncAPIVersion {
		t.Errorf("asyncapi = %v", doc["asyncapi"])
	}
	if uncovered, _ := doc["x-uncovered-events"].([]any); len(uncovered) != 1 || uncovered[0] != "ledger.entry_posted" {
		t.Errorf("an event with no payload type is not named as uncovered: %v", doc["x-uncovered-events"])
	}
	// The one module's one event, plus the three the kernel's own manifest
	// declares (platformkit.event_replayed, security.denied and
	// security.access_requested): the generator adds no event of its own beyond
	// what a manifest says.
	if got := doc["channels"].(map[string]any); len(got) != 4 {
		t.Errorf("channels = %d, want 4", len(got))
	}
}

func firstDifference(a, b []byte) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
