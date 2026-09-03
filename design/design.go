// Package design is the application's colours and type, as the two themes it
// ships and the custom properties they render to.
//
// A theme is a struct of named roles, not a token graph. The previous design
// system expressed the same twenty-three values as a DTCG document with a
// parser, a resolver, an alias graph, nine layer kinds and a validator — 2,200
// lines to answer "what colour is the accent". This is the answer: a struct
// literal per theme, rendered to `--pk-color-*` custom properties that
// ui/style's semantic roles are written in terms of.
//
// That indirection is the whole point of having themes at all. A component
// names a role (`style.SurfaceBrand`); the role is a `--pk-role-*` property
// defined once in terms of a token; the token is what a theme sets. So the
// utility rules are theme-independent, and switching the theme is one attribute
// on <html> rather than a second stylesheet.
//
// Derived from github.com/septagon-oss/pk-design (Apache-2.0); see NOTICE. The
// palette is that project's canonical theme: warm editorial surfaces, ink text,
// a deep green accent.
package design

import (
	"github.com/septagon-oss/platformkit/ui/css"
)

// Theme is one palette. Every field is a CSS colour, and every one of them is
// read: ui/style's role table names each token exactly once, and its test
// fails when a role has no token behind it. A field nothing reads would be a
// value that goes stale without anybody noticing, so there are no spares.
type Theme struct {
	// Name is what the data-theme attribute carries, and the key Themes uses.
	Name string

	// The three surfaces, from the page behind everything to the raised card.
	SurfaceCanvas  string
	SurfacePrimary string
	SurfaceMuted   string

	// Text, in the two weights a screen distinguishes: what is being said, and
	// what is labelling it.
	TextPrimary string
	TextMuted   string

	// Borders: the line between two surfaces, and the one that has to be seen.
	BorderDefault string
	BorderStrong  string

	// The accent is the application's one colour: every primary button, every
	// link, every focused control. AccentOn is what is legible on top of it.
	AccentDefault string
	AccentHover   string
	AccentOn      string

	// Focus is the ring, and it is deliberately not the accent: a focus ring
	// that matches the button it is around is a focus ring nobody can see.
	Focus string

	// The four statuses, each with the surface a soft badge sits on.
	StatusOK        string
	StatusOKBg      string
	StatusWarning   string
	StatusWarningBg string
	StatusDanger    string
	StatusDangerBg  string
	StatusInfo      string
	StatusInfoBg    string

	// The sidebar is its own small palette, because it is inverted in the light
	// theme and is not simply the canvas in the dark one.
	SidebarBg   string
	SidebarText string
	SidebarMute string
}

// The three faces. They are one set for both themes: a theme changes colour,
// and a typeface that changed with the colour scheme would be a different
// product in the dark.
const (
	FontDisplay = `"Iowan Old Style", "Palatino Linotype", Palatino, Georgia, serif`
	FontBody    = `"IBM Plex Sans", Aptos, "Helvetica Neue", sans-serif`
	FontMono    = `"IBM Plex Mono", "SFMono-Regular", Consolas, monospace`
)

// Light is the default theme.
func Light() Theme {
	return Theme{
		Name:            "light",
		SurfaceCanvas:   "#f2efe7",
		SurfacePrimary:  "#fffdf7",
		SurfaceMuted:    "#e9e4d8",
		TextPrimary:     "#15221f",
		TextMuted:       "#5f6b65",
		BorderDefault:   "#cbc5b8",
		BorderStrong:    "#8f988f",
		AccentDefault:   "#0f5d4e",
		AccentHover:     "#0a493e",
		AccentOn:        "#f9fff9",
		Focus:           "#326de6",
		StatusOK:        "#12715d",
		StatusOKBg:      "#dcf3e8",
		StatusWarning:   "#9a5318",
		StatusWarningBg: "#fff0d2",
		StatusDanger:    "#9e3833",
		StatusDangerBg:  "#fbe5e2",
		StatusInfo:      "#2455c4",
		StatusInfoBg:    "#e5ecfa",
		SidebarBg:       "#12201d",
		SidebarText:     "#eff4e9",
		SidebarMute:     "#aebbb2",
	}
}

// Dark is the same product at night: the same hues, with the surfaces and the
// text exchanged and the accent lifted, because a deep green that reads as
// authority on paper reads as absent on ink.
func Dark() Theme {
	return Theme{
		Name:            "dark",
		SurfaceCanvas:   "#0e1614",
		SurfacePrimary:  "#151f1d",
		SurfaceMuted:    "#1e2a27",
		TextPrimary:     "#eef3ec",
		TextMuted:       "#a3b0aa",
		BorderDefault:   "#2c3b37",
		BorderStrong:    "#5c6d67",
		AccentDefault:   "#4bbf9c",
		AccentHover:     "#68d3b2",
		AccentOn:        "#08201a",
		Focus:           "#7aa2ff",
		StatusOK:        "#5fd0ac",
		StatusOKBg:      "#12312a",
		StatusWarning:   "#e0a955",
		StatusWarningBg: "#33240f",
		StatusDanger:    "#f0918a",
		StatusDangerBg:  "#3a1d1b",
		StatusInfo:      "#8fb0f5",
		StatusInfoBg:    "#18244a",
		SidebarBg:       "#0a1210",
		SidebarText:     "#eff4e9",
		SidebarMute:     "#8b9a93",
	}
}

// Pair is the two palettes one installation ships: what its pages look like in
// the light and what they look like in the dark. It is a value rather than a
// package-level pair of functions because a client's own colours are the one
// thing about this design system that is theirs — everything above the tokens
// (the roles, the utilities, the components, the class lists, the stylesheet's
// shape) is unchanged by them, which is the whole point of the role
// indirection. Supplying a Pair is the entire seam: there is no second
// stylesheet, no override layer and no build step.
//
// It is comparable, so ui memoises one stylesheet per Pair rather than
// recomposing on every request.
type Pair struct {
	Light, Dark Theme
}

// Default is the palette this repository ships, and what an application that
// says nothing about colour gets.
func Default() Pair { return Pair{Light: Light(), Dark: Dark()} }

// Both is the pair in the order a picker lists them. The first is the default.
func (p Pair) Both() []Theme { return []Theme{p.Light, p.Dark} }

// tokens is the theme as custom properties, in the order they are declared
// above. It is one list rather than reflection so that the property name and
// the field are next to each other and a rename is one edit.
func (t Theme) tokens() []css.Declaration {
	pairs := [...][2]string{
		{"surface-canvas", t.SurfaceCanvas},
		{"surface-primary", t.SurfacePrimary},
		{"surface-muted", t.SurfaceMuted},
		{"text-primary", t.TextPrimary},
		{"text-muted", t.TextMuted},
		{"border-default", t.BorderDefault},
		{"border-strong", t.BorderStrong},
		{"accent-default", t.AccentDefault},
		{"accent-hover", t.AccentHover},
		{"accent-on", t.AccentOn},
		{"focus", t.Focus},
		{"status-ok", t.StatusOK},
		{"status-okbg", t.StatusOKBg},
		{"status-warning", t.StatusWarning},
		{"status-warningbg", t.StatusWarningBg},
		{"status-danger", t.StatusDanger},
		{"status-dangerbg", t.StatusDangerBg},
		{"status-info", t.StatusInfo},
		{"status-infobg", t.StatusInfoBg},
		{"sidebar-bg", t.SidebarBg},
		{"sidebar-text", t.SidebarText},
		{"sidebar-muted", t.SidebarMute},
	}
	out := make([]css.Declaration, 0, len(pairs)+3)
	for _, p := range pairs {
		out = append(out, css.Decl("--pk-color-"+p[0], css.Literal(p[1])))
	}
	return out
}

// CSS is the token layer of the stylesheet: the light theme on :root, the dark
// one under [data-theme="dark"], and the dark one again under a
// prefers-color-scheme query for a browser whose owner has said what they want
// and an application that has not been told otherwise.
//
// The attribute wins over the query, which is the ordering a theme toggle
// needs: setting data-theme="light" on a machine in dark mode has to mean
// light. That is why the media block is qualified by :root:not([data-theme]).
//
// It takes the two themes rather than reading the two above, so that a client
// with its own colours changes this one argument and nothing else. See Pair.
func CSS(light, dark Theme) *css.Sheet {
	s := css.NewSheet()
	s.Select(":root", append(light.tokens(),
		css.Decl("--pk-font-display", css.Literal(FontDisplay)),
		css.Decl("--pk-font-body", css.Literal(FontBody)),
		css.Decl("--pk-font-mono", css.Literal(FontMono)),
		css.Decl("color-scheme", css.Literal("light")),
	)...)
	s.Select(`[data-theme="dark"]`, append(dark.tokens(),
		css.Decl("color-scheme", css.Literal("dark")),
	)...)
	s.Media("(prefers-color-scheme: dark)", func(inner *css.Sheet) {
		inner.Select(`:root:not([data-theme])`, append(dark.tokens(),
			css.Decl("color-scheme", css.Literal("dark")),
		)...)
	})
	return s
}
