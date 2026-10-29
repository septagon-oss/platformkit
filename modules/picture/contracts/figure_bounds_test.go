package contracts_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/picture/contracts"
)

// textSource adds one bounded leaf text node to the fixture, the only markup the
// figure profile admits beyond the composed profile's vocabulary.
const textSource = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><circle data-choice="round" r="1"/><text x="1" y="2">Notes</text></svg>`

const openSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">`

// TestFigureRefusesSharedBoundsInItsTextPath names where the profile that adds
// bounded leaf text still refuses the shared grammar's limits. Each case is the same
// source with one construct pushed past a bound the composed profile already had;
// a figure that got past any of them would carry markup no composed template allows.
func TestFigureRefusesSharedBoundsInItsTextPath(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
	}{
		{"depth", openSVG + strings.Repeat("<g>", 32) + `<circle data-choice="round" r="1"/>` + strings.Repeat("</g>", 32) + "</svg>", "maximum 2048 elements and depth 32"},
		{"elements", openSVG + strings.Repeat(`<circle data-choice="round" r="1"/>`, 2048) + "</svg>", "maximum 2048 elements and depth 32"},
		{"attributes", `<circle data-choice="round" r="1"` + strings.Repeat(` stroke-linecap="round"`, 21) + "/>", "maximum 20 attributes"},
		{"attribute value", `<circle data-choice="round" r="1" stroke="` + strings.Repeat("a", 65537) + `"/>`, "attribute exceeds 65536 bytes"},
		{"document", strings.Repeat(" ", 262145) + textSource, "source exceeds 262144 bytes"},
		{"characters", strings.Replace(textSource, ">Notes<", ">N\x00otes<", 1), "XML 1.0 UTF-8 without a BOM is required"},
		{"leaf text path", strings.Replace(textSource, "Notes", strings.Repeat("n", 2049), 1), "text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := contracts.CompileFigure("fixture", tc.source, []string{"round"}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal named no bound: %v", err)
			}
		})
	}
}

// TestFigureBoundsLeafText keeps the profile's one addition inside its own bound:
// a label at 1024 bytes compiles, one byte more does not, and the drawing keeps the
// text as one escaped run.
func TestFigureBoundsLeafText(t *testing.T) {
	for _, size := range []int{1024, 1025} {
		source := strings.Replace(textSource, "Notes", strings.Repeat("n", size), 1)
		figure, err := contracts.CompileFigure("fixture", source, []string{"round"})
		if size == 1025 {
			if err == nil || figure.SVG() != "" {
				t.Fatal("a 1025-byte leaf text label compiled")
			}
			continue
		}
		if err != nil {
			t.Fatalf("a 1024-byte leaf text label refused: %v", err)
		}
		if !strings.Contains(figure.SVG(), strings.Repeat("n", 1024)) {
			t.Fatalf("compiled figure lost its label: %s", figure.SVG())
		}
	}
}

// TestFigureRefusesUnsafeConstructs admits exactly one construct the composed
// profile refuses, and keeps every other refusal.
func TestFigureRefusesUnsafeConstructs(t *testing.T) {
	for _, body := range []string{`<script/>`, `<foreignObject/>`, `<use href="#x"/>`, `<image/>`, `<defs/>`, `<style/>`, `<animate/>`,
		`<text font-family="remote">A</text>`, `<text font-size="1rem">A</text>`, `<g data-choice="round"><text><g/></text></g>`,
		`<circle data-choice="square" r="1"/>`, `<circle r="1" onclick="alert(1)"/>`} {
		source := strings.Replace(textSource, "</svg>", body+"</svg>", 1)
		if _, err := contracts.CompileFigure("fixture", source, []string{"round"}); err == nil || len(err.Error()) > 256 {
			t.Fatalf("accepted %s: %v", body, err)
		}
	}
	if _, err := contracts.CompileFigure("fixture", strings.Replace(textSource, "</svg>", `<text x="1" y="2">A &amp; B &lt; C</text></svg>`, 1), []string{"round"}); err != nil {
		t.Fatalf("refused escaped text: %v", err)
	}
}
