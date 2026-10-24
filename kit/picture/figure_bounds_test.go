package picture_test

import (
	"github.com/septagon-oss/platformkit/kit/picture"
	"strings"
	"testing"
)

func TestFigurePreservesSharedBoundsAndBoundsLeafText(t *testing.T) {
	const head = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">`
	const marker = `<g data-choice="round"/>`
	const tail = `</svg>`
	root := func(body string) string { return head + body + tail }
	text := func(value string) string { return `<text>` + value + `</text>` }
	sized := func(size int) string {
		return root(marker + "<!--" + strings.Repeat("x", size-len(root(marker))-7) + "-->")
	}
	for _, tc := range []struct {
		name, source string
		valid        bool
	}{
		{"source bound", sized(262144), true}, {"source overflow", sized(262145), false},
		{"elements bound", root(marker + strings.Repeat("<g/>", 2046)), true}, {"elements overflow", root(marker + strings.Repeat("<g/>", 2047)), false},
		{"depth bound", root(strings.Repeat("<g>", 30) + marker + strings.Repeat("</g>", 30)), true}, {"depth overflow", root(strings.Repeat("<g>", 31) + marker + strings.Repeat("</g>", 31)), false},
		{"attribute bound", root(`<circle data-choice="round" r="` + strings.Repeat("0", 65535) + `1"/>`), true}, {"attribute overflow", root(`<circle data-choice="round" r="` + strings.Repeat("0", 65536) + `1"/>`), false},
		{"numeric bound", root(marker + `<polyline points="` + strings.Repeat("0 0 ", 8190) + `"/>`), true}, {"numeric overflow", root(marker + `<polyline points="` + strings.Repeat("0 0 ", 8191) + `"/>`), false},
		{"text bound", root(marker + text(strings.Repeat("é", 512))), true}, {"text overflow", root(marker + text(strings.Repeat("é", 512)+"x")), false},
		{"total text bound", root(marker + strings.Repeat(text(strings.Repeat("x", 1024)), 16)), true}, {"total text overflow", root(marker + strings.Repeat(text(strings.Repeat("x", 1024)), 16) + text("x")), false},
		{"font bound", root(marker + `<text font-size="256">A</text>`), true}, {"font overflow", root(marker + `<text font-size="257">A</text>`), false},
		{"coordinate bound", root(`<circle data-choice="round" cx="1000000" r="1"/>`), true}, {"coordinate overflow", root(`<circle data-choice="round" cx="1000001" r="1"/>`), false},
		{"viewBox bound", strings.Replace(root(marker), "10 10", "8192 8192", 1), true}, {"viewBox overflow", strings.Replace(root(marker), "10 10", "8193 8192", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := picture.CompileFigure("fixture", tc.source, []string{"round"})
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if err != nil && f.SVG() != "" {
				t.Fatal("partial figure")
			}
		})
	}
}
