package contracts

import "strings"

// Palette is the installation's own accent, per mode, for the site that chose
// none. The question is asked here, in the module that owns the settings row,
// and answered at composition — the shape WriteGate is, for the same reason:
// which colours this installation's chrome is drawn with is a fact about the
// deployment and not about a site, and a module that knew would be a module with
// a customer in it.
//
// These contracts cannot name a palette type: importing design would put a
// stylesheet in the closure of every module that imports them (contrast.go says
// where that line is), so the port answers #rrggbb strings and the composition
// answers them from the pair it named for itself.
//
// mode is ThemeLight or ThemeDark — the two modes a page is drawn in. A mode the
// installation names no colour for is answered with the empty colour, which is
// what a read then answers: an installation with no palette and a site with no
// accent has no accent, and nothing is pinned.
type Palette interface {
	Accent(mode string) string
}

// PaletteFunc is a Palette made of a function, the way usercontracts.AdministrationFunc
// is a Granting made of one: the composition's adapter and a test's literal, with
// no named struct in either. A nil func answers the empty colour for every mode.
type PaletteFunc func(mode string) string

func (f PaletteFunc) Accent(mode string) string {
	if f == nil {
		return ""
	}
	return f(mode)
}

// modeOf is the mode a row's theme is drawn in. ThemeSystem names no mode of its
// own — it follows the visitor — so it resolves against the light one, which is
// the pair's first and the theme an application that says nothing about colour
// gets (design.Pair.Both). A `system` page is not answered by this choice at all:
// the web shell carries both accents and lets the visitor's own preference pick.
func modeOf(theme string) string {
	if theme == ThemeDark {
		return ThemeDark
	}
	return ThemeLight
}

// Accent is the colour a site whose stored colour is `stored` is rendered in for
// `mode`: the stored colour when it names one, the palette's for that mode when
// it names none, and the empty colour when neither names one. It is the whole of
// the resolution rule and the only place the empty colour turns into a colour,
// which is why every read, and only a read, calls it.
func Accent(stored string, palette Palette, mode string) string {
	if stored != "" {
		return stored
	}
	if palette == nil {
		return ""
	}
	return palette.Accent(mode)
}

// Resolve is Accent for this row as it stands: the mode is the row's own theme,
// and a row on ThemeSystem is resolved against the light accent. It answers what
// a reader is shown and never writes anything: the row keeps the empty colour.
func (s *SiteSettings) Resolve(palette Palette) string {
	return Accent(s.PrimaryColor, palette, modeOf(s.Theme))
}

// ClearInstallationAccent sets a colour that repeats the installation's own to
// DefaultPrimaryColor, the value that means "the installation's". A form submits
// what a read gave it, and a save that stored that answer would freeze this
// tenant at the palette of the day and read back as an edit nobody made.
//
// It compares against both modes rather than the row's own, because a form that
// changes the theme from light to dark echoes the accent the read gave for light,
// and that colour is no more a choice there — stored as itself it would meet the
// legibility rule and be refused, which is a form that cannot change its theme.
// It normalises case and space the way Validate does, so it is idempotent, and it
// runs before that rule, which would refuse the palette's own value. A nil
// palette clears nothing: an installation with no accent has no accent to repeat.
func (s *SiteSettings) ClearInstallationAccent(palette Palette) {
	if palette == nil {
		return
	}
	typed := strings.ToLower(strings.TrimSpace(s.PrimaryColor))
	if typed == "" {
		return
	}
	if typed == strings.ToLower(palette.Accent(ThemeLight)) || typed == strings.ToLower(palette.Accent(ThemeDark)) {
		s.PrimaryColor = DefaultPrimaryColor
	}
}
