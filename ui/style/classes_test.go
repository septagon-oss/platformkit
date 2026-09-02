package style_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/style"
)

func TestNewIsEmpty(t *testing.T) {
	if !style.New().IsEmpty() {
		t.Fatal("New() should be empty")
	}
	if style.New().Compile() != "" {
		t.Fatal("New().Compile() should be empty string")
	}
}

func TestBasicChaining(t *testing.T) {
	got := style.New().
		Display(style.DisplayInlineFlex).
		Items(style.ItemsCenter).
		Gap(style.S2).
		Rounded(style.RadiusXL).
		FontWeight(style.FontSemibold).
		Compile()

	want := "inline-flex items-center gap-2 rounded-xl font-semibold"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestMaxHeightUsesGovernedSpacing(t *testing.T) {
	if got, want := style.New().MaxHeight(style.S60).Compile(), "max-h-60"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestColorRoles(t *testing.T) {
	cases := []struct {
		name string
		list style.ClassList
		want string
	}{
		{"bg", style.New().Bg(style.SurfacePrimary), "bg-surface-primary"},
		{"text", style.New().TextColor(style.FgPrimary), "text-fg-primary"},
		{"border-color", style.New().BorderColor(style.BorderPrimary), "border-border-primary"},
		{"ring-color", style.New().RingColor(style.RingBrand), "ring-ring-brand"},
		{"bg-transparent", style.New().Bg(style.ColorTransparent), "bg-transparent"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.list.Compile(); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestSpacingUtilities(t *testing.T) {
	cases := []struct {
		name string
		list style.ClassList
		want string
	}{
		{"px-3.5", style.New().PaddingX(style.S3_5), "px-3.5"},
		{"py-2.5", style.New().PaddingY(style.S2_5), "py-2.5"},
		{"p-4", style.New().Padding(style.S4), "p-4"},
		{"mx-auto", style.New().MarginX(style.SAuto), "mx-auto"},
		{"gap-2", style.New().Gap(style.S2), "gap-2"},
		{"w-full", style.New().Width(style.SFull), "w-full"},
		{"min-h-11", style.New().MinHeight(style.S11), "min-h-11"},
		{"max-h-85vh", style.New().MaxHeightViewport(style.VH85), "max-h-[85vh]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.list.Compile(); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestZLayer(t *testing.T) {
	cases := []struct {
		layer style.ZLayer
		want  string
	}{
		{style.ZBelow, "-z-[10]"},
		{style.ZBase, "z-[0]"},
		{style.ZModal, "z-[1400]"},
		{style.ZPopover, "z-[1500]"},
		{style.ZTooltip, "z-[1700]"},
	}
	for _, c := range cases {
		t.Run(c.layer.String(), func(t *testing.T) {
			if got := c.layer.Class(); got != c.want {
				t.Fatalf("Class() got %q, want %q", got, c.want)
			}
			if got := style.New().ZLayer(c.layer).Compile(); got != c.want {
				t.Fatalf("compile got %q, want %q", got, c.want)
			}
		})
	}
}

func TestStatePrefix(t *testing.T) {
	got := style.New().
		Bg(style.SurfacePrimary).
		On(style.StateHover, func(c style.ClassList) style.ClassList {
			return c.Bg(style.SurfaceHover).TranslateY(style.TranslateNeg05)
		}).
		Compile()
	want := "bg-surface-primary hover:bg-surface-hover hover:-translate-y-0.5"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFractionalTranslateAndPositionOffsets(t *testing.T) {
	got := style.New().
		TranslateX(style.TranslateNegHalf).
		TranslateY(style.TranslateHalf).
		LeftOffset(style.PositionHalf).
		TopOffset(style.PositionHalf).
		Compile()
	want := "-translate-x-1/2 translate-y-1/2 left-1/2 top-1/2"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFocusVisibleRing(t *testing.T) {
	got := style.New().
		On(style.StateFocusVisible, func(c style.ClassList) style.ClassList {
			return c.Outline(style.OutlineNone).
				Ring(style.Ring2).
				RingColor(style.RingBrand).
				RingOffset(style.RingOffset2)
		}).
		Compile()
	want := "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring-brand focus-visible:ring-offset-2"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNestedPrefixStack(t *testing.T) {
	// sm: + hover: should combine as "sm:hover:..."
	got := style.New().
		Breakpoint(style.BreakpointSM, func(c style.ClassList) style.ClassList {
			return c.On(style.StateHover, func(c2 style.ClassList) style.ClassList {
				return c2.Bg(style.SurfaceBrand)
			})
		}).
		Compile()
	want := "sm:hover:bg-surface-brand"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestMerge(t *testing.T) {
	base := style.New().Display(style.DisplayFlex).Items(style.ItemsCenter)
	addition := style.New().Padding(style.S4).Rounded(style.RadiusLG)
	got := base.Merge(addition).Compile()
	want := "flex items-center p-4 rounded-lg"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestImmutability(t *testing.T) {
	base := style.New().Display(style.DisplayFlex)
	branch1 := base.Items(style.ItemsCenter)
	branch2 := base.Items(style.ItemsStart)

	if got := branch1.Compile(); got != "flex items-center" {
		t.Fatalf("branch1: got %q", got)
	}
	if got := branch2.Compile(); got != "flex items-start" {
		t.Fatalf("branch2: got %q", got)
	}
	if got := base.Compile(); got != "flex" {
		t.Fatalf("base leaked: got %q", got)
	}
}

func TestRawEscapeHatch(t *testing.T) {
	got := style.New().Raw("custom-class").Compile()
	if got != "custom-class" {
		t.Fatalf("got %q, want %q", got, "custom-class")
	}
	// Empty raw is a no-op.
	if style.New().Raw("   ").Compile() != "" {
		t.Fatal("empty-ish Raw should be a no-op")
	}
}

func TestRawMultipleClassesRespectPrefix(t *testing.T) {
	got := style.New().On(style.StateHover, func(c style.ClassList) style.ClassList {
		return c.Raw("opacity-80 scale-105")
	}).Compile()
	want := "hover:opacity-80 hover:scale-105"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEmptyValuesAreNoOps(t *testing.T) {
	got := style.New().
		Bg(""). // zero Color
		PaddingX("").
		FontWeight("").
		Compile()
	if got != "" {
		t.Fatalf("empty-value chain should compile to empty, got %q", got)
	}
}

func TestCompileIsDeterministic(t *testing.T) {
	build := func() string {
		return style.New().
			Bg(style.SurfacePrimary).
			PaddingX(style.S4).
			Rounded(style.RadiusLG).
			On(style.StateHover, func(c style.ClassList) style.ClassList {
				return c.Bg(style.SurfaceHover)
			}).
			Compile()
	}
	first := build()
	for i := range 50 {
		if got := build(); got != first {
			t.Fatalf("iteration %d: got %q, want %q", i, got, first)
		}
	}
}

func TestColorCoverageInCompileTables(t *testing.T) {
	// Every Color const that appears in a known role map must be
	// compilable via that role. We verify the Surface + Foreground +
	// Border + Ring roles each handle their canonical slice.
	surfaces := []style.Color{
		style.SurfacePrimary, style.SurfaceSecondary, style.SurfaceTertiary,
		style.SurfaceBrand, style.SurfaceBrandHover, style.SurfaceBrandSoft,
		style.SurfaceSuccess, style.SurfaceSuccessSoft,
		style.SurfaceWarning, style.SurfaceWarningSoft,
		style.SurfaceDanger, style.SurfaceDangerSoft,
		style.SurfaceInfo, style.SurfaceInfoSoft,
		style.SurfaceDisabled, style.SurfaceHover, style.SurfaceActive,
		style.SurfaceOverlay, style.SurfaceInverse,
	}
	for _, c := range surfaces {
		out := style.New().Bg(c).Compile()
		want := fmt.Sprintf("bg-%s", c)
		if out != want {
			t.Fatalf("bg(%q): got %q, want %q", c, out, want)
		}
	}

	fg := []style.Color{
		style.FgPrimary, style.FgSecondary, style.FgTertiary, style.FgMuted,
		style.FgBrand, style.FgOnBrand,
		style.FgSuccess, style.FgWarning, style.FgDanger, style.FgInfo,
		style.FgDisabled, style.FgOnSurface, style.FgOnInverse,
		style.FgLink, style.FgLinkHover,
	}
	for _, c := range fg {
		out := style.New().TextColor(c).Compile()
		want := fmt.Sprintf("text-%s", c)
		if out != want {
			t.Fatalf("text(%q): got %q, want %q", c, out, want)
		}
	}

	borders := []style.Color{
		style.BorderPrimary, style.BorderSecondary, style.BorderBrand,
		style.BorderSuccess, style.BorderWarning, style.BorderDanger, style.BorderInfo,
	}
	for _, c := range borders {
		out := style.New().BorderColor(c).Compile()
		want := fmt.Sprintf("border-%s", c)
		if out != want {
			t.Fatalf("border(%q): got %q, want %q", c, out, want)
		}
	}

	rings := []style.Color{style.RingBrand, style.RingFocus, style.RingDanger}
	for _, c := range rings {
		out := style.New().RingColor(c).Compile()
		want := fmt.Sprintf("ring-%s", c)
		if out != want {
			t.Fatalf("ring(%q): got %q, want %q", c, out, want)
		}
	}
}

func TestAllSpacingsHaveRoles(t *testing.T) {
	for _, s := range style.AllSpacings() {
		// Numeric / fractional steps support all roles.
		// SFull/SAuto may not make sense for some roles but must not panic.
		_ = style.New().PaddingX(s).Compile()
		_ = style.New().Margin(s).Compile()
		_ = style.New().Width(s).Compile()
		_ = style.New().Gap(s).Compile()
	}
}

func TestNoRedundantSpaces(t *testing.T) {
	got := style.New().
		Display(style.DisplayFlex).
		Items("").
		Padding("").
		FontWeight(style.FontSemibold).
		Compile()
	if strings.Contains(got, "  ") {
		t.Fatalf("output contains double space: %q", got)
	}
}
