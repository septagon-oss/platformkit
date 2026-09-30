package internal

// Review round 18 (T-0107). Rounds 14 to 17 each filed the same shape of finding,
// and each cure was a pair added to the gate: the card and the canvas, the fill a
// hovered control moves under its own line, the muted panel, then this module's
// two class-list edges. Every cure works because the rule drawing the line paints
// it in a *role* — `BorderColor(style.BorderPrimary)` — so the pair lands in the
// layer `design.RoleLayer()` declares and `Client.Resolve` measures a client's
// file against it.
//
// This module's prose sheet draws two more lines and reaches past the role layer
// for both. Read off the sheet itself, typed, it declares
// `border-color: var(--pk-color-border-strong)` for `[data-prose] blockquote` (a
// 4 px bar marking a quotation on every article the site serves) and
// `border-color: var(--pk-color-border-default)` for `[data-prose] th, td`, both
// on the page's own ground (`clPage`'s `Bg(style.SurfaceSecondary)`). Those are
// *token* references, and the two halves of the gate read different layers:
// `Theme.Check` walks a fixed list of token pairs, `CheckRoles` walks the role
// layer. `border-default` survives the first — `bodyContrast` names it on the
// canvas at `MinContrastGraphic`, which is why a washed-out `border-default` is
// refused at the door today. `border-strong` is named by neither half, and
// `design.RoleLayer()` declares no role for it at all (grep `"border-strong"` in
// design/roles.go answers nothing), so the token a client may file in
// `design.yaml` decides the colour of a line this repository draws and no door
// measures it. That is review 14's finding for the field's edge, one token over:
// a pair the kernel paints that no gate names, accepted while the line it paints
// disappears.
//
// Three ways to make this pass, all honest:
//
//   - declare a border-strong role in design/roles.go and gate it in
//     edgeRolePairs on the ground the site paints it on, the cure rounds 14 to 17
//     applied to every other line, after which the door refuses the palette;
//   - draw the quotation bar in a role the gate already holds (border-primary, as
//     clHeader and clFooter do two lines above in this same file); or
//   - decide the bar is decoration and name it in r18Decorative beside the
//     reason. This file asserts that list in both directions, so an exemption
//     cannot outlive the line it was written for and cannot be claimed by a rule
//     nobody read.
//
// What this file does not do is invent a number: it demands of a painted line
// exactly `design.MinContrastGraphic`, the floor every other boundary in this
// repository is held to, and it asks the door to refuse a palette only after
// measuring that the palette really is under that floor.

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/css"
)

// r18Decorative names a line this module's sheet draws that the product decides
// carries no visual information, so it is exempt from the graphic floor. It is
// empty as this file is written, and it is asserted in both directions: every
// entry has to be a line the sheet actually draws, and every line the sheet draws
// is either exempt here or measured below.
var r18Decorative = map[string]string{}

// r18Line is one rule of this module's sheet that draws a line: the selector it
// draws it with, the custom property it paints it from, and the ground it stands
// on — the page's fill, since no prose element carries a background of its own.
type r18Line struct {
	selector, paint, ground string
}

// TestEveryLineTheSiteSheetDrawsIsEitherGatedOrNamed is the review's pin: this
// module's own sheet may not draw a line in a colour no door measures.
func TestEveryLineTheSiteSheetDrawsIsEitherGatedOrNamed(t *testing.T) {
	lines := r18Lines(t)
	if len(lines) < 2 {
		// Two rules draw a line today: [data-prose] blockquote and [data-prose] th, td.
		// A count below that is this reader failing to see them, not the sheet getting
		// shorter: a sheet that stopped drawing a line would be answered by the
		// exemption check at the foot of this case, not by a fatal here.
		t.Fatalf("the sweep found %d rules drawing a line in this module's sheet; the reader is broken, not the tree", len(lines))
	}
	names := make([]string, 0, len(lines))
	for _, line := range lines {
		names = append(names, line.key())
	}
	t.Logf("the sheet's own rules draw %d lines: %v", len(lines), names)
	palettes := r17Corpus(t)
	for _, line := range lines {
		if _, exempt := r18Decorative[line.key()]; exempt {
			continue
		}
		r18MeasureLine(t, line, palettes)
		r18DoorRefusesTheSameLine(t, line)
	}
	r18ExemptionsArePainted(t, lines)
}

func (l r18Line) key() string { return l.selector + " " + l.paint }

// r18MeasureLine asks the shipped identity and the generated corpus what the line
// actually measures on the ground the site draws it on.
func r18MeasureLine(t *testing.T, line r18Line, palettes []r17Palette) {
	t.Helper()
	under, unreadable := 0, 0
	shown := 0
	for _, worn := range palettes {
		for _, theme := range worn.pair.Both() {
			painted, ground, ok := r18Resolve(theme, line)
			if !ok {
				unreadable++
				continue
			}
			if got := design.Contrast(painted, ground); got < design.MinContrastGraphic {
				under++
				if shown < 3 {
					shown++
					t.Errorf("%s/%s: %s draws its line in %s on %s and it measures %.2f:1, under the %.1f:1 the gate holds every other boundary in this repository to",
						worn.name, theme.Name, line.selector, line.paint, line.ground, got, design.MinContrastGraphic)
				}
			}
		}
	}
	if unreadable > 0 {
		t.Errorf("%s paints its line from %s on %s, and the resolved layer hands back nothing for one of them in %d of %d readings: the sheet reaches for a name neither the token half nor the role layer defines",
			line.selector, line.paint, line.ground, unreadable, len(palettes)*2)
	}
	if under > 0 {
		t.Errorf("%s paints a line under the graphic floor in %d of the %d palettes a client can be issued", line.selector, under, len(palettes)*2)
	}
}

// r18DoorRefusesTheSameLine files the override a client is entitled to write —
// the line painted in the colour of the ground under it, which is a colour that
// client already ships — and asks the door that accepts a design.yaml what it
// says. A refusal has to name the token, because a client that is refused has to
// know which line it just erased.
func r18DoorRefusesTheSameLine(t *testing.T, line r18Line) {
	t.Helper()
	shipped := design.Default()
	theme := shipped.Light
	ground, ok := r18Ground(theme, line)
	if !ok {
		t.Fatalf("%s: the shipped identity does not resolve %s on %s", line.selector, line.paint, line.ground)
	}
	literal := r18Hex(ground)
	if token, isToken := r18TokenName(line.paint); isToken {
		client := design.Client{
			Slug:   "erasing-a-line",
			Seed:   design.Seed{Sector: "insurance", Name: "Erasing A Line"},
			Tokens: map[string]map[string]string{theme.Name: {token: literal}},
		}
		pair, err := client.Resolve()
		if err == nil {
			erased, _, _ := r18ResolveValues(pair.Light, line)
			t.Errorf("%s draws its line in %s on %s, and the door accepted a design.yaml that sets that token to %s — the ground's own colour, %s. A client can be issued a palette where a line this repository draws cannot be seen and nothing refused it: name the pair the way the other lines are named, or decide the line is decoration and say so in r18Decorative",
				line.selector, line.paint, line.ground, literal, r18Report(erased, ground))
			return
		}
		if !strings.Contains(err.Error(), token) {
			t.Errorf("%s: the door refused an override of %q with an error that does not name it: %v", line.selector, token, err)
		}
		t.Logf("%s: the door refused the same override of %s: %v", line.selector, token, err)
		return
	}
	// The sheet paints its line from a role: the pair belongs to
	// design.EdgeRolePairs, which is where the door reads it.
	role := strings.TrimPrefix(line.paint, "--pk-role-")
	for _, pair := range design.EdgeRolePairs() {
		if pair.Foreground == role && pair.Background == line.ground {
			return
		}
	}
	t.Errorf("%s paints its line from %s on %s, which design.EdgeRolePairs does not name (%s would be refused by no door)", line.selector, line.paint, line.ground, literal)
}

// r18ResolveValues resolves the layer one theme ships and reads the two colours
// of one line off it: what the line is painted with and what it is painted on.
func r18ResolveValues(theme design.Theme, line r18Line) (design.SRGBA, design.SRGBA, bool) {
	values, err := design.ResolveColors(theme.Tokens(), design.RoleLayer())
	if err != nil {
		return design.SRGBA{}, design.SRGBA{}, false
	}
	painted, hasPaint := values[line.paint]
	ground, hasGround := values["--pk-role-"+line.ground]
	if !hasPaint || !hasGround {
		return design.SRGBA{}, design.SRGBA{}, false
	}
	return painted, ground, true
}

func r18Resolve(theme design.Theme, line r18Line) (design.SRGBA, design.SRGBA, bool) {
	return r18ResolveValues(theme, line)
}

// r18Ground is the colour of the ground alone, read apart from the line so a
// hostile override of the line cannot move it.
func r18Ground(theme design.Theme, line r18Line) (design.SRGBA, bool) {
	values, err := design.ResolveColors(theme.Tokens(), design.RoleLayer())
	if err != nil {
		return design.SRGBA{}, false
	}
	ground, ok := values["--pk-role-"+line.ground]
	return ground, ok
}

// r18VarName reads the custom property out of the var() a declaration holds: the
// sheet renders a reference as var(--pk-color-border-strong), and the gate reads
// the property name.
func r18VarName(value string) string {
	return strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(value), "var("), ")")
}

// r18TokenName turns the custom property a sheet paints from into the token name
// a client would file in design.yaml, and says whether it is a token at all.
func r18TokenName(paint string) (string, bool) {
	if !strings.HasPrefix(paint, "--pk-color-") {
		return "", false
	}
	return strings.TrimPrefix(paint, "--pk-color-"), true
}

// r18Hex renders a resolved colour the way a design.yaml would write it.
func r18Hex(color design.SRGBA) string {
	channel := func(value float64) string { return fmt.Sprintf("%02x", int(math.Round(value*255))) }
	return "#" + channel(color[0]) + channel(color[1]) + channel(color[2])
}

// r18Report puts the measured ratio in the failure message.
func r18Report(painted, ground design.SRGBA) string {
	return fmt.Sprintf("%.2f:1 against the ground it is drawn on", design.Contrast(painted, ground))
}

// r18Lines reads the rules this module's sheet ships, typed off the sheet itself,
// and returns the ones that draw a line, on the page's fill.
func r18Lines(t *testing.T) []r18Line {
	t.Helper()
	source, err := os.ReadFile("style.go")
	if err != nil {
		t.Fatalf("read this module's shipped rules: %v", err)
	}
	page := "surface-secondary"
	fill := regexp.MustCompile(`(?m)^\tclPage\s*=.*Bg\(style\.Surface([A-Za-z0-9]+)\)`).FindStringSubmatch(string(source))
	if fill != nil {
		page = "surface-" + r17Kebab(fill[1])
	}
	var lines []r18Line
	err = prose().WalkRules(func(selector string, decls []css.Declaration) error {
		paint, draws := "", false
		for _, decl := range decls {
			if !strings.HasPrefix(decl.Property, "border") {
				continue
			}
			if strings.HasSuffix(decl.Property, "-color") {
				paint, draws = r18VarName(decl.Value.CSS()), true
				continue
			}
			if value := decl.Value.CSS(); strings.Contains(value, "solid") || strings.Contains(value, "dashed") {
				draws = true // a width and a style with no colour: the line is the text colour
			}
		}
		if !draws {
			return nil
		}
		if paint == "" {
			// A width and a style with no colour paints the line in the element's
			// own text colour. No shipped rule does this; recorded rather than
			// guessed at, because the text colour a utility leaves behind is not a
			// border role and the gate reads border roles.
			t.Logf("%s draws a line with no colour of its own: the line is currentColor, which no pair names", selector)
			return nil
		}
		lines = append(lines, r18Line{selector: selector, paint: paint, ground: page})
		return nil
	})
	if err != nil {
		t.Fatalf("walk this module's shipped rules: %v", err)
	}
	return lines
}

// r18ExemptionsArePainted holds the exemption list to the lines that exist: an
// exemption nobody can reach is the second half of the rot rounds 16 and 17
// pinned against.
func r18ExemptionsArePainted(t *testing.T, lines []r18Line) {
	t.Helper()
	painted := map[string]bool{}
	for _, line := range lines {
		painted[line.key()] = true
	}
	for key, reason := range r18Decorative {
		if !painted[key] {
			t.Errorf("r18Decorative exempts %q (%s), which this module's sheet no longer draws: an exemption outlives the line it was written for, so delete it with the rule", key, reason)
		}
	}
}
