package main

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const product = "example.test/product"
const dependency = "example.test/foundation"

func fixtureGraph(edges ...edge) *graph {
	return &graph{product, "/fixture", []moduleRoot{
		{product, "/fixture"}, {dependency, "/fixture/foundation"}, {foundation, "/fixture/platformkit"},
	}, map[string]bool{
		"context": true, "net/http": true, "database/sql/driver": true, "testing": true,
		"html/template": true, "net/http/httptest": true, "database/sql": true,
	}, nil, edges}
}

// fixtureLayers declares every module the fixture graph names in the platform's
// lowest layer, so a case about another rule meets no layer rule by accident.
func fixtureLayers(g *graph) *layerDeclaration {
	declared := newLayerDeclaration()
	add := func(owner string) {
		if owner != "" && declared.assigned[owner] == nil {
			declared.assigned[owner] = &layerAssignment{owner, moduleLayers[0]}
		}
	}
	for _, owner := range g.owned {
		add(owner)
	}
	for _, e := range g.edges {
		from, _, _ := g.owner(e.ownerPath)
		to, _, _ := g.owner(e.to)
		add(from)
		add(to)
	}
	return declared
}

// decideFixture asks the boundary rules of a fixture graph whose modules are all
// in one layer, which is the state every pre-existing case was written against.
func decideFixture(g *graph, grants map[string]*grant, baseline map[baselineKey]*baselineRow) decision {
	return decide(g, grants, baseline, fixtureLayers(g))
}

func TestDirectImportRules(t *testing.T) {
	cases := []struct {
		name, source, target, context, rule string
	}{
		{"own implementation", "modules/a", product + "/modules/a/internal", "production", ""},
		{"matching basenames", "modules/a", dependency + "/modules/a", "production", "foreign-implementation"},
		{"foreign contracts", "modules/a", product + "/modules/b/contracts/value", "production", ""},
		{"foreign internal", "modules/a", product + "/modules/b/internal/store", "production", "foreign-internal"},
		{"internal below contracts", "modules/a", product + "/modules/b/contracts/internal/value", "production", "foreign-internal"},
		{"production helper", "modules/a", product + "/modules/b/contracts/btest", "production", "test-helper-in-production"},
		{"own production helper", "modules/a", product + "/modules/a/contracts/atest/sub", "production", "test-helper-in-production"},
		{"helper imports helper", "modules/a/contracts/atest", product + "/modules/b/contracts/btest", "production", "test-helper-in-production"},
		{"internal test helper", "modules/a", product + "/modules/b/contracts/btest", "internal-test", ""},
		{"external test helper", "modules/a", product + "/modules/b/contracts/btest", "external-test", ""},
		{"test non-contract child", "modules/a", product + "/modules/b/domain", "external-test", "foreign-implementation"},
		{"missing integration", "modules/a", product + "/modules/b", "external-test", "undeclared-integration"},
		{"application constructor", "apps/example", product + "/modules/b", "production", ""},
		{"application internal", "apps/example", product + "/modules/b/internal", "production", "foreign-internal"},
		{"application helper", "apps/example", product + "/modules/b/contracts/btest", "production", "test-helper-in-production"},
		{"undeclared root", "modules/a", "example.test/unknown/modules/a/contracts", "production", "undeclared-module-root"},
		{"contracts lookalike", "modules/a", product + "/modules/b/contracts-extra", "production", "foreign-implementation"},
		{"module lookalike", "modules/a", product + "/modules/a2", "production", "foreign-implementation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := product + "/" + tc.source
			e := edge{source, tc.target, source, tc.context, tc.source + "@external"}
			got := decideFixture(fixtureGraph(e), nil, nil).refused
			if tc.rule == "" && len(got) != 0 || tc.rule != "" && (len(got) != 1 || got[0].rule != tc.rule) {
				t.Fatalf("refusals = %+v, want %q", got, tc.rule)
			}
			if len(got) != 0 && (!strings.Contains(diagnostics(got), source) || !strings.Contains(diagnostics(got), tc.target)) {
				t.Fatal("diagnostic lost a full package path")
			}
		})
	}
}

func TestContractPurity(t *testing.T) {
	g := fixtureGraph()
	allowed := []string{"context", "net/http", "database/sql/driver", product + "/modules/a/contracts/value", dependency + "/modules/a/contracts"}
	for _, pkg := range []string{"crud", "db", "entity", "events", "events/transport", "fault", "limit", "module", "push", "request", "rest", "richtext", "screen", "tenancy"} {
		allowed = append(allowed, foundation+"/kit/"+pkg)
	}
	allowed = append(allowed, "github.com/google/uuid", "golang.org/x/text/currency", "golang.org/x/text/unicode/norm", "golang.org/x/image/webp", "golang.org/x/crypto/argon2")
	for _, target := range allowed {
		if rule := g.purity(target); rule != "" {
			t.Errorf("%s: %s", target, rule)
		}
	}
	for _, target := range []string{"database/sql", "html/template", "net/http/httptest", "testing", "testing/iotest", "C", foundation + "/ui/page", foundation + "/kit/httpx", product + "/modules/a/internal"} {
		if rule := g.purity(target); rule != "contract-adapter" {
			t.Errorf("%s: got %q, want contract-adapter", target, rule)
		}
	}
	for _, target := range []string{"github.com/lib/pq", "github.com/danielgtaylor/huma/v2", "example.test/unknown", "dotless/sdk", "github.com/google/uuid/sub", product + "/kit/db"} {
		if rule := g.purity(target); rule != "contract-external" {
			t.Errorf("%s: got %q, want contract-external", target, rule)
		}
	}
	source := product + "/modules/a/contracts"
	for _, context := range []string{"production", "internal-test", "external-test"} {
		g.edges = []edge{{source, "html/template", source, context, "modules/a/contracts@internal"}}
		if got := decideFixture(g, nil, nil).refused; (len(got) != 0) != (context == "production") {
			t.Errorf("%s: %+v", context, got)
		}
	}
}

func TestIntegrationGrantsAreExact(t *testing.T) {
	source, target := product+"/modules/a/internal", product+"/modules/b"
	g := fixtureGraph(edge{source + "_test", target, source, "external-test", "modules/a/internal@external"})
	row := "modules/a/internal@external " + target + "," + product + "/modules/c Composes two real migrations."
	grants, err := parseGrants("grants", row, g)
	if err != nil {
		t.Fatal(err)
	}
	before := maps.Clone(grants["modules/a/internal@external"].targets)
	if got := decideFixture(g, grants, nil).refused; len(got) != 0 {
		t.Fatal(got)
	}
	notice := notices(grants, decideFixture(g, grants, nil).used)
	if strings.Count(notice, "imports: integration") != 1 || !strings.Contains(notice, "used ["+target+"]") || !strings.Contains(notice, "unused in this build context ["+product+"/modules/c]") || !strings.Contains(notice, "Composes two real migrations.") {
		t.Fatal(notice)
	}
	if !maps.Equal(before, grants["modules/a/internal@external"].targets) {
		t.Fatal("decision mutated the caller's grants")
	}
	for _, change := range []struct{ context, suite, target, want string }{
		{"production", "", target, "foreign-implementation"},
		{"internal-test", "modules/a/internal@internal", target, "undeclared-integration"},
		{"external-test", "modules/a/internal@external", target + "/domain", "foreign-implementation"},
		{"external-test", "modules/a/internal@external", target + "/internal", "foreign-internal"},
		{"external-test", "modules/a/internal@external", product + "/modules/d", "undeclared-integration"},
	} {
		g.edges = []edge{{source, change.target, source, change.context, change.suite}}
		if got := decideFixture(g, grants, nil).refused; len(got) != 1 || got[0].rule != change.want {
			t.Errorf("%+v: %+v", change, got)
		}
	}
}

func TestMalformedGrantsFailClosed(t *testing.T) {
	target := product + "/modules/b"
	valid := "modules/a@external " + target + " Composes migrations."
	for _, text := range []string{
		"modules/a@external " + target,
		"modules/a@unknown " + target + " why",
		"modules/a/../b@external " + target + " why",
		"/modules/a@external " + target + " why",
		"modules/a/*.go@external " + target + " why",
		"modules/a/a_test.go@external " + target + " why",
		"modules/a@external " + target + ", why",
		"modules/a@external " + target + "/contracts why",
		"modules/a@external " + product + "/modules/a why",
		"modules/a@external example.test/missing/modules/b why",
		"modules/a@external " + target + "," + target + " why",
		valid + "\n" + valid,
		"\xff",
	} {
		if _, err := parseGrants("policy", text, fixtureGraph()); err == nil || !strings.Contains(err.Error(), "policy") {
			t.Errorf("accepted %q: %v", text, err)
		}
	}
	grants, err := parseGrants("policy", " # comment\r\n\r\n"+valid+" # literal\r\n", fixtureGraph())
	if err != nil || !strings.Contains(notices(grants, nil), "# literal") {
		t.Fatalf("CRLF/comment parsing: %v; %+v", err, grants)
	}
}

func TestEdgeReportDeduplicatesAndSortsContexts(t *testing.T) {
	a, b := product+"/modules/a", dependency+"/modules/b/contracts"
	g := fixtureGraph(
		edge{a, b, a, "production", ""}, edge{a, b, a, "internal-test", "modules/a@internal"},
		edge{a, b, a, "production", ""}, edge{a + "_test", b, a, "external-test", "modules/a@external"},
		edge{a, a + "/contracts", a, "production", ""}, edge{product + "/apps/app", b, product + "/apps/app", "production", ""},
	)
	want := report{"platformkit.import-edges.v1", []pair{{product + "/modules/a", dependency + "/modules/b", []reportedEdge{
		{a, b, []string{"internal-test", "production"}}, {a + "_test", b, []string{"external-test"}},
	}}}}
	if got := edgeReport(g); !reflect.DeepEqual(got, want) {
		t.Fatalf("report = %+v, want %+v", got, want)
	}
	first, _ := json.Marshal(edgeReport(g))
	slices.Reverse(g.edges)
	second, _ := json.Marshal(edgeReport(g))
	if string(first) != string(second) {
		t.Fatal("record order changed report")
	}
	empty, _ := json.Marshal(edgeReport(fixtureGraph()))
	if string(empty) != `{"schema":"platformkit.import-edges.v1","pairs":[]}` {
		t.Fatal(string(empty))
	}
}

func TestLongestDeclaredRootOwnsItsPackages(t *testing.T) {
	g := fixtureGraph()
	g.roots = append(g.roots, moduleRoot{product + "/nested", "/fixture/nested"})
	if owner, _, unknown := g.owner(product + "/nested/modules/a/contracts"); owner != product+"/nested/modules/a" || unknown {
		t.Fatalf("owner = %q, unknown = %t", owner, unknown)
	}
	if _, _, unknown := g.owner(product + "-extra/modules/a/contracts"); !unknown {
		t.Fatal("prefix lookalike was declared")
	}
}
