package main

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// layersFixture reads one repository's rows as the gate would, so a case states
// its declaration in the committed shape rather than in Go literals.
// fields reads a case's two layer names.
func fields(text string) (string, string) {
	parts := strings.Fields(text)
	return parts[0], parts[1]
}

func layersFixture(t *testing.T, g *graph, rootPath, text string) *layerDeclaration {
	t.Helper()
	declared := newLayerDeclaration()
	if err := parseLayers("scripts/imports-layers.txt", text, g, rootPath, declared); err != nil {
		t.Fatalf("declaration: %v", err)
	}
	return declared
}

// declareFixtureLayers assigns the named modules one layer each, in the order the
// case needs: `product/modules/a generic` becomes one row of the checked
// repository's own file, and a dependency's module goes to a second pass under
// that dependency's root, as readLayers does.
func declareFixtureLayers(t *testing.T, g *graph, rows ...string) *layerDeclaration {
	t.Helper()
	declared := newLayerDeclaration()
	own, foreign := []string{}, []string{}
	for _, row := range rows {
		if strings.HasPrefix(row, product+"/") {
			own = append(own, row)
			continue
		}
		foreign = append(foreign, row)
	}
	for _, pass := range []struct {
		rootPath string
		rows     []string
	}{{product, own}, {dependency, foreign}} {
		if len(pass.rows) == 0 {
			continue
		}
		if err := parseLayers("scripts/imports-layers.txt", strings.Join(pass.rows, "\n"), g, pass.rootPath, declared); err != nil {
			t.Fatalf("declaration: %v", err)
		}
	}
	return declared
}

func TestProductionImportFromALowerLayerRefusesBothPackages(t *testing.T) {
	for _, pair := range []struct{ from, to, layers string }{
		{"a", "b", "platform identity"},
		{"a", "b", "platform generic"},
		{"a", "b", "generic product"},
		{"a", "b", "platform product"},
	} {
		t.Run(pair.layers, func(t *testing.T) {
			source, target := product+"/modules/a/internal", product+"/modules/b/contracts"
			g := fixtureGraph(edge{source, target, source, "production", ""})
			one, two := fields(pair.layers)
			declared := declareFixtureLayers(t, g, product+"/modules/a "+one, product+"/modules/b "+two)
			got := decide(g, nil, nil, declared).refused
			if len(got) != 1 || got[0].rule != "module-layer-upward" || got[0].from != source || got[0].to != target {
				t.Fatalf("refusals = %+v, want one module-layer-upward on %s -> %s", got, source, target)
			}
			if text := diagnostics(got); !strings.Contains(text, source) || !strings.Contains(text, target) {
				t.Fatalf("diagnostic lost a full package path: %s", text)
			}
		})
	}
}

func TestLateralAndDownwardProductionImportsAreAccepted(t *testing.T) {
	source, peer, below := product+"/modules/a/contracts", product+"/modules/b/contracts", product+"/modules/c/contracts"
	for _, shape := range []struct{ from, to, layers string }{
		{source, peer, "identity identity"},
		{source, peer, "generic generic"},
		{source, below, "product platform"},
		{source, below, "identity platform"},
	} {
		t.Run(shape.from, func(t *testing.T) {
			g := fixtureGraph(edge{shape.from, shape.to, shape.from, "production", ""})
			one, two := fields(shape.layers)
			declared := declareFixtureLayers(t, g, product+"/modules/a "+one, product+"/modules/b "+two, product+"/modules/c platform")
			if got := decide(g, nil, nil, declared).refused; len(got) != 0 {
				t.Fatalf("refusals = %+v", got)
			}
		})
	}
	// A pair whose two directions both stay inside one layer is reciprocal, not
	// upward: with its row it is accepted, and the row is announced once.
	a, b := product+"/modules/a", product+"/modules/b/contracts"
	back := product + "/modules/b/internal"
	g := fixtureGraph(edge{a, b, a, "production", ""}, edge{back, product + "/modules/a/contracts", back, "production", ""})
	declared := layersFixture(t, g, product, product+"/modules/a identity\n"+product+"/modules/b identity\n"+
		product+"/modules/a <-> "+product+"/modules/b Two modules that name each other's values by design.\n")
	got := decide(g, nil, nil, declared)
	if len(got.refused) != 0 {
		t.Fatalf("refusals = %+v", got.refused)
	}
	notice := declared.notices(got.spent, got.justified)
	if strings.Count(notice, "<-> "+product+"/modules/b") != 1 || !strings.Contains(notice, "reciprocal in production") || !strings.Contains(notice, "by design") {
		t.Fatalf("notice = %s", notice)
	}
}

func TestUnassignedModuleRefusesByOwner(t *testing.T) {
	t.Run("module with no import at all", func(t *testing.T) {
		g := fixtureGraph()
		g.owned = []string{product + "/modules/billing"}
		got := decide(g, nil, nil, newLayerDeclaration()).refused
		if len(got) != 1 || got[0].rule != "module-layer-missing" || got[0].from != product+"/modules/billing" {
			t.Fatalf("a module invisible to the edge set escaped completeness: %+v", got)
		}
	})
	t.Run("own module an edge reaches", func(t *testing.T) {
		source := product + "/modules/a/contracts"
		g := fixtureGraph(edge{source, product + "/modules/b/contracts", source, "production", ""})
		got := decide(g, nil, nil, declareFixtureLayers(t, g, product+"/modules/b platform")).refused
		if len(got) != 1 || got[0].rule != "module-layer-missing" || got[0].from != product+"/modules/a" {
			t.Fatalf("refusals = %+v", got)
		}
	})
	t.Run("dependency module", func(t *testing.T) {
		source := product + "/modules/a/contracts"
		target := dependency + "/modules/b/contracts"
		g := fixtureGraph(edge{source, target, source, "production", ""})
		got := decide(g, nil, nil, declareFixtureLayers(t, g, product+"/modules/a platform")).refused
		if len(got) != 1 || got[0].rule != "module-layer-missing" || got[0].from != dependency+"/modules/b" {
			t.Fatalf("refusals = %+v", got)
		}
		want := dependency + layerFileSuffix
		if !strings.Contains(diagnostics(got), want) {
			t.Fatalf("diagnostic does not name the owning repository's file %s: %s", want, diagnostics(got))
		}
	})
}

func TestUnassignedModuleAndUndeclaredRootStayApart(t *testing.T) {
	source := product + "/modules/a"
	g := fixtureGraph(edge{source, "example.test/unknown/modules/b/contracts", source, "production", ""})
	got := decide(g, nil, nil, declareFixtureLayers(t, g, product+"/modules/a platform")).refused
	if len(got) != 1 || got[0].rule != "undeclared-module-root" {
		t.Fatalf("an undeclared root gained a layer rule: %+v", got)
	}
	g = fixtureGraph(edge{source, product + "/modules/b/contracts", source, "production", ""})
	if got = decide(g, nil, nil, declareFixtureLayers(t, g, product+"/modules/a platform")).refused; len(got) != 1 || got[0].rule != "module-layer-missing" {
		t.Fatalf("a known owner without a row gained a root rule: %+v", got)
	}
}

func TestMissingLayerDeclarationRefusesEveryModuleItFinds(t *testing.T) {
	root := t.TempDir()
	p := packageFixture("modules/a")
	p.Dir, p.Module.Dir = filepath.Join(root, "modules/a"), root
	p.Imports = []string{product + "/modules/b/contracts"}
	q := packageFixture("modules/b/contracts")
	q.Dir, q.Module.Dir = filepath.Join(root, "modules/b/contracts"), root
	packages := append(jsonStream(t, p), jsonStream(t, q)...)
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), []string{root}, &stdout, &stderr, fakeCommand(t, root, packages))
	if code != 1 || strings.Contains(stdout.String(), "satisfy") {
		t.Fatalf("a repository with no map was accepted with status %d: stdout %s, stderr %s", code, &stdout, &stderr)
	}
	for _, want := range []string{"module-layer-missing", product + "/modules/a", product + "/modules/b", product + layerFileSuffix} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr does not name %s: %s", want, &stderr)
		}
	}
}

func TestUnknownLayerNameFailsClosed(t *testing.T) {
	g := fixtureGraph()
	for _, text := range []string{
		product + "/modules/a side",
		product + "/modules/a platform extra",
		product + "/modules/a platform\n" + product + "/modules/a identity",
		"modules/a platform",
		"a platform",
		product + "/modules/../modules/a platform",
		product + "/modules/a/contracts platform",
		product + "/modules/a platform\n" + dependency + "/modules/b platform",
		dependency + "/modules/b platform\n" + dependency + "/modules/b platform",
		product + "/modules/a <-> " + product + "/modules/b",
		product + "/modules/a <-> " + product + "/modules/a reason",
		product + "/modules/a <-> " + product + "/modules/b/contracts reason",
		"\xff",
	} {
		if err := parseLayers("imports-layers", text, g, product, newLayerDeclaration()); err == nil || !strings.Contains(err.Error(), "imports-layers") {
			t.Errorf("accepted %q: %v", text, err)
		}
	}
	err := parseLayers("imports-layers", product+"/modules/a side", g, product, newLayerDeclaration())
	if err == nil || !strings.Contains(err.Error(), strings.Join(moduleLayers, ", ")) {
		t.Fatalf("an unknown layer name did not print the order: %v", err)
	}
}

func TestLayerNamesComeFromTheGateNotTheRepository(t *testing.T) {
	g := fixtureGraph()
	if err := parseLayers("imports-layers", product+"/modules/a identity-plus-one", g, product, newLayerDeclaration()); err == nil || !strings.Contains(err.Error(), "identity-plus-one") {
		t.Fatalf("a consumer negotiated a layer name: %v", err)
	}
	if err := parseLayers("imports-layers", dependency+"/modules/a platform", g, product, newLayerDeclaration()); err == nil || !strings.Contains(err.Error(), dependency) {
		t.Fatalf("a consumer's file assigned a dependency's module without naming the owner: %v", err)
	}
	declared := newLayerDeclaration()
	if err := parseLayers("imports-layers", product+"/modules/a platform", g, product, declared); err != nil {
		t.Fatal(err)
	}
	if err := parseLayers("imports-layers", dependency+"/modules/a platform", g, dependency, declared); err != nil {
		t.Fatalf("each repository's own file could not assign its own module: %v", err)
	}
	if len(declared.assigned) != 2 || declared.layer(product+"/modules/a") != moduleLayers[0] {
		t.Fatalf("assignments = %+v", declared.assigned)
	}
}

func TestReciprocalProductionPairNeedsItsReason(t *testing.T) {
	forward := edge{product + "/modules/a/contracts", product + "/modules/b/contracts", product + "/modules/a/contracts", "production", ""}
	reverse := edge{product + "/modules/b/internal", product + "/modules/a/contracts", product + "/modules/b/internal", "production", ""}
	assigned := product + "/modules/a identity\n" + product + "/modules/b identity\n"
	g := fixtureGraph(forward, reverse)
	declared := layersFixture(t, g, product, assigned)
	got := decide(g, nil, nil, declared)
	if len(got.refused) != 2 {
		t.Fatalf("one unjustified reciprocal pair refused %d times: %+v", len(got.refused), got.refused)
	}
	for _, r := range got.refused {
		if r.rule != "module-layer-cycle-undeclared" {
			t.Errorf("refusal %v is not the one rule the pair owes", r)
		}
	}
	justified := layersFixture(t, g, product, assigned+product+"/modules/a <-> "+product+"/modules/b Both contract trees name each other's values.\n")
	got = decide(g, nil, nil, justified)
	if len(got.refused) != 0 {
		t.Fatalf("a justified pair was still refused: %+v", got.refused)
	}
	notice := justified.notices(got.spent, got.justified)
	if strings.Count(notice, "<-> "+product+"/modules/b") != 1 || !strings.Contains(notice, "Both contract trees") {
		t.Fatalf("notice = %s", notice)
	}
	// A row for a pair that stopped importing each other is a row that names no
	// fact, and it is itself refused.
	one := fixtureGraph(forward)
	row := layersFixture(t, one, product, assigned+product+"/modules/a <-> "+product+"/modules/b An old reason.\n")
	if refused := decide(one, nil, nil, row).refused; len(refused) != 1 || refused[0].rule != "module-layer-cycle-unused" {
		t.Fatalf("an unused pair row was accepted: %+v", refused)
	}
	// A reciprocal pair of test edges needs no row: only production is judged.
	tests := fixtureGraph(
		edge{product + "/modules/a_test", product + "/modules/b", product + "/modules/a", "external-test", "modules/a@external"},
		edge{product + "/modules/b_test", product + "/modules/a", product + "/modules/b", "external-test", "modules/b@external"},
	)
	if refused := decide(tests, nil, nil, layersFixture(t, tests, product, assigned)).refused; slices.ContainsFunc(refused, func(r refusal) bool {
		return strings.HasPrefix(r.rule, "module-layer-")
	}) {
		t.Fatalf("test source entered the layer rule: %+v", refused)
	}
}

// TestJustificationRowNamesModulesOfOneRepository checks where a pair's row lives.
// Go reports packages for the main module alone, so no build measures a pair whose
// two modules sit in two repositories, and a row spanning them is an exception no
// gate could ever judge: the parser refuses it as an unreadable declaration, from
// either side's file.
func TestJustificationRowNamesModulesOfOneRepository(t *testing.T) {
	g := fixtureGraph()
	for _, row := range []struct{ file, rootPath, text string }{
		{"product-layers", product, product + "/modules/a <-> " + dependency + "/modules/b Both repositories claim this pair."},
		{"foundation-layers", dependency, dependency + "/modules/a <-> " + product + "/modules/b The same pair from the other side."},
	} {
		if err := parseLayers(row.file, row.text, g, row.rootPath, newLayerDeclaration()); err == nil ||
			!strings.Contains(err.Error(), "the repository that holds both of its modules") {
			t.Errorf("%s accepted a pair spanning two repositories: %v", row.file, err)
		}
	}
	declared := layersFixture(t, g, product, product+"/modules/a platform\n"+product+"/modules/b platform\n"+
		product+"/modules/a <-> "+product+"/modules/b Both contract trees name each other's values.\n")
	if len(declared.justifications) != 1 {
		t.Fatalf("a pair of one repository's own modules could not be justified: %+v", declared.justifications)
	}
}

// TestAssignmentOfAModuleThisBuildExcludesIsAnnounced checks the half of the
// declaration the graph cannot answer: a module of the repository that no package of
// this build context reaches keeps its row as a fact about the repository, so its
// state is announced and no refusal follows — a build context that excludes a module
// is legitimate.
func TestAssignmentOfAModuleThisBuildExcludesIsAnnounced(t *testing.T) {
	g := fixtureGraph()
	g.owned = []string{product + "/modules/a"}
	declared := layersFixture(t, g, product, product+"/modules/a platform\n"+product+"/modules/b identity\n")
	got := decide(g, nil, nil, declared)
	if len(got.refused) != 0 {
		t.Fatalf("a row outside this build context refused: %+v", got.refused)
	}
	notice := declared.notices(got.spent, got.justified)
	if !strings.Contains(notice, product+"/modules/a is platform; assigned in this build context") ||
		!strings.Contains(notice, product+"/modules/b is identity; holds no package in this build context") {
		t.Fatalf("notice = %s", notice)
	}
}

func TestLayerRuleJudgesOnlyProductionSource(t *testing.T) {
	source, target := product+"/modules/a_test", product+"/modules/b/contracts"
	for _, context := range []string{"internal-test", "external-test"} {
		g := fixtureGraph(edge{source + "_test", target, source, context, "modules/a@" + context[:len(context)-4] + "al"})
		declared := declareFixtureLayers(t, g, product+"/modules/a platform", product+"/modules/b product")
		if got := decide(g, nil, nil, declared).refused; slices.ContainsFunc(got, func(r refusal) bool {
			return strings.HasPrefix(r.rule, "module-layer-")
		}) {
			t.Errorf("%s: an upward test edge met a layer rule: %+v", context, got)
		}
	}
	// The granted shape this repository's own integration file holds: a test suite
	// of a low module composing a high module's constructor.
	suite, constructor := "modules/history@external", product+"/modules/admin"
	g := fixtureGraph(edge{product + "/modules/history_test", constructor, product + "/modules/history", "external-test", suite})
	declared := declareFixtureLayers(t, g, product+"/modules/history platform", product+"/modules/admin product")
	grants, err := parseGrants("imports-integration", "modules/history@external "+constructor+" Constructs the shell to check restore refusals.", g)
	if err != nil {
		t.Fatal(err)
	}
	if got := decide(g, grants, nil, declared).refused; len(got) != 0 {
		t.Fatalf("a granted test composition met a rule: %+v", got)
	}
}

func TestModuleGraphReportsCompositionApartFromContracts(t *testing.T) {
	contracts, composing := product+"/modules/a/contracts", product+"/modules/a"
	target := product + "/modules/b/contracts"
	g := fixtureGraph(
		edge{contracts, target, contracts, "production", ""},
		edge{composing, target, composing, "production", ""},
		edge{product + "/modules/b/internal", product + "/modules/a/contracts", product + "/modules/b/internal", "production", ""},
	)
	declared := layersFixture(t, g, product, product+"/modules/a identity\n"+product+"/modules/b identity\n"+
		product+"/modules/a <-> "+product+"/modules/b The pair names each other's values by design.\n")
	got := decide(g, nil, nil, declared)
	if len(got.refused) != 0 {
		t.Fatalf("refusals = %+v", got.refused)
	}
	report := moduleGraph(g, declared, got.justified)
	if len(report.Edges) != 2 || !slices.Equal(report.Edges[0].Sources, []string{"contracts"}) || report.Edges[0].From != product+"/modules/a" {
		t.Fatalf("contract edges = %+v", report.Edges)
	}
	if slices.ContainsFunc(report.Edges, func(e moduleEdge) bool { return slices.Contains(e.Sources, "composition") }) {
		t.Fatalf("a composition edge joined the import edges: %+v", report.Edges)
	}
	if len(report.Constructions) != 1 || report.Constructions[0].From != composing || !slices.Equal(report.Constructions[0].Sources, []string{"composition"}) {
		t.Fatalf("compositions = %+v", report.Constructions)
	}
	if report.Edges[0].Direction != directionLateral || !report.Edges[0].Justified || report.Constructions[0].Direction != directionLateral {
		t.Fatalf("computed fields = %+v, %+v", report.Edges[0], report.Constructions[0])
	}
	if len(report.Nodes) != 2 || report.Nodes[0].Repository != product || report.Nodes[0].Layer != "identity" {
		t.Fatalf("nodes = %+v", report.Nodes)
	}
	first, err := json.Marshal(moduleGraph(g, declared, got.justified))
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(g.edges)
	second, err := json.Marshal(moduleGraph(g, declared, decide(g, nil, nil, declared).justified))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("record order changed the aggregate report:\n%s\n%s", first, second)
	}
	if !slices.Equal(report.Layers, moduleLayers) {
		t.Fatalf("layers = %v", report.Layers)
	}
}

func TestCommittedLayerMapAssignsEveryModuleTheTreeHolds(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "scripts", "imports-layers.txt"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := loadRealGraph(t, root)
	if err != nil {
		t.Fatal(err)
	}
	declared, err := readLayers(g)
	if err != nil {
		t.Fatal(err)
	}
	if len(declared.assigned) == 0 {
		t.Fatal("the committed map assigns nothing")
	}
	for _, name := range modulesOnDisk(t, root) {
		if declared.assigned[foundation+"/modules/"+name] == nil {
			t.Errorf("modules/%s holds packages and no row", name)
		}
	}
	for owner := range declared.assigned {
		if !slices.Contains(g.owned, owner) {
			t.Errorf("row %s names no module the tree holds", owner)
		}
	}
	if got := slices.SortedFunc(maps.Keys(declared.assigned), func(a, b string) int { return strings.Compare(a, b) }); len(got) != len(g.owned) {
		t.Errorf("the map holds %d rows and the tree %d modules", len(got), len(g.owned))
	}
	if !strings.Contains(string(data), "<->") {
		t.Fatal("the committed map carries no reciprocal pair")
	}
}

func TestCommittedReciprocalRowsAreOnlyThePairsTheGraphHas(t *testing.T) {
	root := repositoryRoot(t)
	g, err := loadRealGraph(t, root)
	if err != nil {
		t.Fatal(err)
	}
	declared, err := readLayers(g)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := readGrants(root, g)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := readBaseline(root, g)
	if err != nil {
		t.Fatal(err)
	}
	got := decide(g, grants, baseline, declared)
	// A pair is reciprocal when production holds a production edge each way, so the
	// set of source modules of one aggregated pair says it. The rows and that set
	// have to agree exactly, in both directions of the comparison.
	sources := map[layerPair]map[string]bool{}
	for _, e := range g.edges {
		if e.context != "production" {
			continue
		}
		from, _, _ := g.owner(e.ownerPath)
		to, _, _ := g.owner(e.to)
		if from == "" || to == "" || from == to {
			continue
		}
		pair := newLayerPair(from, to)
		if sources[pair] == nil {
			sources[pair] = map[string]bool{}
		}
		sources[pair][from] = true
	}
	reciprocal := map[layerPair]bool{}
	for pair, owners := range sources {
		if len(owners) > 1 {
			reciprocal[pair] = true
		}
	}
	rows := map[layerPair]bool{}
	for pair := range declared.justifications {
		rows[pair] = true
	}
	if !maps.Equal(reciprocal, rows) {
		for pair := range reciprocal {
			if !rows[pair] {
				t.Errorf("production pair %s <-> %s has no row", pair.a, pair.b)
			}
		}
		for pair := range rows {
			if !reciprocal[pair] {
				t.Errorf("row %s <-> %s names no production pair", pair.a, pair.b)
			}
		}
	}
	for _, r := range got.refused {
		t.Errorf("the committed map refuses %s: %s -> %s", r.rule, r.from, r.to)
	}
}

// loadRealGraph reads this repository the way the gate does: Go's own package
// list in this build context, with the workspace turned off so no sibling
// checkout joins the measurement.
func loadRealGraph(t *testing.T, root string) (*graph, error) {
	t.Helper()
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-buildvcs=false")
	return load(t.Context(), root, nil, runGo)
}

func modulesOnDisk(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "modules"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names
}
