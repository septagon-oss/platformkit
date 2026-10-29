// Package ui composes fingerprinted stylesheets and serves browser assets.
// Compose places tokens, base rules, components and client rules in ordered
// cascade layers. Shells retain the returned Sheet for their lifetime.
// Controllers and optional editor assets are embedded; design tooling lives
// in ui/export. See ui/README.md for composition and client-sheet admission.
package ui

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/css"
	"github.com/septagon-oss/platformkit/ui/style"
)

//go:embed assets/js/*.js assets/css/*.css assets/richtext/*.js
var scripts embed.FS

// Controllers are the browser scripts a page loads, in order. The list is here
// rather than in the shell that writes the <script> tags, so that "how much
// JavaScript is there" is answered by reading one slice.
//
// htmx is first because the others configure it. The remaining scripts enhance
// themes, requests, confirmation, sign-in, passkey ceremonies, component
// interaction (including the reader's own time) and gallery controls.
var Controllers = []string{
	"htmx.min.js",
	"htmx-config.js",
	"theme.js",
	"confirm.js",
	"session.js",
	"passkeys.js",
	"components.js",
	"specialists.js",
	"gallery.js",
}

// SSEExtension is the htmx extension that owns a live region's EventSource,
// named for the version it is pinned at in the way every other third-party
// engine in this directory is. It is not in Controllers: those are the scripts
// every page downloads, and this one belongs to the pages that name topics,
// which load it the way a page with a rich text field loads its editor —
// through document.View.Modules' own row, rendered by ui/document's head.
//
// NOTICE pins the bytes by SHA-256 beside the licence that covers them, and a
// test re-reads the digest from the embedded tree, so an edit to the file fails
// the record here rather than a person's browser.
const SSEExtension = "htmx-ext-sse-2.2.4.min.js"

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
// own markup renders with, resolved by ui/style exactly as the components' are
// — which is why they compile into the components layer, shared utilities and
// all — and the rules no class can express, which Compose places in the client
// layer. An attribute selector for a client's grain, a keyframe for an
// animation: a consumer's rules are read, refused or placed by Compose; see
// the refusals on refuseClientSheet.
type Extra struct {
	Lists  []style.ClassList
	Sheets []*css.Sheet
}

// The four cascade layers, in the order Compose declares them.
const (
	layerTokens     = "tokens"
	layerBase       = "base"
	layerComponents = "components"
	layerClient     = "client"
)

// Compose is every page's stylesheet in one palette: that palette's tokens,
// the role variables, the small base layer, one rule per class the components
// and the extras declare, and then the extras' own rules. It is a pure function
// of its arguments. Call it once and keep the value.
//
// The result is four cascade layers in the declared order tokens, base,
// components, client: the palette and roles in the first, the preflight in the
// second, the kernel's own component-state rules and every resolved class list
// in the third — the components' and the extras' together, which is what gives a
// shared utility one rule — and the extras' hand-written sheets in the fourth.
// A layer ranks before specificity, so a rule's layer is decided by what it must
// still win: see base and componentState. A consumer gets one layer it may write
// and no way to name a kernel hook; see refuseClientSheet.
func Compose(theme design.Pair, extra ...Extra) Sheet {
	return place(theme, true, extra...)
}

// ComposeDesign composes and fingerprints the same layers as Compose,
// without client-sheet admission checks. It is for design snapshots that must
// measure proposed CSS; served application sheets use Compose.
func ComposeDesign(theme design.Pair, extra ...Extra) Sheet {
	return place(theme, false, extra...)
}

// place assembles the sheet for both callers; refuseClient is what they ask for.
func place(theme design.Pair, refuseClient bool, extra ...Extra) Sheet {
	sheet := css.NewSheet()
	sheet.LayerOrder(layerTokens, layerBase, layerComponents, layerClient)
	sheet.Layer(layerTokens, func(t *css.Sheet) {
		t.Merge(style.ThemeVars(theme.Light, theme.Dark))
		t.Merge(style.RoleVars())
	})
	sheet.Layer(layerBase, func(b *css.Sheet) { b.Merge(base()) })
	lists := slices.Clone(components.ShellClassLists())
	for _, e := range extra {
		lists = append(lists, e.Lists...)
	}
	classes := composedClasses(lists)
	sheet.Layer(layerComponents, func(c *css.Sheet) {
		c.Merge(componentState())
		c.Merge(rules(lists))
	})
	sheet.Layer(layerClient, func(cl *css.Sheet) {
		for _, e := range extra {
			for _, s := range e.Sheets {
				if refuseClient {
					refuseClientSheet(s, classes)
				}
				cl.Merge(s)
			}
		}
	})
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
	return fingerprinted(css.NewSheet().Layer(layerComponents, func(c *css.Sheet) { c.Merge(rules) }))
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

// composedListSource is the attribution a class the composition's own lists carry
// gets in the refusal: the sheet says which layer ranks above the rule, and no
// file, because the list arrived through Extra from a module this package does not
// know.
const composedListSource = "a class list this composition carries in Extra.Lists"

// composedClasses maps component-layer class names to their declaration
// owners. Extra.Lists share that layer and therefore receive the same protection
// from client selectors as the kernel's classes.
func composedClasses(lists []style.ClassList) map[string]string {
	kernel := kernelClasses()
	out := make(map[string]string, len(kernel)+len(lists))
	for name, source := range kernel {
		out[name] = source
	}
	for _, name := range emitted(lists) {
		if _, listed := out[name]; !listed {
			out[name] = composedListSource
		}
	}
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
