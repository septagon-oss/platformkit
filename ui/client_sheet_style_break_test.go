package ui_test

// The gate refuses the boundary the emitter owns inside the
// stylesheet — a brace, a comment start, an at-rule prelude — and this file is
// the boundary one level up, which nothing read before it.
//
// A composed sheet is not only served as app.css. modules/admin/internal/gallery.go
// renders the composed sheet for the gallery preview as
// h.StyleEl(g.Raw(string(sheet.Body))): the sheet's bytes become the content of a
// <style> element, written raw, exactly as the tenant accent in modules/web is
// written raw and guarded for it. An HTML parser reads a style element's content
// as text and ends the element at the sequence `</style`, whatever the CSS around
// it says, so client text carrying that sequence does not merely leave the block
// it sits in — it leaves the stylesheet, and the bytes after it are markup in a
// page a signed-in reader opens. That is a wider reach than the brace refusal
// (out of a layer) and the at-keyword refusal (into a nested block of
// the same sheet), by one document.
//
// css.Literal validates nothing and g.Raw writes what it is handed, so the gate is
// the only thing between the two. It reads the emitted text, which is where the
// sequence has to be refused: the browser never sees the field it came from.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// TestTheGateRefusesTextThatWouldCloseTheStyleElementTheSheetIsRenderedIn is the
// refusal. Every case names the reason in its message, as the other eight paths
// under refuseClientSheet do; the read is case-folded because HTML tag matching is.
func TestTheGateRefusesTextThatWouldCloseTheStyleElementTheSheetIsRenderedIn(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		sheet *css.Sheet
	}{
		{"markup after a style close in a value", css.NewSheet().Select(".store-hero",
			css.Decl("content", css.Literal("</style><img src=x onerror=alert(1)>")))},
		{"the close upper-cased", css.NewSheet().Select(".store-hero",
			css.Decl("content", css.Literal("</STYLE><script>alert(1)</script>")))},
		{"the close in a selector", css.NewSheet().Select("</style>",
			css.Decl("color", css.VarRef("pk-color-text-primary", "")))},
		{"the close in a @media query", func() *css.Sheet {
			s := css.NewSheet()
			s.Media("all</style>", func(in *css.Sheet) {
				in.Select(".store-hero", css.Decl("color", css.VarRef("pk-color-text-primary", "")))
			})
			return s
		}()},
		{"the close in a @keyframes name", func() *css.Sheet {
			s := css.NewSheet()
			s.Keyframes("</style>", func(k *css.Keyframes) { k.At("0%", css.Decl("opacity", css.Literal("0"))) })
			return s
		}()},
		{"the close in a var() fallback", css.NewSheet().Select(".store-hero",
			css.Decl("color", css.VarRef("pk-color-text-primary", "</style>")))},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var rec any
			var emitted string
			func() {
				defer func() { rec = recover() }()
				emitted = string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{c.sheet}}).Body)
			}()
			if rec == nil {
				t.Fatalf("Compose accepted %s; the browser ends a <style> element at that sequence and modules/admin renders a composed sheet as one, so the bytes after it are markup: %.200s", c.name, emitted)
			}
			msg, ok := rec.(string)
			if !ok || !strings.HasPrefix(msg, "ui: ") || !strings.Contains(strings.ToLower(msg), "</style") {
				t.Errorf("the refusal of %s does not name the sequence it read: %#v", c.name, rec)
			}
		})
	}
}

// TestTextThatIsNotAStyleCloseStillComposes keeps the read at the boundary it
// claims. The HTML rule is a closing tag for this one element, not any angle
// bracket, slash or whitespace-padded lookalike: `</ style` opens no tag, and a
// value that quotes other markup ends nothing. A read that refused those would
// refuse a sheet that composes, and a refused composition ships no bytes at all.
func TestTextThatIsNotAStyleCloseStillComposes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		sheet *css.Sheet
	}{
		{"a less-than in a value", css.NewSheet().Select(".store-hero",
			css.Decl("content", css.Literal(`"a < b"`)))},
		{"quoted markup in a value", css.NewSheet().Select(".store-hero",
			css.Decl("content", css.Literal(`"<b>bold</b>"`)))},
		{"a slash and a space where a tag would open", css.NewSheet().Select(".store-hero",
			css.Decl("content", css.Literal(`"a </ b >"`)))},
		{"a close split from the tag name", css.NewSheet().Select(".store-hero",
			css.Decl("content", css.Literal(`"a </ stylesheet> b"`)))},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			body := string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{c.sheet}}).Body)
			if !strings.Contains(body, "@layer client {") {
				t.Fatalf("the sheet carries no client layer: %.200s", body)
			}
		})
	}
}

// TestTheEmitterWritesWhatItIsHanded is the reason the refusal sits in the gate.
// css writes a sheet's text unchanged, so a sheet that carries the sequence
// reaches whatever renders it; ui/css has no idea where the sheet is going, and
// nothing downstream of Compose escapes it either.
func TestTheEmitterWritesWhatItIsHanded(t *testing.T) {
	t.Parallel()
	sheet := css.NewSheet().Select(".store-hero", css.Decl("content", css.Literal("x</style>y")))
	if !strings.Contains(sheet.CSS(), "</style>") {
		t.Errorf("ui/css did not write the sequence it was handed, so the gate's read has nothing to guard: %s", sheet.CSS())
	}
}
