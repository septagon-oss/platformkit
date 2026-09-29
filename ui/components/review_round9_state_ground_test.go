package components_test

// Review round 9 (T-0107). Round 8's HIGH — the label a filled tone button paints
// on a status fill — is cured and pinned by its own cases, so this round leaves no
// failing case. What it leaves instead is a pin on the most fragile thing it
// could reach: the one ground this package paints that design.GatedRolePairs does
// not name at all.
//
// `hoverBg(style.SurfaceHover)` and the modal close's
// `c.TextColor(style.FgPrimary).Bg(style.SurfaceHover)` paint copy on
// surface-hover, which design/roles.go declares as a mix of 4% of text-primary
// into surface-primary. No gated pair names that ground — not bodyRolePairs, not
// tintedRolePairs, not statusRolePairs — because a hover state is not a rule the
// line-sweep in painted_status_pair_test.go matches: that sweep reads a line that
// opens a declaration (`name = style.New()`) and this ground is painted from
// inside `On(style.StateHover, …)`. Mixing the foreground into the surface always
// costs contrast, so every gated pair that sits at exactly 4.5:1 on surface-hover's
// source sits below 4.5:1 on the hover ground itself. Round 9 swept 448,000
// single-token client overrides through design.Client.Resolve (109,384 accepted)
// and measured the pairs this package actually paints on that ground: fg-primary
// and fg-secondary never fell under the floor, because the pairs that gate them
// (`text-primary`, `text-muted` against three surfaces each) refuse a client before
// they can get near it. One pair did fall under — 4.175:1 for a light theme whose
// `surface-primary` is #0df7f7, a client the gate accepts — and it is the muted
// table-header line, which no kernel rule paints inside a hovered row today.
//
// So this is a margin, not a defect, and a pin is what a margin needs. The cases
// below re-derive the hover grounds and their foregrounds out of this package's own
// declarations — nothing freezes a colour — and refuse the day a client palette or
// a generated one paints copy onto that ground under the floor, including the day
// someone puts muted text inside a hovered row (a plausible one-line change to
// ui/components/table.go) and the day the mix percentage moves. design.MinContrast
// is the floor; no refusal's wording is read anywhere here, and the pinned client
// is asserted *accepted*, which is what makes the measurement reachable.

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// hoverPaintedPairs reads the grounds this package paints on a state, and the
// foreground that sits on each: the rule's own TextColor when it declares one, and
// the page's inherited text colour when it declares none — which is the table row,
// whose cells carry no colour of their own and so read in fg-primary, the colour
// ui's page shell sets for the canvas.
func hoverPaintedPairs(t *testing.T) []paintedPair {
	t.Helper()
	source, err := os.ReadFile("classlists.go")
	if err != nil {
		t.Fatal(err)
	}
	owner := regexp.MustCompile(`(?m)^\s*(cl[A-Za-z]+|"?[a-z]+"?)\s*(=|:)\s*(style\.New\(\)|map\[string\]style\.ClassList\{)`)
	hover := regexp.MustCompile(`hoverBg\(style\.Surface([A-Za-z0-9]+)\)`)
	own := regexp.MustCompile(`TextColor\(style\.Fg([A-Za-z0-9]+)\)`)
	entryOwner := regexp.MustCompile(`(?m)^\s*"([a-z]+)"\s*:`)
	textOnHover := regexp.MustCompile(`TextColor\(style\.Fg([A-Za-z0-9]+)\)\.Bg\(style\.Surface([A-Za-z0-9]+)\)`)
	var out []paintedPair
	rule, entry, entryFg := "", "", ""
	for _, line := range strings.Split(string(source), "\n") {
		if named := owner.FindStringSubmatch(line); named != nil {
			rule, entry, entryFg = named[1], "", ""
			if ownOnEntry := own.FindStringSubmatch(line); ownOnEntry != nil {
				entryFg = "fg-" + kebabConstant(ownOnEntry[1])
			}
		} else if named := entryOwner.FindStringSubmatch(line); named != nil {
			entry, entryFg = named[1], ""
			if ownOnEntry := own.FindStringSubmatch(line); ownOnEntry != nil {
				entryFg = "fg-" + kebabConstant(ownOnEntry[1])
			}
		}
		who := rule
		if entry != "" {
			who = rule + "[" + entry + "]"
		}
		if found := textOnHover.FindStringSubmatch(line); found != nil {
			out = append(out, paintedPair{
				foreground: "fg-" + kebabConstant(found[1]), background: "surface-" + kebabConstant(found[2]),
				paintedBy: who + " (a state rule that sets both)",
			})
			continue
		}
		for _, found := range hover.FindAllStringSubmatch(line, -1) {
			foreground := entryFg
			if foreground == "" {
				foreground = "fg-primary" // inherited from the page canvas
			}
			out = append(out, paintedPair{
				foreground: foreground, background: "surface-" + kebabConstant(found[1]),
				paintedBy: who + " (a state ground under text with no colour of its own)",
			})
		}
	}
	if len(out) < 5 {
		t.Fatalf("the sweep read %d state grounds; classlists.go changed shape and this case now reads nothing", len(out))
	}
	seen := map[string]bool{}
	deduped := make([]paintedPair, 0, len(out))
	for _, p := range out {
		key := p.foreground + "|" + p.background
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, p)
	}
	return deduped
}

// TestStateGroundsCarryTheirCopyAboveTheFloor: the ungated grounds this package
// paints — surface-hover, surface-brand-hover, surface-brand-soft — are measured
// here for the shipped palette and the generator's own family, because no gated
// list names two of them and the mix always reads worse than its source.
func TestStateGroundsCarryTheirCopyAboveTheFloor(t *testing.T) {
	pairs := measurableGrounds(t, hoverPaintedPairs(t))
	under, worst, worstWhere := 0, 21.0, "design.Default()"
	measure := func(label string, p design.Pair) {
		for _, theme := range p.Both() {
			values, err := design.ResolveColors(theme.Tokens(), design.RoleLayer())
			if err != nil {
				t.Fatal(err)
			}
			for _, pair := range pairs {
				ratio := design.Contrast(values[roleCSS(pair.foreground)], values[roleCSS(pair.background)])
				if ratio < worst {
					worst, worstWhere = ratio, pair.foreground+" on "+pair.background+" in "+theme.Name+" "+label
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
		t.Errorf("%d themes of %d palettes paint copy on a state ground below the %g:1 floor; the worst measured %.3f:1 on %s",
			under, seeds, design.MinContrast, worst, worstWhere)
	}
	names := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		names = append(names, pair.foreground+" on "+pair.background)
	}
	t.Logf("%d state pairs swept over design.Default() and %d generated palettes, worst %.3f:1 on %s: %s",
		len(pairs), seeds, worst, worstWhere, strings.Join(names, ", "))
}

// TestStateGroundsHoldForAClientAtTheEdgeOfTheGate takes the client round 9's
// 448,000-override sweep found at the edge — a light theme whose surface-primary is
// #0df7f7, which design.Client.Resolve accepts, and whose gated fg-muted pair sits
// at 4.503:1, three thousandths above the floor. On the hover ground mixed from that
// surface the same muted colour measures 4.175:1: the margin a gated pair keeps
// against the floor is what separates the painted copy from the refusal, so the
// pair this package does paint is measured here at that client. The case asserts
// the client is accepted first, so it does not read a refusal to decide anything.
func TestStateGroundsHoldForAClientAtTheEdgeOfTheGate(t *testing.T) {
	client := design.Client{
		Slug: "probe", Seed: design.Seed{Sector: "retail", Name: "Probe"},
		Tokens: map[string]map[string]string{"light": {"surface-primary": "#0df7f7"}},
	}
	resolved, err := client.Resolve()
	if err != nil {
		t.Fatalf("design.Client.Resolve refused the client this case is pinned at (%s), so the edge it measures has moved: %v",
			client.Tokens["light"]["surface-primary"], err)
	}
	values, err := design.ResolveColors(resolved.Light.Tokens(), design.RoleLayer())
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range measurableGrounds(t, hoverPaintedPairs(t)) {
		ratio := design.Contrast(values[roleCSS(pair.foreground)], values[roleCSS(pair.background)])
		if ratio < design.MinContrast {
			t.Errorf("%s on %s measures %.3f:1 in the accepted client edge (light --pk-color-surface-primary %s), under the %g:1 floor: %s",
				pair.foreground, pair.background, ratio, client.Tokens["light"]["surface-primary"],
				design.MinContrast, pair.paintedBy)
		}
	}
}
