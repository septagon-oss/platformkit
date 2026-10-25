package contracts_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/picture/contracts"
)

const figureSource = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 80"><circle data-choice="round" cx="20" cy="25" r="10" fill="#112233"/><circle data-choice="round" cx="50" cy="25" r="10" fill="#112233"/><rect data-choice="square" x="70" y="15" width="20" height="20" fill="#AABBCC"/><text x="10" y="65" font-size="12" fill="#000000">Shapes &amp; marks</text></svg>`
const figureOracle = `<svg height="80" preserveAspectRatio="xMidYMid meet" viewBox="0 0 100 80" width="100" xmlns="http://www.w3.org/2000/svg"><circle cx="20" cy="25" data-choice="round" fill="#112233" r="10"></circle><circle cx="50" cy="25" data-choice="round" fill="#112233" r="10"></circle><rect data-choice="square" fill="#AABBCC" height="20" width="20" x="70" y="15"></rect><text fill="#000000" font-size="12" x="10" y="65">Shapes &amp; marks</text></svg>`

func TestFigureCanonicalBytesAndHighlights(t *testing.T) {
	f, err := contracts.CompileFigure("fixture", figureSource, []string{"round", "square"})
	if err != nil {
		t.Fatal(err)
	}
	if f.SVG() != figureOracle || fmt.Sprintf("%x", f.Digest()) != "9c06ab701b2ed93d3c2ed5cec5f42a9ed814de1e2e8fe307d70fb43c7ec39687" {
		t.Fatalf("unexpected canonical figure: %s", f.SVG())
	}
	selected, err := f.Render([]string{"round"})
	want := strings.ReplaceAll(figureOracle, `data-choice="round"`, `data-choice="round" data-selected="true"`)
	if err != nil || selected != want {
		t.Fatalf("highlight: %s, %v", selected, err)
	}
	neutral, err := f.Render(nil)
	if err != nil || neutral != figureOracle || f.SVG() != neutral {
		t.Fatal("render mutated declaration")
	}
	for _, ids := range [][]string{{"round", "round"}, {"unknown"}} {
		if output, err := f.Render(ids); output != "" || err == nil {
			t.Fatalf("accepted %v", ids)
		}
	}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var copy contracts.Figure
	if err := json.Unmarshal(b, &copy); err != nil || copy.SVG() != f.SVG() {
		t.Fatalf("round trip: %v", err)
	}
	for _, bad := range []string{strings.Replace(string(b), "9c06ab70", "00000000", 1), strings.Replace(string(b), `{"digest":`, `{"extra":1,"digest":`, 1), strings.Replace(string(b), `{"digest":`, `{"svg":"", "digest":`, 1)} {
		if err := json.Unmarshal([]byte(bad), &copy); err == nil {
			t.Fatal("accepted forged figure")
		}
	}
}

func TestFigureRefusesUnsafeMarkupAndMappings(t *testing.T) {
	for _, body := range []string{`<script/>`, `<foreignObject/>`, `<use href="#x"/>`, `<image/>`, `<defs/>`, `<text><g/></text>`, `<text font-family="remote">x</text>`, `<circle r="1" onclick="alert(1)"/>`, `<circle r="1" style="fill:red"/>`, `<circle r="1" fill="url(x)"/>`, `<g data-choice="round"><circle data-choice="round" r="1"/></g>`, `<circle data-choice="unknown" r="1"/>`, `<circle data-part="x" data-option="y" r="1"/>`} {
		source := strings.Replace(figureSource, "</svg>", body+"</svg>", 1)
		f, err := contracts.CompileFigure("fixture", source, []string{"round", "square"})
		if err == nil || f.Digest() != ([32]byte{}) || len(err.Error()) > 256 {
			t.Fatalf("accepted %s: %v", body, err)
		}
	}
	if _, err := contracts.CompileFigure("fixture", figureSource, []string{"round", "square", "missing"}); err == nil {
		t.Fatal("accepted missing mapping")
	}
	if _, err := contracts.CompileFigure("fixture", strings.Replace(figureSource, "Shapes &amp; marks", strings.Repeat("x", 1025), 1), []string{"round", "square"}); err == nil {
		t.Fatal("accepted oversized text")
	}
}
