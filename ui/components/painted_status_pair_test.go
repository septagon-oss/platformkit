package components_test

// The gate over painted pairs is a list, and a list is only as wide as the rule
// that reads it. So this case does not take the list on trust: it reads this
// package's own declarations — every rule that paints text on a tint in the same
// declaration, and the one tinted panel whose copy is declared somewhere else
// and merged in — turns each style constant into the role a browser paints, and
// refuses a pair design.GatedRolePairs does not name. Round 7 found exactly one
// such pair, the muted reason line inside the failed media panel, and that pair
// is why the gated list carries a status-tint section at all.
//
// Nothing here freezes a colour: move a panel onto another ground, or the copy
// onto another tone, and the case asks about the pair that results. What it holds
// fixed is the invariant — a pair this package paints at body size is a pair the
// one gate measures, because Client.Resolve and ui/export refuse over that list
// and no second copy of it exists to disagree with.

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/style"
)

// paintedPair is one foreground and ground this package paints together, with
// the rule that paints it.
type paintedPair struct {
	foreground, background, paintedBy string
}

// TestMediaPanelCopyPaintsAGatedPair is the pair no single rule declares:
// mediaAbsent merges clMediaPanel(status) onto the panel and fills it with
// clEmptyDesc, so the ground and the colour are written a file apart and no rule
// that reads one declaration in isolation can see them together.
func TestMediaPanelCopyPaintsAGatedPair(t *testing.T) {
	classlists, err := os.ReadFile("classlists.go")
	if err != nil {
		t.Fatal(err)
	}
	media, err := os.ReadFile("media.go")
	if err != nil {
		t.Fatal(err)
	}
	// The composition, read where it is written. If the reason line moves out of
	// the tinted panel, no pair is painted and there is nothing to gate.
	if !strings.Contains(string(media), "clMediaAbsent.Merge(clMediaPanel(status))") ||
		!regexp.MustCompile(`func mediaAbsent[\s\S]*clEmptyDesc`).MatchString(string(media)) {
		t.Skip("mediaAbsent no longer paints a reason line inside a tinted panel, so no pair is derived")
	}
	background, ok := declaredPaint(string(classlists), "clMediaFailed", `Bg\(style\.Surface([A-Za-z0-9]+)\)`, "surface-")
	if !ok {
		t.Fatal("clMediaFailed no longer declares a surface this case can read")
	}
	foreground, ok := declaredPaint(string(classlists), "clEmptyDesc", `TextColor\(style\.Fg([A-Za-z0-9]+)\)`, "fg-")
	if !ok {
		t.Fatal("clEmptyDesc no longer declares a foreground this case can read")
	}
	assertGated(t, []paintedPair{{
		foreground: foreground, background: background,
		paintedBy: "clMediaFailed's ground under clEmptyDesc's colour, merged by mediaAbsent",
	}})
}

// TestTintRulesPaintGatedPairs is the sweep: every rule in this package that
// paints text on a tint in one declaration — the badge's four tones, the alert
// variants, the brand badge and the active navigation link, all at body size —
// has to name a pair the gate measures.
func TestTintRulesPaintGatedPairs(t *testing.T) {
	source, err := os.ReadFile("classlists.go")
	if err != nil {
		t.Fatal(err)
	}
	entry := regexp.MustCompile(`(?m)^\s*"?[a-zA-Z]+"?\s*[:=]\s*style\.New\(\)`)
	paint := regexp.MustCompile(
		`Bg\(style\.Surface([A-Za-z0-9]+)\)\.TextColor\(style\.Fg([A-Za-z0-9]+)\)` +
			`|TextColor\(style\.Fg([A-Za-z0-9]+)\)\.Bg\(style\.Surface([A-Za-z0-9]+)\)`)
	owner := regexp.MustCompile(`(?m)^\t(cl[A-Za-z]+|"[a-z]+")\s*(=|:)`)
	var painted []paintedPair
	rule := ""
	for _, line := range strings.Split(string(source), "\n") {
		if named := owner.FindStringSubmatch(line); named != nil {
			rule = named[1]
		}
		if !entry.MatchString(line) {
			continue
		}
		found := paint.FindStringSubmatch(line)
		if found == nil {
			continue
		}
		foreground, background := found[2], found[1]
		if foreground == "" {
			foreground, background = found[3], found[4]
		}
		if !strings.Contains(background, "Soft") {
			continue // A plain surface is the body list's own sweep.
		}
		painted = append(painted, paintedPair{
			foreground: "fg-" + kebabConstant(foreground), background: "surface-" + kebabConstant(background),
			paintedBy: rule + " paints " + strings.TrimSpace(line),
		})
	}
	if len(painted) < 8 {
		t.Fatalf("the sweep read %d rules painting text on a tint; classlists.go changed shape and this case now reads nothing", len(painted))
	}
	assertGated(t, painted)
}

// TestStatusRolePairsClearTheFloorOnTheShippedPalette: the list is only worth
// refusing over if the palette this repository ships holds it, measured through
// the same resolution the stylesheet resolves its roles with.
func TestStatusRolePairsClearTheFloorOnTheShippedPalette(t *testing.T) {
	values, err := design.ResolveColors(design.Default().Light.Tokens(), style.RoleColors())
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range design.StatusRolePairs() {
		got := design.Contrast(values[pair.Foreground], values[pair.Background])
		if got < pair.Min {
			t.Errorf("%s on %s measures %.2f:1, below the %.1f:1 the body text this role paints requires",
				pair.Foreground, pair.Background, got, pair.Min)
		}
	}
}

func assertGated(t *testing.T, painted []paintedPair) {
	t.Helper()
	gated := map[string]bool{}
	for _, pair := range design.GatedRolePairs() {
		gated[roleOf(pair.Foreground)+"|"+roleOf(pair.Background)] = true
	}
	for _, pair := range painted {
		if !gated[pair.foreground+"|"+pair.background] {
			t.Errorf("%s on %s is painted by %s, and design.GatedRolePairs names no such pair: a client whose design.yaml resolves a palette that paints it below 4.5:1 is accepted by Client.Resolve, which measures only the pairs that list names",
				pair.foreground, pair.background, pair.paintedBy)
		}
	}
}

// declaredRole reads one class list's declaration out of this package's source
// and turns the style constant it names into the role a browser paints.
func declaredPaint(source, name, pattern, prefix string) (string, bool) {
	declaration := regexp.MustCompile(`(?m)^\s*` + name + `\s*=\s*style\.New\(\)([^\n]*)`).FindStringSubmatch(source)
	if declaration == nil {
		return "", false
	}
	found := regexp.MustCompile(pattern).FindStringSubmatch(declaration[1])
	if found == nil {
		return "", false
	}
	return prefix + kebabConstant(found[1]), true
}

// roleOf strips the prefix CheckRoles resolves under, so a name read out of a Go
// constant and a name the layer declares compare as the same role.
func roleOf(name string) string { return strings.TrimPrefix(name, "--pk-role-") }

// kebabConstant turns the Go field name of a style constant into the role a browser
// paints: SurfaceWarningSoft is --pk-role-surface-warning-soft.
func kebabConstant(name string) string {
	var out strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				out.WriteByte('-')
			}
			out.WriteByte(c + ('a' - 'A'))
			continue
		}
		out.WriteByte(c)
	}
	return out.String()
}
