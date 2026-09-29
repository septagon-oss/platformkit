package components_test

// Review round 8 (T-0107). Round 7's HIGH was a body-size foreground painted on
// a status ground that no gated pair named; the cure adopted the rule "a body
// foreground on a status tint is a measured pair" and gated fg-muted on the four
// soft tints. This case asks the same question of the other ground a status tone
// is painted on: the filled tone button, whose label nobody measures.
//
// ButtonWithSlots replaces a button's whole appearance with clButtonTone[tone]
// (ui/components/button.go: if tone != "neutral" { appearance = clButtonTone[tone] }),
// so a button painted Tone: "danger" — which is what ui/resource/resource.go puts
// on the delete form of every generated list, at Size: "md", text-sm — gets
// Bg(style.SurfaceDanger) under TextColor(style.FgOnBrand). surface-danger reads
// --pk-color-status-danger and fg-on-brand reads --pk-color-accent-on, and no list
// the gate reads pairs those two: bodyRolePairs names no fg-on-brand at all,
// tintedRolePairs names the brand tint, statusRolePairs names each tone on its own
// soft tint plus the muted line, and the token gate pairs accent-on with
// accent-default and accent-hover only. accent-on is one of the twenty-two tokens
// a client may override, so the door is open with one line of design.yaml:
//
//	client sand, seed health/Havenkit, tokens.light.accent-on = "#aacc44"
//	  design.Client.Resolve -> accepted; the success button's label measures 3.497:1
//	  (warning 3.949:1) while all twenty-two token pairs and every gated role pair hold
//	client reef, same seed, tokens.dark.accent-on = "#662200"
//	  design.Client.Resolve -> accepted; danger 3.872:1, info 3.885:1, warning 4.305:1
//
// The shipped palette hides it, as last time: design.Default() holds every one of
// these pairs, which is why no page, screenshot or axe sweep in this repository
// shows it. Nothing here freezes today's colours — the pairs are read out of this
// package's own map, and both assertions have a passing branch: name the pair in
// design.StatusRolePairs (which FromSeed, Client.Resolve and ui/export all read) and
// repair the generated fill or the label against it, and the pair compares, the
// two clients above are refused, and every palette reaches the floor. No refusal's
// wording is asked for anywhere here.

import (
	"hash/fnv"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// TestToneButtonLabelPairIsGated asks whether the label a filled tone button
// paints on a status ground is a pair design.Client.Resolve measures.
func TestToneButtonLabelPairIsGated(t *testing.T) {
	// Only the four status grounds. The brand button's label is the same role on a
	// brand fill, and that pair is measured — as the token gate's accent-on against
	// accent-default — so this case asks about the grounds nothing measures.
	pairs := statusFills(paintedToneButtonPairs(t, readSource(t, "classlists.go")))
	if len(pairs) == 0 {
		t.Fatal("clButtonTone declares no Bg(style.Surface…)/TextColor(style.Fg…) pair on a status fill this case can read")
	}

	gated := map[string]bool{}
	for _, p := range design.GatedRolePairs() {
		gated[roleKey(p.Foreground)+"|"+roleKey(p.Background)] = true
	}
	for _, p := range pairs {
		if !gated[roleKey(p[0])+"|"+roleKey(p[1])] {
			t.Errorf("clButtonTone paints %s on %s at body size (clButtonSize[\"md\"] is text-sm), and design.GatedRolePairs names no such pair: design.Client.Resolve measures only the pairs that list names, so a client whose design.yaml moves that status fill is accepted with an unreadable button label",
				p[0], p[1])
		}
	}
}

// TestToneButtonLabelReachesTheFloorOnGeneratedPalettes measures the same pairs
// over the palettes the generator emits, the way design.FromSeed measures the
// pairs it names: both themes of every palette, the shipped pair included.
func TestToneButtonLabelReachesTheFloorOnGeneratedPalettes(t *testing.T) {
	pairs := statusFills(paintedToneButtonPairs(t, readSource(t, "classlists.go")))
	under, worst, worstWhere := 0, 21.0, "design.Default()"
	measure := func(label string, p design.Pair) {
		for _, theme := range p.Both() {
			values, err := design.ResolveColors(theme.Tokens(), design.RoleLayer())
			if err != nil {
				t.Fatal(err)
			}
			for _, pair := range pairs {
				ratio := design.Contrast(values[roleCSS(pair[0])], values[roleCSS(pair[1])])
				if ratio < worst {
					worst, worstWhere = ratio, pair[0]+" on "+pair[1]+" in "+theme.Name+" "+label
				}
				if ratio < design.MinContrast {
					under++
				}
			}
		}
	}
	measure("design.Default()", design.Default())
	seeds := forEachSeed(measure)
	if under > 0 {
		t.Errorf("%d themes of %d generated palettes paint a filled tone button's label below the %g:1 body floor; the worst measured %.3f:1 on %s",
			under, seeds, design.MinContrast, worst, worstWhere)
	}
}

// TestClientResolveRefusesAButtonLabelThatFallsOffTheFill walks through the door
// rather than the measurement: an override that puts the label this package
// paints under the floor is an override the gate refuses. Both colours below are
// literals from the eight-step-per-channel sweep this round ran over all
// twenty-two overridable tokens (32,076 single-token clients, 11,169 accepted,
// 127 of them painting one of these pairs under the floor); they are the worst
// the sweep found, and they are colours a client could reasonably write down.
func TestClientResolveRefusesAButtonLabelThatFallsOffTheFill(t *testing.T) {
	fills := statusFills(paintedToneButtonPairs(t, readSource(t, "classlists.go")))
	if len(fills) == 0 {
		t.Fatal("clButtonTone paints no label on a status fill, so this case has nothing to ask about")
	}
	clients := []design.Client{
		{Slug: "sand", Seed: design.Seed{Sector: "health", Name: "Havenkit"},
			Tokens: map[string]map[string]string{"light": {"accent-on": "#aacc44"}}},
		{Slug: "reef", Seed: design.Seed{Sector: "health", Name: "Havenkit"},
			Tokens: map[string]map[string]string{"dark": {"accent-on": "#662200"}}},
	}
	for _, client := range clients {
		resolved, err := client.Resolve()
		if err != nil {
			t.Logf("client %s was refused: the override its file names cannot be applied", client.Slug)
			continue // the gate refused it, which is the behaviour this case asks for
		}
		theme, name := resolved.Light, "light"
		if _, ok := client.Tokens["dark"]; ok {
			theme, name = resolved.Dark, "dark"
		}
		values, err := design.ResolveColors(theme.Tokens(), design.RoleLayer())
		if err != nil {
			t.Fatal(err)
		}
		for _, pair := range fills {
			if got := design.Contrast(values[roleCSS(pair[0])], values[roleCSS(pair[1])]); got < design.MinContrast {
				t.Errorf("design.Client.Resolve accepted client %s, whose %s --pk-color-accent-on is %s: the finished pair paints %s on %s at %.3f:1, under the %g:1 floor the gate enforces on every pair it names, and this package paints that copy at text-sm on a Tone: %q button",
					client.Slug, name, client.Tokens[name]["accent-on"], pair[0], pair[1], got, design.MinContrast, strings.TrimPrefix(pair[1], "surface-"))
			}
		}
	}
}

// statusFills keeps the painted pairs whose ground is a status surface, which is
// what this case is about; the brand button's label is already measured, as the
// token gate's accent-on against accent-default pair.
func statusFills(pairs [][2]string) [][2]string {
	var out [][2]string
	for _, p := range pairs {
		switch p[1] {
		case "surface-success", "surface-warning", "surface-danger", "surface-info":
			out = append(out, p)
		}
	}
	return out
}

// paintedToneButtonPairs reads the filled tone buttons out of this package's own
// declarations: every "tone": … Bg(style.SurfaceX) … TextColor(style.FgY) entry.
func paintedToneButtonPairs(t *testing.T, source string) [][2]string {
	t.Helper()
	block := regexp.MustCompile("(?s)clButtonTone = map\\[string\\]style\\.ClassList\\{(.+?)\n\\t\\}").FindStringSubmatch(source)
	if block == nil {
		t.Fatal("clButtonTone is no longer a map literal this case can read")
	}
	var out [][2]string
	for _, entry := range strings.Split(block[1], "\n") {
		name := regexp.MustCompile(`"([a-z]+)"`).FindStringSubmatch(entry)
		background := regexp.MustCompile(`Bg\(style\.Surface([A-Za-z0-9]+)\)`).FindStringSubmatch(entry)
		foreground := regexp.MustCompile(`TextColor\(style\.Fg([A-Za-z0-9]+)\)`).FindStringSubmatch(entry)
		if name == nil || background == nil || foreground == nil {
			continue
		}
		out = append(out, [2]string{"fg-" + kebab(foreground[1]), "surface-" + kebab(background[1])})
	}
	return out
}

func readSource(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// roleKey accepts either vocabulary a role is named in and returns the declared
// spelling, so the comparison does not depend on which one
// design.GatedRolePairs happens to speak.
func roleKey(name string) string {
	if trimmed, ok := strings.CutPrefix(name, "--pk-role-"); ok {
		return trimmed
	}
	return name
}

func roleCSS(name string) string {
	if strings.HasPrefix(name, "--pk-role-") {
		return name
	}
	return "--pk-role-" + name
}

// forEachSeed generates the same grid of seeds round 7 used — eight sectors, the
// six-value colour grid for a brand colour, deterministic by name — and hands
// every palette to the caller. It returns how many the generator accepted.
func forEachSeed(measure func(label string, pair design.Pair)) int {
	sectors := []string{"retail", "finance", "health", "energy", "public", "legal", "shelter", "logistics"}
	grid := []int{0x00, 0x33, 0x66, 0x99, 0xcc, 0xff}
	seeds := 0
	for i := 0; i < 400; i++ {
		h := fnv.New64a()
		_, _ = h.Write([]byte("review-round-8-" + itoa(i)))
		sum := h.Sum64()
		sector := sectors[int(sum%uint64(len(sectors)))]
		brand := ""
		if i%3 != 0 {
			brand = hexAt(grid, sum)
		}
		pair, err := design.FromSeed(design.Seed{
			Sector: sector, Name: "Client" + itoa(i), Brand: brand,
		})
		if err != nil {
			continue
		}
		seeds++
		measure(sector+"/Client"+itoa(i), pair)
	}
	return seeds
}
