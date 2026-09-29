package style_test

// Review round 6 (T-0107). This branch rests on one claim: that a client's
// design.yaml is accepted by the same list a stylesheet is built from —
// design/roles.go declares the --pk-role-* layer, design.Client.Resolve refuses
// a finished pair through Pair.CheckRoles(RoleLayer(), GatedRolePairs()), and
// ui/export.checkLegible refuses a Pair at the seam where it becomes a
// stylesheet. Round 5 proved the two seams disagreed while each believed it held
// the whole gate, and the cure moved the declarations rather than the arithmetic.
//
// Nothing yet read the two seams through their own doors: round 5's case measures
// a hand-assembled list (append(BodyRolePairs(), TintedRolePairs()...)), which has
// the same content as GatedRolePairs but is not the value either seam reads, and
// the delivery's own tests call design.GatedRolePairs — the list under review — so
// a gate and a renderer that each drifted to a new shared list would stay green.
// These two cases read each seam only through its public door.
//
// Both pass at these bytes and both have a passing branch: the first fails the day
// the gate measures a role the emitted :root block does not declare, or measures a
// different declaration than the one it renders (a second copy of the layer, the
// defect's shape); the second fails if the door and the export ever disagree about
// a palette again — in either direction, since a gate that refused a legible
// client would strand a brand that cannot write a design.yaml at all.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/export"
	"github.com/septagon-oss/platformkit/ui/style"
)

// TestGatedRolesAreDeclaredAndEmittedAsOneDeclaration pins the single source:
// every role the gate measures is a role the emitted :root block declares, with
// the same value the gate resolved. The gate measures the layer a browser paints
// only while the two read one table.
func TestGatedRolesAreDeclaredAndEmittedAsOneDeclaration(t *testing.T) {
	t.Parallel()
	declarations := map[string]design.ColorValue{}
	for _, role := range style.RoleColors() {
		if _, again := declarations[role.Name]; again {
			t.Fatalf("%s is declared twice, so one caller reads one copy and another the next", role.Name)
		}
		declarations[role.Name] = role.Value
	}
	sheet := style.RoleVars().CSS()
	if sheet == "" {
		t.Fatal("the role layer emits no CSS: nothing here could be compared to the gate")
	}
	measured := map[string]bool{}
	for _, pair := range style.GatedRolePairs() {
		measured[pair.Foreground] = true
		measured[pair.Background] = true
	}
	if len(measured) < 10 {
		t.Fatalf("the gated set names %d roles, which measures no layer", len(measured))
	}
	for name := range measured {
		value, declared := declarations[name]
		if !declared {
			t.Errorf("the gate measures %s, which style.RoleColors does not declare", name)
			continue
		}
		css, err := value.CSS()
		if err != nil {
			t.Errorf("%s: the declaration the gate read will not render: %v", name, err)
			continue
		}
		if !strings.Contains(sheet, name+": "+css+";") {
			t.Errorf("the gate measured %s as %q and the emitted :root block declares it some other way:\n%s",
				name, css, roleLine(sheet, name))
		}
	}
}

// roleLine returns the declaration of one role in the emitted sheet, for the
// failure message. It returns the fact rather than a claim about it.
func roleLine(sheet, name string) string {
	for _, line := range strings.Split(sheet, "\n") {
		if strings.Contains(line, name+":") {
			return strings.TrimSpace(line)
		}
	}
	return name + " appears nowhere in the emitted sheet"
}

// TestDoorAndExportRefuseTheSamePalettes reads the two seams of the gate through
// their own public doors over palettes that differ only in one token: the door is
// design.Client.Resolve (the route a client's design.yaml takes) and the seam is
// ui/export.ExportTokens (where a Pair becomes the token document a native
// application reads). Each case also carries the same palette built by hand, so
// the door's refusal can be compared with what the finished pair actually
// measures rather than with what the door says.
func TestDoorAndExportRefuseTheSamePalettes(t *testing.T) {
	t.Parallel()
	seed := design.Seed{Sector: "insurance", Name: "Meridian"}
	generated, err := design.FromSeed(seed)
	if err != nil {
		t.Fatalf("FromSeed(%v): %v", seed, err)
	}
	// A literal from the sweep review round 5 ran: it holds all 22 token pairs of
	// the theme and drops the brand badge's own label under the floor the badge's
	// copy requires. The three are legal overrides by Client.Validate, which the
	// case checks, so the refusal below can only come from the gate.
	cases := []struct {
		label string
		theme string
		token string
		value string
		paint func(p *design.Pair, v string)
	}{
		{
			label: "no override at all",
			theme: "dark", token: "surface-primary", value: generated.Dark.SurfacePrimary,
			paint: func(p *design.Pair, v string) { p.Dark.SurfacePrimary = v },
		},
		{
			label: "a no-op override of the accent's own label",
			theme: "light", token: "accent-on", value: generated.Light.AccentOn,
			paint: func(p *design.Pair, v string) { p.Light.AccentOn = v },
		},
		{
			label: "a warm neutral card surface",
			theme: "dark", token: "surface-primary", value: "#2e2920",
			paint: func(p *design.Pair, v string) { p.Dark.SurfacePrimary = v },
		},
		{
			label: "the accent itself",
			theme: "dark", token: "accent-default", value: "#f46218",
			paint: func(p *design.Pair, v string) { p.Dark.AccentDefault = v },
		},
		{
			label: "the primary text colour",
			theme: "dark", token: "text-primary", value: "#fa19fa",
			paint: func(p *design.Pair, v string) { p.Dark.TextPrimary = v },
		},
	}
	for _, c := range cases {
		// Reachability, without either seam's own output: the literal is a legal
		// override and the hand-built pair is the palette the door would return.
		if err := (design.Client{Slug: "meridian", Seed: seed, Tokens: map[string]map[string]string{
			c.theme: {c.token: c.value},
		}}).Validate(); err != nil {
			t.Fatalf("%s: the case measures nothing, %s.%s=%s is not a legal override: %v",
				c.label, c.theme, c.token, c.value, err)
		}
		hand := generated
		c.paint(&hand, c.value)
		if err := hand.Check(); err != nil {
			t.Fatalf("%s: the case measures nothing, the token half already refuses the hand-built pair: %v", c.label, err)
		}
		gateErr := hand.CheckRoles(design.RoleLayer(), design.GatedRolePairs())

		client := design.Client{Slug: "meridian", Seed: seed, Tokens: map[string]map[string]string{
			c.theme: {c.token: c.value},
		}}
		door, doorErr := client.Resolve()
		if doorErr == nil && door != hand {
			t.Errorf("%s: the door returned a pair that is not the one it was handed: %s.%s=%s",
				c.label, c.theme, c.token, c.value)
		}
		_, exportErr := export.ExportTokens(hand, "light", "dark")

		if (gateErr == nil) != (exportErr == nil) {
			t.Errorf("%s: the gate and the export disagree about one palette: gate %v, export %v",
				c.label, gateErr, exportErr)
		}
		if (doorErr == nil) != (exportErr == nil) {
			t.Errorf("%s: the door that accepts a client's file and the seam that ships a sheet disagree: door %v, export %v",
				c.label, doorErr, exportErr)
		}
		// A refusal says which pair it measured, in the role layer's own words.
		if exportErr != nil && gateErr != nil {
			role := strings.SplitN(strings.TrimPrefix(gateErr.Error(), "dark: "), " on ", 2)[0]
			if role == "" || !strings.Contains(exportErr.Error(), role) {
				t.Errorf("%s: the export refused without naming the pair the gate named (%s): %v",
					c.label, role, exportErr)
			}
		}
	}
}
