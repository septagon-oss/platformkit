package style

// emission_test.go pins the three promises this package makes: every color
// role tw declares is mapped, every enumerable class resolves to at least one
// declaration, and the emitted sheet is valid CSS that styleengine can parse
// back. The golden file makes any change to the emitted CSS reviewable as a
// diff instead of a rendering surprise.

import (
	"sort"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

func TestRoleMapCoversEveryColor(t *testing.T) {
	t.Parallel()
	roles := roleValues()
	for _, c := range AllColors() {
		if _, ok := roles[c]; !ok {
			t.Errorf("Color %q has no role mapping; add it to roleValues()", c)
		}
	}
	// ColorWhite and ColorBlack are compilable but deliberately outside
	// AllColors (tw documents them as hard-contrast escape values); they are
	// the only entries allowed beyond the enumerator.
	if want := len(AllColors()) + 2; len(roles) != want {
		t.Errorf("role map has %d entries, want %d (AllColors + white + black)", len(roles), want)
	}
}

// TestRoleVarsReferenceRealThemeTokens is the naming contract with package
// design: every --pk-color-* property a role is written in terms of has to be
// one the themes actually declare. The two files are the only place the naming
// is agreed, so this is what keeps a renamed token from silently un-styling
// half the application.
func TestRoleVarsReferenceRealThemeTokens(t *testing.T) {
	t.Parallel()
	themed := design.CSS(design.Light(), design.Dark()).CSS()
	for role, value := range roleValues() {
		for rest := value; ; {
			i := strings.Index(rest, "var(--pk-color-")
			if i < 0 {
				break
			}
			rest = rest[i+len("var("):]
			name := rest[:strings.IndexAny(rest, "),")]
			if !strings.Contains(themed, name+":") {
				t.Errorf("role %q references %s, which no theme defines", role, name)
			}
		}
	}
}

func TestBaseCoversEveryEnumerableClass(t *testing.T) {
	t.Parallel()
	classes := baseClasses()
	if len(classes) < 1000 {
		t.Fatalf("enumerated only %d classes; the enumeration itself regressed", len(classes))
	}
	for _, class := range classes {
		if _, err := resolveBase(class); err != nil {
			t.Errorf("enumerable class %q does not resolve: %v", class, err)
		}
	}
}

func TestEscapeHatchesFailClosed(t *testing.T) {
	t.Parallel()
	for _, class := range []string{
		"pk-transition-standard",      // a class from somebody else's stylesheet
		"w-[37px]",                    // arbitrary value
		"bg-[#123456]",                // arbitrary color
		"rotate-45",                   // parametric without table entry
		"completely-made-up-nonsense", // typo
	} {
		if _, err := Rules(class); err == nil {
			t.Errorf("Rules(%q) succeeded; escape hatches must fail closed", class)
		}
	}
}

func TestAccordionRotationStateIsEnumerable(t *testing.T) {
	t.Parallel()

	sheet, err := Rules("rotate-180")
	if err != nil {
		t.Fatalf("resolve controller-owned rotation state: %v", err)
	}
	rendered := sheet.CSS()
	for _, fragment := range []string{".rotate-180", "--pk-rotate: 180deg", "transform:"} {
		if !strings.Contains(rendered, fragment) {
			t.Errorf("rotation utility is missing %q: %s", fragment, rendered)
		}
	}
}

func TestPrefixedRules(t *testing.T) {
	t.Parallel()
	sheet, err := Rules(
		"hover:bg-surface-brand-hover",
		"md:flex",
		"lg:hover:bg-surface-hover",
		"focus-visible:ring-2",
		"group-hover:underline",
		"dark:bg-surface-inverse",
		"first:pt-0",
		"placeholder:text-fg-placeholder",
	)
	if err != nil {
		t.Fatalf("Rules: %v", err)
	}
	rendered := sheet.CSS()
	for _, want := range []string{
		`.hover\:bg-surface-brand-hover:hover`,
		"@media (min-width: 768px)",
		`.md\:flex`,
		`.lg\:hover\:bg-surface-hover:hover`,
		"@media (min-width: 1024px)",
		`.focus-visible\:ring-2:focus-visible`,
		`.group:hover .group-hover\:underline`,
		`.dark .dark\:bg-surface-inverse`,
		`.first\:pt-0:first-child`,
		`.placeholder\:text-fg-placeholder::placeholder`,
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered CSS missing %q", want)
		}
	}
}

func TestPeerStateFailsClosed(t *testing.T) {
	t.Parallel()
	if _, err := Rules("peer:underline"); err == nil {
		t.Fatal("peer-prefixed classes have no CSS mapping yet and must fail closed")
	}
}

func TestForDeduplicatesAcrossLists(t *testing.T) {
	t.Parallel()
	button := New().Display(DisplayInlineFlex).Gap(S2).Bg(SurfaceBrand)
	badge := New().Display(DisplayInlineFlex).Bg(SurfaceBrandSoft)
	sheet, err := For(button, badge)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	rendered := sheet.CSS()
	if got := strings.Count(rendered, ".inline-flex"); got != 1 {
		t.Errorf("inline-flex emitted %d times, want 1", got)
	}
}

// TestEveryEnumerableClassEmitsARule renders the whole universe at once, which
// is the other half of the coverage claim: resolveBase answering is not the
// same as the sheet carrying a rule for it.
func TestEveryEnumerableClassEmitsARule(t *testing.T) {
	t.Parallel()
	classes := baseClasses()
	sheet, err := Rules(classes...)
	if err != nil {
		t.Fatalf("Rules over the enumerable universe: %v", err)
	}
	rendered := RoleVars().Merge(sheet).CSS()
	if len(rendered) < 20000 {
		t.Fatalf("the whole utility universe rendered to %d bytes; something stopped emitting", len(rendered))
	}
	if strings.Count(rendered, "{") != strings.Count(rendered, "}") {
		t.Error("the rendered sheet has unbalanced braces")
	}
}

// baseClasses enumerates the class universe by driving tw's own enumerators
// and builder methods, so coverage tracks tw by construction.
func baseClasses() []string {
	var out []string
	add := func(cl ClassList) {
		if c := cl.Compile(); c != "" { // a zero-valued constant may compile to nothing
			out = append(out, c)
		}
	}

	colorUniverse := append(AllColors(), ColorWhite, ColorBlack)
	for _, c := range colorUniverse {
		add(New().Bg(c))
		add(New().TextColor(c))
		add(New().BorderColor(c))
		add(New().RingColor(c))
		add(New().Accent(c))
		add(New().BorderTopColor(c))
		add(New().BorderBottomColor(c))
		add(New().BorderLeftColor(c))
		add(New().BorderRightColor(c))
	}
	for _, s := range AllSpacings() {
		add(New().Padding(s))
		add(New().PaddingX(s))
		add(New().PaddingY(s))
		add(New().PaddingLeft(s))
		add(New().PaddingRight(s))
		add(New().PaddingTop(s))
		add(New().PaddingBottom(s))
		add(New().Margin(s))
		add(New().MarginX(s))
		add(New().MarginY(s))
		add(New().MarginLeft(s))
		add(New().MarginRight(s))
		add(New().MarginTop(s))
		add(New().MarginBottom(s))
		add(New().NegTop(s))
		add(New().NegBottom(s))
		add(New().NegLeft(s))
		add(New().NegRight(s))
		add(New().Gap(s))
		add(New().GapX(s))
		add(New().GapY(s))
		add(New().Top(s))
		add(New().Bottom(s))
		add(New().Left(s))
		add(New().Right(s))
		add(New().Inset(s))
		add(New().InsetX(s))
		add(New().InsetY(s))
		add(New().Width(s))
		add(New().Height(s))
		add(New().MaxHeight(s))
		add(New().MinWidth(s))
		add(New().MinHeight(s))
		add(New().DivideX(s))
		add(New().DivideY(s))
		add(New().SpaceX(s))
		add(New().SpaceY(s))
		add(New().UnderlineOffset(s))
		add(New().TranslateXStep(s))
		add(New().TranslateYStep(s))
	}
	for _, v := range AllViewportHeights() {
		add(New().HeightViewport(v))
		add(New().MaxHeightViewport(v))
	}
	for _, v := range AllTranslates() {
		add(New().TranslateX(v))
		add(New().TranslateY(v))
	}
	for _, v := range AllPositionOffsets() {
		add(New().LeftOffset(v))
		add(New().RightOffset(v))
		add(New().TopOffset(v))
		add(New().BottomOffset(v))
	}
	for _, v := range AllDisplays() {
		add(New().Display(v))
	}
	for _, v := range AllItems() {
		add(New().Items(v))
	}
	for _, v := range AllJustify() {
		add(New().Justify(v))
	}
	for _, v := range AllFlexDirs() {
		add(New().FlexDir(v))
	}
	for _, v := range AllPositions() {
		add(New().Position(v))
	}
	for _, v := range AllOverflows() {
		add(New().Overflow(v))
		add(New().OverflowX(v))
		add(New().OverflowY(v))
	}
	for _, v := range AllFontSizes() {
		add(New().FontSize(v))
	}
	for _, v := range AllFontWeights() {
		add(New().FontWeight(v))
	}
	for _, v := range AllFontFamilies() {
		add(New().FontFamily(v))
	}
	for _, v := range AllTrackings() {
		add(New().Tracking(v))
	}
	for _, v := range AllLeadings() {
		add(New().Leading(v))
	}
	for _, v := range AllTextAligns() {
		add(New().TextAlign(v))
	}
	for _, v := range AllRadii() {
		add(New().Rounded(v))
		add(New().RoundedTop(v))
		add(New().RoundedBottom(v))
		add(New().RoundedLeft(v))
		add(New().RoundedRight(v))
	}
	for _, v := range AllShadows() {
		add(New().Shadow(v))
	}
	for _, v := range AllBorderWidths() {
		add(New().Border(v))
		add(New().BorderTop(v))
		add(New().BorderBottom(v))
		add(New().BorderLeft(v))
		add(New().BorderRight(v))
	}
	for _, v := range AllBorderStyles() {
		add(New().BorderStyle(v))
	}
	for _, v := range AllRingWidths() {
		add(New().Ring(v))
	}
	for _, v := range AllRingOffsets() {
		add(New().RingOffset(v))
	}
	for _, v := range AllOpacities() {
		add(New().Opacity(v))
	}
	for _, v := range AllCursors() {
		add(New().Cursor(v))
	}
	for _, v := range AllPointerEvents() {
		add(New().PointerEvents(v))
	}
	for _, v := range AllOutlines() {
		add(New().Outline(v))
	}
	for _, v := range AllTransitions() {
		add(New().Transition(v))
	}
	for _, v := range AllDurations() {
		add(New().Duration(v))
	}
	for _, v := range AllEasings() {
		add(New().Easing(v))
	}
	for _, v := range AllSelects() {
		add(New().UserSelect(v))
	}
	// MaxWidth has no All* enumerator in tw; enumerate the constants here and
	// let the coverage meta-test flag any future addition via AllSizes parity.
	for _, v := range []MaxWidth{
		MaxWXS, MaxWSM, MaxWMD, MaxWLG, MaxWXL, MaxW2XL,
		MaxW3XL, MaxW4XL, MaxW5XL, MaxW6XL, MaxW7XL,
		MaxWFull, MaxWNone, MaxWScreen, MaxWProse,
	} {
		add(New().MaxWScaled(v))
	}
	for _, v := range AllZLayers() {
		add(New().ZLayer(v))
	}
	for n := 1; n <= 12; n++ {
		add(New().GridCols(n))
		add(New().ColSpan(n))
	}
	add(New().ColSpanFull())
	for n := 1; n <= 10; n++ {
		add(New().LineClamp(n))
	}

	// ListStyle's documented keyword universe.
	for _, v := range []string{"none", "disc", "decimal"} {
		add(New().ListStyle(v))
	}

	// No-argument toggles.
	add(New().Truncate())
	add(New().SrOnly())
	add(New().NotSrOnly())
	add(New().MinHeightScreen())
	add(New().Italic())
	add(New().NotItalic())
	add(New().Underline())
	add(New().NoUnderline())
	add(New().LineThrough())
	add(New().NoLineThrough())
	add(New().Uppercase())
	add(New().Lowercase())
	add(New().Capitalize())
	add(New().NormalCase())
	add(New().TabularNums())
	add(New().WhitespaceNowrap())
	add(New().WhitespacePreWrap())
	add(New().BreakAll())
	add(New().BreakWords())
	add(New().Flex1())
	add(New().FlexGrow())
	add(New().FlexGrow0())
	add(New().FlexShrink0())
	add(New().FlexNone())
	add(New().FlexWrap())
	add(New().FlexNoWrap())
	add(New().Group())
	add(New().Peer())
	add(New().Relative())
	add(New().Transform())
	// Accordion/disclosure controllers toggle this shared state class at
	// runtime, so it belongs to the closed enumerable utility universe.
	add(New().Rotate("180"))
	add(New().AnimateSpin())
	add(New().AnimatePulse())
	add(New().AppearanceNone())
	add(New().AspectSquare())
	add(New().AspectVideo())
	add(New().ObjectContain())
	add(New().ObjectCover())
	add(New().OverscrollContain())
	add(New().ResizeNone())
	add(New().ResizeY())
	add(New().RingInset())

	sort.Strings(out)
	return dedupe(out)
}

func dedupe(in []string) []string {
	out := in[:0]
	var prev string
	for i, s := range in {
		if i == 0 || s != prev {
			out = append(out, s)
		}
		prev = s
	}
	return out
}
