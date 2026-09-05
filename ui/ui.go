// Package ui is the browser half of the application: one stylesheet and four
// controllers, served as static files beside the API.
//
// # The stylesheet is a Go value
//
// There is no CSS build. ui/components declares every class list it renders
// with; ui/style resolves exactly those classes to rules; design supplies the
// tokens they are written in terms of. Compose folds the three, plus whatever a
// consumer adds, into one Sheet, and a Sheet is bytes and their fingerprint.
//
// A consumer calls Compose once, at mount, and carries the Sheet in its chrome.
// Nothing is memoised here because nothing is called twice: the value is the
// cache. That is also why a second consumer needs no machinery of its own — the
// first client storefront copied a memo, an in-memory file and an overlay tree
// to add fourteen class lists, and appended its resolution to the kernel's
// bytes, so a shared utility had two rules.
//
// # There is no framework
//
// htmx is vendored, minified, under its MIT licence, and it is the only
// third-party byte the browser runs. Everything else is the controllers in
// assets/js, listed in Controllers.
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

// Sheet is a composed stylesheet: the bytes a browser downloads and the first
// eight bytes of their SHA-256 as hex. A page puts the fingerprint in the
// asset's query string, so a deploy that changes a rule — or a client that
// changes a colour — changes the URL and a browser that cached the old one asks
// again.
type Sheet struct {
	Body        []byte
	Fingerprint string
}

// Extra is what one consumer adds to the kernel's sheet: the class lists its
// own markup renders with, resolved by ui/style exactly as the components' are,
// and the rules no class can express — an attribute selector for a client's
// grain, a keyframe for an animation.
type Extra struct {
	Lists  []style.ClassList
	Sheets []*css.Sheet
}

// Compose is every page's stylesheet in one palette: that palette's tokens,
// the role variables, the small base layer, one rule per class the components
// and the extras declare, and then the extras' own rules. It is a pure function
// of its arguments. Call it once and keep the value.
//
// The components' lists and the extras' lists are resolved together, which is
// what gives a shared utility one rule: a consumer that appended its own
// resolution to the kernel's bytes emitted .flex twice.
func Compose(theme design.Pair, extra ...Extra) Sheet {
	sheet := css.NewSheet()
	sheet.Merge(design.CSS(theme.Light, theme.Dark))
	sheet.Merge(style.RoleVars())
	sheet.Merge(base())
	lists := slices.Clone(components.ShellClassLists())
	for _, e := range extra {
		lists = append(lists, e.Lists...)
	}
	sheet.Merge(rules(lists))
	for _, e := range extra {
		for _, s := range e.Sheets {
			sheet.Merge(s)
		}
	}
	return fingerprinted(sheet)
}

// Gallery is the second sheet: the rules for the classes only the gallery's own
// components emit and app.css therefore does not carry. It is the difference
// and not the whole set — the two share most of the utility alphabet, and a
// second copy of .flex would make the pair bigger than the one sheet it
// replaced.
//
// It carries no tokens, no roles and no base layer: it is loaded beside
// app.css, never instead of it, and it is the same bytes whatever the palette,
// because a utility rule is written in terms of a role and a role is written in
// terms of a token. That is why it takes no theme, and why it is computed once.
var Gallery = sync.OnceValue(func() Sheet {
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

// Assets is the tree a shell serves under its asset prefix: app.css is the
// sheet it composed, gallery.css is Gallery, js/ is Controllers, and then any
// trees the consumer adds — its own scripts — looked up in order after the
// computed files and before the kernel's.
func Assets(app Sheet, more ...fs.FS) fs.FS {
	js, err := fs.Sub(scripts, "assets")
	if err != nil {
		panic("ui: the embedded scripts are not where they were embedded: " + err.Error())
	}
	return overlay{
		files:  map[string][]byte{"app.css": app.Body, "gallery.css": Gallery().Body},
		layers: append(slices.Clone(more), js),
	}
}

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
		panic("ui: a declared class cannot be rendered by the style engine: " + err.Error())
	}
	return out
}

func fingerprinted(sheet *css.Sheet) Sheet {
	out := []byte(sheet.CSS())
	sum := sha256.Sum256(out)
	return Sheet{Body: out, Fingerprint: hex.EncodeToString(sum[:8])}
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

// overlay is computed files in front of a stack of trees: the sheets, which
// have no source to embed because they are composed, then whatever a consumer
// adds, then the kernel's scripts. It is a dozen lines rather than a build step
// that writes files the repository would then have to ignore.
type overlay struct {
	files  map[string][]byte
	layers []fs.FS
}

func (o overlay) Open(name string) (fs.File, error) {
	if body, ok := o.files[name]; ok {
		return &memFile{name: name, body: body}, nil
	}
	for _, layer := range o.layers {
		if f, err := layer.Open(name); err == nil {
			return f, nil
		}
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}
