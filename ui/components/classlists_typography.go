package components

import "github.com/septagon-oss/platformkit/ui/style"

var (
	// Text and headings.
	clTextColor = map[string]style.Color{
		"primary": style.FgPrimary, "secondary": style.FgSecondary, "tertiary": style.FgTertiary, "muted": style.FgMuted,
		"brand": style.FgBrand, "success": style.FgSuccess, "warning": style.FgWarning,
		"danger": style.FgDanger, "info": style.FgInfo,
	}
	clTextSize = map[string]style.FontSize{
		"xs": style.TextXS, "sm": style.TextSM, "base": style.TextBase,
		"lg": style.TextLG, "xl": style.TextXL, "2xl": style.Text2XL,
		"3xl": style.Text3XL, "4xl": style.Text4XL, "5xl": style.Text5XL,
	}
	clTextWeight = map[string]style.FontWeight{
		"thin": style.FontThin, "extralight": style.FontExtralight, "light": style.FontLight,
		"normal": style.FontNormal, "medium": style.FontMedium, "semibold": style.FontSemibold,
		"bold": style.FontBold, "extrabold": style.FontExtrabold, "black": style.FontBlack,
	}
	clTextAlign = map[string]style.TextAlign{
		"left": style.TextLeft, "center": style.TextCenter,
		"right": style.TextRight, "justify": style.TextJustify,
	}
	clTextTransform = map[string]style.ClassList{
		"none": {}, "uppercase": style.New().Uppercase(),
		"lowercase": style.New().Lowercase(), "capitalize": style.New().Capitalize(),
	}
	clTextItalic    = style.New().Italic()
	clTextUnderline = style.New().Underline()
	clTextNoWrap    = style.New().WhitespaceNowrap()
	clTruncate      = style.New().Truncate()
	// clHeadingBase carries no break rule of its own. A heading does have to break a token nothing can
	// hyphenate, and it does so because the frame's content region sets overflow-wrap: anywhere and the
	// property inherits — see clShellMain. On the component it would be refused by the design tool, which
	// builds text only under ordinary Unicode line breaking.
	clHeadingBase = style.New().FontFamily(style.FontSerif).TextColor(style.FgPrimary).
			FontWeight(style.FontSemibold)
	clHeadingLevel = map[int]style.ClassList{
		1: style.New().FontSize(style.Text3XL),
		2: style.New().FontSize(style.Text2XL),
		3: style.New().FontSize(style.TextXL),
		4: style.New().FontSize(style.TextLG),
		5: style.New().FontSize(style.TextBase),
		6: style.New().FontSize(style.TextSM).Uppercase().Tracking(style.TrackingWider),
	}
)
