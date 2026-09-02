package style

// This file contains the single source of truth for mapping the
// package's typed constants to Tailwind utility fragments.
//
// Every literal Tailwind class string used by the builder lives here.
// Callers and other packages build ClassList values using the typed
// API; the compiled output is produced exclusively from the tables
// below. This design makes it trivial to audit or mechanically
// enforce "no raw utility strings" policies in a larger codebase.

const (
	classPrefixMinH       = "min-h-"
	classPrefixRounded    = "rounded-"
	classPrefixBorder     = "border-"
	classPrefixTranslateY = "translate-y-"
	classPrefixTranslateX = "translate-x-"
)

// prefixOrEmpty returns prefix+v when v is non-empty, or "" when v is empty.
// Shared by multiple class* helper functions to eliminate duplicated empty-check + prefix patterns.
func prefixOrEmpty(v, prefix string) string {
	if v == "" {
		return ""
	}
	return prefix + v
}

// classBg maps a Color const to its "bg-*" Tailwind class.
//
// Every non-zero Color yields a "bg-<semantic-name>" class. A previous
// partial switch only covered a subset (mainly surface + a handful of
// fg colors) which meant tw.Bg(FgDisabled), tw.Bg(FgSuccess), etc.
// silently returned empty — leading to dot-indicator slots rendering
// with no background. The uniform `bg-` + string() mapping below makes
// Bg() total over the Color domain, matching classText's behaviour.
func classBg(c Color) string {
	if c == "" {
		return ""
	}
	if c == ColorTransparent {
		return "bg-transparent"
	}
	return "bg-" + string(c)
}

// classText maps a Color const to its "text-*" Tailwind class.
// Any semantic color may be applied as a text color when the design
// treats it as such (e.g., using surface-brand as the tick color on a
// checkbox). Surface/Border/Ring colors are accepted in addition to
// the Fg family.
func classText(c Color) string {
	if c == "" {
		return ""
	}
	if c == ColorTransparent {
		return "text-transparent"
	}
	return "text-" + string(c)
}

// classBorder maps a Color const to its "border-*" Tailwind class.
func classBorder(c Color) string {
	switch c {
	case ColorTransparent:
		return "border-transparent"
	case BorderPrimary:
		return "border-border-primary"
	case BorderSecondary:
		return "border-border-secondary"
	case BorderBrand:
		return "border-border-brand"
	case BorderSuccess:
		return "border-border-success"
	case BorderWarning:
		return "border-border-warning"
	case BorderDanger:
		return "border-border-danger"
	case BorderInfo:
		return "border-border-info"
	case SurfaceBrand:
		return "border-surface-brand"
	}
	return ""
}

// classRing maps a Color const to its "ring-*" Tailwind class.
// Any semantic color may be used as a ring color; the named Ring*
// constants (RingBrand/RingFocus/RingDanger) are the canonical
// focus-ring palette, but Border* and other colors are accepted when
// the design uses a non-focus ring.
func classRing(c Color) string {
	if c == "" {
		return ""
	}
	switch c {
	case RingBrand:
		return "ring-ring-brand"
	case RingFocus:
		return "ring-ring-focus"
	case RingDanger:
		return "ring-ring-danger"
	}
	return "ring-" + string(c)
}

// classPaddingX maps Spacing → "px-*".
func classPaddingX(s Spacing) string {
	if s == "" {
		return ""
	}
	return "px-" + string(s)
}

// classPaddingY maps Spacing → "py-*".
func classPaddingY(s Spacing) string {
	if s == "" {
		return ""
	}
	return "py-" + string(s)
}

// classPadding maps Spacing → "p-*".
func classPadding(s Spacing) string {
	if s == "" {
		return ""
	}
	return "p-" + string(s)
}

// classMarginX, classMarginY, classMargin, and per-side variants.
func classMarginX(s Spacing) string {
	if s == "" {
		return ""
	}
	return "mx-" + string(s)
}

func classMarginY(s Spacing) string {
	if s == "" {
		return ""
	}
	return "my-" + string(s)
}

func classMargin(s Spacing) string {
	if s == "" {
		return ""
	}
	return "m-" + string(s)
}

func classMarginLeft(s Spacing) string {
	if s == "" {
		return ""
	}
	return "ml-" + string(s)
}

func classMarginRight(s Spacing) string {
	if s == "" {
		return ""
	}
	return "mr-" + string(s)
}

func classMarginTop(s Spacing) string {
	if s == "" {
		return ""
	}
	return "mt-" + string(s)
}

func classMarginBottom(s Spacing) string {
	if s == "" {
		return ""
	}
	return "mb-" + string(s)
}

func classPaddingLeft(s Spacing) string {
	if s == "" {
		return ""
	}
	return "pl-" + string(s)
}

func classPaddingRight(s Spacing) string {
	if s == "" {
		return ""
	}
	return "pr-" + string(s)
}

func classPaddingTop(s Spacing) string {
	if s == "" {
		return ""
	}
	return "pt-" + string(s)
}

func classPaddingBottom(s Spacing) string {
	if s == "" {
		return ""
	}
	return "pb-" + string(s)
}

// Absolute-position offset helpers. Used when positioning absolute
// children (icons, badges, close buttons) inside a relative parent.
func classTop(s Spacing) string {
	if s == "" {
		return ""
	}
	return "top-" + string(s)
}

func classBottom(s Spacing) string {
	if s == "" {
		return ""
	}
	return "bottom-" + string(s)
}

func classLeft(s Spacing) string {
	if s == "" {
		return ""
	}
	return "left-" + string(s)
}

func classRight(s Spacing) string {
	if s == "" {
		return ""
	}
	return "right-" + string(s)
}

func classInset(s Spacing) string {
	if s == "" {
		return ""
	}
	return "inset-" + string(s)
}

func classInsetX(s Spacing) string {
	if s == "" {
		return ""
	}
	return "inset-x-" + string(s)
}

func classInsetY(s Spacing) string {
	if s == "" {
		return ""
	}
	return "inset-y-" + string(s)
}

// classGap maps Spacing → "gap-*".
func classGap(s Spacing) string {
	if s == "" {
		return ""
	}
	return "gap-" + string(s)
}

// classGapX / classGapY emit axis-specific gap utilities.
func classGapX(s Spacing) string {
	if s == "" {
		return ""
	}
	return "gap-x-" + string(s)
}

func classGapY(s Spacing) string {
	if s == "" {
		return ""
	}
	return "gap-y-" + string(s)
}

// classGridColsRaw emits "grid-cols-<raw>" for arbitrary-value grid
// templates like "[auto_1fr]".
func classGridColsRaw(raw string) string {
	if raw == "" {
		return ""
	}
	return "grid-cols-" + raw
}

// classWidth maps Spacing → "w-*".
func classWidth(s Spacing) string {
	if s == "" {
		return ""
	}
	return "w-" + string(s)
}

// classHeight maps Spacing → "h-*".
func classHeight(s Spacing) string {
	if s == "" {
		return ""
	}
	return "h-" + string(s)
}

func classHeightViewport(v ViewportHeight) string {
	if v == "" {
		return ""
	}
	return "h-[" + string(v) + "vh]"
}

func classMaxHeightViewport(v ViewportHeight) string {
	if v == "" {
		return ""
	}
	return "max-h-[" + string(v) + "vh]"
}

// classMinWidth maps Spacing → "min-w-*".
func classMinWidth(s Spacing) string {
	if s == "" {
		return ""
	}
	return "min-w-" + string(s)
}

// classMinHeight maps Spacing → "min-h-*".
func classMinHeight(s Spacing) string {
	if s == "" {
		return ""
	}
	return classPrefixMinH + string(s)
}

// classRounded maps Radius → "rounded-*".
func classRounded(r Radius) string {
	if r == "" {
		return ""
	}
	if r == RadiusBase {
		return "rounded"
	}
	return classPrefixRounded + string(r)
}

// classRoundedSide emits a per-side border-radius utility,
// e.g. "rounded-t", "rounded-l-lg". Side is one of "t"/"b"/"l"/"r".
func classRoundedSide(side string, r Radius) string {
	if side == "" {
		return classRounded(r)
	}
	if r == "" {
		return ""
	}
	if r == RadiusBase {
		return classPrefixRounded + side
	}
	return classPrefixRounded + side + "-" + string(r)
}

// classShadow maps Shadow → "shadow-*".
func classShadow(sh Shadow) string {
	if sh == "" {
		return ""
	}
	if sh == ShadowBase {
		return "shadow"
	}
	return "shadow-" + string(sh)
}

// classDisplay maps Display → display class.
func classDisplay(d Display) string {
	if d == "" {
		return ""
	}
	return string(d)
}

// classItems maps Items → "items-*".
func classItems(i Items) string {
	if i == "" {
		return ""
	}
	return "items-" + string(i)
}

// classJustify maps Justify → "justify-*".
func classJustify(j Justify) string {
	if j == "" {
		return ""
	}
	return "justify-" + string(j)
}

// classFlexDir maps FlexDir → flex direction.
func classFlexDir(f FlexDir) string {
	if f == "" {
		return ""
	}
	return "flex-" + string(f)
}

// classPosition maps Position → position class.
func classPosition(p Position) string {
	if p == "" {
		return ""
	}
	return string(p)
}

// classFontSize maps FontSize → "text-*".
func classFontSize(f FontSize) string {
	if f == "" {
		return ""
	}
	return "text-" + string(f)
}

// classFontWeight maps FontWeight → "font-*".
func classFontWeight(f FontWeight) string {
	if f == "" {
		return ""
	}
	return "font-" + string(f)
}

// classFontFamily maps FontFamily → "font-*" (sans/serif/mono).
func classFontFamily(f FontFamily) string {
	if f == "" {
		return ""
	}
	return "font-" + string(f)
}

// classTracking maps Tracking → "tracking-*".
func classTracking(t Tracking) string {
	if t == "" {
		return ""
	}
	return "tracking-" + string(t)
}

// classLeading maps Leading → "leading-*".
func classLeading(l Leading) string {
	if l == "" {
		return ""
	}
	return "leading-" + string(l)
}

// classTextAlign maps TextAlign → "text-*".
func classTextAlign(t TextAlign) string {
	if t == "" {
		return ""
	}
	return "text-" + string(t)
}

// classTransition maps Transition → "transition-*".
func classTransition(t Transition) string {
	if t == "" {
		return ""
	}
	if t == TransitionAll {
		return "transition-all"
	}
	return "transition-" + string(t)
}

// classDuration maps Duration → "duration-*".
func classDuration(d Duration) string {
	if d == "" {
		return ""
	}
	return "duration-" + string(d)
}

// classEasing maps Easing → "ease-*".
func classEasing(e Easing) string {
	if e == "" {
		return ""
	}
	return "ease-" + string(e)
}

// classBorderWidth maps BorderWidth → "border-*".
func classBorderWidth(b BorderWidth) string {
	if b == Border1 {
		return "border"
	}
	return classPrefixBorder + string(b)
}

// classBorderSide emits a side-specific border width utility,
// e.g. "border-t", "border-l-2".
func classBorderSide(side string, b BorderWidth) string {
	if side == "" {
		return classBorderWidth(b)
	}
	if b == Border1 {
		return classPrefixBorder + side
	}
	return classPrefixBorder + side + "-" + string(b)
}

// classBorderSideColor emits a per-side border-color utility,
// e.g. "border-t-white", "border-l-border-primary".
func classBorderSideColor(side string, c Color) string {
	if side == "" || c == "" {
		return ""
	}
	return classPrefixBorder + side + "-" + string(c)
}

// classRingWidth maps RingWidth → "ring-*". Every width emits
// "ring-<N>" — including Ring1 which emits "ring-1". (Tailwind's bare
// "ring" utility defaults to 3px, which is different from Ring1.)
func classRingWidth(r RingWidth) string {
	if r == "" {
		return ""
	}
	return "ring-" + string(r)
}

// classRingOffset maps RingOffset → "ring-offset-*".
func classRingOffset(r RingOffset) string {
	if r == "" {
		return ""
	}
	return "ring-offset-" + string(r)
}

// classOpacity maps Opacity → "opacity-*".
func classOpacity(o Opacity) string {
	if o == "" {
		return ""
	}
	return "opacity-" + string(o)
}

// classCursor maps Cursor → "cursor-*".
func classCursor(c Cursor) string {
	if c == "" {
		return ""
	}
	return "cursor-" + string(c)
}

// classPointerEvents maps PointerEvents → "pointer-events-*".
func classPointerEvents(p PointerEvents) string {
	if p == "" {
		return ""
	}
	return "pointer-events-" + string(p)
}

// classOutline maps Outline → "outline-*".
func classOutline(o Outline) string {
	if o == "" {
		return ""
	}
	return "outline-" + string(o)
}

// classBorderStyle maps BorderStyle → "border-<style>".
func classBorderStyle(b BorderStyle) string {
	if b == "" {
		return ""
	}
	return classPrefixBorder + string(b)
}

// classResize maps ResizeMode → "resize" / "resize-*".
// ResizeModeBoth emits bare "resize" (Tailwind's default resize
// utility); all other values use the "resize-<mode>" form.
func classResize(m ResizeMode) string {
	if m == "" {
		return ""
	}
	if m == ResizeModeBoth {
		return "resize"
	}
	return "resize-" + string(m)
}

// classSelect maps Select → "select-*".
func classSelect(s Select) string {
	if s == "" {
		return ""
	}
	return "select-" + string(s)
}

// classOverflow maps Overflow → "overflow-*".
func classOverflow(o Overflow) string {
	if o == "" {
		return ""
	}
	return "overflow-" + string(o)
}

// classTranslateY maps Translate → "translate-y-*" or "-translate-y-*".
func classTranslateY(t Translate) string {
	if t == "" {
		return ""
	}
	switch t {
	case TranslateNeg05:
		return "-translate-y-0.5"
	case TranslateNegHalf:
		return "-translate-y-1/2"
	case TranslateNeg1:
		return "-translate-y-1"
	case TranslateNeg2:
		return "-translate-y-2"
	default:
		return classPrefixTranslateY + string(t)
	}
}

// classTranslateX maps Translate → "translate-x-*" or "-translate-x-*".
func classTranslateX(t Translate) string {
	if t == "" {
		return ""
	}
	switch t {
	case TranslateNeg05:
		return "-translate-x-0.5"
	case TranslateNegHalf:
		return "-translate-x-1/2"
	case TranslateNeg1:
		return "-translate-x-1"
	case TranslateNeg2:
		return "-translate-x-2"
	default:
		return classPrefixTranslateX + string(t)
	}
}

// Static utility atoms. Exported names so ClassList methods reference
// these rather than inlining strings.

const (
	classUnderline   = "underline"
	classNoUnderline = "no-underline"
	classSrOnly      = "sr-only"
	classTruncate    = "truncate"
	classRelative    = "relative"
	classAntialiased = "antialiased"
	classFlexShrink0 = "flex-shrink-0"
	classTransform   = "transform"
	classFlex1       = "flex-1"
	classRingInset   = "ring-inset"
	classFlexWrap    = "flex-wrap"
	classFlexNoWrap  = "flex-nowrap"
	classUppercase   = "uppercase"
	classLowercase   = "lowercase"
	classCapitalize  = "capitalize"
	classNormalCase  = "normal-case"
)

// classUnderlineOffset emits "underline-offset-<step>".
func classUnderlineOffset(step string) string {
	if step == "" {
		return ""
	}
	return "underline-offset-" + step
}

// classTranslateXSpacing / classTranslateYSpacing emit translate-x-<Spacing>
// using the Spacing scale. Distinct from classTranslateX (which uses the
// Translate enum with its neg-* prefixes) — these are for forward offsets
// driven by tokens.
func classTranslateXSpacing(s Spacing) string {
	if s == "" {
		return ""
	}
	return classPrefixTranslateX + string(s)
}

func classTranslateYSpacing(s Spacing) string {
	if s == "" {
		return ""
	}
	return classPrefixTranslateY + string(s)
}

// classSpaceX / classSpaceY emit "space-x-<Spacing>" / "space-y-<Spacing>"
// — Tailwind's adjacent-sibling spacing utility.
func classSpaceX(s Spacing) string {
	if s == "" {
		return ""
	}
	return "space-x-" + string(s)
}

func classSpaceY(s Spacing) string {
	if s == "" {
		return ""
	}
	return "space-y-" + string(s)
}

func classMaxW(v string) string { return prefixOrEmpty(v, "max-w-") }

func classMinH(v string) string { return prefixOrEmpty(v, classPrefixMinH) }

func classMaxH(v string) string { return prefixOrEmpty(v, "max-h-") }

// classNegMargin emits "-m{side}-{step}" — negative margin on one side.
// Side is one of: "t"/"b"/"l"/"r"/"x"/"y"/"" (all).
func classNegMargin(side string, s Spacing) string {
	if s == "" {
		return ""
	}
	if side == "" {
		return "-m-" + string(s)
	}
	return "-m" + side + "-" + string(s)
}

// classRotate emits "rotate-<deg>" or "-rotate-<deg>".
func classRotate(deg string) string {
	if deg == "" {
		return ""
	}
	return "rotate-" + deg
}

// classBackdropBlur emits "backdrop-blur-<size>".
func classBackdropBlur(size string) string {
	if size == "" {
		return ""
	}
	return "backdrop-blur-" + size
}

// classOrigin emits "origin-<side>" for transform-origin.
func classOrigin(side string) string {
	if side == "" {
		return ""
	}
	return "origin-" + side
}

// classOverflowX / classOverflowY emit axis-specific overflow.
func classOverflowX(o Overflow) string {
	if o == "" {
		return ""
	}
	return "overflow-x-" + string(o)
}

func classOverflowY(o Overflow) string {
	if o == "" {
		return ""
	}
	return "overflow-y-" + string(o)
}

// classObjectFit emits "object-<fit>".
func classObjectFit(fit string) string {
	if fit == "" {
		return ""
	}
	return "object-" + fit
}

// classDivideX / classDivideY emit "divide-x-<step>" / "divide-y-<step>"
// for borders between adjacent children.
func classDivideX(s Spacing) string {
	if s == "" {
		return "divide-x"
	}
	return "divide-x-" + string(s)
}

func classDivideY(s Spacing) string {
	if s == "" {
		return "divide-y"
	}
	return "divide-y-" + string(s)
}

// classListStyle emits "list-<style>" (decimal/disc/none).
func classListStyle(style string) string {
	if style == "" {
		return ""
	}
	return "list-" + style
}

// classBgWithOpacity emits "bg-<color>/<opacity>" for alpha-blended
// backgrounds (e.g., "bg-surface-overlay/75").
func classBgWithOpacity(c Color, opacity string) string {
	if c == "" || opacity == "" {
		return ""
	}
	return "bg-" + string(c) + "/" + opacity
}

// classTextWithOpacity emits "text-<color>/<opacity>" for alpha-blended
// text colors (e.g., "text-white/80").
func classTextWithOpacity(c Color, opacity string) string {
	if c == "" || opacity == "" {
		return ""
	}
	return "text-" + string(c) + "/" + opacity
}

// classBorderWithOpacity emits "border-<color>/<opacity>" for
// alpha-blended border colors (e.g., "border-white/30").
func classBorderWithOpacity(c Color, opacity string) string {
	if c == "" || opacity == "" {
		return ""
	}
	return classPrefixBorder + string(c) + "/" + opacity
}

// classAspectRaw emits "aspect-[<raw>]" for arbitrary-value aspect
// ratios like "4/3".
func classAspectRaw(raw string) string {
	if raw == "" {
		return ""
	}
	return "aspect-[" + raw + "]"
}

// classTranslateXRaw / classTranslateYRaw emit translate-* / -translate-*
// with a raw suffix like "1/2" for fractional offsets.
func classTranslateXRaw(raw string) string {
	if raw == "" {
		return ""
	}
	return classPrefixTranslateX + raw
}

func classTranslateYRaw(raw string) string {
	if raw == "" {
		return ""
	}
	return classPrefixTranslateY + raw
}

func classNegTranslateX(raw string) string {
	if raw == "" {
		return ""
	}
	return "-translate-x-" + raw
}

func classNegTranslateY(raw string) string {
	if raw == "" {
		return ""
	}
	return "-translate-y-" + raw
}

// Static additional atoms used 3+ times across components.
const (
	classItalic            = "italic"
	classNotItalic         = "not-italic"
	classAppearanceNone    = "appearance-none"
	classWhitespaceNoWrap  = "whitespace-nowrap"
	classWhitespacePreWrap = "whitespace-pre-wrap"
	classBreakWords        = "break-words"
	classBreakAll          = "break-all"
	classTabularNums       = "tabular-nums"
	classObjectCover       = "object-cover"
	classObjectContain     = "object-contain"
	classAspectSquare      = "aspect-square"
	classAspectVideo       = "aspect-video"
	classAnimatePulse      = "animate-pulse"
	classAnimateSpin       = "animate-spin"
	classLineThrough       = "line-through"
	classNoLineThrough     = "no-line-through"
)

// classGridCols emits "grid-cols-N" for a CSS grid template. N must be
// positive; 0 or negative → empty (the typed tables and All* enumerators flags it).
func classGridCols(n int) string {
	if n <= 0 {
		return ""
	}
	return "grid-cols-" + itoa(n)
}

// classNegTop / classNegRight / classNegBottom / classNegLeft emit
// "-top-<step>" / "-right-<step>" / etc. — negative positional
// offsets on the Spacing scale. Empty Spacing → empty string.
func classNegTop(s Spacing) string {
	if s == "" {
		return ""
	}
	return "-top-" + string(s)
}

func classNegRight(s Spacing) string {
	if s == "" {
		return ""
	}
	return "-right-" + string(s)
}

func classNegBottom(s Spacing) string {
	if s == "" {
		return ""
	}
	return "-bottom-" + string(s)
}

func classNegLeft(s Spacing) string {
	if s == "" {
		return ""
	}
	return "-left-" + string(s)
}

// classLeftRaw / classRightRaw / classTopRaw / classBottomRaw emit
// fractional or arbitrary-value positional offsets (e.g., "left-1/2",
// "top-full") not expressible via the Spacing scale.
func classLeftRaw(v string) string {
	if v == "" {
		return ""
	}
	return "left-" + v
}

func classRightRaw(v string) string {
	if v == "" {
		return ""
	}
	return "right-" + v
}

func classTopRaw(v string) string {
	if v == "" {
		return ""
	}
	return "top-" + v
}

func classBottomRaw(v string) string {
	if v == "" {
		return ""
	}
	return "bottom-" + v
}

func classMinWidthRaw(v string) string { return prefixOrEmpty(v, "min-w-") }

func classMaxWidthRaw(v string) string { return prefixOrEmpty(v, "max-w-") }

func classMinHeightRaw(v string) string { return prefixOrEmpty(v, classPrefixMinH) }

func classMaxHeightRaw(v string) string { return prefixOrEmpty(v, "max-h-") }

// classWidthRaw emits "w-<v>" for an arbitrary-value width.
func classWidthRaw(v string) string {
	if v == "" {
		return ""
	}
	return "w-" + v
}

// classHeightRaw emits "h-<v>" for an arbitrary-value height.
func classHeightRaw(v string) string {
	if v == "" {
		return ""
	}
	return "h-" + v
}

// classPaddingTopRaw emits "pt-<v>" for an arbitrary-value top padding.
func classPaddingTopRaw(v string) string {
	if v == "" {
		return ""
	}
	return "pt-" + v
}

// classRoundedRaw emits "rounded-<v>" for an arbitrary-value radius.
func classRoundedRaw(v string) string {
	if v == "" {
		return ""
	}
	return classPrefixRounded + v
}

// classColSpan emits "col-span-N" for a grid column span.
func classColSpan(n int) string {
	if n <= 0 {
		return ""
	}
	return "col-span-" + itoa(n)
}

// classColSpanFull is Tailwind's "col-span-full" — span every column
// of the current grid regardless of column count.
const classColSpanFull = "col-span-full"

// classLineClamp emits "line-clamp-N" for a multi-line text clamp.
func classLineClamp(n int) string {
	if n <= 0 {
		return ""
	}
	return "line-clamp-" + itoa(n)
}

// classAccent emits "accent-<color>" for native form chrome tinting.
func classAccent(c Color) string {
	if c == "" {
		return ""
	}
	return "accent-" + string(c)
}

// itoa converts a non-negative int to its decimal string without
// pulling in strconv at the Tailwind-compile layer. The value is
// expected to be a small grid column count.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [10]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}

const (
	classFlexGrow          = "flex-grow"
	classFlexGrow0         = "flex-grow-0"
	classGroup             = "group"
	classPeer              = "peer"
	classWhitespacePre     = "whitespace-pre"
	classOverscrollContain = "overscroll-contain"
	// classContainer is Tailwind's responsive container utility — an
	// element whose max-width steps through the configured breakpoints.
	// Emitted verbatim; callers typically combine it with MarginX(SAuto)
	// for horizontal centering.
	classContainer = "container"
)

// classZIndexRaw emits "z-<raw>" for legacy z-index values ("40",
// "50", "auto") that don't map onto the typed ZLayer semantic scale.
func classZIndexRaw(raw string) string {
	if raw == "" {
		return ""
	}
	return "z-" + raw
}

// classFlexNone is Tailwind's "flex-none" atom — prevents a flex
// child from growing or shrinking.
const classFlexNone = "flex-none"
