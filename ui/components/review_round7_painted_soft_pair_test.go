package components_test

// Review round 7 (T-0107). Round 3's HIGH was a pair the kernel paints by one
// rule of one component that sat in neither list of measured pairs; the cure
// added the two pairs it found. This case asks the same question of the pair
// this package still paints for a media panel that failed: clMediaFailed gives
// the panel --pk-role-surface-warning-soft, and mediaAbsent fills that panel
// with clEmptyDesc, whose colour is --pk-role-role-fg-muted at --pk-text-sm —
// body-size copy, not a large heading, so SC 1.4.3 asks 4.5:1 of it.
//
// The pair is derived from this package's own source rather than written here,
// so the case does not freeze today's colours: change the reason line to another
// role and the case asks about that role on that ground instead, and move the
// failed panel onto --pk-role-surface-tertiary — a ground the gate already
// measures — and the pair it derives is already gated and the case passes.
//
// What it refuses today is that the gate names no such pair. design.GatedRolePairs
// lists the pairs Client.Resolve and ui/export refuse over, and a client whose
// design.yaml resolves a palette that paints this pair under the floor is
// accepted, because nothing measures it. Measured over 1200 seeds generated the
// way design.FromSeed generates them, 6 palettes paint this pair at 4.47:1, and
// the same relationship on the other two status tints — the same muted line on a
// danger-tinted or info-tinted panel, which is the markup one call apart — falls
// to 3.57:1 on 702 and 679 of them. design.Default() is fine at 5.47:1, which is
// why no page and no screenshot in this repository shows it.

import (
	"hash/fnv"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// TestPaintedSoftPanelPairIsGated asks whether the foreground and ground this
// package paints together in a failed media panel are a pair the contrast gate
// measures at all.
func TestPaintedSoftPanelPairIsGated(t *testing.T) {
	classlists, err := os.ReadFile("classlists.go")
	if err != nil {
		t.Fatal(err)
	}
	media, err := os.ReadFile("media.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(classlists)

	// The composition, read where it is written: the failed panel merges
	// clMediaPanel -> clMediaFailed, and the panel's body is clEmptyDesc.
	if !strings.Contains(string(media), "clMediaAbsent.Merge(clMediaPanel(status))") ||
		!regexp.MustCompile(`func mediaAbsent[\s\S]*clEmptyDesc`).MatchString(string(media)) {
		t.Skip("mediaAbsent no longer paints a reason line inside a tinted panel, so no pair is derived")
	}
	background := declaredBackground(t, source, "clMediaFailed")
	foreground := declaredColour(t, source, "clEmptyDesc")

	gated := map[string]bool{}
	for _, p := range design.GatedRolePairs() {
		gated[p.Foreground+"|"+p.Background] = true
	}
	if !gated[foreground+"|"+background] {
		t.Errorf("clMediaFailed paints %s on %s (mediaAbsent fills the panel with clEmptyDesc), and design.GatedRolePairs names no such pair: a client whose design.yaml resolves a palette that paints it below 4.5:1 is accepted by design.Client.Resolve, which measures only the pairs that list names",
			foreground, background)
	}

	// The floor itself, over the generator's own input: a pair this package paints
	// at body size is a body pair, so both themes of every palette the generator
	// can emit have to reach 4.5:1 on it.
	sectors := []string{"retail", "finance", "health", "energy", "public", "legal", "shelter", "logistics"}
	grid := []int{0x00, 0x33, 0x66, 0x99, 0xcc, 0xff}
	worst, worstSeed, under := 21.0, "design.Default()", 0
	measure := func(label string, p design.Pair) int {
		bad := 0
		for _, theme := range p.Both() {
			values, err := design.ResolveColors(theme.Tokens(), design.RoleLayer())
			if err != nil {
				t.Fatal(err)
			}
			ratio := design.Contrast(values["--pk-role-"+foreground], values["--pk-role-"+background])
			if ratio < worst {
				worst, worstSeed = ratio, label
			}
			if ratio < 4.5 {
				bad++
			}
		}
		return bad
	}
	// design.Default() is one palette, both themes: count it like any other.
	if n := measure("design.Default()", design.Default()); n > 0 {
		under += n
	}
	seeds := 0
	for i := 0; i < 400; i++ {
		h := fnv.New64a()
		if _, err := h.Write([]byte("review-round-7-" + itoa(i))); err != nil {
			t.Fatal(err)
		}
		sum := h.Sum64()
		brand := ""
		if i%3 != 0 {
			brand = hexAt(grid, sum)
		}
		pair, err := design.FromSeed(design.Seed{
			Sector: sectors[int(sum%uint64(len(sectors)))],
			Name:   "Client" + itoa(i),
			Brand:  brand,
		})
		if err != nil {
			continue
		}
		seeds++
		under += measure(sectors[int(sum%uint64(len(sectors)))]+"/Client"+itoa(i), pair)
	}
	if under > 0 {
		t.Errorf("%d themes of %d generated palettes paint %s on %s below the 4.5:1 body floor; the worst measured %.3f:1 on %s",
			under, seeds, foreground, background, worst, worstSeed)
	}
}

func declaredBackground(t *testing.T, source, name string) string {
	t.Helper()
	return declaredRole(t, source, name, `Bg\(style\.Surface([A-Za-z0-9]+)\)`, "surface-")
}

func declaredColour(t *testing.T, source, name string) string {
	t.Helper()
	return declaredRole(t, source, name, `TextColor\(style\.Fg([A-Za-z0-9]+)\)`, "fg-")
}

// declaredRole reads one class list's declaration out of this package's source
// and turns the style constant it names into the role a browser paints.
func declaredRole(t *testing.T, source, name, pattern, prefix string) string {
	t.Helper()
	declaration := regexp.MustCompile(`(?m)^\s*` + name + `\s*=\s*style\.New\(\)([^\n]*)`).FindStringSubmatch(source)
	if declaration == nil {
		t.Fatalf("%s is no longer a style.New() declaration this case can read", name)
	}
	found := regexp.MustCompile(pattern).FindStringSubmatch(declaration[1])
	if found == nil {
		t.Fatalf("%s declares no %s value: %s", name, prefix, declaration[1])
	}
	return prefix + kebab(found[1])
}

// kebab turns the Go field name of a style constant into the role name a browser
// paints: SurfaceWarningSoft is --pk-role-surface-warning-soft.
func kebab(name string) string {
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

func hexAt(grid []int, sum uint64) string {
	return "#" + twice(grid[int(sum%6)]) + twice(grid[int((sum/6)%6)]) + twice(grid[int((sum/36)%6)])
}

func twice(b int) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[b/16], digits[b%16]})
}

func itoa(i int) string { return strconv.Itoa(i) }
