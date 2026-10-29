package components

import "github.com/septagon-oss/platformkit/ui/style"

var (
	// min-w-0 and no overflow of its own: a flex child defaults to min-width
	// auto, so a wide table would push the column instead of scrolling inside
	// it, and clipping here would hide the columns rather than let the table's
	// own overflow-auto reach them.
	clShellColumn = style.New().Flex1().Display(style.DisplayFlex).FlexDir(style.FlexCol).
			MinWidth(style.S0)
	// Break long tokens at the frame so content cannot widen its flex column.
	// Standalone components retain their ordinary text-layout contract.
	clShellMain = style.New().Flex1().PaddingX(style.S6).PaddingY(style.S6).SpaceY(style.S6).BreakAnywhere()
	// clShellFooterMeasure bounds what a composition puts *inside* the footer. The footer itself keeps
	// no max-width of its own: its top border is the frame's rule and must span the column. The bound is
	// the one Bare (ui/document) and the reference app's fault page already chose — 24rem — because the
	// design floor refuses a body measure above 75 characters and an unbounded footer sentence measured
	// 189ch at 1440px (384px is 64ch at 12px, 48ch at 16px).
	clShellFooterMeasure = style.New().MaxWScaled(style.MaxWSM)
	clToolbar            = style.New().Display(style.DisplayFlex).Items(style.ItemsStart).
				Justify(style.JustifyBetween).Gap(style.S4).FlexWrap()
	clToolbarCopy    = style.New().SpaceY(style.S1)
	clToolbarActions = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).Gap(style.S2)
	clForm           = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Gap(style.S4)
	clFormActions    = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).
				Justify(style.JustifyEnd).Gap(style.S2).PaddingTop(style.S2)
	clIconSize = map[string]style.ClassList{
		"xs":  style.New().Width(style.S3).Height(style.S3),
		"sm":  style.New().Width(style.S4).Height(style.S4),
		"md":  style.New().Width(style.S5).Height(style.S5),
		"lg":  style.New().Width(style.S6).Height(style.S6),
		"xl":  style.New().Width(style.S8).Height(style.S8),
		"2xl": style.New().Width(style.S12).Height(style.S12),
	}
	clBadgeSize = map[string]style.ClassList{
		"xs":  style.New().PaddingX(style.S2).PaddingY(style.S0_5).FontSize(style.TextXS),
		"sm":  style.New().PaddingX(style.S2_5).PaddingY(style.S0_5).FontSize(style.TextXS),
		"md":  style.New().PaddingX(style.S2_5).PaddingY(style.S0_5).FontSize(style.TextSM),
		"lg":  style.New().PaddingX(style.S3).PaddingY(style.S1).FontSize(style.TextSM),
		"xl":  style.New().PaddingX(style.S3).PaddingY(style.S1).FontSize(style.TextBase),
		"2xl": style.New().PaddingX(style.S4).PaddingY(style.S1).FontSize(style.TextBase),
	}
	// Inputs.
	// A field is a column of copy with a control in it — label, control, help sentence, error. The two
	// lists below are alternatives rather than a merge because the sheet emits classes in sorted order, so
	// a `max-w-none` could never override a `max-w-sm` on one element.
	//
	// Neither list bounds the field's own width. A measure on this element — the same 24rem the footer's
	// sentence takes — is what refused to reach a client's design document: the design tool projects a
	// composition only when its sizing is unconstrained (`composition constrained sizing requires further
	// conversion`), so a max-width on a flex column put every Input, Select, Textarea, Checkbox, Form and
	// document selection out of the projection — 44 refusals at the head this cure replaces. The measure
	// the design floor reads is the measure of the *sentences*: those are paragraphs, bounded where they
	// are written, on clHelp and clFieldErr below, which the projection carries as a wrapping paragraph.
	clFieldWrap     = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Gap(style.S1_5)
	clFieldWrapFull = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Gap(style.S1_5).Width(style.SFull)
	clInputSize     = map[string]style.ClassList{
		"sm": style.New().PaddingX(style.S3).PaddingY(style.S1_5).FontSize(style.TextSM),
		"md": style.New().PaddingX(style.S3).PaddingY(style.S2).FontSize(style.TextSM),
		"lg": style.New().PaddingX(style.S4).PaddingY(style.S2_5).FontSize(style.TextBase),
	}
	clTextareaManual = style.New().ResizeY()
	clTextareaAuto   = style.New().ResizeNone().Overflow(style.OverflowHidden)
	clTextareaMeta   = style.New().Display(style.DisplayFlex).Items(style.ItemsStart).
				Justify(style.JustifyBetween).Gap(style.S2)
	clTextareaSupporting = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Gap(style.S1)
	clSelectCell         = style.New().ColSpanFull().RowSpanFull()
	clSelectGrid         = style.New().Display(style.DisplayGrid).GridCols(1).GridRows(1)
	clSelectSize         = style.New().PaddingLeft(style.S3).PaddingRight(style.S10).
				PaddingY(style.S2).FontSize(style.TextSM).AppearanceNone().Merge(clSelectCell)
	clSelectIndicator = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).
				Justify(style.JustifyEnd).PaddingRight(style.S3).PointerEvents(style.PointerNone).Merge(clSelectCell)
	clSpinnerSize = map[string]style.ClassList{
		"xs":  style.New().Width(style.S3).Height(style.S3),
		"sm":  style.New().Width(style.S4).Height(style.S4),
		"md":  style.New().Width(style.S6).Height(style.S6),
		"lg":  style.New().Width(style.S8).Height(style.S8),
		"xl":  style.New().Width(style.S10).Height(style.S10),
		"2xl": style.New().Width(style.S12).Height(style.S12),
	}
	// Layouts.
	clFlex      = style.New().Display(style.DisplayFlex)
	clGrid      = style.New().Display(style.DisplayGrid)
	clContainer = style.New().MarginX(style.SAuto).Width(style.SFull).PaddingX(style.S4)
	clGapScale  = map[string]style.Spacing{
		"0": style.S0, "1": style.S1, "2": style.S2, "3": style.S3, "4": style.S4,
		"5": style.S5, "6": style.S6, "8": style.S8,
	}
	// Geometry only. The crop used to live here, which meant a caller who asked
	// for `fit: "contain"` got `object-cover object-contain` on one element and
	// whichever the stylesheet happened to declare last won — the caller's sentence
	// about the picture lost to source order. The crop is Media's Fit to answer, in
	// one place, in whatever order the classes arrive.
	clCardImageVertical   = style.New().Width(style.SFull)
	clCardImageHorizontal = style.New().Width(style.S48)
	// The two crops, named. They are declared here rather than built inline where
	// they are used, because a class the registry cannot see is a class the
	// stylesheet may never grow — TestRenderedClassesAreDeclared failed on exactly
	// that the first time, and `object-cover` would have shipped unstyled.
	clImageCover     = style.New().ObjectCover()
	clImageContain   = style.New().ObjectContain()
	clCardHorizontal = style.New().Display(style.DisplayFlex)
	clCardVertical   = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Flex1()
)
