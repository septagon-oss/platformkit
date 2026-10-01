package design

// accent.go measures the one colour a person can choose.
//
// A generated theme is repaired on the way out of FromSeed and a client's
// override is refused by Client.Resolve, both of which are doors a *file* passes
// through. The other place a colour enters a page is a row: the site settings a
// tenant edits, which no file and no generator ever sees, and which a browser
// paints over the tokens layer at the moment the page is drawn. The gate cannot
// be applied at the write alone, because a row can hold a colour that was
// accepted before the gate existed — and the paint is the thing a reader sees.
// So the door that paints asks this package what colour it may paint, the same
// way the export does.
//
// What it asks is the same measurement every other door makes: the theme with the
// accent replaced, run through Theme.Check and Theme.CheckRoles, and the theme it
// is not painting measured once beside it. Not a hand-listed set of grounds beside
// the gate: an accent is painted as text (fg-link, fg-brand), as a fill a label
// sits on (surface-brand, and the soft tint mixed from itself), and as a ring, so
// measuring it against one surface would certify a button and leave a link
// unread — the failure this package has been called out for five times.

import (
	"fmt"
	"strings"
)

// accentToken is the token the accent is painted from. Four roles and one token
// pair read it directly (roles.go: surface-brand, fg-brand, fg-link,
// ring-brand) and three more are mixed from it, which is why a colour that
// reaches it is measured as the whole layer rather than as one pair.
const accentToken = "accent-default"

// LegibleAccent returns the colour this pair can paint where accent-default is
// painted in the named theme, given the colour somebody asked for.
//
// Two answers, in this order:
//
//  1. the colour itself, when the finished theme clears both halves of the gate
//     with it, which is how a brand colour keeps all of itself;
//  2. otherwise the nearest colour along the line settle walks — its own hue,
//     saturation lost as it moves toward the pole that reads on this theme's
//     surfaces — that clears them, which is the same walk FromSeed repairs a
//     generated accent with, run against the pair the caller holds rather than
//     the one the generator drew.
//
// An error is returned when no colour could be named: a theme other than light or
// dark names no surfaces to measure against; a value this package cannot parse or
// that carries alpha names no ratio (Theme.Check's reason, and the one
// Client.Validate gives for the same refusal); and a theme that fails its own gate
// for every colour — one that names no colours at all, say — has no surfaces to
// search. A caller that gets an error paints no accent at all for that mode, which
// leaves the page in the palette it already links — the installation's own accent,
// which that palette's own gate issued — rather than in a colour nobody measured.
// That is why there is no third answer here: not painting is how a caller paints
// the palette's accent. The answer is per mode: a pair broken in one theme is
// asked nothing about the other, and a site whose dark half reads still wears its
// tenant's colour in the dark.
//
// The value comes back as the #rrggbb literal a theme stores, lower-case, which
// is the spelling a caller writes into a stylesheet and the one the gate re-reads.
func (p Pair) LegibleAccent(theme, value string) (string, error) {
	var target Theme
	switch theme {
	case "light":
		target = p.Light
	case "dark":
		target = p.Dark
	default:
		return "", fmt.Errorf("design: theme %q names no surfaces to measure %q against: a theme is light or dark", theme, value)
	}
	raw, err := parseColor(value)
	if err != nil {
		return "", fmt.Errorf("design: accent %w", err)
	}
	if raw[3] != 1 {
		return "", fmt.Errorf("design: accent %s carries alpha %.2f: a colour with alpha has no contrast until it is composited, so paint an opaque colour", value, raw[3])
	}
	candidate := strings.ToLower(value)
	if target.withAccent(candidate).gated() {
		return candidate, nil
	}
	if lit, ok := settleAccent(candidate, target, func(probe string) bool {
		return target.withAccent(probe).gated()
	}); ok {
		return lit, nil
	}
	return "", fmt.Errorf("design: theme %s of this pair holds no colour %s could be repaired into: no colour along its own hue clears the gate, so paint this palette's own accent and say so", theme, candidate)
}

// withAccent is this theme with its accent set to value — the substitution an
// unlayered :root declaration makes in the browser, and the measurement
// LegibleAccent repeats at every step of its walk. A value this package cannot set
// leaves the theme as it stands, which the gate then reports: the only way here is
// a token that is not a token, since every caller's value is one parseColor read.
func (t Theme) withAccent(value string) Theme {
	updated, err := t.setColor(accentToken, value)
	if err != nil {
		return t
	}
	return updated
}

// gated reports whether this theme clears both halves of the gate: the twenty-two
// tokens it sets and the role layer a browser paints with them. It is
// Client.Resolve's acceptance test, read as a yes-or-no rather than as a refusal,
// for the caller that would rather search for a colour that passes than report the
// one that does not.
func (t Theme) gated() bool {
	return t.Check() == nil && t.CheckRoles(RoleLayer(), GatedRolePairs()) == nil
}

// settleAccent is settle's walk with the caller's own test in place of a list of
// grounds: the same line — the hue kept, the saturation lost as the colour moves
// toward the pole that reads on this theme's own surfaces — tested at every step
// it can take. The pole is read from the theme rather than named by the caller:
// a theme that paints light copy on dark surfaces has to be walked toward light,
// and the copy it paints is the colour the gate already certifies everywhere.
// It reports false when no step reached the caller's test, leaving the caller to
// the answer it holds rather than to a colour outside the walk.
func settleAccent(candidate string, theme Theme, ok func(string) bool) (string, bool) {
	towardLight := Luminance(mustParse(theme.TextPrimary)) > Luminance(mustParse(theme.SurfaceCanvas))
	hue, saturation, value := rgbToHSV(mustParse(candidate))
	step := 1.0 / 255
	if towardLight {
		for t := step; t <= 1+1e-9; t += step {
			lit := hsv(hue, saturation*(1-t), value+(1-value)*t)
			if ok(lit) {
				return lit, true
			}
		}
		return "", false
	}
	for t := step; t <= 1+1e-9; t += step {
		lit := hsv(hue, saturation*(1-t), value*(1-t))
		if ok(lit) {
			return lit, true
		}
	}
	return "", false
}
