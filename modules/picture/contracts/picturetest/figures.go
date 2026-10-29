package picturetest

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/picture/contracts"
)

// FigureDecision is the declaration boundary exercised by every figure adapter.
type FigureDecision interface {
	DeclareFigure(string, string, []string) (contracts.Figure, error)
}

// DeclareFigure delegates the same bounded decision used by field declarations.
func (*Fake) DeclareFigure(name, source string, ids []string) (contracts.Figure, error) {
	return contracts.CompileFigure(name, source, ids)
}

// RunFigures compares repeated regions and refusals against literal expectations.
func RunFigures(t *testing.T, factory func() FigureDecision) {
	t.Helper()
	const source = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><circle data-choice="round" r="1"/><g data-choice="round"><text x="2" y="3">A &amp; B</text></g></svg>`
	const want = `<svg height="10" preserveAspectRatio="xMidYMid meet" viewBox="0 0 10 10" width="10" xmlns="http://www.w3.org/2000/svg"><circle data-choice="round" data-selected="true" r="1"></circle><g data-choice="round" data-selected="true"><text x="2" y="3">A &amp; B</text></g></svg>`
	figure, err := factory().DeclareFigure("fixture", source, []string{"round"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := figure.Render([]string{"round"})
	if err != nil || body != want {
		t.Fatalf("%s: %v", body, err)
	}
	for _, body := range []string{strings.Replace(source, `<circle`, `<script`, 1), strings.Replace(source, `r="1"`, `r="1" onclick="x()"`, 1), strings.Replace(source, `>A &amp; B</text>`, `><g/></text>`, 1)} {
		got, err := factory().DeclareFigure("fixture", body, []string{"round"})
		if err == nil || got.SVG() != "" {
			t.Fatal("unsafe figure admitted")
		}
	}
	if body, err := figure.Render([]string{"missing"}); err == nil || body != "" {
		t.Fatal("unknown highlight admitted")
	}
	if strings.Contains(figure.SVG(), "data-selected") {
		t.Fatal("render mutated declaration")
	}
}
