package contracts

import (
	"fmt"
	"math"
	"strings"
)

// The two canvases a tenant's accent is drawn on, and the least it must read
// against each of them.
//
// They are the kit's own surfaces — design.Theme.SurfaceCanvas, the sheet the
// generated pages are composed from — written here as two hex values rather than
// imported, because a module's contracts/ names no theme, no SDK and no renderer
// (it is the boundary rule the whole of contracts/ lives by: importing design
// would put a stylesheet in the closure of every module that imports these
// contracts). What keeps the duplication honest is not a comment:
// TestTheCanvasesAreTheKitsOwn, in modules/site/module_test.go, asks package by
// package that both values still equal design.Light().SurfaceCanvas and
// design.Dark().SurfaceCanvas, so a theme that moves turns a test red naming both
// spellings rather than leaving a second, quieter palette registry behind it.
const (
	CanvasLight = "#f2efe7"
	CanvasDark  = "#0e1614"
)

// MinAccentRatio is what the threshold is for. An accent drawn on a canvas
// is a graphic, not body text, and 3:1 is the target this repository already
// publishes for functional non-text boundaries and cues
// (ui/components/shared-web-components.md, with the WCAG non-text-contrast link
// beside it).
//
// It is 3 and not 4.5 for a second, harder reason: 4.5 against both canvases
// refuses #2563eb, the colour this repository ships as DefaultPrimaryColor, and so
// it would refuse every tenant that never chose one — a rule that indicts the
// default is a bug handed to the next person as a rule.
const MinAccentRatio = 3.0

// ContrastRatio is the WCAG 2.x contrast ratio between two #rrggbb colours: the
// relative luminance of each, then (lighter + 0.05) / (darker + 0.05).
//
// The formula is the standard's, not an approximation of it, and it is written out
// here rather than imported because it is nine lines of arithmetic and the
// alternative is a third-party colour package in the closure of every module that
// reads a site's settings. Both arguments must be #rrggbb — the same grammar
// Validate enforces on a stored colour — and a caller that cannot say which canvas
// it means gets an error naming the value rather than a ratio it would misread.
func ContrastRatio(colour, canvas string) (float64, error) {
	l, err := relativeLuminance(colour)
	if err != nil {
		return 0, err
	}
	r, err := relativeLuminance(canvas)
	if err != nil {
		return 0, err
	}
	lighter, darker := math.Max(l, r), math.Min(l, r)
	return (lighter + 0.05) / (darker + 0.05), nil
}

// AccentRatios answers how a proposed accent reads against the light canvas and
// against the dark one, in hundredths (4.50, 3.55), and whether the colour is a
// colour at all.
//
// The rounding is here and nowhere else, and it is a presentation rounding: the
// threshold in Validate compares the unrounded numbers, because a rule that
// rounded first would let a 2.994 through. Both canvases are answered for
// whichever theme the tenant chose — `system` means the visitor may be on either,
// and the document that quotes these two numbers says which canvas each belongs
// to.
func AccentRatios(colour string) (light, dark float64, ok bool) {
	l, err := ContrastRatio(colour, CanvasLight)
	if err != nil {
		return 0, 0, false
	}
	d, err := ContrastRatio(colour, CanvasDark)
	if err != nil {
		return 0, 0, false
	}
	return roundHundredths(l), roundHundredths(d), true
}

// readsOnBothCanvases is the whole of the new rule: does this colour read at
// least MinAccentRatio against the light canvas and against the dark one? The
// comparison is unrounded — a rule that rounded first would let a 2.994 through.
// It is `>=`, so a colour landing exactly on the threshold is inside the band;
// no #rrggbb colour lands exactly there against these two canvases, and the
// conformance case pins the pair the edge sits between rather than the boundary
// itself (see sitetest: "the band's edge sits between #8b8b8b and #8a8a8a").
//
// It is unexported because it is not a thing a caller decides: Validate runs it,
// which is the one place the SQL service, the change apply and the conformance
// fake all reach, so the fake cannot pass a colour the database would refuse.
func readsOnBothCanvases(colour string) bool {
	light, err := ContrastRatio(colour, CanvasLight)
	if err != nil {
		return false
	}
	dark, err := ContrastRatio(colour, CanvasDark)
	if err != nil {
		return false
	}
	return light >= MinAccentRatio && dark >= MinAccentRatio
}

// relativeLuminance is the standard's L: each sRGB channel linearised, then
// 0.2126 R + 0.7152 G + 0.0722 B. Case is folded first, because a colour typed as
// #2563EB is the same colour; anything that is not #rrggbb after the fold is an
// error naming the value, not a ratio of 0.
func relativeLuminance(colour string) (float64, error) {
	colour = strings.ToLower(colour)
	if !hexColor.MatchString(colour) {
		return 0, fmt.Errorf("site: %q is not a colour; a colour is #rrggbb", colour)
	}
	var c [3]float64
	for i := range 3 {
		c[i] = channel(colour[1+2*i : 3+2*i])
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2], nil
}

// channel is one sRGB channel as the standard wants it: the byte over 255, then
// the transfer curve undone. The two constants are the standard's, and the break
// at 0.03928 is where the linear segment hands over to the gamma one.
//
// It cannot fail, because the only caller has already matched the string against
// hexColor: two characters, each a hex digit, is the whole of what this parses.
func channel(pair string) float64 {
	var n int
	for i := 0; i < 2; i++ {
		if d := pair[i]; d <= '9' {
			n = n*16 + int(d-'0')
		} else {
			n = n*16 + int(d-'a') + 10
		}
	}
	v := float64(n) / 255
	if v <= 0.03928 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func roundHundredths(v float64) float64 { return math.Round(v*100) / 100 }
