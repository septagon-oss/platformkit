package ui_test

// Review round 5's file. The client layer is the one layer a consumer may write,
// and ui/ui.go states the reason the layers exist at all: "Every rule Compose
// emits is in a layer — an unlayered rule beats every layer and would undo the
// order statement." refuseClientSheet already enforces the same promise against
// a consumer sheet that declares a typed @layer of its own (css.Sheet.UsesLayers).
//
// What nothing enforces is the same escape written as text. Compose hands a
// consumer rule to the browser by writing its selector and its declaration
// values out verbatim, and the gate reads the selector as one selector. A brace
// inside that text is not a selector to the emitter: it is a block boundary, so
// the bytes after it stop being the consumer's rule and become rules of their
// own — outside @layer client, unlayered, and therefore above every kernel rule
// whatever the order statement says. The cases below ask for the refusal the
// typed path already gives: a consumer sheet whose bytes cannot sit inside the
// layer it is placed in is refused, and a sheet whose bytes can is composed
// exactly as it is today.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// rootDeclRE matches a :root block or a custom-property declaration position —
// not a var(--pk-…) read, which is what a client sheet is meant to write.
var rootDeclRE = regexp.MustCompile(`:root\s*\{|[{;]\s*--pk-[a-z0-9-]+\s*:`)

func compose(t *testing.T, sheet *css.Sheet) (body string, refused any) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			refused = r
		}
	}()
	body = string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{sheet}}).Body)
	return body, nil
}

// TestReviewAConsumerSheetCannotCloseItsLayer is the finding: three ways a
// consumer rule's own text ends the client layer early or opens a layer the
// consumer does not own, none of which the gate refuses today.
func TestReviewAConsumerSheetCannotCloseItsLayer(t *testing.T) {
	t.Parallel()
	closing := css.NewSheet()
	closing.Select(".store-card}.grain", css.Decl("color", css.VarRef("pk-color-fg-primary", "")))
	unbalanced := css.NewSheet()
	unbalanced.Select(".grain",
		css.Decl("background-image", css.Literal("none}body{position:fixed;inset:0}")))
	nested := css.NewSheet()
	nested.Select(".grain{@layer base{body", css.Decl("color", css.VarRef("pk-color-fg-primary", "")))
	throughValue := css.NewSheet()
	throughValue.Select(".store-card", css.Decl("background-image",
		css.Literal("none}:root{--pk-color-accent-default:crimson}")))

	for _, tc := range []struct {
		name  string
		sheet *css.Sheet
	}{
		{"a selector that closes the layer", closing},
		{"a value whose brace count does not close", unbalanced},
		{"a selector that opens another layer", nested},
		{"a :root token carried in a value", throughValue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, refused := compose(t, tc.sheet)
			if refused != nil {
				t.Logf("refused: %v", refused)
				return
			}
			at := strings.Index(body, "@layer client {")
			if at < 0 {
				t.Fatal("the composed sheet carries no client layer")
			}
			depth, closed := 0, -1
			for i := at; i < len(body); i++ {
				switch body[i] {
				case '{':
					depth++
				case '}':
					depth--
					if depth == 0 {
						closed = i
						break
					}
				}
				if closed >= 0 {
					break
				}
			}
			if closed < 0 {
				t.Errorf("the client layer never closes: every byte after offset %d is inside it, so the sheet is unbalanced\nclient layer: %s",
					at, strings.ReplaceAll(body[at:], "\n", " ⏎ "))
				return
			}
			// A rule smuggled inside the layer still outranks the kernel's own
			// tokens layer, because the client layer ranks last: refuseClientSheet
			// refuses a client rule that names :root or a --pk- property, and this
			// text carries both once the browser has parsed what the gate read as a
			// value.
			if layer := body[at : closed+1]; rootDeclRE.MatchString(layer) {
				t.Errorf("the client layer now carries a :root rule or a --pk- declaration, both of which refuseClientSheet says it refuses: %s",
					strings.ReplaceAll(layer, "\n", " ⏎ "))
			}
			t.Errorf("Compose placed a consumer rule the client layer cannot hold: the layer's own block ends %d bytes before the sheet does, so the consumer's bytes after it are emitted outside every layer and outrank every kernel rule. The gate refuses a typed @layer for exactly this reason.\nclient layer: %s",
				len(body)-(closed+1),
				strings.ReplaceAll(body[at:closed+1], "\n", " ⏎ "))
		})
	}
}

// TestReviewAWellFormedConsumerSheetStillComposes is the control the fix has to
// keep: refusing bytes the layer cannot hold must not refuse a consumer sheet
// written the way ui.Extra documents it. A selector is a selector, a value is a
// value, and the layer carries the rest.
func TestReviewAWellFormedConsumerSheetStillComposes(t *testing.T) {
	t.Parallel()
	sheet := css.NewSheet()
	sheet.Select(".store-card[data-grain=oak]", css.Decl("color", css.VarRef("pk-color-fg-primary", "")))
	sheet.Media("(min-width: 48rem)", func(inner *css.Sheet) {
		inner.Select(".store-card", css.Decl("padding", css.Literal("2rem")))
	})
	body, refused := compose(t, sheet)
	if refused != nil {
		t.Fatalf("Compose refused a consumer rule that fits its layer: %v", refused)
	}
	at := strings.Index(body, "@layer client {")
	if at < 0 {
		t.Fatal("the composed sheet carries no client layer")
	}
	rest := body[at:]
	if end := strings.Index(rest, "[data-grain=oak]"); end < 0 {
		t.Fatal("the consumer's rule is missing from the client layer")
	}
	if !strings.Contains(rest, "@media (min-width: 48rem)") {
		t.Error("the consumer's media rule is missing from the client layer")
	}
	if strings.Contains(strings.SplitN(rest, "@layer client {", 2)[1], "@layer") {
		t.Error("a @layer appears inside the client layer: the control sheet declared none")
	}
}
