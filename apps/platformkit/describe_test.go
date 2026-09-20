package main

// describe_test.go keeps the committed description honest.
//
// testdata/composition.json is the reference application's own description, and
// it is worth committing only while a test refuses a change nobody meant to
// make. The comparison is byte for byte, because the file's whole value is that
// the bytes are the composition: reordering a module list, sorting something
// that used to be in manifest order or letting a value out of the environment
// into the document all show up here as a diff.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// goldenComposition is the description of the modules compose lists, in the
// order it lists them, at the role `run` defaults to. Regenerate it with
// PLATFORMKIT_UPDATE_GOLDEN=1 and read the diff before committing it: every
// line it moves is a route, a permission or a migration somebody added or took
// away.
const goldenComposition = "testdata/composition.json"

// goldenCatalog is the same composition as Backstage descriptors — the system,
// the components and the APIs — and it moves for the same reasons and only those.
const goldenCatalog = "testdata/catalog-info.yaml"

func TestDescribeMatchesTheCommittedComposition(t *testing.T) {
	d := describedComposition(t)
	out, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		t.Fatalf("encode the description: %v", err)
	}
	checkGolden(t, goldenComposition, append(out, '\n'))
}

// TestBackstageMatchesTheCommittedCatalog is the same claim about the same
// composition in the catalog's format: which components exist, which of them
// depend on which, and which APIs each provides. A subscription added somewhere
// else in the composition has to show up here as an edge somebody reviewed.
func TestBackstageMatchesTheCommittedCatalog(t *testing.T) {
	raw, err := app.Backstage(describedComposition(t))
	if err != nil {
		t.Fatalf("Backstage: %v", err)
	}
	checkGolden(t, goldenCatalog, raw)
}

// describedComposition is the reference application's description, read back
// through the boot gates Run runs. The connection is the one httpx.New insists
// on, on a schema of this test's own: Describe issues no query against it, and
// migrating is what makes the schema a real application's rather than an empty
// one.
func describedComposition(t *testing.T) app.Description {
	t.Helper()
	_, cfg := configure(t)
	_, conn := dbtest.Schema(t)

	c := compose(cfg)
	a, err := app.New(t.Context(), cfg, c.modules, appOptions(c, app.All))
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	d, err := a.Describe(t.Context(), conn)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	return d
}

// checkGolden compares what the composition just produced with what is committed,
// and rewrites the file when PLATFORMKIT_UPDATE_GOLDEN says to. It is the same
// bargain for a JSON description and a YAML stream: bytes, not parsed equality,
// because a file nobody can diff is a file nobody reviews.
func checkGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if os.Getenv("PLATFORMKIT_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("rewrote %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v; regenerate it with PLATFORMKIT_UPDATE_GOLDEN=1", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is not what this composition describes.%s\nRerun with PLATFORMKIT_UPDATE_GOLDEN=1 and commit the file with the change that moved it.",
			path, firstDifference(got, want))
	}
}

// firstDifference is the line the two documents stop agreeing on, and both
// sides of it. A byte offset into a five-thousand-line JSON file tells a reader
// nothing; the first line that changed tells them which module to go look at.
func firstDifference(got, want []byte) string {
	g, w := strings.Split(string(got), "\n"), strings.Split(string(want), "\n")
	for i := 0; i < len(g) && i < len(w); i++ {
		if g[i] != w[i] {
			return fmt.Sprintf("\nline %d\n  composed: %s\n  committed: %s", i+1, strings.TrimSpace(g[i]), strings.TrimSpace(w[i]))
		}
	}
	return fmt.Sprintf("\nthe two documents differ in length: %d lines composed, %d committed", len(g), len(w))
}

// TestTheCommittedCompositionCarriesWhatOnlyTheCompositionKnows is the other
// half: the file is not merely stable, it says things. Each claim is one of the
// questions the description exists to answer, and each is checked against the
// document rather than against Go values a test could have written wrong
// together with the code.
func TestTheCommittedCompositionCarriesWhatOnlyTheCompositionKnows(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "composition.json"))
	if err != nil {
		t.Fatalf("read the committed description: %v", err)
	}
	var doc struct {
		DescribeVersion int `json:"describeVersion"`
		Role            string
		Transport       string
		Pool            struct {
			MaxOpenConns    int    `json:"maxOpenConns"`
			ConnMaxLifetime string `json:"connMaxLifetime"`
		}
		Modules []struct {
			Name          string   `json:"name"`
			Permissions   []any    `json:"permissions"`
			SubscribeAll  bool     `json:"subscribeAll"`
			Migrations    []string `json:"migrations"`
			Routes        []any    `json:"routes"`
			Resources     []any    `json:"resources"`
			Subscriptions []string `json:"subscriptions"`
		}
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the committed description is not JSON: %v", err)
	}
	if doc.DescribeVersion != app.DescribeVersion {
		t.Errorf("describeVersion = %d, want %d", doc.DescribeVersion, app.DescribeVersion)
	}
	// The role and transport are the defaults `run` gives a laptop, and the pool
	// is db.DefaultPool: config.yaml sets none of the three, and a document that
	// carried a machine's own answers would never match twice.
	if doc.Role != string(app.All) || doc.Transport != "memory" {
		t.Errorf("role %q transport %q, want %q and memory", doc.Role, doc.Transport, app.All)
	}
	if doc.Pool.MaxOpenConns != 16 || doc.Pool.ConnMaxLifetime != "30m0s" {
		t.Errorf("pool = %+v, want the defaults 16 and 30m0s", doc.Pool)
	}

	by := map[string]int{}
	for i, m := range doc.Modules {
		by[m.Name] = i
	}
	// Composition order, with the kernel's own routes last and nowhere else.
	for _, want := range []string{"tenant", "task", "audit", "admin", "web"} {
		if _, ok := by[want]; !ok {
			t.Errorf("no module %q in the description", want)
		}
	}
	last := doc.Modules[len(doc.Modules)-1]
	if last.Name != "kernel" {
		t.Errorf("the last entry is %q, want the kernel's own routes", last.Name)
	}
	for _, probe := range []string{"/health", "/ready"} {
		if strings.Contains(string(raw), `"`+probe+`"`) {
			t.Errorf("the description carries %s, which is not an operation", probe)
		}
	}

	// audit is the one manifest that subscribes to everything, and the expansion
	// it gets is the fact worth recording: the name of every event, and that it
	// arrived by declaration rather than by luck.
	if !doc.Modules[by["audit"]].SubscribeAll || len(doc.Modules[by["audit"]].Subscriptions) < 20 {
		t.Errorf("audit subscribes to %d events with subscribeAll=%v",
			len(doc.Modules[by["audit"]].Subscriptions), doc.Modules[by["audit"]].SubscribeAll)
	}
	// A module's SQL is its migration history: the owner is the manifest and the
	// files are the ledger's rows.
	if len(doc.Modules[by["task"]].Migrations) == 0 {
		t.Error("task carries no migration files")
	}
	// The pages an application serves are registered by the shell, not by the
	// module whose rows they show: /admin/task/tasks belongs to admin, and only
	// the recording says so.
	if len(doc.Modules[by["admin"]].Routes) < 10 || len(doc.Modules[by["task"]].Routes) == 0 {
		t.Errorf("admin carries %d routes and task %d",
			len(doc.Modules[by["admin"]].Routes), len(doc.Modules[by["task"]].Routes))
	}
	// And the web module's two routes carry no /web/ prefix and publish no
	// event, so nothing but the recording says who serves them.
	if len(doc.Modules[by["web"]].Routes) == 0 {
		t.Error("web carries no routes, so the description would send a reader to the kernel for GET /")
	}
	// The resources the generated screens are built from, with the singleton
	// that has no list and the operator-only write that no customer's wildcard
	// reaches.
	if len(doc.Modules[by["site"]].Resources) == 0 || len(doc.Modules[by["billing"]].Resources) == 0 {
		t.Errorf("site carries %d resources and billing %d",
			len(doc.Modules[by["site"]].Resources), len(doc.Modules[by["billing"]].Resources))
	}
}

// TestEveryEventInTheCompositionSaysWhatItCarries is the claim the manifest field
// exists for: a name is not the whole story, and every event this application
// emits also says what arrives in it.
//
// The CRUD events are checked against the resource the generated screens are
// built from rather than against a schema written into this test, because that is
// the fact being asserted: kit/rest publishes the entity it just wrote, so
// task.task.created carries exactly the task the mounted Spec describes. A
// Payloads entry naming the wrong struct — a DTO somebody kept, the plan for a
// task — fails here. A lifecycle event is the module's own to type, so only its
// presence is checked.
func TestEveryEventInTheCompositionSaysWhatItCarries(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "composition.json"))
	if err != nil {
		t.Fatalf("read the committed description: %v", err)
	}
	var doc struct {
		Modules []struct {
			Name   string `json:"name"`
			Events []struct {
				Name   string          `json:"name"`
				Schema json.RawMessage `json:"schema"`
			} `json:"events"`
			Resources []struct {
				Entity string          `json:"entity"`
				Schema json.RawMessage `json:"schema"`
			} `json:"resources"`
		} `json:"modules"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the committed description is not JSON: %v", err)
	}

	events := 0
	for _, m := range doc.Modules {
		// Both sides of the comparison below are written by the same encoder, so
		// bytes are the equality that means "the same fields".
		mounted := map[string]json.RawMessage{}
		for _, r := range m.Resources {
			mounted[r.Entity] = r.Schema
		}
		for _, e := range m.Events {
			events++
			if len(e.Schema) == 0 {
				t.Errorf("module %q: event %q carries no payload schema", m.Name, e.Name)
				continue
			}
			owner, rest, _ := strings.Cut(e.Name, ".")
			entity, verb, isCRUD := strings.Cut(rest, ".")
			resource, ok := mounted[entity]
			if !isCRUD || owner != m.Name || !ok || verb != "created" && verb != "updated" && verb != "deleted" {
				continue
			}
			if !bytes.Equal(e.Schema, resource) {
				t.Errorf("module %q: event %q is not the %q the mounted resource describes", m.Name, e.Name, entity)
			}
		}
	}
	if events < 40 {
		t.Errorf("the composition emits %d events, which is fewer than the committed document carries", events)
	}
}
