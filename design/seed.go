package design

import (
	"fmt"
	"hash/fnv"
	"math"
	"strings"
)

// Seed is what a client says about itself instead of writing hex: its sector,
// its name, and the one brand colour it may already own. Those three values are
// the whole input to FromSeed, which is what makes a client's identity auditable
// — a reader sees the decision in one line rather than reverse-engineering it
// from a stylesheet.
//
// Sector and Name are required, and both matter: together they separate two
// clients in one sector. Brand is optional and, when present, must be a colour
// literal this package parses. It fixes the accent's hue, never its lightness,
// because lightness is what the contrast gate decides.
type Seed struct {
	Sector string `yaml:"sector"`
	Name   string `yaml:"name"`
	Brand  string `yaml:"brand,omitempty"`
}

// Validate refuses a seed that would generate an identity nothing could
// attribute: no name, no sector, or a brand colour this package cannot read.
func (s Seed) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("design: seed requires a name")
	}
	if strings.TrimSpace(s.Sector) == "" {
		return fmt.Errorf("design: seed requires a sector")
	}
	if s.Brand == "" {
		return nil
	}
	color, err := parseColor(s.Brand)
	if err != nil {
		return fmt.Errorf("design: seed brand: %w", err)
	}
	if color[3] != 1 {
		return fmt.Errorf("design: seed brand %q must be opaque", s.Brand)
	}
	return nil
}

// seedKey is the seed's own fingerprint: name and sector, trimmed and
// lower-cased, so "Acme" and "acme " are one client rather than two palettes.
func (s Seed) seedKey() string {
	return strings.ToLower(strings.TrimSpace(s.Sector)) + "\x00" + strings.ToLower(strings.TrimSpace(s.Name))
}

func (s Seed) hash(salt string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s.seedKey()))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(salt))
	return h.Sum64()
}

// unit spreads one 64-bit hash over [0,1) the way the standard library's
// rand.Uint64 does, so every derived choice is identical on every architecture.
func unit(hash uint64) float64 {
	return float64(hash>>11) / float64(int64(1)<<53)
}

// goldenStep is the golden-angle conjugate of a full turn. Stepping a hash by it
// puts neighbouring seeds on distant hues instead of neighbouring ones, which is
// why "Acme" and "Acmes" do not wear near-identical palettes.
var goldenStep = 0.5 + math.Sqrt(5)/2

// Hue returns the accent hue this seed generates at: the brand's hue when the
// seed names a brand colour, otherwise the seed's hash stepped around the wheel.
// It is exported because the separation a client is promised is a hue separation,
// and a reader should be able to ask two seeds what they were told.
func (s Seed) Hue() (float64, error) {
	if err := s.Validate(); err != nil {
		return 0, err
	}
	if s.Brand != "" {
		color, err := parseColor(s.Brand)
		if err != nil {
			return 0, err
		}
		hue, _, _ := rgbToHSV(color)
		return hue, nil
	}
	return math.Mod(unit(s.hash("hue"))*goldenStep, 1) * 360, nil
}

// FromSeed generates the pair a client wears from its seed: the same decisions
// every time for the same seed, and a pair that passes Pair.Check before it is
// returned. The generator holds the lightness of every foreground it emits, so
// an identity that cannot reach MinContrast returns an error and no pair rather
// than a palette that fails on screen.
//
// Surfaces, accent, focus and the sidebar follow the seed. The four status
// colours keep their semantic hues — a danger red that moves because the client
// is an insurance firm is not a status colour any more.
func FromSeed(seed Seed) (Pair, error) {
	hue, err := seed.Hue()
	if err != nil {
		return Pair{}, err
	}
	surfaceSat := 0.06 + unit(seed.hash("surface"))*0.10
	accentSat := 0.45 + unit(seed.hash("accent"))*0.40
	pair := Pair{
		Light: generatedTheme("light", hue, surfaceSat, accentSat),
		Dark:  generatedTheme("dark", hue, surfaceSat, accentSat),
	}
	if err := pair.Check(); err != nil {
		return Pair{}, fmt.Errorf("design: seed %q generates a pair that fails its own gate: %w", seed.seedKey(), err)
	}
	return pair, nil
}

// generatedTheme builds one theme of a generated pair. Surfaces come first,
// because every foreground is repaired against the surface it will sit on. The
// accent is repaired against all three surfaces, not the two a button happens to
// own: ui/components also paints it as body-size text with no background of its
// own, so the pair the gate reads (accent on surface-muted) is a pair a page
// shows, and enforce is what keeps the generator's own output on its side of it.
func generatedTheme(name string, hue, surfaceSat, accentSat float64) Theme {
	theme := Theme{Name: name}
	surface := func(sat, value float64) string { return hsv(hue, sat, value) }
	if name == "dark" {
		theme.SurfaceCanvas = surface(surfaceSat*1.6, 0.10)
		theme.SurfacePrimary = surface(surfaceSat*1.5, 0.14)
		theme.SurfaceMuted = surface(surfaceSat*1.4, 0.19)
		theme.BorderDefault = surface(surfaceSat*1.3, 0.27)
		theme.BorderStrong = surface(surfaceSat*1.2, 0.45)
		theme.TextPrimary = hsv(hue, 0.10, 0.95)
		theme.TextMuted = enforce(hsv(hue, 0.14, 0.74), theme.SurfaceCanvas, theme.SurfacePrimary, theme.SurfaceMuted)
		theme.AccentDefault = enforce(hsv(hue, accentSat*0.75, 0.74), theme.SurfaceCanvas, theme.SurfacePrimary, theme.SurfaceMuted)
		theme.AccentHover = enforce(hsv(hue, accentSat*0.70, 0.86), theme.SurfaceCanvas, theme.SurfacePrimary, theme.SurfaceMuted)
		theme.AccentOn = enforce("#0a0a0a", theme.AccentDefault, theme.AccentHover)
		theme.Focus = enforce(hsv(complement(hue), 0.60, 0.72), theme.SurfaceCanvas)
		theme.SidebarBg = surface(surfaceSat*1.8, 0.07)
		theme.SidebarText = enforce("#f4f7f2", theme.SidebarBg)
		theme.SidebarMute = enforce(hsv(hue, 0.12, 0.72), theme.SidebarBg)
	} else {
		theme.SurfaceCanvas = surface(surfaceSat, 0.945)
		theme.SurfacePrimary = surface(surfaceSat*0.55, 0.995)
		theme.SurfaceMuted = surface(surfaceSat*1.35, 0.90)
		theme.BorderDefault = surface(surfaceSat*1.1, 0.80)
		theme.BorderStrong = surface(surfaceSat, 0.52)
		theme.TextPrimary = enforce(hsv(hue, 0.32, 0.12), theme.SurfaceCanvas, theme.SurfacePrimary)
		theme.TextMuted = enforce(hsv(hue, 0.24, 0.46), theme.SurfaceCanvas, theme.SurfacePrimary, theme.SurfaceMuted)
		theme.AccentDefault = enforce(hsv(hue, accentSat, 0.38), theme.SurfaceCanvas, theme.SurfacePrimary, theme.SurfaceMuted)
		theme.AccentHover = enforce(hsv(hue, min(1, accentSat*1.1), 0.30), theme.SurfaceCanvas, theme.SurfacePrimary, theme.SurfaceMuted)
		theme.AccentOn = enforce("#fbfffb", theme.AccentDefault, theme.AccentHover)
		theme.Focus = enforce(hsv(complement(hue), 0.68, 0.85), theme.SurfaceCanvas)
		theme.SidebarBg = hsv(hue, 0.38, 0.13)
		theme.SidebarText = enforce("#f6faf3", theme.SidebarBg)
		theme.SidebarMute = enforce(hsv(hue, 0.14, 0.74), theme.SidebarBg)
	}
	for _, status := range statusRoles {
		bg, fg := statusPalette(name == "dark", status.hue, surfaceSat)
		status.set(&theme, fg, bg)
	}
	return theme
}

// statusRole binds a semantic status to the hue that always means it and the two
// fields that hold it.
type statusRole struct {
	hue  float64
	set  func(*Theme, string, string)
	name string
}

// statusRoles are the four statuses whose hue is never generated.
var statusRoles = []statusRole{
	{155, func(t *Theme, fg, bg string) { t.StatusOK, t.StatusOKBg = fg, bg }, "ok"},
	{38, func(t *Theme, fg, bg string) { t.StatusWarning, t.StatusWarningBg = fg, bg }, "warning"},
	{3, func(t *Theme, fg, bg string) { t.StatusDanger, t.StatusDangerBg = fg, bg }, "danger"},
	{222, func(t *Theme, fg, bg string) { t.StatusInfo, t.StatusInfoBg = fg, bg }, "info"},
}

// statusPalette returns the badge and the text on it for one status in one
// theme. The hue is semantic; only how dark the text is, and how much of the
// hue is left in the badge, follows the seed.
func statusPalette(dark bool, hue, surfaceSat float64) (bg, fg string) {
	if dark {
		bg = hsv(hue, min(0.95, surfaceSat*1.6+0.14), 0.18)
		return bg, enforce(hsv(hue, 0.55, 0.74), bg)
	}
	bg = hsv(hue, min(0.95, surfaceSat*0.9+0.10), 0.96)
	return bg, enforce(hsv(hue, 0.62, 0.42), bg)
}

// complement is the hue the focus ring wears: the accent's hue turned halfway
// around the wheel, so a ring is never the colour of the control it rings.
func complement(hue float64) float64 { return math.Mod(hue+180, 360) }

// enforce repairs a candidate against every background it will sit on, moving it
// along the line from where it is toward the neutral pole that reads best on
// those backgrounds: it loses saturation as it gains lightness, the way a tint
// does, and reaches white or black if it has to. A candidate that already passes
// comes back untouched, which is how a brand colour keeps as much of itself as
// the gate allows. The value tested is the literal that would be stored, so the
// byte a theme actually holds is what reached the gate.
func enforce(candidate string, backgrounds ...string) string {
	bgs := make([]SRGBA, 0, len(backgrounds))
	for _, background := range backgrounds {
		bgs = append(bgs, mustParse(background))
	}
	if reaches(mustParse(candidate), bgs) {
		return candidate
	}
	hue, saturation, value := rgbToHSV(mustParse(candidate))
	towardLight := Contrast(hsvRGB(hue, 0, 1), bgs[0]) >= Contrast(hsvRGB(hue, 0, 0), bgs[0])
	step := 1.0 / 255
	if towardLight {
		for t := step; t <= 1; t += step {
			lit := hsv(hue, saturation*(1-t), value+(1-value)*t)
			if reaches(mustParse(lit), bgs) {
				return lit
			}
		}
		return "#ffffff"
	}
	for t := step; t <= 1; t += step {
		lit := hsv(hue, saturation*(1-t), value*(1-t))
		if reaches(mustParse(lit), bgs) {
			return lit
		}
	}
	return "#000000"
}

// reaches reports one colour against every background at once.
func reaches(color SRGBA, backgrounds []SRGBA) bool {
	for _, background := range backgrounds {
		if Contrast(color, background) < MinContrast {
			return false
		}
	}
	return true
}

// mustParse reads a literal this package wrote. A value it cannot read becomes
// mid grey, which fails every gate downstream rather than passing one quietly.
func mustParse(value string) SRGBA {
	color, err := parseColor(value)
	if err != nil {
		return SRGBA{0.5, 0.5, 0.5, 1}
	}
	return color
}

// MinDistance is how far apart two clients' palettes have to be for a reader to
// call them two clients: the number a register of a multi-client process checks
// before it will hold a new pair. The generator cannot promise it — hashing two
// names can land them a hundredth of a degree apart in hue — so what the kernel
// promises instead is that Distance tells the truth, Colliding reads it, and a
// process refuses the second client rather than wearing both.
// The value is measured, not chosen: over the 28-seed corpus the closest two
// palettes whose seeds sit 20 degrees or more apart in hue are 0.0145 apart, and
// the closest two seeds the hash happened to place on neighbouring hues are
// 0.0048 apart. A floor of 0.01 admits the first and refuses the second.
const MinDistance = 0.01

// Colliding reports two pairs too close to tell apart. A client set rejects a
// pair that collides with one it already holds; the client that arrives second
// changes its seed or names a brand colour, which is a decision somebody can
// read in design.yaml.
func Colliding(a, b Pair) bool { return Distance(a, b) < MinDistance }

// identityTokens are the tokens a seed decides. The four status colours are not
// among them: their hues are semantic and identical in every client, so leaving
// them in a distance measure would dilute it with twelve tokens that never move.
var identityTokens = []string{
	"surface-canvas", "surface-primary", "surface-muted",
	"text-primary", "text-muted", "accent-default", "accent-hover", "accent-on",
	"focus", "sidebar-bg", "sidebar-text", "sidebar-muted",
}

// Distance reports how far apart two pairs look: the mean absolute difference
// over the identity tokens of both themes, per sRGB channel, in [0,1]. It is
// coarse on purpose — it asks whether two clients would be taken for one
// another, not whether one token differs in the last digit.
func Distance(a, b Pair) float64 {
	var total float64
	count := 0
	for i, left := range a.Both() {
		lx, rx := left.colorTable(), b.Both()[i].colorTable()
		for _, name := range identityTokens {
			name := "--pk-color-" + name
			leftColor, rightColor := lx[name], rx[name]
			for channel := range 3 {
				total += math.Abs(leftColor[channel] - rightColor[channel])
				count++
			}
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

// colorTable is colorValues with the failure folded away for measures, which
// report zero rather than panic on a theme that names no colour.
func (t Theme) colorTable() map[string]SRGBA {
	values, err := t.colorValues()
	if err != nil {
		return map[string]SRGBA{}
	}
	return values
}

// hsv renders one HSV triple as the #rrggbb literal a Theme field stores. Hue is
// in degrees, saturation and value in [0,1].
func hsv(hue, saturation, value float64) string {
	color := hsvRGB(hue, saturation, value)
	return fmt.Sprintf("#%02x%02x%02x",
		int(math.Round(color[0]*255)), int(math.Round(color[1]*255)), int(math.Round(color[2]*255)))
}

func hsvRGB(hue, saturation, value float64) SRGBA {
	h := math.Mod(hue, 360) / 60
	if h < 0 {
		h += 6
	}
	c := value * saturation
	x := c * (1 - math.Abs(math.Mod(h, 2)-1))
	m := value - c
	var r, g, b float64
	switch {
	case h < 1:
		r, g, b = c, x, 0
	case h < 2:
		r, g, b = x, c, 0
	case h < 3:
		r, g, b = 0, c, x
	case h < 4:
		r, g, b = 0, x, c
	case h < 5:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return SRGBA{r + m, g + m, b + m, 1}
}

// rgbToHSV reads a colour back as hue in degrees and saturation, value in [0,1].
func rgbToHSV(color SRGBA) (hue, saturation, value float64) {
	r, g, b := color[0], color[1], color[2]
	top := math.Max(r, math.Max(g, b))
	low := math.Min(r, math.Min(g, b))
	delta := top - low
	if delta != 0 {
		switch top {
		case r:
			hue = math.Mod((g-b)/delta, 6) * 60
		case g:
			hue = ((b-r)/delta + 2) * 60
		default:
			hue = ((r-g)/delta + 4) * 60
		}
		if hue < 0 {
			hue += 360
		}
	}
	if top == 0 {
		return hue, 0, 0
	}
	return hue, delta / top, top
}
