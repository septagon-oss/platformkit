package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContractBaselineAdmitsOnlyTheEdgeItNames(t *testing.T) {
	source, target := product+"/modules/a/contracts", "html/template"
	rows, err := parseBaseline("baseline", "modules/a/contracts "+target+" Render returns template.HTML in the published signature.", fixtureGraph())
	if err != nil {
		t.Fatal(err)
	}
	g := fixtureGraph(edge{source, target, source, "production", ""})
	decision := decideFixture(g, nil, rows)
	if len(decision.refused) != 0 {
		t.Fatalf("refusals = %+v", decision.refused)
	}
	notice := baselineNotices(rows, decision.carried)
	if strings.Count(notice, "imports: contract baseline") != 1 || !strings.Contains(notice, "modules/a/contracts -> "+target+"; carried by production source in this build context; Render returns") {
		t.Fatal(notice)
	}
	for _, other := range []string{"net/http/httptest", "github.com/lib/pq", foundation + "/kit/httpx"} {
		g.edges = []edge{{source, target, source, "production", ""}, {source, other, source, "production", ""}}
		if refused := decideFixture(g, nil, rows).refused; len(refused) != 1 || refused[0].to != other || refused[0].rule != "contract-external" && refused[0].rule != "contract-adapter" {
			t.Errorf("%s: %+v", other, refused)
		}
	}
	// A production edge alone carries a row: the same import from actual test
	// source is never the frozen surface the row describes.
	for _, suite := range []string{"modules/a/contracts@internal", "modules/a/contracts@external"} {
		g.edges = []edge{{source, target, source, "internal-test", suite}}
		if refused := decideFixture(g, nil, rows).refused; len(refused) != 1 || refused[0].rule != "contract-baseline-unused" {
			t.Errorf("%s: %+v", suite, refused)
		}
	}
	// A row waives the contract purity rule alone. Each shape below is refused by
	// another rule as well, and the row carrying it leaves that refusal standing.
	for _, reach := range []struct{ target, rule string }{
		{product + "/modules/b/internal", "foreign-internal"},
		{product + "/modules/b/contracts/btest", "test-helper-in-production"},
		{"example.test/unknown/modules/b/contracts", "undeclared-module-root"},
	} {
		g.edges = []edge{{source, reach.target, source, "production", ""}}
		rows, err := parseBaseline("baseline", "modules/a/contracts "+reach.target+" Names the dependency the frozen surface publishes.", g)
		if err != nil {
			t.Fatal(err)
		}
		if refused := decideFixture(g, nil, rows).refused; len(refused) != 1 || refused[0].rule != reach.rule {
			t.Errorf("%s: %+v", reach.target, refused)
		}
	}
}

func TestContractBaselineShrinksWhenItsEdgeMoves(t *testing.T) {
	source := product + "/modules/a/contracts"
	rows, err := parseBaseline("baseline", "modules/a/contracts html/template Render returns template.HTML in the published signature.\n"+
		"modules/a/contracts net/http/httptest ServeHTTP answers in the published handler.", fixtureGraph())
	if err != nil {
		t.Fatal(err)
	}
	g := fixtureGraph(
		edge{source, "net/http/httptest", source, "production", ""},
		edge{source, "testing", source, "production", ""},
	)
	decision := decideFixture(g, nil, rows)
	if len(decision.refused) != 2 || decision.refused[0].rule != "contract-baseline-unused" || decision.refused[1].rule != "contract-adapter" {
		t.Fatalf("refusals = %+v", decision.refused)
	}
	text := diagnostics(decision.refused)
	for _, want := range []string{"OUT OF BOUNDS [contract-baseline-unused] " + source + " -> html/template (baseline)", "remove this row", "row reason: Render returns template.HTML in the published signature."} {
		if !strings.Contains(text, want) {
			t.Fatalf("diagnostics lost %q:\n%s", want, text)
		}
	}
	notice := baselineNotices(rows, decision.carried)
	if !strings.Contains(notice, "html/template; carried by no production source in this build context") || !strings.Contains(notice, "net/http/httptest; carried by production source in this build context") {
		t.Fatalf("row census incomplete:\n%s", notice)
	}
}

func TestContractBaselineRowsAreExact(t *testing.T) {
	valid := "modules/a/contracts html/template Render returns template.HTML."
	for _, text := range []string{
		"modules/a/contracts html/template",
		"modules/a html/template why",
		"modules/a/internal html/template why",
		"modules/a/contracts/atest html/template why",
		"modules/../a/contracts html/template why",
		"/modules/a/contracts html/template why",
		"modules/a/contracts.go html/template why",
		"kit/httpx html/template why",
		"apps/app/contracts html/template why",
		"modules/a/contracts modules/a/contracts why",
		"modules/a/contracts modules/b/internal why",
		"modules/a/contracts html/template why\n" + valid,
		"\xff",
	} {
		if _, err := parseBaseline("baseline", text, fixtureGraph()); err == nil || !strings.Contains(err.Error(), "baseline") {
			t.Errorf("accepted %q: %v", text, err)
		}
	}
	rows, err := parseBaseline("baseline", " # comment\r\n\r\n"+valid+" # literal\r\n", fixtureGraph())
	if err != nil || len(rows) != 1 || !strings.Contains(baselineNotices(rows, nil), "# literal") {
		t.Fatalf("CRLF/comment parsing: %v; %+v", err, rows)
	}
}

func TestContractBaselineIsReadFromTheCheckedRepository(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "scripts/imports-layers.txt", product+"/modules/a platform\n")
	p := packageFixture("modules/a/contracts")
	p.Dir, p.Module.Dir = filepath.Join(root, "modules/a/contracts"), root
	p.Imports = []string{"html/template"}
	for _, malformed := range []string{"modules/a/contracts html/template\n", "\xff\n"} {
		writeFixture(t, root, "scripts/imports-contract-baseline.txt", malformed)
		for _, args := range [][]string{{root}, {"--edges", root}} {
			var stdout, stderr bytes.Buffer
			if code := run(t.Context(), args, &stdout, &stderr, fakeCommand(t, root, jsonStream(t, p))); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "imports-contract-baseline.txt") {
				t.Fatalf("%q code %d, stdout %s, stderr %s", malformed, code, &stdout, &stderr)
			}
		}
	}
	file := filepath.Join(root, "scripts/imports-contract-baseline.txt")
	for _, row := range []struct{ body, want string }{
		{"modules/a/contracts github.com/lib/pq Wrong dependency, so no row is carried.\n", "contract-baseline-unused"},
		{"modules/a/contracts html/template Render returns template.HTML.\n", ""},
	} {
		if err := os.WriteFile(file, []byte(row.body), 0o644); err != nil {
			t.Fatal(err)
		}
		accepted := row.want == ""
		var stdout, stderr bytes.Buffer
		code := run(t.Context(), []string{root}, &stdout, &stderr, fakeCommand(t, root, jsonStream(t, p)))
		if accepted != (code == 0) {
			t.Fatalf("%q returned %d, stdout %s, stderr %s", row.body, code, &stdout, &stderr)
		}
		if accepted && !strings.Contains(stdout.String(), "contract baseline rows above") || !accepted && !strings.Contains(stderr.String(), row.want) {
			t.Fatalf("%q stdout %s, stderr %s", row.body, &stdout, &stderr)
		}
	}
}
