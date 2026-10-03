package ui_test

// Review round 6 of T-0108. The delivery of 66b6756 closed the escape round 5
// found — text inside a declaration *value* that ends the declaration — by
// reading each value "in the parts a browser splits it into". It split the
// value and left the property unread beyond one prefix test.
//
// That is the same channel, one field over. The emitter writes
// `selector { property: value; }` (ui/css/css.go, Declaration.CSS), so the
// property arrives in the browser's text with the same terminators around it as
// the value does: a `;` inside the property field ends a declaration there and
// the text before the `:` becomes a property of its own. refuseClientSheet reads
// d.Value.CSS() split on ";" for exactly this reason and never splits
// d.Property, and the only thing the property field is asked is
// strings.HasPrefix(d.Property, "--pk-").
//
// So the two things the gate's own doc block promises it refuses — a --pk-
// property and a raw colour, "the palette is named in one place" — both pass on
// a consumer rule that never puts a brace in its text at all, which is the case
// boundaryRE was written for and cannot see. And because the emitted --pk-
// declaration sits in @layer client, which the order statement ranks after
// tokens, it wins the custom property over the tokens layer, which is the
// inversion ui/ui.go's package doc says the layers exist to prevent.
//
// The cases below ask for the refusal the value field already gets, and carry a
// control the other way — a consumer sheet that writes a custom property of its
// own, a var() read, a @media block and a data: URL — so the cure cannot answer
// the escape by refusing the client layer itself.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// TestTheGateReadsTheTextAPropertyFieldDeclares is the finding: a consumer
// rule whose *property* field carries a semicolon declares a second declaration
// the gate never looks at. Each case must be refused; today Compose accepts all
// three and the refusal the value field gets is what they ask for.
func TestTheGateReadsTheTextAPropertyFieldDeclares(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		sheet *css.Sheet
		// want names what the composed sheet must not contain: the browser-facing
		// text the case smuggles past the gate.
		want string
	}{
		{
			name:  "a kernel property declared through the property field",
			sheet: css.NewSheet().Select(".store-card", css.Decl("color:transparent;--pk-color-accent-default", css.Literal("currentColor"))),
			want:  "--pk-color-accent-default",
		},
		{
			name:  "a raw colour declared through the property field",
			sheet: css.NewSheet().Select(".store-card", css.Decl("color:#0f5d4e;outline-color", css.Literal("currentColor"))),
			want:  "#0f5d4e",
		},
		{
			name: "a kernel property declared through a keyframe stop's property field",
			sheet: css.NewSheet().Keyframes("store-pulse", func(k *css.Keyframes) {
				k.At("from", css.Decl("color:red;--pk-role-surface-brand", css.Literal("teal")))
			}),
			want: "--pk-role-surface-brand",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var refused any
			var body string
			func() {
				defer func() { refused = recover() }()
				body = string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{tc.sheet}}).Body)
			}()
			if refused != nil {
				t.Logf("refused as it must be: %v", refused)
				return
			}
			at := strings.Index(body, "@layer client {")
			if at < 0 {
				t.Fatal("the composed sheet carries no client layer")
			}
			end := strings.Index(body[at:], "\n}")
			if end < 0 {
				end = len(body) - at
			}
			t.Errorf("Compose accepted a consumer rule that declares %q inside the property field, which refuseClientSheet says it refuses: the emitter writes `property: value;`, so a semicolon in the property field ends a declaration and the text before the colon becomes a property of its own. client layer:\n%s",
				tc.want, body[at:at+end+2])
		})
	}
}

// TestAConsumerSheetThatWritesItsOwnTextStillComposes is the control the
// fix must keep green: the text a consumer is entitled to write — a custom
// property of its own, a var(--pk-…) read, a @media block, a keyframe and a
// data: URL whose value carries a semicolon and brackets — composes, and lands
// in the client layer. A cure that refuses these refused the layer, not the
// escape.
func TestAConsumerSheetThatWritesItsOwnTextStillComposes(t *testing.T) {
	t.Parallel()
	sheet := css.NewSheet()
	sheet.Select(".store-card",
		css.Decl("--store-card-radius", css.Literal("0.5rem")),
		css.Decl("color", css.VarRef("pk-color-fg-primary", "")),
		css.Decl("background-image", css.Literal(`url("data:image/svg+xml,%3csvg viewbox='0 0 8 8'%3e%3c/path%3e%3c/svg%3e")`)),
	)
	sheet.Media("(min-width: 40rem)", func(in *css.Sheet) {
		in.Select(".store-card", css.Decl("grid-template-columns", css.Literal("repeat(2, minmax(0, 1fr))")))
	})
	sheet.Keyframes("store-pulse", func(k *css.Keyframes) {
		k.At("from", css.Decl("opacity", css.Literal("1")))
		k.At("to", css.Decl("opacity", css.Literal("0.4")))
	})

	var refused any
	var body string
	func() {
		defer func() { refused = recover() }()
		body = string(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{sheet}}).Body)
	}()
	if refused != nil {
		t.Fatalf("Compose refused a consumer sheet that writes nothing but its own text: %v", refused)
	}
	at := strings.Index(body, "@layer client {")
	if at < 0 {
		t.Fatal("the composed sheet carries no client layer")
	}
	// The client layer runs to the end of the sheet: these are the last rules.
	client := body[at:]
	for _, want := range []string{"--store-card-radius", "var(--pk-color-fg-primary)", "data:image/svg+xml", "@media (min-width: 40rem)", "@keyframes store-pulse", "repeat(2, minmax(0, 1fr))"} {
		if !strings.Contains(client, want) {
			t.Errorf("the client layer does not carry %q:\n%s", want, client)
		}
	}
	if strings.Contains(body[:at], "store-card") {
		t.Errorf("a consumer rule reached a kernel layer:\n%s", body[:at])
	}
}
