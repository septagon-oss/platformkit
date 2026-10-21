package design

import (
	"fmt"
	"strings"
	"testing"
)

// Review round 11. Round 10's cure named the two surfaces the focus ring's offset
// draws it onto, beside the canvas it already read. This round mutated that cure —
// `git archive HEAD`, one line deleted from bodyContrast, nothing else — and found
// the tree catches the loss of a ground in exactly one place: the shape pin
// TestRingGateNamesEveryGroundTheOffsetPutsItOn, which counts the entries in the
// list. No case that goes through the door notices. Round 10's own behavioural case
// is right to be lenient on the refusal branch (it must not read its verdict out of
// a sentence), and the delivery's witness #006cf6 fails on the muted surface, so
// with {"focus","surface-primary",MinContrastGraphic} deleted every case in design,
// ui/style, ui/export, ui/components and kit/designconfig passes while the
// acceptance door has quietly stopped measuring the card:
//
//	$ go test ./design/ ./ui/style/ ./ui/export/ ./ui/components/ ./kit/designconfig/ -count=1
//	--- FAIL: TestRingGateNamesEveryGroundTheOffsetPutsItOn   # the shape pin, alone
//	ok  ui/style   ok ui/export   ok ui/components   ok kit/designconfig
//
// This file closes that hole through the door rather than through the list: it takes
// a ring that clears the canvas and fails on the card, and asks for what Theme.Check
// promises in its own comment — the pair it measured and the ratio it got. Those are
// properties of the fixed behaviour, not of the defect: a gate that measures the
// card names the card, and one that has stopped measuring it names another ground or
// accepts the ring.
//
// Why the card and not all three grounds. The ground a ring fails on is the surface
// whose luminance sits nearest the ring's, and a theme's surfaces are ordered: in a
// light theme text-muted is gated on all three, so each stays lighter than 0.69 and
// the binding ground is the darkest of them; in a dark theme each stays darker than
// 0.17 and the binding ground is the lightest. The card lies between the other two in
// both shipped themes, so a ring can reach it first — round 10's witness does — but
// cannot reach the canvas first without the muted surface failing before it. Measured
// rather than assumed: this round searched 1600 override combinations of
// {surface-canvas, surface-muted, surface-primary, focus} through Client.Resolve for
// a client refused for `focus on surface-canvas` and found none (searched=1600,
// canvas-witnesses=0). The canvas entry is pinned by the shape pin and is not
// witnessable; the card entry was witnessable and, until this file, was not.

func TestClientResolveNamesTheCardWhenTheRingFailsOnTheCard(t *testing.T) {
	const value = "#905546"
	client := Client{
		Slug:   "roundeleven",
		Seed:   Seed{Sector: "health", Name: "Focus Probe"},
		Tokens: map[string]map[string]string{"dark": {"focus": value}},
	}

	// Measure the same pair the gate reads, from outside it, so the number below is
	// this file's own reading and not the refusal's quote of itself. Client.Resolve
	// refuses this client, so the theme is rebuilt from the same seed and the same
	// override through the door's own setter.
	seeded, err := FromSeed(client.Seed)
	if err != nil {
		t.Fatalf("the seed alone was refused: %v", err)
	}
	dark := seeded.Dark
	dark, err = dark.setColor("focus", value)
	if err != nil {
		t.Fatalf("apply the ring override: %v", err)
	}
	values, err := dark.colorValues()
	if err != nil {
		t.Fatalf("resolve the overridden dark theme: %v", err)
	}
	onCard := Contrast(values["--pk-color-focus"], values["--pk-color-surface-primary"])
	onCanvas := Contrast(values["--pk-color-focus"], values["--pk-color-surface-canvas"])
	if onCanvas < MinContrastGraphic {
		t.Fatalf("this witness is about the card: its ring measures %.3f:1 on the canvas, at or under the floor", onCanvas)
	}
	if onCard >= MinContrastGraphic {
		t.Fatalf("this witness no longer fails on the card (%.3f:1); pick one that does", onCard)
	}

	_, err = client.Resolve()
	if err == nil {
		t.Fatalf("a ring at %.3f:1 on the canvas and %.3f:1 on the card was accepted: bodyContrast does not measure the ring on the surface clCardFrame paints", onCanvas, onCard)
	}
	for _, want := range []string{
		"focus", "surface-primary",
		fmt.Sprintf("%.2f:1", onCard),             // the ratio it measured, not any ratio
		fmt.Sprintf("%.1f:1", MinContrastGraphic), // the floor it measured against
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not carry %q, so it did not measure the ring on the card: name the ground in bodyContrast and say which one you measured", err, want)
		}
	}
}
