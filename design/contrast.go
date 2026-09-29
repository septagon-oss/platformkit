package design

import (
	"fmt"
	"math"
)

// The two contrast floors this package gates, both from WCAG 2.2. MinContrast is
// SC 1.4.3, the ratio text below 18pt must reach the surface it sits on.
// MinContrastGraphic is SC 1.4.11, the ratio a graphical object such as a focus
// ring must reach — the standard asks less of a ring than of a sentence, and a
// gate that asked the same of both would refuse a ring nobody could mistake for
// text. FromSeed refuses its own output at these floors and so refuses a
// client's named token at them: one gate, applied to a generated theme and an
// override alike.
const (
	MinContrast        = 4.5
	MinContrastGraphic = 3.0
)

// Luminance returns the WCAG 2.2 relative luminance of an sRGB colour: each
// channel linearised, then weighted 0.2126/0.7152/0.0722. The result is in
// [0,1] for an opaque input; a transparent channel contributes as the standard
// defines it, so a colour composited over a known backdrop is the caller's job.
func Luminance(c SRGBA) float64 {
	var sum float64
	for i, weight := range [...]float64{0.2126, 0.7152, 0.0722} {
		sum += weight * linearChannel(c[i])
	}
	return sum
}

// Contrast returns the WCAG 2.2 contrast ratio of two colours: 1 when they are
// identical, 21 between black and white. It is symmetric, and it reads the
// channels it is given, so an alpha-blended colour must be composited first.
func Contrast(a, b SRGBA) float64 {
	la, lb := Luminance(a), Luminance(b)
	if lb > la {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// linearChannel applies WCAG 2.2's own piecewise linearisation. The break is the
// standard's literal 0.03928, not the sRGB transfer function's 0.04045 (which is
// what the pk-design ancestor this is ported from uses): over the 256 values one
// 8-bit channel can take, no value falls between them — 10/255 = 0.039216 is
// below both and 11/255 = 0.043137 is above both — so the two agree on every
// colour a theme can hold, and TestWCAGThresholdDoesNotDependOnTheBranchConstant
// pins that rather than leaving the next reader to wonder which is a bug.
func linearChannel(channel float64) float64 {
	if channel <= 0.03928 {
		return channel / 12.92
	}
	return math.Pow((channel+0.055)/1.055, 2.4)
}

// contrastPair is one foreground and one background token, named as the themes
// name them, that the gate reads together, with the floor that applies to it.
type contrastPair struct {
	foreground, background string
	min                    float64
}

// bodyContrast lists the pairs a reader actually reads: text on the three
// surfaces, the accent used as text and the text set on it, the focus ring on the
// page, each status on its own badge, and the sidebar's two text tones on its own
// background. Borders and tinted surfaces carry no information of their own and
// are deliberately not in this list.
var bodyContrast = []contrastPair{
	{"text-primary", "surface-canvas", MinContrast},
	{"text-primary", "surface-primary", MinContrast},
	// The muted surface is the third surface a card raises itself onto, and the
	// kernel's components paint ordinary text there (a neutral badge, a read-only
	// field). A foreground certified against the two lighter surfaces says nothing
	// about it: text on a grey is the pair that fails first.
	{"text-primary", "surface-muted", MinContrast},
	{"text-muted", "surface-canvas", MinContrast},
	{"text-muted", "surface-primary", MinContrast},
	{"text-muted", "surface-muted", MinContrast},
	{"accent-default", "surface-canvas", MinContrast},
	{"accent-default", "surface-primary", MinContrast},
	// The accent is not only a button fill. ui/components paints it as body-size
	// text with no background of its own — the brand text utility, the outline and
	// link button variants, a brand detail value — so it lands on whichever
	// surface a card raised itself onto. Of the three, surface-muted is the one
	// that binds: it is the lightest surface in a dark theme, where foregrounds
	// are light, and the darkest in a light theme, where they are dark. Certifying
	// the accent against the two lighter surfaces only said it reads there.
	{"accent-default", "surface-muted", MinContrast},
	{"accent-hover", "surface-muted", MinContrast},
	{"accent-on", "accent-default", MinContrast},
	{"accent-on", "accent-hover", MinContrast},
	{"focus", "surface-canvas", MinContrastGraphic},
	{"status-ok", "status-okbg", MinContrast},
	{"status-warning", "status-warningbg", MinContrast},
	{"status-danger", "status-dangerbg", MinContrast},
	{"status-info", "status-infobg", MinContrast},
	// A status tone is not only a badge. clDetailValueTone paints the four of them
	// at text-sm inside clCardFrame (surface-primary) and clTextColor/clFieldErr
	// paint them on the page canvas, with no badge behind them, so each is a body
	// foreground on every surface a card can be raised onto — not only on the
	// badge the token named after it certifies.
	{"status-ok", "surface-canvas", MinContrast},
	{"status-ok", "surface-primary", MinContrast},
	{"status-ok", "surface-muted", MinContrast},
	{"status-warning", "surface-canvas", MinContrast},
	{"status-warning", "surface-primary", MinContrast},
	{"status-warning", "surface-muted", MinContrast},
	{"status-danger", "surface-canvas", MinContrast},
	{"status-danger", "surface-primary", MinContrast},
	{"status-danger", "surface-muted", MinContrast},
	{"status-info", "surface-canvas", MinContrast},
	{"status-info", "surface-primary", MinContrast},
	{"status-info", "surface-muted", MinContrast},
	{"sidebar-text", "sidebar-bg", MinContrast},
	{"sidebar-muted", "sidebar-bg", MinContrast},
}

// Check reports the first body role of this theme that does not reach
// MinContrast against the surface it sits on, naming both tokens and the ratio
// measured. A theme that names no colour is not a theme: every token the gate
// reads has to parse.
func (t Theme) Check() error {
	values, err := t.colorValues()
	if err != nil {
		return err
	}
	// Before any ratio: a colour that carries alpha has no contrast at all until
	// something is composited behind it, and ResolveColors premultiplies, so an
	// 80 %-opaque foreground would read as a darker opaque one and pass a gate it
	// should never have been measured by. The ancestor refuses the same way.
	for _, field := range colorFields {
		if color := values["--pk-color-"+field.name]; color[3] != 1 {
			return fmt.Errorf("%s: %s carries alpha %.2f: a colour with alpha has no contrast until it is composited, so name an opaque colour", t.Name, field.name, color[3])
		}
	}
	for _, pair := range bodyContrast {
		foreground, background := values["--pk-color-"+pair.foreground], values["--pk-color-"+pair.background]
		if got := Contrast(foreground, background); got < pair.min {
			return fmt.Errorf("%s: %s on %s measures %.2f:1, below %.1f:1",
				t.Name, pair.foreground, pair.background, got, pair.min)
		}
	}
	return nil
}

// RolePair is one foreground and one background role a stylesheet paints text
// with, and the floor that pair must reach. Roles are the layer a browser reads
// (the --pk-role-* declarations ui/style emits, some of them derived from tokens
// rather than equal to one); tokens are the layer a theme sets. Measuring only
// the token layer certifies colours nobody paints, which is how a palette that
// passes a gate can still paint text nobody can read.
//
// This package names no role: the owner of the role layer hands over the
// declarations it emits and the pairs it composes, so design keeps importing
// nothing of ui and stays the one place a ratio is measured.
type RolePair struct {
	Foreground, Background string
	// Min is the floor, usually MinContrast or MinContrastGraphic.
	Min float64
}

// CheckRoles resolves the caller's role declarations on top of this theme's
// tokens and reports the first pair of pairs that does not reach its own floor,
// naming both roles and the ratio measured. An opaque colour requirement comes
// first, for the reason Theme.Check gives: a role carrying alpha has no ratio
// until something is composited behind it.
func (t Theme) CheckRoles(roles []ColorToken, pairs []RolePair) error {
	values, err := ResolveColors(t.Tokens(), roles)
	if err != nil {
		return fmt.Errorf("%s: role layer: %w", t.Name, err)
	}
	for _, pair := range pairs {
		if pair.Min <= 0 || pair.Min > 21 {
			return fmt.Errorf("%s: role pair %s on %s names floor %v: a pair must name the ratio it requires", t.Name, pair.Foreground, pair.Background, pair.Min)
		}
		var missing []string
		for _, name := range [...]string{pair.Foreground, pair.Background} {
			color, ok := values[name]
			if !ok {
				missing = append(missing, name)
				continue
			}
			if color[3] != 1 {
				return fmt.Errorf("%s: %s carries alpha %.2f: a colour with alpha has no contrast until it is composited, so gate a pair on an opaque role", t.Name, name, color[3])
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("%s: role pair %s on %s names %v, which the resolved role layer does not define", t.Name, pair.Foreground, pair.Background, missing)
		}
		if got := Contrast(values[pair.Foreground], values[pair.Background]); got < pair.Min {
			return fmt.Errorf("%s: %s on %s measures %.2f:1, below the %.1f:1 the body text this role paints requires",
				t.Name, pair.Foreground, pair.Background, got, pair.Min)
		}
	}
	return nil
}

// CheckRoles reports the first theme of this pair whose role layer fails.
func (p Pair) CheckRoles(roles []ColorToken, pairs []RolePair) error {
	for _, theme := range p.Both() {
		if err := theme.CheckRoles(roles, pairs); err != nil {
			return err
		}
	}
	return nil
}

// Check reports the first theme of this pair that fails its own body roles.
// ui.Compose takes a Pair, so this is the unit the gate is applied at: a light
// theme that reads and a dark theme that does not is not shippable.
func (p Pair) Check() error {
	for _, theme := range p.Both() {
		if err := theme.Check(); err != nil {
			return err
		}
	}
	return nil
}
