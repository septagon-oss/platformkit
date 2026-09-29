// Package ui is the browser half of the application: one stylesheet and small browser
// controllers, served as static files beside the API.
//
// # The stylesheet is a Go value
//
// There is no CSS build. ui/components declares every class list it renders
// with; ui/style resolves exactly those classes to rules; design supplies the
// tokens they are written in terms of. Compose folds the three, plus whatever a
// consumer adds, into one Sheet, and a Sheet is bytes and their fingerprint.
//
// The Sheet has four cascade layers — tokens, base, components, client — and
// the order statement Compose emits is what says so. Precedence by file order
// is a rumour: a consumer appended to the kernel's bytes beat the kernel for
// one reason (it was later) and lost for one reason (a selector), and nobody
// could read which applied. With layers a consumer's rules are in the one
// layer they may write, the order the kernel ranks in is the order statement,
// and a !important in the client layer still loses to a !important in a kernel
// layer, which is the only direction layers reverse.
//
// A layer ranks before specificity and specificity never crosses a layer, so the
// layer a rule goes in is decided by what it must still win. Every rule Compose
// emits is in a layer — an unlayered rule beats every layer and would undo the
// order statement — and a rule about one of the kernel's own components shares
// the components layer with the utilities on that component, where its selector
// still decides the tie; see base and componentState.
//
// A consumer calls Compose once, at mount, and carries the Sheet in its chrome.
// Nothing is memoised here because nothing is called twice: the value is the
// cache. That is also why a second consumer needs no machinery of its own — the
// first client storefront copied a memo, an in-memory file and an overlay tree
// to add fourteen class lists, and appended its resolution to the kernel's
// bytes, so a shared utility had two rules.
//
// # Application controllers
//
// htmx is vendored, minified, under its MIT licence, and it is the only
// third-party script in application pages. The optional Storybook.js development
// interface is built separately under ui/storybook and served through authorization.
// The application enhancements live in assets/js, listed in Controllers.
//
// # What is not here
//
// Design snapshots, token export, proposals and the storybook composition are
// ui/export. This package is what a shell needs to serve a page and nothing a
// design tool needs, so ui/page depends on it without linking the tooling.
package ui

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"regexp"
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
// htmx is first because the others configure it. The remaining scripts enhance
// themes, requests, confirmation, sign-in, component interaction and gallery controls.
var Controllers = []string{
	"htmx.min.js",
	"htmx-config.js",
	"theme.js",
	"confirm.js",
	"session.js",
	"components.js",
	"gallery.js",
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

// rawColourRE matches the ways a rule says a colour without naming a token: a
// hex triplet or an rgb()/hsl() call. A named colour ("red") is words a review
// catches, not a pattern a gate can tell from a keyword.
var rawColourRE = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|\b(?:rgb|hsl)a?\(`)

// boundaryRE matches what ends a block, or starts a comment that swallows one,
// inside text Compose writes out unchanged. The emitter owns those boundaries: a
// rule is `selector { property: value; }` and the braces are not the consumer's
// to supply. Text that carries one stops being a declaration and becomes the
// boundary a browser acts on, so every byte after it is emitted outside the block
// the emitter opened — outside @layer client, unlayered, and therefore above every
// layer the order statement names. css.VarRef refuses the same characters in a
// fallback for the same reason and css.Literal validates nothing, so the gate
// reads the emitted text rather than the field it was handed.
var boundaryRE = regexp.MustCompile(`[{}]|/\*`)

// styleCloseRE matches the one byte sequence that ends a <style> element, in the
// text Compose writes out unchanged. The sheet's own boundaries are the previous
// regex's; this one is the boundary of the element a sheet is placed in, and the
// kernel does place one there: modules/admin renders the composed sheet as
// h.StyleEl(g.Raw(sheet.Body)) for the gallery preview, and g.Raw writes bytes as
// written — the same reason the tenant accent beside it carries an anchored hex
// pattern. An HTML parser reads a style element's content as text and ends it at
// `</style`, case-insensitively, whatever the CSS around it says, so client text
// that carries the sequence does not leave the block it sits in: it leaves the
// stylesheet, and the bytes after it are parsed as markup in a page a signed-in
// reader opens. Tag matching ignores case, so the read does too; the tag ends the
// same way at `>`, a space or a `/`, all three of which the prefix already catches.
var styleCloseRE = regexp.MustCompile(`(?i)</style`)

// customDeclRE matches a name in the kernel's own custom-property namespace in
// the position a declaration occupies. It reads the prefix, not a name pattern:
// a browser ends a declaration at `;`, so whatever follows one in the emitted
// text occupies that position whatever characters its author typed, and the
// namespace is the namespace. A var(--pk-…) read is not a declaration and stays
// legal: reading the palette by name is what a client layer is for.
var customDeclRE = regexp.MustCompile(`^\s*--pk-`)

// atRuleHead returns the at-rule a selector list begins with, or "" when it
// begins with a selector. `@` is what makes an at-keyword token and a block opens
// wherever a browser reads one: Rule.CSS writes the selector into the position
// ahead of the `{` the emitter supplies, so text that tokenises as an at-keyword
// there stops naming a rule's subject and names the block a browser is then
// reading — a sheet whose rule says `@layer tokens` states a layer of its own
// inside the client layer Compose placed it in, and boundaryRE cannot see it
// because the text carries no brace. A selector may hold `@` where a token
// already carries one: an attribute value ([data-email="a@b.com"]) is inside a
// bracket and a quoted string, so the scan steps over both. An escaped `@` is
// reached by no scan and needs none — `\40` makes an ident token, never an
// at-keyword, so a `\` ahead of the `@` means the browser reads a selector.
//
// The read is made at a head and nowhere else, because a head is where a rule's
// kind is decided: a browser reads that kind from the first token — `{` opens a
// style rule, an at-keyword opens an at-rule, anything else starts a qualified
// rule whose prelude must then parse as a selector list — and the emitter supplies
// every brace and every rule-ending `;` around consumer text, so the positions
// sheet.Heads visits are exactly the positions where consumer text can begin a rule.
// That is the scope, stated rather than implied. A head like
// `.store-hero @layer base` is accepted: the rule begins with `.`, so the
// at-keyword inside the prelude only makes a selector list the browser discards —
// the consumer loses its own rule to its own typo, and nothing leaves the layer.
// A declaration's text is the other position a `;` reaches, and it is read part by
// part below; an at-rule stated there needs a `{` to nest, which boundaryRE
// refuses, so it states no block either.
func atRuleHead(selector string) string {
	for at := 0; ; {
		if at = skipSpace(selector, at); at >= len(selector) {
			return ""
		}
		if selector[at] == '@' {
			name, _ := readIdent(selector, at+1) // the name the browser resolves escapes in
			return "@" + name
		}
		for ; at < len(selector) && selector[at] != ','; at++ {
			switch selector[at] {
			case '[':
				at = endOfBracket(selector, at) - 1 // the loop's at++ steps past the ]
			case '"', '\'':
				at = skipQuoted(selector, at) - 1
			}
		}
		at++ // past the comma: the next part of the list begins its own selector
	}
}

// unbraced reports whether text carries c outside a bracketed attribute or a
// quoted value. attrNames and atRuleHead step over those two for the same reason:
// inside one the byte is character data in a token the browser has already
// opened, and outside one it is the syntax a browser acts on — [data-email="a;b"]
// keeps its semicolon and the one after it ends the rule.
func unbraced(text string, c byte) bool {
	for at := 0; at < len(text); at++ {
		switch text[at] {
		case c:
			return true
		case '[':
			at = endOfBracket(text, at) - 1
		case '"', '\'':
			at = skipQuoted(text, at) - 1
		}
	}
	return false
}

// refuseClientSheet is the rule a consumer sheet lives under: no @layer of its
// own (Compose places it), no text that would end the block or the declaration
// Compose is writing around it (boundaryRE, the escape a browser acts on and a
// field-by-field read of the rule would not see), no text that would end the
// <style> element the sheet is rendered inside (styleCloseRE, the same escape one
// level up, out of the stylesheet rather than out of a rule), no selector that is an at-rule
// prelude rather than a selector (atRuleHead: the text that decides which block
// the rule lands in, invisible to a brace read because the emitter supplies the
// brace), no attribute name the kernel renders, in whichever of the several
// spellings a browser resolves to the same name (see attrNames and kernelHooks),
// and no :root (that markup is the kernel's) and no raw colour or --pk- property
// (the palette is named in one place). It reads every declaration the sheet
// carries, keyframe stops included, because the browser applies those too, and it
// reads each declaration's emitted text — property and value together — in the
// parts a browser splits it into, because the emitter writes `property: value;`
// and a semicolon in either field makes the text after it a second declaration
// with a property of its own, which is where a --pk- name and a raw colour hide
// from a read of the value field alone. The head read (atRuleHead, unbraced) is
// made at every position the emitter writes ahead of a brace it supplies — each
// selector, each keyframe offset, each @media query and @keyframes name — because
// a `;` there ends the rule the emitter is writing and hands the text after it to
// the browser as the head of the next one, so `.store-card; @layer base` states a
// layer of its own and a @media prelude that says `all; @layer tokens` states two
// at-rules where the emitter wrote one; a prelude merely unparseable, with no `;`
// in it, invalidates its own at-rule and the browser discards the block it carries
// where that block sits inside @layer client. Neither lets a rule leave its layer —
// a nested @layer inside client is the sublayer client.tokens, ranked inside the
// client subtree — and the read is made anyway because the sheet would carry an
// at-rule the first sentence says Compose places. It panics for the reason VarRef
// panics: a consumer sheet is Go source wired at mount, so a violation is a
// build-time fact and the refused composition ships no bytes.
//
// The attribute refusal reads names, not reachability: a selector that mentions
// a kernel word anywhere is refused, the consumer's own class beside it included
// — .store-card[data-state=featured] is refused although only the consumer's
// element carries .store-card. That is the scope, chosen. The kernel's attribute
// vocabulary is one namespace, and "could this selector ever match a kernel
// element" is a question only a browser's selector engine answers; answering it
// here would mean shipping one, and a gate that answers a weaker question than
// the promise would quietly refuse less than it claims. A consumer that styled
// its own element with an ordinary word the kernel happens to use renames its own
// hook, which the message says; ui_test.go pins the refusal, so widening or
// narrowing it is a decision somebody reads rather than a slip.
func refuseClientSheet(sheet *css.Sheet) {
	if sheet.UsesLayers() {
		panic("ui: a client sheet declares its own @layer; Compose places every client rule in the client layer")
	}
	if err := sheet.Verbatim(func(text string) error {
		if close := styleCloseRE.FindString(text); close != "" {
			return fmt.Errorf("ui: a client rule carries %q inside %.120q: the kernel renders the composed sheet as the content of a <style> element, whose text an HTML parser ends at %s, so text carrying it ends the stylesheet the emitter wrote and the bytes after it are read as markup, outside the sheet, outside every layer and in a different language; a stylesheet never needs the sequence, and a value that quotes markup is text the kernel renders, not a sheet's", close, text, close)
		}
		if at := boundaryRE.FindString(text); at != "" {
			return fmt.Errorf("ui: a client rule carries %q inside %.120q: Compose writes what opens and closes a block, so text that carries a brace or a comment start ends the block it sits in and the bytes after it are emitted outside the client layer the gate reads, which is how a consumer's rules leave their layer; a rule that needs a block is a @media or @keyframes, which the emitter closes", at, text)
		}
		return nil
	}); err != nil {
		panic(err.Error())
	}
	if err := sheet.Heads(func(head string) error {
		if at := atRuleHead(head); at != "" {
			return fmt.Errorf("ui: a client rule's head %.120q begins with %s, which is not a selector: Compose writes this text ahead of the `{` it supplies, so an at-keyword there names the block a browser is then reading rather than a rule's subject, and a rule that begins %s states a block of its own inside the client layer Compose places it in; a consumer rule that needs a block writes css.Sheet.Media or css.Sheet.Keyframes, whose braces the emitter closes", head, at, at)
		}
		if unbraced(head, ';') {
			return fmt.Errorf("ui: a client rule's head %.120q carries a `;` ahead of the `{` Compose supplies: a browser ends a rule at `;`, so the text after one is read as the head of the next rule inside the client layer, and a head that says `.store-card; @layer base` or a @media query that says `all; @layer tokens` states an at-rule of its own where the emitter wrote one rule; a `;` inside a bracketed attribute value is data in a token the browser already opened and stays legal", head)
		}
		return nil
	}); err != nil {
		panic(err.Error())
	}
	err := sheet.WalkRules(func(selector string, decls []css.Declaration) error {
		if strings.Contains(selector, ":root") {
			return fmt.Errorf("ui: a client rule names %q: the root element is the kernel's to style, and the tokens it carries are named in one place", selector)
		}
		for _, name := range attrNames(selector) {
			if renderedBy, kernel := kernelHooks[name]; kernel {
				return fmt.Errorf("ui: a client rule names the kernel's own attribute %q (%s), which %s renders: the refusal reads attribute names, not what a selector can reach, so a rule beside your own class is refused too; style your own hook and let the layer carry it", name, selector, renderedBy)
			}
		}
		for _, d := range decls {
			// The gate reads what the browser will read: the declaration as the
			// emitter writes it, `property: value;`. A semicolon in either field ends
			// a declaration there, so the text after one is a second declaration and
			// whatever precedes its colon is the property a browser will name — the
			// kernel's namespace or a raw colour included. Reading the two fields
			// apart, or one of them, is how the escape survives a check.
			for _, part := range strings.Split(d.CSS(), ";") {
				if customDeclRE.MatchString(part) {
					return fmt.Errorf("ui: a client rule (%s) declares a --pk- property: %.120q is a declaration to a browser — the --pk- namespace is the kernel's to name, and a semicolon in a property or a value makes the text after it a second declaration", d.Property, part)
				}
				if raw := rawColourRE.FindString(part); raw != "" {
					return fmt.Errorf("ui: a client rule (%s) carries the raw colour %q in %.120q; read a token with css.VarRef", d.Property, raw, part)
				}
			}
		}
		return nil
	})
	if err != nil {
		panic(err.Error())
	}
}

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
	sheet.Layer(layerComponents, func(c *css.Sheet) {
		c.Merge(componentState())
		c.Merge(rules(lists))
	})
	sheet.Layer(layerClient, func(cl *css.Sheet) {
		for _, e := range extra {
			for _, s := range e.Sheets {
				refuseClientSheet(s)
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
// component asked for, and the page's own box model, margins, colours and type.
//
// It is written here rather than as a vendored reset, because a reset is a
// thousand lines of undoing decisions browsers stopped making a decade ago.
//
// What lives here is what a utility may still overrule: every selector is an
// element or a universal selector, so a class on the element outspecifies it in
// any layer. A rule about one of the kernel's own components does not belong
// here — see componentState, which sits in the layer the utilities sit in.
func base() *css.Sheet {
	s := css.NewSheet()
	v := func(name string) css.Value { return css.VarRef(name, "") }
	// Width utilities opt into a border; CSS otherwise defaults to style none.
	// Start at zero so undecorated elements do not acquire a medium-width border.
	s.Select("*, *::before, *::after",
		css.Decl("box-sizing", css.Literal("border-box")),
		css.Decl("border-width", css.Literal("0")),
		css.Decl("border-style", css.Literal("solid")))
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
		// Override ordinary utility and consumer rules while retaining completion
		// events for declared animations and transitions. A nonzero duration must
		// not activate the default transition-property: all on ordinary content;
		// later utility and consumer declarations can still name their properties.
		inner.Select("*, *::before, *::after",
			css.Decl("animation-duration", css.Literal("0.01ms !important")),
			css.Decl("animation-iteration-count", css.Literal("1 !important")),
			css.Decl("transition-property", css.Literal("none")),
			css.Decl("transition-duration", css.Literal("0.01ms !important")),
			css.Decl("scroll-behavior", css.Literal("auto !important")))
	})
	return s
}

// componentState is the other half of what base() used to be: every rule the
// kernel writes about one of its own components — the hidden hook, the radius a
// component owes its role, the checkbox's projected indicator, the modal that is
// not open. These are placed in @layer components, ahead of the resolved class
// lists, and not in @layer base with the preflight.
//
// A layer ranks before specificity, and specificity never crosses a layer, so a
// role rule in an earlier layer than the utilities loses to one class sitting on
// the element it governs whatever it declares. `dialog[data-component=modal]:not([open])`
// loses to the `flex` on the same dialog and a dismissed modal keeps covering the
// page; `[data-checkbox-box]` loses to the `text-transparent` on the same box and
// a ticked box paints no mark. In the components layer the role's own selector
// decides the tie again — and, staying ahead of the class lists, it still loses
// the equal-specificity ties it lost before layers existed, so a class a
// component's markup asks for is still the last word about that class.
func componentState() *css.Sheet {
	s := css.NewSheet()
	v := func(name string) css.Value { return css.VarRef(name, "") }
	// Component layout utilities must respect Hidden. Keep the override scoped
	// so consumer HTML can reveal its own hidden content in print styles.
	s.Select("[data-component][hidden]", css.Decl("display", css.Literal("none !important")))
	s.Select("[data-component=button]", css.Decl("border-radius", v("pk-radius-button")))
	s.Select("[data-component=card]", css.Decl("border-radius", v("pk-radius-card")))
	s.Select("[data-modal-panel]", css.Decl("border-radius", v("pk-radius-modal")))
	// A native checkbox owns value and keyboard state. Its projected indicator
	// follows the input even without JavaScript and after native form reset.
	s.Select("[data-component=checkbox]:has(> input:enabled)",
		css.Decl("opacity", css.Literal("1")), css.Decl("cursor", css.Literal("pointer")))
	s.Select("[data-component=checkbox][hidden]", css.Decl("display", css.Literal("none !important")))
	s.Select("[data-component=checkbox] > [data-checkbox-box]",
		css.Decl("background-color", v("pk-color-surface-primary")),
		css.Decl("border-color", v("pk-color-border-default")),
		css.Decl("color", v("pk-color-accent-on")))
	s.Select("[data-component=checkbox] > input:is(:checked,:indeterminate) + [data-checkbox-box]",
		css.Decl("background-color", v("pk-color-accent-default")),
		css.Decl("border-color", v("pk-color-accent-default")))
	s.Select("[data-component=checkbox] [data-checkbox-checkmark], [data-component=checkbox] [data-checkbox-bar]",
		css.Decl("display", css.Literal("none !important")))
	s.Select("[data-component=checkbox] > input:checked:not(:indeterminate) + [data-checkbox-box] [data-checkbox-checkmark], [data-component=checkbox] > input:indeterminate + [data-checkbox-box] [data-checkbox-bar]",
		css.Decl("display", css.Literal("block !important")))
	s.Select("[data-component=checkbox] [data-checkbox-bar]", css.Decl("background-color", css.Literal("currentColor")))
	s.Select("[data-component=checkbox] > input:focus-visible + [data-checkbox-box]",
		css.Decl("outline", css.Literal("2px solid var(--pk-color-focus)")),
		css.Decl("outline-offset", css.Literal("2px")))
	s.Select("[data-component=checkbox]:has(> input:disabled)",
		css.Decl("opacity", css.Literal("0.5")), css.Decl("cursor", css.Literal("not-allowed")))
	s.Media("(forced-colors: active)", func(s *css.Sheet) {
		s.Select("[data-component=checkbox] > [data-checkbox-box]",
			css.Decl("forced-color-adjust", css.Literal("none")),
			css.Decl("background-color", css.Literal("Canvas")),
			css.Decl("border-color", css.Literal("CanvasText")),
			css.Decl("color", css.Literal("CanvasText")))
		s.Select("[data-component=checkbox] > input:focus-visible + [data-checkbox-box]",
			css.Decl("outline-color", css.Literal("Highlight")))
		s.Select("[data-component=checkbox] > input:is(:checked,:indeterminate) + [data-checkbox-box]",
			css.Decl("background-color", css.Literal("Highlight")),
			css.Decl("border-color", css.Literal("Highlight")),
			css.Decl("color", css.Literal("HighlightText")))
		s.Select("[data-component=checkbox]:has(> input:disabled)", css.Decl("opacity", css.Literal("1")))
		s.Select("[data-component=checkbox] > input:disabled + [data-checkbox-box]",
			css.Decl("border-color", css.Literal("GrayText")))
	})
	s.Select("dialog[data-component=modal]",
		css.Decl("width", css.Literal("100%")), css.Decl("height", css.Literal("100%")),
		css.Decl("max-width", css.Literal("none")), css.Decl("max-height", css.Literal("none")),
		css.Decl("margin", css.Literal("0")), css.Decl("border", css.Literal("0")),
		css.Decl("background", css.Literal("transparent")), css.Decl("color", css.Literal("inherit")))
	s.Select("dialog[data-component=modal]:not([open])", css.Decl("display", css.Literal("none")))
	s.Select("dialog[data-component=modal]::backdrop", css.Decl("background", css.Literal("transparent")))
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
