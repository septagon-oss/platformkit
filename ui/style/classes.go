package style

import "strings"

// ClassList is an immutable, composable typed builder for Tailwind
// class strings. Every method returns a new ClassList; the builder
// itself holds no resolved strings until Compile() is called.
//
// The IR is a flat slice of segments. A segment is either:
//   - A utility class (resolved at append time, already a string)
//   - A prefix frame (opens a group like "hover:") and its end marker
//
// This flat design lets Compile() walk the segments once, tracking the
// active prefix stack, and emit the final space-separated output in
// O(n).
type ClassList struct {
	segments []segment
}

// segment is one item in the IR.
type segment struct {
	kind   segKind
	prefix string // for sgOpenPrefix: the Tailwind prefix (e.g., "hover:")
	class  string // for sgClass: the resolved Tailwind utility
}

type segKind uint8

const (
	sgClass       segKind = iota // a resolved utility class string
	sgOpenPrefix                 // opens a prefix frame (state / breakpoint)
	sgClosePrefix                // closes a prefix frame
)

// New returns an empty ClassList.
func New() ClassList {
	return ClassList{}
}

// clone returns a shallow copy of cl with cap for one more append.
// All mutators use this to preserve the immutable/builder semantics.
func (cl ClassList) clone(extra int) ClassList {
	out := make([]segment, len(cl.segments), len(cl.segments)+extra)
	copy(out, cl.segments)
	return ClassList{segments: out}
}

// append adds a resolved utility class if it is non-empty.
func (cl ClassList) append(class string) ClassList {
	if class == "" {
		return cl
	}
	out := cl.clone(1)
	out.segments = append(out.segments, segment{kind: sgClass, class: class})
	return out
}

// ---------------------------------------------------------------------
// Layout
// ---------------------------------------------------------------------

// Display sets the CSS display mode (flex, grid, block, etc.).
func (cl ClassList) Display(d Display) ClassList { return cl.append(classDisplay(d)) }

// Items sets align-items.
func (cl ClassList) Items(i Items) ClassList { return cl.append(classItems(i)) }

// Justify sets justify-content.
func (cl ClassList) Justify(j Justify) ClassList { return cl.append(classJustify(j)) }

// FlexDir sets flex-direction.
func (cl ClassList) FlexDir(f FlexDir) ClassList { return cl.append(classFlexDir(f)) }

// Position sets CSS position.
func (cl ClassList) Position(p Position) ClassList { return cl.append(classPosition(p)) }

// Relative is a shorthand for Position(PositionRelative).
func (cl ClassList) Relative() ClassList { return cl.append(classRelative) }

// Overflow sets overflow on both axes.
func (cl ClassList) Overflow(o Overflow) ClassList { return cl.append(classOverflow(o)) }

// ---------------------------------------------------------------------
// Spacing
// ---------------------------------------------------------------------

// Padding sets padding on all sides.
func (cl ClassList) Padding(s Spacing) ClassList { return cl.append(classPadding(s)) }

// PaddingX sets horizontal padding.
func (cl ClassList) PaddingX(s Spacing) ClassList { return cl.append(classPaddingX(s)) }

// PaddingY sets vertical padding.
func (cl ClassList) PaddingY(s Spacing) ClassList { return cl.append(classPaddingY(s)) }

// Margin sets margin on all sides.
func (cl ClassList) Margin(s Spacing) ClassList { return cl.append(classMargin(s)) }

// MarginX sets horizontal margin.
func (cl ClassList) MarginX(s Spacing) ClassList { return cl.append(classMarginX(s)) }

// MarginY sets vertical margin.
func (cl ClassList) MarginY(s Spacing) ClassList { return cl.append(classMarginY(s)) }

// MarginLeft / MarginRight / MarginTop / MarginBottom — per-side margin.
func (cl ClassList) MarginLeft(s Spacing) ClassList   { return cl.append(classMarginLeft(s)) }
func (cl ClassList) MarginRight(s Spacing) ClassList  { return cl.append(classMarginRight(s)) }
func (cl ClassList) MarginTop(s Spacing) ClassList    { return cl.append(classMarginTop(s)) }
func (cl ClassList) MarginBottom(s Spacing) ClassList { return cl.append(classMarginBottom(s)) }

// PaddingLeft / PaddingRight / PaddingTop / PaddingBottom — per-side padding.
func (cl ClassList) PaddingLeft(s Spacing) ClassList   { return cl.append(classPaddingLeft(s)) }
func (cl ClassList) PaddingRight(s Spacing) ClassList  { return cl.append(classPaddingRight(s)) }
func (cl ClassList) PaddingTop(s Spacing) ClassList    { return cl.append(classPaddingTop(s)) }
func (cl ClassList) PaddingBottom(s Spacing) ClassList { return cl.append(classPaddingBottom(s)) }

// Positional offsets. Apply to an absolutely/fixed positioned element
// (call Position first). Use Spacing consts (including SPX/S0) so the
// offset flows from the same scale as padding/margin.
func (cl ClassList) Top(s Spacing) ClassList    { return cl.append(classTop(s)) }
func (cl ClassList) Bottom(s Spacing) ClassList { return cl.append(classBottom(s)) }
func (cl ClassList) Left(s Spacing) ClassList   { return cl.append(classLeft(s)) }
func (cl ClassList) Right(s Spacing) ClassList  { return cl.append(classRight(s)) }

// Inset applies an offset to all four sides.
func (cl ClassList) Inset(s Spacing) ClassList { return cl.append(classInset(s)) }

// InsetX / InsetY apply to horizontal / vertical pairs.
func (cl ClassList) InsetX(s Spacing) ClassList { return cl.append(classInsetX(s)) }
func (cl ClassList) InsetY(s Spacing) ClassList { return cl.append(classInsetY(s)) }

// Gap sets flex/grid gap.
func (cl ClassList) Gap(s Spacing) ClassList { return cl.append(classGap(s)) }

// Width sets width.
func (cl ClassList) Width(s Spacing) ClassList { return cl.append(classWidth(s)) }

// Height sets height.
func (cl ClassList) Height(s Spacing) ClassList { return cl.append(classHeight(s)) }

// MaxHeight sets max-height from the governed spacing scale.
func (cl ClassList) MaxHeight(s Spacing) ClassList { return cl.append(classMaxH(string(s))) }

// HeightViewport sets a height from the governed viewport-relative scale.
func (cl ClassList) HeightViewport(v ViewportHeight) ClassList {
	return cl.append(classHeightViewport(v))
}

// MaxHeightViewport sets a max-height from the governed viewport-relative scale.
func (cl ClassList) MaxHeightViewport(v ViewportHeight) ClassList {
	return cl.append(classMaxHeightViewport(v))
}

// MinWidth sets min-width.
func (cl ClassList) MinWidth(s Spacing) ClassList { return cl.append(classMinWidth(s)) }

// MinHeight sets min-height.
func (cl ClassList) MinHeight(s Spacing) ClassList { return cl.append(classMinHeight(s)) }

// ---------------------------------------------------------------------
// Color
// ---------------------------------------------------------------------

// Bg applies a background color.
func (cl ClassList) Bg(c Color) ClassList { return cl.append(classBg(c)) }

// TextColor applies a foreground color.
// (Named TextColor rather than Text to avoid collision with FontSize.)
func (cl ClassList) TextColor(c Color) ClassList { return cl.append(classText(c)) }

// BorderColor applies a border color.
func (cl ClassList) BorderColor(c Color) ClassList { return cl.append(classBorder(c)) }

// RingColor applies a ring color.
func (cl ClassList) RingColor(c Color) ClassList { return cl.append(classRing(c)) }

// ---------------------------------------------------------------------
// Border + Ring
// ---------------------------------------------------------------------

// Border applies a border width (use BorderColor for the color).
// The zero BorderWidth (Border1) emits the base "border" class.
func (cl ClassList) Border(b BorderWidth) ClassList { return cl.append(classBorderWidth(b)) }

// BorderTop / BorderBottom / BorderLeft / BorderRight apply a one-side
// border width. Pass Border1 for the default thickness.
func (cl ClassList) BorderTop(b BorderWidth) ClassList { return cl.append(classBorderSide("t", b)) }

func (cl ClassList) BorderBottom(b BorderWidth) ClassList { return cl.append(classBorderSide("b", b)) }

func (cl ClassList) BorderLeft(b BorderWidth) ClassList { return cl.append(classBorderSide("l", b)) }

func (cl ClassList) BorderRight(b BorderWidth) ClassList { return cl.append(classBorderSide("r", b)) }

// BorderTopColor / BorderBottomColor / BorderLeftColor / BorderRightColor
// apply a per-side border color, e.g. BorderTopColor(ColorWhite) →
// "border-t-white". Use alongside BorderTop etc. for the width.
func (cl ClassList) BorderTopColor(c Color) ClassList { return cl.append(classBorderSideColor("t", c)) }

func (cl ClassList) BorderBottomColor(c Color) ClassList {
	return cl.append(classBorderSideColor("b", c))
}

func (cl ClassList) BorderLeftColor(c Color) ClassList {
	return cl.append(classBorderSideColor("l", c))
}

func (cl ClassList) BorderRightColor(c Color) ClassList {
	return cl.append(classBorderSideColor("r", c))
}

// Ring applies a focus-ring width (use RingColor for the color).
func (cl ClassList) Ring(r RingWidth) ClassList { return cl.append(classRingWidth(r)) }

// RingInset makes the focus ring draw on the inside edge of the
// element — Tailwind's "ring-inset" utility.
func (cl ClassList) RingInset() ClassList { return cl.append(classRingInset) }

// RingOffset applies a ring offset width.
func (cl ClassList) RingOffset(r RingOffset) ClassList { return cl.append(classRingOffset(r)) }

// Rounded applies a border-radius step. RadiusBase emits "rounded".
func (cl ClassList) Rounded(r Radius) ClassList { return cl.append(classRounded(r)) }

// Shadow applies a box-shadow step. ShadowBase emits "shadow".
func (cl ClassList) Shadow(s Shadow) ClassList { return cl.append(classShadow(s)) }

// Outline applies an outline style.
func (cl ClassList) Outline(o Outline) ClassList { return cl.append(classOutline(o)) }

// BorderStyle applies a border-style utility (e.g., border-dashed).
// Use alongside Border() for width and BorderColor() for color.
func (cl ClassList) BorderStyle(b BorderStyle) ClassList { return cl.append(classBorderStyle(b)) }

// ---------------------------------------------------------------------
// Typography
// ---------------------------------------------------------------------

// FontSize applies a text-size step.
func (cl ClassList) FontSize(f FontSize) ClassList { return cl.append(classFontSize(f)) }

// FontWeight applies a font-weight.
func (cl ClassList) FontWeight(f FontWeight) ClassList { return cl.append(classFontWeight(f)) }

// FontFamily applies a font-family (Tailwind's font-sans/serif/mono).
func (cl ClassList) FontFamily(f FontFamily) ClassList { return cl.append(classFontFamily(f)) }

// Tracking applies a letter-spacing step.
func (cl ClassList) Tracking(t Tracking) ClassList { return cl.append(classTracking(t)) }

// Leading applies a line-height step.
func (cl ClassList) Leading(l Leading) ClassList { return cl.append(classLeading(l)) }

// TextAlign applies a text-align.
func (cl ClassList) TextAlign(t TextAlign) ClassList { return cl.append(classTextAlign(t)) }

// Underline applies underline text-decoration.
func (cl ClassList) Underline() ClassList { return cl.append(classUnderline) }

// NoUnderline removes underline text-decoration.
func (cl ClassList) NoUnderline() ClassList { return cl.append(classNoUnderline) }

// Truncate clips overflowing text with ellipsis.
func (cl ClassList) Truncate() ClassList { return cl.append(classTruncate) }

// SrOnly makes content accessible only to screen readers.
func (cl ClassList) SrOnly() ClassList { return cl.append(classSrOnly) }

// NotSrOnly undoes SrOnly, which is what a skip link needs: hidden until it is
// focused, and then a real control in the corner of the page.
func (cl ClassList) NotSrOnly() ClassList { return cl.append(classNotSrOnly) }

// MinHeightScreen is min-h-screen: at least the viewport. It is not a Spacing
// step, so it is a method of its own rather than a value MinHeight takes.
func (cl ClassList) MinHeightScreen() ClassList { return cl.append(classMinHScreen) }

// Uppercase / Lowercase / Capitalize / NormalCase set text-transform.
// NormalCase is the default text-transform (no transform) and is
// required when a base class carries uppercase/lowercase/capitalize
// and a specific slot wants to reset to the element's natural case.
func (cl ClassList) Uppercase() ClassList  { return cl.append(classUppercase) }
func (cl ClassList) Lowercase() ClassList  { return cl.append(classLowercase) }
func (cl ClassList) Capitalize() ClassList { return cl.append(classCapitalize) }
func (cl ClassList) NormalCase() ClassList { return cl.append(classNormalCase) }

// UnderlineOffset sets the underline offset (Tailwind's "underline-offset-N").
// Use the Spacing const names (S4, S8) for semantic offsets, or supply a
// raw numeric step if the design uses a non-standard value.
func (cl ClassList) UnderlineOffset(step Spacing) ClassList {
	return cl.append(classUnderlineOffset(string(step)))
}

// FlexShrink0 prevents a flex child from shrinking below its intrinsic
// size — Tailwind's "flex-shrink-0" utility.
func (cl ClassList) FlexShrink0() ClassList { return cl.append(classFlexShrink0) }

// Flex1 makes a flex child fill available space — Tailwind's "flex-1".
func (cl ClassList) Flex1() ClassList { return cl.append(classFlex1) }

// FlexWrap allows flex children to wrap to the next line — Tailwind's
// "flex-wrap" utility. Complements FlexDir for multi-line flex rows.
func (cl ClassList) FlexWrap() ClassList { return cl.append(classFlexWrap) }

// FlexNoWrap forces all flex children onto a single line — Tailwind's
// "flex-nowrap" utility. Use alongside Overflow to show a scroll when
// items exceed the container.
func (cl ClassList) FlexNoWrap() ClassList { return cl.append(classFlexNoWrap) }

// FlexGrow / FlexGrow0 toggle the flex-grow property.
// FlexNone prevents a flex child from growing or shrinking —
// Tailwind's "flex-none" utility.
func (cl ClassList) FlexNone() ClassList { return cl.append(classFlexNone) }

func (cl ClassList) FlexGrow() ClassList  { return cl.append(classFlexGrow) }
func (cl ClassList) FlexGrow0() ClassList { return cl.append(classFlexGrow0) }

// Group / Peer apply Tailwind's variant-enabler utilities. A parent
// with `group` exposes `group-hover:*` / `group-focus:*` to
// descendants; `peer` does the same for siblings via `peer-*`.
func (cl ClassList) Group() ClassList { return cl.append(classGroup) }
func (cl ClassList) Peer() ClassList  { return cl.append(classPeer) }

// RoundedTop / RoundedBottom / RoundedLeft / RoundedRight apply a
// per-side border-radius step. Use Rounded(...) for all four corners.
func (cl ClassList) RoundedTop(r Radius) ClassList    { return cl.append(classRoundedSide("t", r)) }
func (cl ClassList) RoundedBottom(r Radius) ClassList { return cl.append(classRoundedSide("b", r)) }
func (cl ClassList) RoundedLeft(r Radius) ClassList   { return cl.append(classRoundedSide("l", r)) }
func (cl ClassList) RoundedRight(r Radius) ClassList  { return cl.append(classRoundedSide("r", r)) }

// Transform opts the element into Tailwind's transform utility surface.
// Required when applying translate/rotate/scale via utility classes in
// Tailwind v2-compatible output.
func (cl ClassList) Transform() ClassList { return cl.append(classTransform) }

// ---------------------------------------------------------------------
// Motion
// ---------------------------------------------------------------------

// Transition applies a transition property group.
func (cl ClassList) Transition(t Transition) ClassList { return cl.append(classTransition(t)) }

// Duration applies a transition duration.
func (cl ClassList) Duration(d Duration) ClassList { return cl.append(classDuration(d)) }

// Easing applies a transition easing curve.
func (cl ClassList) Easing(e Easing) ClassList { return cl.append(classEasing(e)) }

// TranslateY applies a vertical translate via the Translate enum
// (handles negative offsets like TranslateNeg05).
func (cl ClassList) TranslateY(t Translate) ClassList { return cl.append(classTranslateY(t)) }

// TranslateX applies a horizontal translate via the Translate enum.
func (cl ClassList) TranslateX(t Translate) ClassList { return cl.append(classTranslateX(t)) }

// TranslateXStep applies a horizontal translate using the Spacing
// scale (e.g., tw.S5 → "translate-x-5"). Use this when the offset
// lines up with the platform spacing scale; use TranslateX for
// negative/fractional offsets outside the scale.
func (cl ClassList) TranslateXStep(s Spacing) ClassList { return cl.append(classTranslateXSpacing(s)) }

// TranslateYStep — vertical equivalent of TranslateXStep.
func (cl ClassList) TranslateYStep(s Spacing) ClassList { return cl.append(classTranslateYSpacing(s)) }

// SpaceX / SpaceY add horizontal / vertical spacing between adjacent
// sibling elements (Tailwind's "space-x-*" / "space-y-*").
func (cl ClassList) SpaceX(s Spacing) ClassList { return cl.append(classSpaceX(s)) }
func (cl ClassList) SpaceY(s Spacing) ClassList { return cl.append(classSpaceY(s)) }

// MaxW sets max-width using Tailwind's named scale. Use MaxWScale
// for spacing-based max-widths (max-w-<step>).
func (cl ClassList) MaxW(name string) ClassList { return cl.append(classMaxW(name)) }

// MinH sets min-height. Accepts Spacing const names plus special
// values like "screen" / "full".
func (cl ClassList) MinH(v string) ClassList { return cl.append(classMinH(v)) }

// MaxH sets max-height. Same value space as MinH.
func (cl ClassList) MaxH(v string) ClassList { return cl.append(classMaxH(v)) }

// NegMargin sets a negative margin on the requested side.
// side is one of "" (all sides), "t", "b", "l", "r", "x", "y".
func (cl ClassList) NegMargin(side string, s Spacing) ClassList {
	return cl.append(classNegMargin(side, s))
}

// Rotate sets a Tailwind rotate-* transform. deg is a numeric step
// like "45", "90", "180" matching the Tailwind rotate scale.
func (cl ClassList) Rotate(deg string) ClassList { return cl.append(classRotate(deg)) }

// BackdropBlur applies a backdrop-blur utility.
func (cl ClassList) BackdropBlur(size string) ClassList {
	return cl.append(classBackdropBlur(size))
}

// Origin sets transform-origin (top-right, bottom-left, etc.).
func (cl ClassList) Origin(side string) ClassList { return cl.append(classOrigin(side)) }

// OverflowX / OverflowY apply axis-specific overflow.
func (cl ClassList) OverflowX(o Overflow) ClassList { return cl.append(classOverflowX(o)) }
func (cl ClassList) OverflowY(o Overflow) ClassList { return cl.append(classOverflowY(o)) }

// ObjectFit sets object-fit (cover/contain/fill/none/scale-down).
func (cl ClassList) ObjectFit(fit string) ClassList { return cl.append(classObjectFit(fit)) }

// ObjectCover is a shorthand for ObjectFit("cover").
func (cl ClassList) ObjectCover() ClassList { return cl.append(classObjectCover) }

// DivideX / DivideY add borders between adjacent children.
// Pass empty Spacing for default; pass a Spacing const for a specific width.
func (cl ClassList) DivideX(s Spacing) ClassList { return cl.append(classDivideX(s)) }
func (cl ClassList) DivideY(s Spacing) ClassList { return cl.append(classDivideY(s)) }

// ListStyle sets list-style-type (decimal/disc/none).
func (cl ClassList) ListStyle(style string) ClassList { return cl.append(classListStyle(style)) }

// BgOpacity sets a background color with an alpha modifier, e.g.
// Bg(SurfaceOverlay, "75") → "bg-surface-overlay/75".
func (cl ClassList) BgOpacity(c Color, opacity string) ClassList {
	return cl.append(classBgWithOpacity(c, opacity))
}

// TextColorOpacity sets a text color with an alpha modifier, e.g.
// TextColorOpacity(ColorWhite, "80") → "text-white/80".
func (cl ClassList) TextColorOpacity(c Color, opacity string) ClassList {
	return cl.append(classTextWithOpacity(c, opacity))
}

// BorderColorOpacity sets a border color with an alpha modifier, e.g.
// BorderColorOpacity(ColorWhite, "30") → "border-white/30".
func (cl ClassList) BorderColorOpacity(c Color, opacity string) ClassList {
	return cl.append(classBorderWithOpacity(c, opacity))
}

// ObjectContain applies Tailwind's "object-contain" utility — scales
// the content to fit without cropping.
func (cl ClassList) ObjectContain() ClassList { return cl.append(classObjectContain) }

// AspectRaw applies an arbitrary-value aspect ratio, e.g. "4/3" →
// "aspect-[4/3]". Prefer AspectVideo()/AspectSquare() when they match.
func (cl ClassList) AspectRaw(raw string) ClassList { return cl.append(classAspectRaw(raw)) }

// MaxWScaled applies Tailwind's named max-width scale via a typed
// handle. Distinct from MaxW which takes an untyped string.
func (cl ClassList) MaxWScaled(name MaxWidth) ClassList { return cl.append(classMaxW(string(name))) }

// TranslateXRaw / TranslateYRaw emit a raw-suffixed translate value
// for non-Spacing offsets like "1/2" (center-offset idiom).
func (cl ClassList) TranslateXRaw(raw string) ClassList {
	return cl.append(classTranslateXRaw(raw))
}

func (cl ClassList) TranslateYRaw(raw string) ClassList {
	return cl.append(classTranslateYRaw(raw))
}

func (cl ClassList) NegTranslateX(raw string) ClassList {
	return cl.append(classNegTranslateX(raw))
}

func (cl ClassList) NegTranslateY(raw string) ClassList {
	return cl.append(classNegTranslateY(raw))
}

// Italic / NotItalic set font-style.
func (cl ClassList) Italic() ClassList    { return cl.append(classItalic) }
func (cl ClassList) NotItalic() ClassList { return cl.append(classNotItalic) }

// AppearanceNone removes native OS chrome from form controls.
func (cl ClassList) AppearanceNone() ClassList { return cl.append(classAppearanceNone) }

// Resize applies a textarea-resize mode (`none` / `y` / `x` / `both`).
func (cl ClassList) Resize(mode ResizeMode) ClassList { return cl.append(classResize(mode)) }

// ResizeNone is a shorthand for Resize(ResizeModeNone): disables
// the textarea resize handle entirely.
func (cl ClassList) ResizeNone() ClassList { return cl.Resize(ResizeModeNone) }

// ResizeY is a shorthand for Resize(ResizeModeY): allows vertical
// resize only (the default for auto-grow textareas).
func (cl ClassList) ResizeY() ClassList { return cl.Resize(ResizeModeY) }

// WhitespaceNowrap prevents text wrapping.
func (cl ClassList) WhitespaceNowrap() ClassList { return cl.append(classWhitespaceNoWrap) }

// WhitespacePreWrap preserves user whitespace and wraps long lines —
// Tailwind's "whitespace-pre-wrap" utility. Used by transcript and
// console bubbles that render AI output with embedded newlines.
func (cl ClassList) WhitespacePreWrap() ClassList { return cl.append(classWhitespacePreWrap) }

// BreakWords allows long words to break.
func (cl ClassList) BreakWords() ClassList { return cl.append(classBreakWords) }

// BreakAll lets a browser break a line at any character (Tailwind's
// "break-all"). Use for user-supplied long tokens like hashes / URLs.
func (cl ClassList) BreakAll() ClassList { return cl.append(classBreakAll) }

// GridCols applies a CSS grid template with N equally-sized columns
// ("grid-cols-N"). Use together with Display(DisplayGrid).
func (cl ClassList) GridCols(n int) ClassList { return cl.append(classGridCols(n)) }

// GridColsRaw applies an arbitrary-value grid template, e.g.
// "[auto_1fr]" → "grid-cols-[auto_1fr]". Reserved for non-integer
// column templates not expressible via GridCols.
func (cl ClassList) GridColsRaw(raw string) ClassList { return cl.append(classGridColsRaw(raw)) }

// GapX / GapY set axis-specific flex/grid gaps (gap-x-*, gap-y-*).
func (cl ClassList) GapX(s Spacing) ClassList { return cl.append(classGapX(s)) }
func (cl ClassList) GapY(s Spacing) ClassList { return cl.append(classGapY(s)) }

// AnimatePulse applies Tailwind's "animate-pulse" utility.
func (cl ClassList) AnimatePulse() ClassList { return cl.append(classAnimatePulse) }

// LineClamp truncates a paragraph after N lines (Tailwind's
// "line-clamp-N"). Use N >= 1; N == 0 emits "line-clamp-none".
func (cl ClassList) LineClamp(n int) ClassList { return cl.append(classLineClamp(n)) }

// AnimateSpin applies Tailwind's "animate-spin" utility.
func (cl ClassList) AnimateSpin() ClassList { return cl.append(classAnimateSpin) }

// LineThrough / NotLineThrough set the line-through text-decoration.
func (cl ClassList) LineThrough() ClassList   { return cl.append(classLineThrough) }
func (cl ClassList) NoLineThrough() ClassList { return cl.append(classNoLineThrough) }

// TabularNums applies Tailwind's "tabular-nums" font-variant-numeric
// utility — forces fixed-width digits, used by counter displays.
func (cl ClassList) TabularNums() ClassList { return cl.append(classTabularNums) }

// Accent applies an accent color (Tailwind's "accent-*"). Used by
// form controls like range inputs and checkboxes to tint the native
// chrome with a design-system color.
func (cl ClassList) Accent(c Color) ClassList { return cl.append(classAccent(c)) }

// LeftRaw / TopRaw / BottomRaw / RightRaw apply fractional or
// arbitrary-value positional offsets that aren't on the Spacing scale,
// like "left-1/2" or "top-full". The only raw Tailwind literal lives in
// tw/compile.go — callers pass a typed RawOffset string.
func (cl ClassList) LeftRaw(v string) ClassList   { return cl.append(classLeftRaw(v)) }
func (cl ClassList) RightRaw(v string) ClassList  { return cl.append(classRightRaw(v)) }
func (cl ClassList) TopRaw(v string) ClassList    { return cl.append(classTopRaw(v)) }
func (cl ClassList) BottomRaw(v string) ClassList { return cl.append(classBottomRaw(v)) }

// LeftOffset / RightOffset / TopOffset / BottomOffset apply one governed
// fractional overlay position. Unlike the Raw forms, every accepted value is
// part of emission's closed utility universe.
func (cl ClassList) LeftOffset(v PositionOffset) ClassList {
	return cl.append(classLeftRaw(string(v)))
}
func (cl ClassList) RightOffset(v PositionOffset) ClassList {
	return cl.append(classRightRaw(string(v)))
}
func (cl ClassList) TopOffset(v PositionOffset) ClassList {
	return cl.append(classTopRaw(string(v)))
}
func (cl ClassList) BottomOffset(v PositionOffset) ClassList {
	return cl.append(classBottomRaw(string(v)))
}

// NegTop / NegRight / NegBottom / NegLeft apply a negative positional
// offset on the Spacing scale (e.g., NegTop(tw.S1) emits "-top-1").
// Used by badge and pill patterns that sit outside the parent's
// bounding box.
func (cl ClassList) NegTop(s Spacing) ClassList    { return cl.append(classNegTop(s)) }
func (cl ClassList) NegRight(s Spacing) ClassList  { return cl.append(classNegRight(s)) }
func (cl ClassList) NegBottom(s Spacing) ClassList { return cl.append(classNegBottom(s)) }
func (cl ClassList) NegLeft(s Spacing) ClassList   { return cl.append(classNegLeft(s)) }

// MinWidthRaw applies a raw min-width value such as "[3ch]" or "0".
// Reserved for arbitrary-value utilities outside the Spacing scale.
func (cl ClassList) MinWidthRaw(v string) ClassList { return cl.append(classMinWidthRaw(v)) }

// MaxWidthRaw applies a raw max-width value such as "[30rem]" or "[80px]"
// — for arbitrary pixel/rem widths outside the named MaxWidth scale.
func (cl ClassList) MaxWidthRaw(v string) ClassList { return cl.append(classMaxWidthRaw(v)) }

// MinHeightRaw applies a raw min-height value such as "[6rem]".
// Reserved for arbitrary-value utilities outside the Spacing scale.
func (cl ClassList) MinHeightRaw(v string) ClassList { return cl.append(classMinHeightRaw(v)) }

// MaxHeightRaw applies a raw max-height value such as "[72]" or "[6rem]"
// — for arbitrary pixel/rem heights outside the named MaxH scale.
func (cl ClassList) MaxHeightRaw(v string) ClassList { return cl.append(classMaxHeightRaw(v)) }

// WidthRaw applies a raw width value such as "[80px]" — an arbitrary
// pixel/rem width outside the Spacing scale.
func (cl ClassList) WidthRaw(v string) ClassList { return cl.append(classWidthRaw(v)) }

// Container applies Tailwind's responsive "container" utility — an
// element whose max-width steps through the configured breakpoints.
// Typically paired with MarginX(SAuto) for horizontal centering.
func (cl ClassList) Container() ClassList { return cl.append(classContainer) }

// HeightRaw applies a raw height value such as "[60vh]" — an arbitrary
// viewport/px/rem height outside the Spacing scale.
func (cl ClassList) HeightRaw(v string) ClassList { return cl.append(classHeightRaw(v)) }

// PaddingTopRaw applies a raw padding-top value such as "[20vh]" — an
// arbitrary-value padding outside the Spacing scale. Used for viewport-
// relative vertical padding (command palette top offset, hero layouts).
func (cl ClassList) PaddingTopRaw(v string) ClassList { return cl.append(classPaddingTopRaw(v)) }

// RoundedRaw applies a raw border-radius value such as "[20px]" — an
// arbitrary-value radius outside the Radius scale.
func (cl ClassList) RoundedRaw(v string) ClassList { return cl.append(classRoundedRaw(v)) }

// ColSpan applies a CSS grid column span ("col-span-N"). N must be
// positive; 0 or negative → empty.
func (cl ClassList) ColSpan(n int) ClassList { return cl.append(classColSpan(n)) }

// ColSpanFull emits Tailwind's "col-span-full" — span every column of
// the current grid regardless of column count. Distinct from ColSpan(N)
// which takes a numeric count.
func (cl ClassList) ColSpanFull() ClassList { return cl.append(classColSpanFull) }

// OverscrollContain sets overscroll-behavior to contain (Tailwind's
// "overscroll-contain"). Prevents scroll chaining out of a container.
func (cl ClassList) OverscrollContain() ClassList { return cl.append(classOverscrollContain) }

// AspectSquare / AspectVideo set aspect-ratio.
func (cl ClassList) AspectSquare() ClassList { return cl.append(classAspectSquare) }
func (cl ClassList) AspectVideo() ClassList  { return cl.append(classAspectVideo) }

// ---------------------------------------------------------------------
// State & interaction
// ---------------------------------------------------------------------

// Opacity applies an opacity percentage.
func (cl ClassList) Opacity(o Opacity) ClassList { return cl.append(classOpacity(o)) }

// Cursor applies a cursor style.
func (cl ClassList) Cursor(c Cursor) ClassList { return cl.append(classCursor(c)) }

// PointerEvents applies a pointer-events style.
func (cl ClassList) PointerEvents(p PointerEvents) ClassList {
	return cl.append(classPointerEvents(p))
}

// UserSelect applies a user-select style.
func (cl ClassList) UserSelect(s Select) ClassList { return cl.append(classSelect(s)) }

// ZLayer applies a typed z-index layer.
func (cl ClassList) ZLayer(z ZLayer) ClassList { return cl.append(z.Class()) }

// ZIndexRaw applies a Tailwind z-index class using a raw suffix ("40",
// "50", "auto"). Reserved for components preserving legacy z-index
// values that do not map to the typed ZLayer enum.
func (cl ClassList) ZIndexRaw(raw string) ClassList { return cl.append(classZIndexRaw(raw)) }

// ZIndex is an alias for ZLayer.
func (cl ClassList) ZIndex(z ZLayer) ClassList { return cl.ZLayer(z) }

// ---------------------------------------------------------------------
// Prefix wrappers (state + breakpoint)
// ---------------------------------------------------------------------

// On wraps the build function's output in a state prefix (e.g.,
// "hover:"). Nested On calls are supported and prefixes stack in order.
func (cl ClassList) On(state State, build func(ClassList) ClassList) ClassList {
	return cl.prefixed(state.Prefix(), build)
}

// Breakpoint wraps the build function's output in a responsive prefix
// (e.g., "sm:"). Nested Breakpoint calls are supported.
func (cl ClassList) Breakpoint(bp Breakpoint, build func(ClassList) ClassList) ClassList {
	return cl.prefixed(bp.Prefix(), build)
}

// prefixed is the shared implementation of On/Breakpoint.
func (cl ClassList) prefixed(prefix string, build func(ClassList) ClassList) ClassList {
	if prefix == "" || build == nil {
		return cl
	}
	inner := build(New())
	if len(inner.segments) == 0 {
		return cl
	}
	out := cl.clone(len(inner.segments) + 2)
	out.segments = append(out.segments, segment{kind: sgOpenPrefix, prefix: prefix})
	out.segments = append(out.segments, inner.segments...)
	out.segments = append(out.segments, segment{kind: sgClosePrefix})
	return out
}

// ---------------------------------------------------------------------
// Composition
// ---------------------------------------------------------------------

// Merge appends another ClassList to this one.
func (cl ClassList) Merge(other ClassList) ClassList {
	if len(other.segments) == 0 {
		return cl
	}
	out := cl.clone(len(other.segments))
	out.segments = append(out.segments, other.segments...)
	return out
}

// Raw is the escape hatch. Pass pre-audited Tailwind classes straight
// through. Use only for runtime-computed values (e.g., HTMX attributes)
// or incremental migration; the typed tables and All* enumerators linter watches this.
func (cl ClassList) Raw(classes string) ClassList {
	fields := strings.Fields(classes)
	if len(fields) == 0 {
		return cl
	}
	out := cl.clone(len(fields))
	for _, class := range fields {
		out.segments = append(out.segments, segment{kind: sgClass, class: class})
	}
	return out
}

// Len returns the number of segments currently in the builder.
// Exposed for tests — do not rely on it for runtime logic.
func (cl ClassList) Len() int { return len(cl.segments) }

// IsEmpty reports whether the builder has no segments.
func (cl ClassList) IsEmpty() bool { return len(cl.segments) == 0 }

// ---------------------------------------------------------------------
// Compile
// ---------------------------------------------------------------------

// Compile resolves the builder into a space-separated Tailwind class
// string. Output is deterministic and matches the append order. Prefix
// frames apply their prefix to each utility class between the
// open/close markers.
func (cl ClassList) Compile() string {
	if len(cl.segments) == 0 {
		return ""
	}
	var prefixStack []string
	var sb strings.Builder
	for _, s := range cl.segments {
		switch s.kind {
		case sgClass:
			sb = cl.writeClassSegment(sb, s, prefixStack)
		case sgOpenPrefix:
			prefixStack = append(prefixStack, s.prefix)
		case sgClosePrefix:
			if len(prefixStack) == 0 {
				continue
			}
			prefixStack = prefixStack[:len(prefixStack)-1]
		}
	}
	return sb.String()
}

// writeClassSegment appends one class segment to the buffer, applying
// active prefix frames. Separated from Compile to reduce cognitive
// complexity.
func (ClassList) writeClassSegment(sb strings.Builder, s segment, prefixStack []string) strings.Builder {
	if s.class == "" {
		return sb
	}
	if sb.Len() > 0 {
		sb.WriteByte(' ')
	}
	for _, prefix := range prefixStack {
		sb.WriteString(prefix)
	}
	sb.WriteString(s.class)
	return sb
}
