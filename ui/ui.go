// Package ui is the browser half of the application: one stylesheet and four
// controllers, served as static files beside the API.
//
// # The stylesheet is a Go value
//
// There is no CSS build. ui/components declares every class list it renders
// with; ui/style resolves exactly those classes to rules; design supplies the
// tokens they are written in terms of. Stylesheet composes the three, once, at
// the first request, and the result is a byte slice.
//
// That is the whole reason for the Go-side emission: a Tailwind build would
// need node, a config file, a content glob that scans Go source for strings
// that look like classes, and an artifact that is either committed (which the
// rules here forbid) or generated in CI (which means the repository does not
// build a working application on its own). None of that exists, and a component
// that is deleted takes its CSS with it because nothing declares its list any
// more.
//
// # There is no framework
//
// htmx is vendored, minified, under its MIT licence, and it is the only
// third-party byte the browser runs. Everything else is four controllers of a
// few dozen lines each, in assets/js, and they are the ones listed in
// Controllers.
package ui

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/css"
	"github.com/septagon-oss/platformkit/ui/style"
)

//go:embed assets/js/*.js
var scripts embed.FS

// Controllers are the browser scripts a page loads, in order. The list is here
// rather than in the shell that writes the <script> tags, so that "how much
// JavaScript is there" is answered by reading one slice.
//
// htmx is first because the others configure it. There are four of ours and
// they are the four interactions a server-rendered application cannot express:
// a theme that must survive a reload, a validation error that must not cost a
// page, a destructive action that must be confirmed, and a sign-in form that
// posts to a JSON route.
var Controllers = []string{
	"htmx.min.js",
	"htmx-config.js",
	"theme.js",
	"confirm.js",
	"session.js",
}

// There are two stylesheets, and the split is about what a page downloads
// rather than about how the CSS is written. app.css is the tokens, the role
// variables, the base layer and one rule per class an application's own pages
// can emit. gallery.css is the rest: the components only /admin/_gallery
// renders — a divider, an empty state, the skeletons, the modal and the tabs —
// which was a third of the bytes on every page of an application that renders
// none of them.
//
// Both render once. The composition is deterministic, so the second caller gets
// the same bytes as the first and the fingerprints are stable for the life of
// the process.
// sheets memoises one app.css per palette. A Pair is comparable and there is
// one of them in an ordinary application, so this is a map with a single entry;
// it is a map rather than a package-level value because whose colours these are
// is the caller's business, and a client with its own is not a second build.
var sheets sync.Map // design.Pair -> composed

type composed struct {
	body        []byte
	fingerprint string
}

func stylesheet(theme design.Pair) composed {
	if hit, ok := sheets.Load(theme); ok {
		return hit.(composed)
	}
	sheet := css.NewSheet()
	sheet.Merge(design.CSS(theme.Light, theme.Dark))
	sheet.Merge(style.RoleVars())
	sheet.Merge(base())
	sheet.Merge(rules(components.ShellClassLists()))
	body, fingerprint := fingerprinted(sheet)
	out := composed{body: body, fingerprint: fingerprint}
	sheets.Store(theme, out)
	return out
}

// galleryStylesheet is the second sheet: the rules for the classes only the
// gallery's own components emit and app.css therefore does not carry. It is the
// difference and not the whole set — the two share most of the utility
// alphabet, and a second copy of `.flex` would make the pair bigger than the
// one sheet it replaced.
//
// It carries no tokens, no roles and no base layer: it is loaded beside the
// first, never instead of it, and it is the same bytes whatever the palette,
// because a utility rule is written in terms of a role and a role is written in
// terms of a token. That is why it needs no Pair.
var galleryStylesheet = sync.OnceValues(func() ([]byte, string) {
	shell := emitted(components.ShellClassLists())
	var only []string
	for _, name := range emitted(components.GalleryClassLists()) {
		if !slices.Contains(shell, name) {
			only = append(only, name)
		}
	}
	rules, err := style.Rules(only...)
	if err != nil {
		panic("ui: the gallery declares a class the style engine cannot render: " + err.Error())
	}
	return fingerprinted(css.NewSheet().Merge(rules))
})

// emitted is the sorted set of class names a set of lists compiles to.
func emitted(lists []style.ClassList) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range lists {
		for _, name := range strings.Fields(list.Compile()) {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// rules resolves a set of class lists, or panics. A class a component declares
// that ui/style cannot resolve is a stylesheet with a hole in it, and it is a
// fact about this build rather than about this request: the class lists are
// package-level values. ui/components' own test asserts the same thing, so
// reaching this means the test was deleted.
func rules(lists []style.ClassList) *css.Sheet {
	out, err := style.For(lists...)
	if err != nil {
		panic("ui: the components declare a class the style engine cannot render: " + err.Error())
	}
	return out
}

func fingerprinted(sheet *css.Sheet) ([]byte, string) {
	out := []byte(sheet.CSS())
	sum := sha256.Sum256(out)
	return out, hex.EncodeToString(sum[:8])
}

// Stylesheet is every page's stylesheet, in one palette: that palette's tokens,
// the role variables, the small base layer, and one rule per class an
// application's own pages declare. Only the first of those four depends on the
// argument.
func Stylesheet(theme design.Pair) []byte { return stylesheet(theme).body }

// GalleryStylesheet is the second sheet, linked by the gallery page alone.
func GalleryStylesheet() []byte { b, _ := galleryStylesheet(); return b }

// Fingerprint is the first eight bytes of the stylesheet's SHA-256, as hex. The
// shell puts it in the asset's query string, so a deploy that changes a
// component — or a client that changes a colour — changes the URL and a browser
// that cached the old one asks again.
func Fingerprint(theme design.Pair) string { return stylesheet(theme).fingerprint }

// GalleryFingerprint is the same, for the second sheet.
func GalleryFingerprint() string { _, f := galleryStylesheet(); return f }

// Assets is the tree served under the shell's asset prefix: the two stylesheets
// and the controllers at js/. It is built per call because it is called once,
// at mount.
func Assets(theme design.Pair) fs.FS {
	js, err := fs.Sub(scripts, "assets")
	if err != nil {
		panic("ui: the embedded scripts are not where they were embedded: " + err.Error())
	}
	return overlay{under: js, files: map[string][]byte{
		"app.css":     Stylesheet(theme),
		"gallery.css": GalleryStylesheet(),
	}}
}

// base is the small layer no utility can express: the document's own box model,
// margins, colours and type. Everything else in the stylesheet is a utility a
// component asked for; these eleven rules are the page itself.
//
// It is written here rather than as a vendored reset, because a reset is a
// thousand lines of undoing decisions browsers stopped making a decade ago.
func base() *css.Sheet {
	s := css.NewSheet()
	v := func(name string) css.Value { return css.VarRef(name, "") }
	s.Select("*, *::before, *::after", css.Decl("box-sizing", css.Literal("border-box")))
	s.Select("html",
		css.Decl("-webkit-text-size-adjust", css.Literal("100%")),
		css.Decl("font-family", v("pk-font-body")),
		css.Decl("line-height", css.Literal("1.5")))
	s.Select("body",
		css.Decl("margin", css.Literal("0")),
		css.Decl("min-height", css.Literal("100vh")),
		css.Decl("background-color", v("pk-color-surface-canvas")),
		css.Decl("color", v("pk-color-text-primary")),
		css.Decl("-moz-osx-font-smoothing", css.Literal("grayscale")),
		css.Decl("-webkit-font-smoothing", css.Literal("antialiased")))
	s.Select("h1, h2, h3, h4, h5, h6",
		css.Decl("font-family", v("pk-font-display")),
		css.Decl("margin", css.Literal("0")),
		css.Decl("line-height", css.Literal("1.2")))
	s.Select("p, figure, blockquote, dl, dd", css.Decl("margin", css.Literal("0")))
	s.Select("code, pre, kbd, samp", css.Decl("font-family", v("pk-font-mono")))
	s.Select("button, input, select, textarea",
		css.Decl("font", css.Literal("inherit")),
		css.Decl("color", css.Literal("inherit")))
	// A button with no background of its own gets the browser's, and the
	// browser's under `color-scheme: dark` is a mid grey nothing here was
	// designed against: the tabs' labels failed contrast in the dark theme
	// against #6b6b6b and passed in the light one against a near-white, which
	// is how a defect hides. Every button this application renders declares its
	// own surface, so the default is no surface at all.
	s.Select("button", css.Decl("background-color", css.Literal("transparent")))
	s.Select("table", css.Decl("border-collapse", css.Literal("collapse")))
	// A navigation list is not a bulleted list. The marker inherits the
	// document's text colour rather than the link's, so on the inverted sidebar
	// it was invisible in the light theme and a row of dots in the dark one —
	// which is how a defect ships: it looked right in the theme it was built in.
	s.Select("nav ul, nav ol",
		css.Decl("list-style", css.Literal("none")),
		css.Decl("margin", css.Literal("0")),
		css.Decl("padding", css.Literal("0")))
	s.Select("a", css.Decl("color", css.Literal("inherit")), css.Decl("text-decoration", css.Literal("none")))
	s.Select("img, svg", css.Decl("display", css.Literal("block")), css.Decl("max-width", css.Literal("100%")))
	s.Select("dialog::backdrop", css.Decl("background", css.Literal("rgb(0 0 0 / 0.45)")))
	s.Media("(prefers-reduced-motion: reduce)", func(inner *css.Sheet) {
		inner.Select("*, *::before, *::after",
			css.Decl("animation-duration", css.Literal("0.01ms")),
			css.Decl("animation-iteration-count", css.Literal("1")),
			css.Decl("transition-duration", css.Literal("0.01ms")),
			css.Decl("scroll-behavior", css.Literal("auto")))
	})
	return s
}

// overlay is the embedded tree with the computed files added: the stylesheets,
// which have no source to embed because they are composed. It is fifteen lines
// rather than a build step that writes files the repository would then have to
// ignore.
type overlay struct {
	under fs.FS
	files map[string][]byte
}

func (o overlay) Open(name string) (fs.File, error) {
	if body, ok := o.files[name]; ok {
		return &memFile{name: name, body: body}, nil
	}
	return o.under.Open(name)
}
