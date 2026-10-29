package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func packageFixture(name string) goPackage {
	return goPackage{ImportPath: product + "/" + name, Dir: "/fixture/" + name, Name: "subject",
		Module: &module{Path: product, Dir: "/fixture", Main: true}, GoFiles: []string{"source.go"}}
}

func testRecords(p goPackage) []goPackage {
	result := []goPackage{p}
	if len(p.TestGoFiles) > 0 {
		v := p
		v.ImportPath, v.ForTest = p.ImportPath+" ["+p.ImportPath+".test]", p.ImportPath
		v.GoFiles = append(slices.Clone(p.GoFiles), p.TestGoFiles...)
		v.Imports = append(slices.Clone(p.Imports), p.TestImports...)
		result = append(result, v)
	}
	if len(p.XTestGoFiles) > 0 {
		v := p
		v.ImportPath, v.ForTest, v.Name = p.ImportPath+"_test ["+p.ImportPath+".test]", p.ImportPath, p.Name+"_test"
		v.GoFiles, v.Imports = p.XTestGoFiles, p.XTestImports
		result = append(result, v)
	}
	result = append(result, goPackage{ImportPath: p.ImportPath + ".test", Dir: p.Dir, Name: "main", Module: p.Module,
		GoFiles: []string{"/cache/testmain.go"}, Imports: []string{p.ImportPath, "os", "testing", "testing/internal/testdeps"}})
	return result
}

func normalizedFixture(t *testing.T, packages []goPackage) *graph {
	t.Helper()
	g := fixtureGraph()
	if err := normalize(g, packages); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestTestAugmentationPreservesProduction(t *testing.T) {
	p := packageFixture("modules/a")
	target := product + "/modules/b/contracts/btest"
	p.Imports, p.TestImports, p.XTestImports = []string{target}, []string{target}, []string{target}
	p.TestGoFiles, p.XTestGoFiles = []string{"local_test.go"}, []string{"external_test.go"}
	records := testRecords(p)
	records = append(records, records...)
	g := normalizedFixture(t, records)
	got := decideFixture(g, nil, nil).refused
	if len(got) != 1 || got[0].rule != "test-helper-in-production" || got[0].context != "production" {
		t.Fatalf("refusals = %+v", got)
	}
	report := edgeReport(g)
	if len(report.Pairs) != 1 || report.Pairs[0].From != product+"/modules/a" || len(report.Pairs[0].Edges) != 2 {
		t.Fatalf("unexpected synthetic edges or owner: %+v", report)
	}
	if !slices.Equal(report.Pairs[0].Edges[0].Contexts, []string{"internal-test", "production"}) {
		t.Fatal(report)
	}
	p.Imports = nil
	if got := decideFixture(normalizedFixture(t, testRecords(p)), nil, nil).refused; len(got) != 0 {
		t.Fatalf("actual tests refused: %+v", got)
	}
}

func TestRebuiltDependencyRetainsProductionProvenance(t *testing.T) {
	p := packageFixture("modules/a")
	p.XTestGoFiles = []string{"external_test.go"}
	q := packageFixture("modules/q")
	q.Imports = []string{product + "/modules/b/contracts/btest"}
	rebuilt := q
	rebuilt.ForTest, rebuilt.ImportPath = p.ImportPath, q.ImportPath+" ["+p.ImportPath+".test]"
	records := append(testRecords(p), q, rebuilt)
	got := decideFixture(normalizedFixture(t, records), nil, nil).refused
	if len(got) != 1 || got[0].from != q.ImportPath || got[0].context != "production" {
		t.Fatalf("refusals = %+v", got)
	}
	if err := normalize(fixtureGraph(), append(testRecords(p), rebuilt)); err == nil {
		t.Fatal("accepted a rebuilt dependency with no ordinary provenance")
	}
}

func TestOrdinaryTestSuffixesGrantNothing(t *testing.T) {
	for _, suffix := range []string{"_test", ".test"} {
		p := packageFixture("modules/a" + suffix)
		p.Name = "main"
		p.Imports = []string{product + "/modules/b/contracts/btest", "testing", "testing/internal/testdeps", "os"}
		base := packageFixture("modules/a")
		base.TestGoFiles = []string{"local_test.go"}
		baseRecords := testRecords(base)
		// The ordinary .test path replaces the generated executable in this input.
		if suffix == ".test" {
			baseRecords = baseRecords[:len(baseRecords)-1]
		}
		got := decideFixture(normalizedFixture(t, append(baseRecords, p)), nil, nil).refused
		if len(got) != 1 || got[0].from != p.ImportPath || got[0].rule != "test-helper-in-production" {
			t.Fatalf("%s: %+v", suffix, got)
		}
	}
}

func TestImportMapUsesValidatedSyntheticIdentities(t *testing.T) {
	p := packageFixture("modules/a")
	p.TestGoFiles = []string{"local_test.go"}
	p.TestImports = []string{product + "/modules/b/contracts"}
	records := testRecords(p)
	q := packageFixture("modules/b/contracts")
	v := q
	v.ForTest, v.ImportPath = p.ImportPath, q.ImportPath+" ["+p.ImportPath+".test]"
	records[1].Imports = []string{v.ImportPath}
	records[1].ImportMap = map[string]string{q.ImportPath: v.ImportPath}
	g := normalizedFixture(t, append(records, q, v))
	if len(g.edges) != 1 || g.edges[0].to != q.ImportPath {
		t.Fatal(g.edges)
	}
	// go list without -deps can reference the rebuilt dependency without
	// emitting its record; the ordinary package and tested package identify it.
	g = normalizedFixture(t, append(records, q))
	if len(g.edges) != 1 || g.edges[0].to != q.ImportPath {
		t.Fatal(g.edges)
	}
	records[1].Imports = []string{q.ImportPath + " [absent.test]"}
	if err := normalize(fixtureGraph(), records); err == nil {
		t.Fatal("accepted an unvalidated annotation")
	}
}

func TestIncompleteGraphIsRefused(t *testing.T) {
	mutations := map[string]func(*goPackage){
		"missing identity":           func(p *goPackage) { p.ImportPath = "" },
		"missing module":             func(p *goPackage) { p.Module = nil },
		"missing directory":          func(p *goPackage) { p.Dir = "" },
		"wrong directory":            func(p *goPackage) { p.Dir = filepath.Join(p.Dir, "other") },
		"incomplete":                 func(p *goPackage) { p.Incomplete = true },
		"package error":              func(p *goPackage) { p.Error = &packageError{Err: "broken import", ImportStack: []string{"one", "two"}} },
		"dependency error":           func(p *goPackage) { p.DepsErrors = []packageError{{Err: "broken dependency"}} },
		"test imports without files": func(p *goPackage) { p.TestImports = []string{"testing"} },
		"test files without variant": func(p *goPackage) { p.TestGoFiles = []string{"local_test.go"} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			p := packageFixture("modules/a")
			mutate(&p)
			if err := normalize(fixtureGraph(), []goPackage{p}); err == nil {
				t.Fatal("accepted incomplete metadata")
			}
		})
	}
	p := packageFixture("modules/a")
	p.TestGoFiles, p.TestImports = []string{"local_test.go"}, []string{"testing"}
	records := testRecords(p)
	records[1].Imports = nil
	if err := normalize(fixtureGraph(), records); err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("missing direct test import was accepted: %v", err)
	}
	records = testRecords(p)
	records[1].Imports = append(records[1].Imports, product+"/modules/b")
	if err := normalize(fixtureGraph(), records); err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatalf("test import without source provenance was accepted: %v", err)
	}
}
