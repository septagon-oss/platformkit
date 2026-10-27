package main

import (
	"strings"
	"testing"
)

// TestADependencyDeclaredLayerRefusesAnUpwardImportFromThisBuild checks that the
// layer a declared dependency reads for its own module weighs this build's verdict
// and not only its report: a production import that climbs from one module of the
// checked repository into a module the dependency ranks higher is one
// module-layer-upward refusal, and the refusal prints the layer each of the two
// repositories declared. A run that weighed the foreign module as unassigned here
// would answer that build with no refusal at all, and module-layer-missing could
// not say it either, because the row that makes the import upward exists one
// repository over — so the import the platform's order forbids would pass with a
// declaration this build read and then set aside.
func TestADependencyDeclaredLayerRefusesAnUpwardImportFromThisBuild(t *testing.T) {
	source, target := product+"/modules/a/contracts", dependency+"/modules/b/contracts"
	g := fixtureGraph(edge{source, target, source, "production", ""})
	g.owned = []string{product + "/modules/a"}
	declared := declareFixtureLayers(t, g, product+"/modules/a generic", dependency+"/modules/b product")
	got := decide(g, nil, nil, declared).refused
	if len(got) != 1 || got[0].rule != "module-layer-upward" || got[0].from != source || got[0].to != target {
		t.Fatalf("refusals = %+v, want one module-layer-upward on %s -> %s", got, source, target)
	}
	// The two ranks the two repositories own, printed by this build rather than by
	// either map: a diagnostic that dropped the far side's layer would read as a
	// row this build could repair on its own.
	want := product + "/modules/a generic -> " + dependency + "/modules/b product"
	if text := diagnostics(got); !strings.Contains(text, want) {
		t.Errorf("the refusal does not print both declared layers as %q: %s", want, text)
	}
}
