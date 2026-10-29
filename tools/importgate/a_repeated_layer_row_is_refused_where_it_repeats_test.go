package main

import (
	"strings"
	"testing"
)

const layerFile = "scripts/imports-layers.txt"

// TestRepeatedLayerRowIsRefusedAtTheRowThatRepeats checks the three shapes of a
// layer declaration that says one thing twice: one module given two layers, one
// pair justified twice, and text that is not UTF-8. Each refuses the read, naming
// the row to remove, so a half-parsed map is never reported as a verdict. A pair is
// keyed in sorted order, so its second reading may be written the other way round
// and stays the same exception.
func TestRepeatedLayerRowIsRefusedAtTheRowThatRepeats(t *testing.T) {
	g := fixtureGraph()
	for _, row := range []struct{ text, at, want string }{
		{product + "/modules/a platform\n" + product + "/modules/a identity\n",
			layerFile + ":2:", "duplicate module " + product + "/modules/a"},
		{product + "/modules/a <-> " + product + "/modules/b Both contract trees name each other's values.\n" +
			product + "/modules/b <-> " + product + "/modules/a The same pair, the other way round.\n",
			layerFile + ":2:", "duplicate pair " + product + "/modules/a <-> " + product + "/modules/b"},
		{product + "/modules/a \xff\n", layerFile + ":", "module layer rows must be UTF-8"},
	} {
		err := parseLayers(layerFile, row.text, g, product, newLayerDeclaration())
		if err == nil || !strings.Contains(err.Error(), row.want) {
			t.Errorf("a declaration reading %q answered %v, wanting it refused for %q", row.text, err, row.want)
			continue
		}
		if !strings.HasPrefix(err.Error(), row.at) {
			t.Errorf("refusal %q does not begin at %s", err, row.at)
		}
	}
}
