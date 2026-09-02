package components

// classlists.go declares every tw ClassList the renderers compose. This is
// the load-bearing inversion of the usual CSS workflow: the stylesheet is
// derived FROM these declarations via tw/emission.For(ClassLists()...), so a
// class that no component declares is never emitted, and a declared class is
// always backed by a rule. TestRenderedClassesAreDeclared closes the loop
// from the other side.

import "github.com/septagon-oss/platformkit/ui/style"

// hoverBg returns a hover-state background modifier.
func hoverBg(c style.Color) func(style.ClassList) style.ClassList {
	return func(cl style.ClassList) style.ClassList { return cl.Bg(c) }
}

func buttonLoadingIndicator(color style.Color) style.ClassList {
	return style.New().TextColor(color).BorderTopColor(color)
}

var (

	// The application frame. See shell.go: markup written outside this package
	// would have no rules in the stylesheet, so the frame is a component too.
	clShell = style.New().Display(style.DisplayFlex).MinHeightScreen().
		Bg(style.SurfaceSecondary).TextColor(style.FgPrimary)
	// min-w-0 and no overflow of its own: a flex child defaults to min-width
	// auto, so a wide table would push the column instead of scrolling inside
	// it, and clipping here would hide the columns rather than let the table's
	// own overflow-auto reach them.
	clShellColumn = style.New().Flex1().Display(style.DisplayFlex).FlexDir(style.FlexCol).
			MinWidth(style.S0)
	clShellHeader = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).
			Justify(style.JustifyBetween).Gap(style.S4).PaddingX(style.S6).PaddingY(style.S3).
			Bg(style.SurfacePrimary).BorderBottom(style.Border1).BorderColor(style.BorderPrimary)
	clShellMain   = style.New().Flex1().PaddingX(style.S6).PaddingY(style.S6).SpaceY(style.S6)
	clShellFooter = style.New().PaddingX(style.S6).PaddingY(style.S4).FontSize(style.TextXS).
			TextColor(style.FgMuted).BorderTop(style.Border1).BorderColor(style.BorderPrimary)
	clSkipLink = style.New().SrOnly().
			On(style.StateFocus, func(c style.ClassList) style.ClassList {
			return c.NotSrOnly().Position(style.PositionAbsolute).Left(style.S4).Top(style.S4).
				ZLayer(style.ZOverlay).Rounded(style.RadiusMD).PaddingX(style.S3).PaddingY(style.S2).
				Bg(style.SurfacePrimary).TextColor(style.FgPrimary).Shadow(style.ShadowLG)
		})
	clToolbar = style.New().Display(style.DisplayFlex).Items(style.ItemsStart).
			Justify(style.JustifyBetween).Gap(style.S4).FlexWrap()
	clToolbarCopy    = style.New().SpaceY(style.S1)
	clToolbarActions = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).Gap(style.S2)
	clForm           = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Gap(style.S4)
	clFormActions    = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).
				Justify(style.JustifyEnd).Gap(style.S2).PaddingTop(style.S2)
	clConfirmDialog = style.New().MaxWScaled(style.MaxWSM).Rounded(style.RadiusLG).
			Border(style.Border1).BorderColor(style.BorderPrimary).Bg(style.SurfacePrimary).
			TextColor(style.FgPrimary).Padding(style.S6).SpaceY(style.S3).Shadow(style.ShadowLG)
	clConfirmTitle   = style.New().FontSize(style.TextLG).FontWeight(style.FontSemibold)
	clConfirmMessage = style.New().FontSize(style.TextSM).TextColor(style.FgSecondary)
	// Shared fragments.
	clIcon     = style.New().Display(style.DisplayInlineBlock).FlexShrink0()
	clIconSize = map[string]style.ClassList{
		"xs":  style.New().Width(style.S3).Height(style.S3),
		"sm":  style.New().Width(style.S4).Height(style.S4),
		"md":  style.New().Width(style.S5).Height(style.S5),
		"lg":  style.New().Width(style.S6).Height(style.S6),
		"xl":  style.New().Width(style.S8).Height(style.S8),
		"2xl": style.New().Width(style.S12).Height(style.S12),
	}
	clIconTone = map[string]style.ClassList{
		"neutral": style.New(),
		"brand":   style.New().TextColor(style.FgBrand),
		"success": style.New().TextColor(style.FgSuccess),
		"warning": style.New().TextColor(style.FgWarning),
		"danger":  style.New().TextColor(style.FgDanger),
		"info":    style.New().TextColor(style.FgInfo),
	}

	clFocusRing = style.New().
			On(style.StateFocusVisible, func(c style.ClassList) style.ClassList {
			return c.Ring(style.Ring2).RingColor(style.RingFocus).RingOffset(style.RingOffset2)
		})

	// Button: base plus one list per variant.
	// Variant discipline, applied to every base+variant pair in this file: a
	// base fragment never declares a property any of its variants declares.
	// Two single-class utilities on one element have equal specificity, so
	// stylesheet order — not merge order — would pick the winner.
	// TestComposedListsHaveNoPropertyCollisions enforces this structurally.
	clButtonBase = style.New().
			Display(style.DisplayInlineFlex).Items(style.ItemsCenter).Justify(style.JustifyCenter).
			Gap(style.S2).FontWeight(style.FontSemibold).
			Rounded(style.RadiusMD).Border(style.Border1).
			Cursor(style.CursorPointer).
			Transition(style.TransitionColors).
			On(style.StateDisabled, func(c style.ClassList) style.ClassList {
			return c.Cursor(style.CursorNotAllowed).Opacity(style.Opacity50)
		}).
		Merge(clFocusRing)

	clButtonVariant = map[string]style.ClassList{
		"primary": style.New().NoUnderline().Bg(style.SurfaceBrand).TextColor(style.FgOnBrand).BorderColor(style.ColorTransparent).
			On(style.StateHover, hoverBg(style.SurfaceBrandHover)),
		"secondary": style.New().NoUnderline().Bg(style.SurfacePrimary).TextColor(style.FgPrimary).
			BorderColor(style.BorderPrimary).
			On(style.StateHover, hoverBg(style.SurfaceHover)),
		"outline": style.New().NoUnderline().Bg(style.ColorTransparent).TextColor(style.FgBrand).
			BorderColor(style.BorderBrand).
			On(style.StateHover, hoverBg(style.SurfaceBrandSoft)),
		"ghost": style.New().NoUnderline().Bg(style.ColorTransparent).TextColor(style.FgPrimary).BorderColor(style.ColorTransparent).
			On(style.StateHover, hoverBg(style.SurfaceHover)),
		"link": style.New().Bg(style.ColorTransparent).TextColor(style.FgLink).BorderColor(style.ColorTransparent).Underline().
			On(style.StateHover, func(c style.ClassList) style.ClassList { return c.TextColor(style.FgLinkHover) }),
	}
	clButtonTone = map[string]style.ClassList{
		"neutral": style.New(),
		"brand": style.New().NoUnderline().Bg(style.SurfaceBrand).TextColor(style.FgOnBrand).BorderColor(style.ColorTransparent).
			On(style.StateHover, hoverBg(style.SurfaceBrandHover)),
		"success": style.New().NoUnderline().Bg(style.SurfaceSuccess).TextColor(style.FgOnBrand).BorderColor(style.ColorTransparent),
		"warning": style.New().NoUnderline().Bg(style.SurfaceWarning).TextColor(style.FgOnBrand).BorderColor(style.ColorTransparent),
		"danger":  style.New().Bg(style.SurfaceDanger).TextColor(style.FgOnBrand).BorderColor(style.ColorTransparent),
		"info":    style.New().Bg(style.SurfaceInfo).TextColor(style.FgOnBrand).BorderColor(style.ColorTransparent),
	}

	clButtonSize = map[string]style.ClassList{
		"xs":  style.New().FontSize(style.TextXS).PaddingX(style.S2).PaddingY(style.S1),
		"sm":  style.New().FontSize(style.TextSM).PaddingX(style.S3).PaddingY(style.S1_5),
		"md":  style.New().FontSize(style.TextSM).PaddingX(style.S4).PaddingY(style.S2),
		"lg":  style.New().FontSize(style.TextBase).PaddingX(style.S5).PaddingY(style.S2_5),
		"xl":  style.New().FontSize(style.TextLG).PaddingX(style.S6).PaddingY(style.S3),
		"2xl": style.New().FontSize(style.TextXL).PaddingX(style.S8).PaddingY(style.S4),
	}

	clButtonFull     = style.New().Width(style.SFull)
	clButtonIconOnly = style.New().MinWidth(style.S11).MinHeight(style.S11)
	// Loading indicators use the same foreground contract as their button.
	// This keeps filled actions on-brand while preserving contrast for neutral
	// secondary, outline, ghost, and link variants.
	clButtonLoadingVariant = map[string]style.ClassList{
		"primary":   buttonLoadingIndicator(style.FgOnBrand),
		"secondary": buttonLoadingIndicator(style.FgPrimary),
		"outline":   buttonLoadingIndicator(style.FgBrand),
		"ghost":     buttonLoadingIndicator(style.FgPrimary),
		"link":      buttonLoadingIndicator(style.FgLink),
	}
	clButtonLoadingTone = map[string]style.ClassList{
		"neutral": style.New(),
		"brand":   buttonLoadingIndicator(style.FgOnBrand),
		"success": buttonLoadingIndicator(style.FgOnBrand),
		"warning": buttonLoadingIndicator(style.FgOnBrand),
		"danger":  buttonLoadingIndicator(style.FgOnBrand),
		"info":    buttonLoadingIndicator(style.FgOnBrand),
	}
	clButtonDisabledLink = style.New().
				Cursor(style.CursorNotAllowed).
				Opacity(style.Opacity50).
				PointerEvents(style.PointerNone)

	// Badge / Tag.
	clBadgeBase = style.New().
			Display(style.DisplayInlineFlex).Items(style.ItemsCenter).Gap(style.S1).
			Rounded(style.RadiusFull).FontWeight(style.FontMedium)

	clBadgeVariant = map[string]style.ClassList{
		"primary":   style.New().Bg(style.SurfaceBrandSoft).TextColor(style.FgBrand),
		"secondary": style.New().NoUnderline().Bg(style.SurfaceTertiary).TextColor(style.FgSecondary),
		"outline":   style.New().Border(style.Border1).BorderColor(style.BorderPrimary).TextColor(style.FgPrimary),
	}
	clBadgeTone = map[string]style.ClassList{
		"neutral": style.New().Bg(style.SurfaceTertiary).TextColor(style.FgSecondary),
		"brand":   style.New().Bg(style.SurfaceBrandSoft).TextColor(style.FgBrand),
		"success": style.New().Bg(style.SurfaceSuccessSoft).TextColor(style.FgSuccess),
		"warning": style.New().Bg(style.SurfaceWarningSoft).TextColor(style.FgWarning),
		"danger":  style.New().Bg(style.SurfaceDangerSoft).TextColor(style.FgDanger),
		"info":    style.New().Bg(style.SurfaceInfoSoft).TextColor(style.FgInfo),
	}
	clBadgeSize = map[string]style.ClassList{
		"xs":  style.New().PaddingX(style.S2).PaddingY(style.S0_5).FontSize(style.TextXS),
		"sm":  style.New().PaddingX(style.S2_5).PaddingY(style.S0_5).FontSize(style.TextXS),
		"md":  style.New().PaddingX(style.S2_5).PaddingY(style.S0_5).FontSize(style.TextSM),
		"lg":  style.New().PaddingX(style.S3).PaddingY(style.S1).FontSize(style.TextSM),
		"xl":  style.New().PaddingX(style.S3).PaddingY(style.S1).FontSize(style.TextBase),
		"2xl": style.New().PaddingX(style.S4).PaddingY(style.S1).FontSize(style.TextBase),
	}

	clBadgeDot     = style.New().Width(style.S1_5).Height(style.S1_5).Rounded(style.RadiusFull)
	clBadgeDotTone = map[string]style.ClassList{
		"neutral": style.New().Bg(style.FgSecondary),
		"brand":   style.New().Bg(style.FgBrand),
		"success": style.New().Bg(style.FgSuccess),
		"warning": style.New().Bg(style.FgWarning),
		"danger":  style.New().Bg(style.FgDanger),
		"info":    style.New().Bg(style.FgInfo),
	}
	// The parent already contributes gap-1. Padding, rather than an additional
	// margin, keeps the second four-pixel offset inside the authored layer so
	// browser and native auto-layout targets resolve the same eight-pixel visual
	// separation before count and removal affordances.
	clBadgeCount = style.New().Display(style.DisplayInlineFlex).Items(style.ItemsCenter).
			PaddingLeft(style.S1).FontWeight(style.FontSemibold).TabularNums()
	clBadgeRemove = style.New().PaddingLeft(style.S1).Display(style.DisplayInlineFlex).
			Items(style.ItemsCenter).Justify(style.JustifyCenter).
			Bg(style.ColorTransparent).Border(style.Border0).
			PaddingY(style.S0).PaddingRight(style.S0).
			Rounded(style.RadiusFull).Cursor(style.CursorPointer).
			On(style.StateHover, func(c style.ClassList) style.ClassList { return c.Opacity(style.Opacity75) }).
			Merge(clFocusRing)

	// Alert.
	clAlertBase = style.New().
			Display(style.DisplayFlex).Items(style.ItemsStart).Gap(style.S3).Rounded(style.RadiusLG).
			Border(style.Border1)
	clAlertRegular  = style.New().Padding(style.S4)
	clAlertCompact  = style.New().PaddingX(style.S3).PaddingY(style.S2)
	clAlertBordered = style.New().BorderLeft(style.Border4)

	clAlertVariant = map[string]style.ClassList{
		"neutral": style.New().Bg(style.SurfaceTertiary).TextColor(style.FgSecondary).BorderColor(style.BorderPrimary),
		"success": style.New().NoUnderline().Bg(style.SurfaceSuccessSoft).TextColor(style.FgSuccess).BorderColor(style.BorderSuccess),
		"warning": style.New().NoUnderline().Bg(style.SurfaceWarningSoft).TextColor(style.FgWarning).BorderColor(style.BorderWarning),
		"danger":  style.New().Bg(style.SurfaceDangerSoft).TextColor(style.FgDanger).BorderColor(style.BorderDanger),
		"info":    style.New().Bg(style.SurfaceInfoSoft).TextColor(style.FgInfo).BorderColor(style.BorderInfo),
	}

	clAlertTitle   = style.New().FontWeight(style.FontSemibold).FontSize(style.TextSM)
	clAlertMessage = style.New().FontSize(style.TextSM)
	clAlertBody    = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Gap(style.S1).Flex1()
	clAlertIcon    = style.New().
			MarginTop(style.S0_5).Display(style.DisplayFlex).Height(style.S9).Width(style.S9).
			FlexShrink0().Items(style.ItemsCenter).Justify(style.JustifyCenter).Rounded(style.RadiusFull)
	clAlertActions = style.New().MarginTop(style.S3).Display(style.DisplayFlex).FlexWrap().
			Items(style.ItemsCenter).Gap(style.S3).FontSize(style.TextSM)
	clAlertClose = style.New().MarginLeft(style.SAuto).Display(style.DisplayInlineFlex).
			FlexShrink0().Items(style.ItemsCenter).Justify(style.JustifyCenter).
			Rounded(style.RadiusMD).Padding(style.S1_5).Cursor(style.CursorPointer).
			Transition(style.TransitionColors).Merge(clFocusRing)

	// Inputs.
	clFieldWrap     = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Gap(style.S1_5)
	clFieldWrapFull = style.New().Width(style.SFull)
	clLabel         = style.New().FontSize(style.TextSM).FontWeight(style.FontMedium).TextColor(style.FgPrimary)
	clHelp          = style.New().FontSize(style.TextXS).TextColor(style.FgMuted)
	clFieldErr      = style.New().FontSize(style.TextXS).TextColor(style.FgDanger)
	clRequired      = style.New().TextColor(style.FgDanger)

	clInputDisabled = style.New().Bg(style.SurfaceDisabled).Cursor(style.CursorNotAllowed)
	clInput         = style.New().
			Display(style.DisplayBlock).Width(style.SFull).
			Rounded(style.RadiusMD).Border(style.Border1).
			Bg(style.SurfacePrimary).TextColor(style.FgPrimary).
			On(style.StatePlaceholder, func(c style.ClassList) style.ClassList { return c.TextColor(style.FgPlaceholder) }).
			On(style.StateDisabled, func(c style.ClassList) style.ClassList {
			return c.Merge(clInputDisabled)
		}).
		Merge(clFocusRing)

	clInputNormal   = style.New().BorderColor(style.BorderPrimary)
	clInputError    = style.New().BorderColor(style.BorderDanger)
	clInputReadOnly = style.New().Bg(style.SurfaceSecondary).TextColor(style.FgSecondary).
			Shadow(style.ShadowNone).Cursor(style.CursorDefault)
	clInputTone = map[string]style.ClassList{
		"neutral": style.New().BorderColor(style.BorderPrimary),
		"success": style.New().BorderColor(style.BorderSuccess),
		"warning": style.New().BorderColor(style.BorderWarning),
		"danger":  style.New().BorderColor(style.BorderDanger),
	}
	clInputSize = map[string]style.ClassList{
		"sm": style.New().PaddingX(style.S3).PaddingY(style.S1_5).FontSize(style.TextSM),
		"md": style.New().PaddingX(style.S3).PaddingY(style.S2).FontSize(style.TextSM),
		"lg": style.New().PaddingX(style.S4).PaddingY(style.S2_5).FontSize(style.TextBase),
	}
	clTextareaManual = style.New().ResizeY()
	clTextareaAuto   = style.New().ResizeNone().Overflow(style.OverflowHidden)
	clTextareaFull   = style.New().Width(style.SFull)
	clTextareaMeta   = style.New().Display(style.DisplayFlex).Items(style.ItemsStart).
				Justify(style.JustifyBetween).Gap(style.S2)
	clTextareaSupporting = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Gap(style.S1)
	clTextareaCounter    = style.New().FontSize(style.TextXS).TextColor(style.FgMuted).TabularNums().FlexShrink0()
	clInputIconWrap      = style.New().Position(style.PositionRelative)
	clInputIconStart     = style.New().
				Position(style.PositionAbsolute).InsetY(style.S0).Left(style.S0).
				Display(style.DisplayFlex).Items(style.ItemsCenter).
				PaddingLeft(style.S3).PointerEvents(style.PointerNone)
	clInputIconEnd = style.New().
			Position(style.PositionAbsolute).InsetY(style.S0).Right(style.S0).
			Display(style.DisplayFlex).Items(style.ItemsCenter).
			PaddingRight(style.S3).PointerEvents(style.PointerNone)
	clInputPadStart = style.New().PaddingLeft(style.S10)
	clInputPadEnd   = style.New().PaddingRight(style.S10)

	// Modal is the governed centered-dialog / mobile-sheet overlay. The root
	// also doubles as the empty HTMX swap target used by server-loaded forms.
	clModalRoot = style.New().Position(style.PositionFixed).Inset(style.S0).ZIndex(style.ZModal).
			Display(style.DisplayFlex).Justify(style.JustifyCenter).Padding(style.S4).
			OverflowY(style.OverflowAuto)
	clModalCentered    = style.New().Items(style.ItemsCenter)
	clModalBottomSheet = style.New().Items(style.ItemsEnd).
				Breakpoint(style.BreakpointSM, func(c style.ClassList) style.ClassList { return c.Items(style.ItemsCenter) })
	clModalOverlay = style.New().Position(style.PositionAbsolute).Inset(style.S0).
			BgOpacity(style.SurfaceOverlay, string(style.Opacity50)).Transition(style.TransitionOpacity)
	clModalPanel = style.New().Position(style.PositionRelative).Display(style.DisplayFlex).
			FlexDir(style.FlexCol).Width(style.SFull).MaxHeightViewport(style.VH85).
			Overflow(style.OverflowHidden).Rounded(style.Radius2XL).
			Border(style.Border1).BorderColor(style.BorderPrimary).
			Bg(style.SurfacePrimary).Shadow(style.Shadow2XL).TextAlign(style.TextLeft)
	clModalPanelSize = map[string]style.ClassList{
		"small":  style.New().MaxWScaled(style.MaxWSM),
		"medium": style.New().MaxWScaled(style.MaxWLG),
		"large":  style.New().MaxWScaled(style.MaxW3XL),
		"xl":     style.New().MaxWScaled(style.MaxW5XL),
		"full":   style.New().MaxWScaled(style.MaxWFull),
	}
	clModalHeader = style.New().Display(style.DisplayFlex).Items(style.ItemsStart).
			Justify(style.JustifyBetween).Gap(style.S4).PaddingX(style.S6).PaddingY(style.S4).
			Bg(style.SurfaceSecondary).FlexShrink0()
	clModalTitleBlock  = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Gap(style.S1).MinWidth(style.S0)
	clModalTitle       = style.New().FontSize(style.TextLG).FontWeight(style.FontSemibold).TextColor(style.FgPrimary)
	clModalDescription = style.New().FontSize(style.TextSM).TextColor(style.FgMuted)
	clModalBody        = style.New().Flex1().OverflowY(style.OverflowAuto).Padding(style.S6)
	clModalFooter      = style.New().FlexShrink0().
				Bg(style.SurfaceSecondary).PaddingX(style.S6).PaddingY(style.S4)
	clModalSeparator = style.New().Width(style.SFull).Height(style.SPX).
				Bg(style.BorderPrimary).FlexShrink0()
	clModalClose = style.New().Display(style.DisplayInlineFlex).FlexShrink0().Items(style.ItemsCenter).
			Justify(style.JustifyCenter).Rounded(style.RadiusMD).Padding(style.S1_5).
			TextColor(style.FgMuted).Bg(style.ColorTransparent).Border(style.Border0).
			Cursor(style.CursorPointer).Transition(style.TransitionColors).
			On(style.StateHover, func(c style.ClassList) style.ClassList { return c.TextColor(style.FgPrimary).Bg(style.SurfaceHover) }).
			Merge(clFocusRing)
	clModalCancel = style.New().Display(style.DisplayInlineFlex).Items(style.ItemsCenter).
			Justify(style.JustifyCenter).Rounded(style.RadiusMD).Border(style.Border1).
			BorderColor(style.BorderPrimary).Bg(style.SurfacePrimary).PaddingX(style.S4).PaddingY(style.S2).
			FontSize(style.TextSM).FontWeight(style.FontMedium).TextColor(style.FgSecondary).
			Cursor(style.CursorPointer).Transition(style.TransitionColors).
			On(style.StateHover, func(c style.ClassList) style.ClassList { return c.Bg(style.SurfaceHover) }).
			Merge(clFocusRing)

	clCheckbox = style.New().
			Width(style.S4).Height(style.S4).Rounded(style.RadiusSM).
			Border(style.Border1).BorderColor(style.BorderPrimary).
			Cursor(style.CursorPointer).Merge(clFocusRing)

	clCheckboxRoot = style.New().Display(style.DisplayInlineFlex).Items(style.ItemsStart).
			Gap(style.S3).Cursor(style.CursorPointer)
	clCheckboxRootDisabled = style.New().Cursor(style.CursorNotAllowed).Opacity(style.Opacity50)
	clCheckboxInput        = style.New().Position(style.PositionAbsolute).
				Height(style.SPX).Width(style.SPX).MinHeight(style.S0).MinWidth(style.S0).
				AppearanceNone().Opacity(style.Opacity0).PointerEvents(style.PointerNone)
	clCheckboxIndicator = style.New().MarginTop(style.S0_5).Display(style.DisplayFlex).
				Height(style.S5).Width(style.S5).FlexShrink0().Items(style.ItemsCenter).
				Justify(style.JustifyCenter).Rounded(style.RadiusMD).Border(style.Border1).
				Transition(style.TransitionColors)
	clCheckboxIndicatorIdle = style.New().BorderColor(style.BorderPrimary).
				Bg(style.SurfacePrimary).TextColor(style.ColorTransparent)
	clCheckboxIndicatorActive = style.New().BorderColor(style.BorderBrand).
					Bg(style.SurfaceBrand).TextColor(style.FgOnBrand)
	clCheckboxCheckmark = style.New().Height(style.S3).Width(style.S3)
	clCheckboxBar       = style.New().Height(style.S0_5).Width(style.S2_5).
				Rounded(style.RadiusFull).Bg(style.SurfacePrimary)
	clCheckboxLabel = style.New().Truncate().PaddingTop(style.S0_5).
			FontSize(style.TextSM).TextColor(style.FgPrimary)

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

	clHeadingBase  = style.New().FontFamily(style.FontSerif).TextColor(style.FgPrimary).FontWeight(style.FontSemibold)
	clHeadingLevel = map[int]style.ClassList{
		1: style.New().FontSize(style.Text3XL),
		2: style.New().FontSize(style.Text2XL),
		3: style.New().FontSize(style.TextXL),
		4: style.New().FontSize(style.TextLG),
		5: style.New().FontSize(style.TextBase),
		6: style.New().FontSize(style.TextSM).Uppercase().Tracking(style.TrackingWider),
	}

	// Structure.
	clDividerH         = style.New().Border(style.Border0).BorderTop(style.Border1).BorderColor(style.BorderPrimary).MarginY(style.S4)
	clDividerV         = style.New().Border(style.Border0).BorderLeft(style.Border1).BorderColor(style.BorderPrimary).MarginX(style.S4)
	clDividerText      = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).Gap(style.S4)
	clDividerTextLine  = style.New().Flex1().BorderTop(style.Border1).BorderColor(style.BorderPrimary)
	clDividerTextLabel = style.New().FontSize(style.TextSM).TextColor(style.FgTertiary)

	clSpinner = style.New().
			Display(style.DisplayInlineBlock).Rounded(style.RadiusFull).
			Border(style.Border2).BorderColor(style.BorderSecondary).
			AnimateSpin()
	clSpinnerSize = map[string]style.ClassList{
		"xs":  style.New().Width(style.S3).Height(style.S3),
		"sm":  style.New().Width(style.S4).Height(style.S4),
		"md":  style.New().Width(style.S6).Height(style.S6),
		"lg":  style.New().Width(style.S8).Height(style.S8),
		"xl":  style.New().Width(style.S10).Height(style.S10),
		"2xl": style.New().Width(style.S12).Height(style.S12),
	}
	clSpinnerTone = map[string]style.ClassList{
		"neutral": style.New().BorderTopColor(style.FgSecondary),
		"brand":   style.New().BorderTopColor(style.FgBrand),
		"success": style.New().BorderTopColor(style.FgSuccess),
		"warning": style.New().BorderTopColor(style.FgWarning),
		"danger":  style.New().BorderTopColor(style.FgDanger),
		"info":    style.New().BorderTopColor(style.FgInfo),
	}

	// Skeleton — pulsing placeholder that holds the geometry of content that
	// has not arrived yet. Same feedback family as Spinner. The base carries
	// only surface and motion; each shape owns its dimensions and radius, so
	// merges never stack two rounded-* utilities (Merge is a plain append).
	clSkeleton     = style.New().Bg(style.SurfaceTertiary).AnimatePulse()
	clSkeletonText = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Gap(style.S2)
	// Text lines fill the row; the last line is capped at the named xs width
	// (percent widths are outside tw's enumerable universe) so a paragraph
	// placeholder reads as prose, not a solid slab.
	clSkeletonLine      = style.New().Width(style.SFull).Rounded(style.RadiusMD)
	clSkeletonLineLast  = style.New().Width(style.SFull).MaxWScaled(style.MaxWXS).Rounded(style.RadiusMD)
	clSkeletonBlockSize = map[string]style.ClassList{
		"sm": style.New().Width(style.SFull).Height(style.S16).Rounded(style.RadiusMD),
		"md": style.New().Width(style.SFull).Height(style.S24).Rounded(style.RadiusMD),
		"lg": style.New().Width(style.SFull).Height(style.S40).Rounded(style.RadiusMD),
	}
	clSkeletonLineSize = map[string]style.ClassList{
		"sm": style.New().Height(style.S3),
		"md": style.New().Height(style.S4),
		"lg": style.New().Height(style.S5),
	}
	clSkeletonCircleSize = map[string]style.ClassList{
		"sm": style.New().Width(style.S8).Height(style.S8).Rounded(style.RadiusFull),
		"md": style.New().Width(style.S12).Height(style.S12).Rounded(style.RadiusFull),
		"lg": style.New().Width(style.S16).Height(style.S16).Rounded(style.RadiusFull),
	}

	clEmpty = style.New().
		Display(style.DisplayFlex).FlexDir(style.FlexCol).Items(style.ItemsCenter).
		Justify(style.JustifyCenter).Gap(style.S3).TextAlign(style.TextCenter)
	clEmptyPad      = style.New().Padding(style.S12)
	clEmptyBordered = style.New().Border(style.Border1).BorderColor(style.BorderPrimary).
			BorderStyle(style.BorderStyle("dashed")).Rounded(style.RadiusLG)
	clEmptyCompact = style.New().Padding(style.S6)
	clEmptyTitle   = style.New().FontFamily(style.FontSerif).FontSize(style.TextLG).
			FontWeight(style.FontSemibold).TextColor(style.FgPrimary)
	clEmptyDesc = style.New().FontSize(style.TextSM).TextColor(style.FgMuted).MaxWScaled(style.MaxWMD)

	clLink = style.New().TextColor(style.FgLink).Underline().UnderlineOffset(style.S2).
		On(style.StateHover, func(c style.ClassList) style.ClassList { return c.TextColor(style.FgLinkHover) }).
		Merge(clFocusRing)

	// Layouts.
	clStack     = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol)
	clFlex      = style.New().Display(style.DisplayFlex)
	clGrid      = style.New().Display(style.DisplayGrid)
	clContainer = style.New().MarginX(style.SAuto).Width(style.SFull).PaddingX(style.S4)

	clGapScale = map[string]style.Spacing{
		"0": style.S0, "1": style.S1, "2": style.S2, "3": style.S3, "4": style.S4,
		"5": style.S5, "6": style.S6, "8": style.S8,
	}

	// Table.
	clTableWrap = style.New().Width(style.SFull).Overflow(style.OverflowAuto).
			Rounded(style.RadiusLG).Border(style.Border1).BorderColor(style.BorderPrimary)
	clTable       = style.New().Width(style.SFull).FontSize(style.TextSM).TextColor(style.FgPrimary)
	clTableHead   = style.New().Bg(style.SurfaceSecondary).TextAlign(style.TextLeft)
	clTableThBase = style.New().FontWeight(style.FontSemibold).
			FontSize(style.TextXS).Uppercase().Tracking(style.TrackingWider).TextColor(style.FgMuted)
	clTableTh  = clTableThBase.Merge(style.New().PaddingX(style.S4).PaddingY(style.S3))
	clTableTd  = style.New().PaddingX(style.S4).PaddingY(style.S3).BorderTop(style.Border1).BorderColor(style.BorderPrimary)
	clTableRow = style.New().On(style.StateHover, hoverBg(style.SurfaceHover))
	clTableTdC = style.New().PaddingX(style.S4).PaddingY(style.S2)
	// Sortable headers render a real button so keyboards and readers get the
	// affordance; aria-sort lives on the th, per WAI-ARIA sortable-table
	// practice. The cell's padding moves onto the button to keep the whole
	// header surface clickable.
	clTableThSort  = style.New().PaddingX(style.S0).PaddingY(style.S0)
	clTableSortBtn = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).Gap(style.S1).
			Width(style.SFull).PaddingX(style.S4).PaddingY(style.S3).
			FontWeight(style.FontSemibold).FontSize(style.TextXS).Uppercase().
			Tracking(style.TrackingWider).TextColor(style.FgMuted).
			Bg(style.ColorTransparent).Border(style.Border0).Cursor(style.CursorPointer).
			On(style.StateHover, func(c style.ClassList) style.ClassList { return c.TextColor(style.FgPrimary) }).
			Merge(clFocusRing)
	clTableRowAlt   = style.New().Bg(style.SurfaceSecondary)
	clTableTdStrong = style.New().FontWeight(style.FontSemibold).TextColor(style.FgPrimary)

	// DetailList. The section copy and semantic description-list markup stay
	// presentation-neutral; renderers use semanticRole as a machine key, never
	// as visible or ARIA copy.
	clDetailList   = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Gap(style.S3).MinWidth(style.S0)
	clDetailHeader = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Gap(style.S1).
			MinWidth(style.S0)
	clDetailTitle = style.New().FontSize(style.TextLG).FontWeight(style.FontSemibold).
			TextColor(style.FgPrimary).BreakWords()
	clDetailDescription = style.New().FontSize(style.TextSM).TextColor(style.FgSecondary).BreakWords()
	clDetailItems       = style.New().Overflow(style.OverflowHidden).Rounded(style.RadiusLG).
				Border(style.Border1).BorderColor(style.BorderPrimary).Bg(style.SurfacePrimary)
	clDetailRow = style.New().Display(style.DisplayGrid).GridCols(1).Gap(style.S2).
			PaddingX(style.S4).PaddingY(style.S3).
			Breakpoint(style.BreakpointSM, func(cl style.ClassList) style.ClassList {
			return cl.GridCols(2).Gap(style.S6)
		})
	clDetailRowSeparated    = style.New().BorderTop(style.Border1).BorderColor(style.BorderPrimary)
	clDetailTerm            = style.New().MinWidth(style.S0).FontSize(style.TextSM).FontWeight(style.FontMedium).TextColor(style.FgSecondary).BreakWords()
	clDetailTermDescription = style.New().Display(style.DisplayBlock).MarginTop(style.S1).
				FontSize(style.TextXS).FontWeight(style.FontNormal).TextColor(style.FgMuted).BreakWords()
	clDetailValue     = style.New().MinWidth(style.S0).FontSize(style.TextSM).FontWeight(style.FontSemibold).BreakWords()
	clDetailValueTone = map[string]style.ClassList{
		"neutral": style.New().TextColor(style.FgPrimary),
		"brand":   style.New().TextColor(style.FgBrand),
		"success": style.New().TextColor(style.FgSuccess),
		"warning": style.New().TextColor(style.FgWarning),
		"danger":  style.New().TextColor(style.FgDanger),
		"info":    style.New().TextColor(style.FgInfo),
	}

	// Card. clCard remains the compiled default frame used by delivery and
	// skeleton projections; the smaller fragments let CardWithSlots own
	// section padding and variants without a private downstream style stack.
	clCardFrame = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Gap(style.S2).
			Rounded(style.RadiusLG).Bg(style.SurfacePrimary).Overflow(style.OverflowHidden)
	clCardSectioned    = style.New().Gap(style.S0)
	clCardBorder       = style.New().Border(style.Border1).BorderColor(style.BorderPrimary)
	clCardPadNone      = style.New().Padding(style.S0)
	clCardPadSmall     = style.New().Padding(style.S3)
	clCardPadDefault   = style.New().Padding(style.S5)
	clCardPadMedium    = style.New().Padding(style.S6)
	clCardPadLarge     = style.New().Padding(style.S8)
	clCardShadowSmall  = style.New().Shadow(style.ShadowSM)
	clCardShadowMedium = style.New().Shadow(style.ShadowBase)
	clCardShadowLarge  = style.New().Shadow(style.ShadowLG)
	clCard             = clCardFrame.Merge(clCardBorder).Merge(clCardPadDefault).Merge(clCardShadowSmall)
	clCardClickable    = style.New().Cursor(style.CursorPointer).Transition(style.TransitionShadow).
				On(style.StateHover, func(c style.ClassList) style.ClassList { return c.Shadow(style.ShadowMD) }).
				Merge(clFocusRing)
	clCardHoverable = style.New().Transition(style.TransitionShadow).
			On(style.StateHover, func(c style.ClassList) style.ClassList { return c.Shadow(style.ShadowLG) })
	clCardTitle = style.New().FontFamily(style.FontSerif).FontSize(style.TextLG).
			FontWeight(style.FontSemibold).TextColor(style.FgPrimary)
	clCardDesc            = style.New().FontSize(style.TextSM).TextColor(style.FgMuted)
	clCardHeader          = style.New().BorderBottom(style.Border1).BorderColor(style.BorderPrimary)
	clCardFooter          = style.New().BorderTop(style.Border1).BorderColor(style.BorderPrimary).Bg(style.SurfaceSecondary)
	clCardImageVertical   = style.New().Width(style.SFull).ObjectCover()
	clCardImageHorizontal = style.New().Width(style.S48).ObjectCover()
	clCardHorizontal      = style.New().Display(style.DisplayFlex)
	clCardVertical        = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Flex1()

	// Breadcrumb.
	clBreadcrumb = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).Gap(style.S2).FontSize(style.TextSM).
			ListStyle("none").Margin(style.S0).Padding(style.S0)
	clBreadcrumbSep = style.New().TextColor(style.FgTertiary)
	clBreadcrumbCur = style.New().TextColor(style.FgPrimary).FontWeight(style.FontMedium)

	// Sidebar.
	clSidebarRootAdmin = style.New().Display(style.DisplayHidden).
				Breakpoint(style.BreakpointLG, func(c style.ClassList) style.ClassList {
			return c.Display(style.DisplayFlex).FlexShrink0()
		}).Transition(style.TransitionAll)
	clSidebarRootContent = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).
				Transition(style.TransitionAll)
	clSidebarWidthCollapsed = style.New().Breakpoint(style.BreakpointLG, func(c style.ClassList) style.ClassList {
		return c.Width(style.S16)
	})
	clSidebarWidthExpanded = style.New().Breakpoint(style.BreakpointLG, func(c style.ClassList) style.ClassList {
		return c.Width(style.S64)
	})
	clSidebarDisabled    = style.New().Opacity(style.Opacity50)
	clSidebarInner       = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Height(style.SFull)
	clSidebarColumnAdmin = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Flex1().
				Bg(style.SurfaceInverse).PaddingTop(style.S5).PaddingBottom(style.S4).OverflowY(style.OverflowAuto)
	clSidebarColumnContent = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Flex1().
				Bg(style.SurfacePrimary).OverflowY(style.OverflowVisible)
	clSidebarBrandAdmin = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).FlexShrink0().
				PaddingX(style.S4).MarginBottom(style.S8)
	clSidebarBrandContent = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Gap(style.S3).
				FlexShrink0().MarginBottom(style.S4)
	clSidebarBrandLink      = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).Merge(clFocusRing)
	clSidebarBrandText      = style.New().FontSize(style.TextXL).FontWeight(style.FontBold).TextColor(style.FgOnInverse)
	clSidebarNavWrapAdmin   = style.New().MarginTop(style.S5).Flex1().Display(style.DisplayFlex).FlexDir(style.FlexCol)
	clSidebarNavWrapContent = style.New().Flex1().Display(style.DisplayFlex).FlexDir(style.FlexCol)
	clSidebarNavAdmin       = style.New().Flex1().PaddingX(style.S2).SpaceY(style.S1)
	clSidebarNavContent     = style.New().Flex1().Display(style.DisplayFlex).FlexDir(style.FlexCol).Gap(style.S4)
	clSidebarLinkAdmin      = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).Gap(style.S3).
				FontSize(style.TextSM).FontWeight(style.FontMedium).Rounded(style.RadiusMD).
				Transition(style.TransitionColors).Merge(clFocusRing)
	clSidebarLinkContent = style.New().Display(style.DisplayFlex).Items(style.ItemsStart).Gap(style.S2).
				FontSize(style.TextSM).FontWeight(style.FontMedium).Rounded(style.RadiusMD).
				Transition(style.TransitionColors).Merge(clFocusRing)
	clSidebarLinkPadExpanded  = style.New().PaddingX(style.S2).PaddingY(style.S2)
	clSidebarLinkPadCollapsed = style.New().PaddingX(style.S2).PaddingY(style.S2).
					Justify(style.JustifyCenter)
	clSidebarLinkActiveAdmin = style.New().Bg(style.SurfaceOverlay).TextColor(style.FgOnInverse)
	clSidebarLinkIdleAdmin   = style.New().TextColor(style.FgOnInverse).
					On(style.StateHover, func(c style.ClassList) style.ClassList {
			return c.Bg(style.SurfaceOverlay).TextColor(style.FgOnInverse)
		})
	clSidebarLinkActiveContent = style.New().Bg(style.SurfaceBrandSoft).TextColor(style.FgBrand)
	clSidebarLinkIdleContent   = style.New().TextColor(style.FgSecondary).
					On(style.StateHover, func(c style.ClassList) style.ClassList {
			return c.Bg(style.SurfaceSecondary).TextColor(style.FgPrimary)
		})
	clSidebarItemDisabled = style.New().Opacity(style.Opacity50).PointerEvents(style.PointerNone).
				Cursor(style.CursorNotAllowed)
	clSidebarSection       = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Gap(style.S2)
	clSidebarSectionHeader = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).Gap(style.S2).
				PaddingX(style.S2).PaddingBottom(style.S2).BorderBottom(style.Border1).
				BorderColor(style.BorderPrimary).FontSize(style.TextXS).FontWeight(style.FontBold)
	clSidebarSectionHeaderAdmin   = clSidebarSectionHeader.TextColor(style.FgOnInverse)
	clSidebarSectionHeaderContent = clSidebarSectionHeader.TextColor(style.FgSecondary)
	clSidebarSectionGlyph         = style.New().TextColor(style.FgBrand)
	clSidebarSectionList          = style.New().Display(style.DisplayFlex).FlexDir(style.FlexCol).Gap(style.S0_5)
	clSidebarPrefixAdmin          = style.New().MinWidth(style.S10).FontSize(style.TextXS).
					FontWeight(style.FontSemibold).TextColor(style.FgOnInverse)
	clSidebarPrefixContent = style.New().MinWidth(style.S10).FontSize(style.TextXS).
				FontWeight(style.FontSemibold).TextColor(style.FgSecondary)
	clSidebarLabelVisible  = style.New().MinWidth(style.S0).Flex1()
	clSidebarLabelHidden   = style.New().MinWidth(style.S0).Flex1().Display(style.DisplayHidden)
	clSidebarLabelContent  = style.New().MinWidth(style.S0).Flex1().BreakWords()
	clSidebarNestedGroup   = style.New().SpaceY(style.S1)
	clSidebarNestedIndent  = style.New().MarginLeft(style.S4).SpaceY(style.S1)
	clSidebarFooterAdmin   = style.New().MarginTop(style.SAuto).PaddingX(style.S4).PaddingTop(style.S4)
	clSidebarFooterContent = style.New().MarginTop(style.S4)

	// Pagination.
	clPagination = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).Gap(style.S1)
	clPageBtn    = style.New().Display(style.DisplayInlineFlex).Items(style.ItemsCenter).
			Justify(style.JustifyCenter).MinWidth(style.S8).Height(style.S8).
			Rounded(style.RadiusMD).FontSize(style.TextSM).Merge(clFocusRing)
	clPageIdle = style.New().TextColor(style.FgSecondary).
			On(style.StateHover, hoverBg(style.SurfaceHover))
	clPageCur = style.New().Bg(style.SurfaceBrand).TextColor(style.FgOnBrand).FontWeight(style.FontSemibold)

	// Tabs.
	clTabsRoot                    = style.New().Display(style.DisplayFlex).Width(style.SFull).Gap(style.S4)
	clTabsRootHorizontal          = style.New().FlexDir(style.FlexCol)
	clTabsRootVertical            = style.New().FlexDir(style.FlexRow)
	clTabsListBase                = style.New().Display(style.DisplayFlex).Gap(style.S1)
	clTabsListHorizontal          = style.New().FlexDir(style.FlexRow)
	clTabsListVertical            = style.New().FlexDir(style.FlexCol)
	clTabsListUnderlineHorizontal = style.New().BorderBottom(style.Border1).
					BorderColor(style.BorderPrimary)
	clTabsListUnderlineVertical = style.New().BorderRight(style.Border1).
					BorderColor(style.BorderPrimary).PaddingRight(style.S4)
	clTabsButtonBase = style.New().Display(style.DisplayInlineFlex).Items(style.ItemsCenter).
				PaddingX(style.S3).PaddingY(style.S2).FontSize(style.TextSM).
				FontWeight(style.FontMedium).Transition(style.TransitionColors).Merge(clFocusRing)
	clTabsButtonPills               = style.New().Rounded(style.RadiusMD)
	clTabsButtonUnderlineHorizontal = style.New().BorderBottom(style.Border2).NegMargin("b", style.SPX)
	clTabsButtonUnderlineVertical   = style.New().BorderRight(style.Border2).NegMargin("r", style.SPX)
	clTabsPillsActive               = style.New().Bg(style.SurfaceBrand).TextColor(style.FgOnBrand)
	clTabsPillsIdle                 = style.New().TextColor(style.FgSecondary).
					On(style.StateHover, func(c style.ClassList) style.ClassList {
			return c.TextColor(style.FgPrimary).Bg(style.SurfaceSecondary)
		})
	clTabsUnderlineActive = style.New().BorderColor(style.BorderBrand).TextColor(style.FgBrand)
	clTabsUnderlineIdle   = style.New().BorderColor(style.ColorTransparent).TextColor(style.FgSecondary).
				On(style.StateHover, func(c style.ClassList) style.ClassList {
			return c.TextColor(style.FgPrimary).BorderColor(style.BorderSecondary)
		})
	clTabsDisabled = style.New().Opacity(style.Opacity50).Cursor(style.CursorNotAllowed)
	clTabsIcon     = style.New().MarginRight(style.S2)
	clTabsBadge    = style.New().MarginLeft(style.S2).Display(style.DisplayInlineFlex).
			Items(style.ItemsCenter).Rounded(style.RadiusFull).Bg(style.SurfaceTertiary).
			PaddingX(style.S2).PaddingY(style.S0_5).FontSize(style.TextXS).
			FontWeight(style.FontMedium).TextColor(style.FgSecondary)
	clTabsPanels = style.New().MinWidth(style.S0).Flex1()
	clTabsPanel  = style.New().MinWidth(style.S0)
	clTabsLazy   = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).
			Justify(style.JustifyCenter).PaddingY(style.S8)
	clTabsLazyLabel = style.New().TextColor(style.FgSecondary)
)

// ClassLists returns every ClassList the renderers compose, base and variant
// alike. Applications derive their stylesheet from it:
//
//	sheet, err := emission.For(web.ClassLists()...)
func ClassLists() []style.ClassList {
	out := []style.ClassList{
		clShell, clShellColumn, clShellHeader, clShellMain, clShellFooter, clSkipLink,
		clToolbar, clToolbarCopy, clToolbarActions, clForm, clFormActions,
		clConfirmDialog, clConfirmTitle, clConfirmMessage,
		clIcon, clFocusRing, clButtonBase, clButtonFull, clButtonIconOnly, clButtonDisabledLink,
		clBadgeBase, clBadgeDot, clBadgeCount, clBadgeRemove,
		clAlertBase, clAlertRegular, clAlertCompact, clAlertBordered,
		clAlertTitle, clAlertMessage, clAlertBody, clAlertIcon, clAlertActions, clAlertClose,
		clFieldWrap, clFieldWrapFull, clLabel, clHelp, clFieldErr, clRequired,
		clInput, clInputNormal, clInputError, clInputReadOnly, clInputDisabled,
		clInputIconWrap, clInputIconStart, clInputIconEnd, clInputPadStart, clInputPadEnd,
		clModalRoot, clModalCentered, clModalBottomSheet, clModalOverlay,
		clModalPanel, clModalHeader, clModalTitleBlock, clModalTitle,
		clModalDescription, clModalBody, clModalFooter, clModalSeparator, clModalClose, clModalCancel,
		clTextareaManual, clTextareaAuto, clTextareaFull, clTextareaMeta,
		clTextareaSupporting, clTextareaCounter,
		clCheckbox, clCheckboxRoot, clCheckboxRootDisabled,
		clCheckboxInput, clCheckboxIndicator, clCheckboxIndicatorIdle,
		clCheckboxIndicatorActive, clCheckboxCheckmark, clCheckboxBar, clCheckboxLabel,
		clTruncate, clHeadingBase,
		clDividerH, clDividerV, clDividerText, clDividerTextLine, clDividerTextLabel,
		clSpinner, clEmpty, clEmptyPad, clEmptyBordered, clEmptyCompact, clEmptyTitle, clEmptyDesc,
		clSkeleton, clSkeletonText, clSkeletonLine, clSkeletonLineLast,
		clLink, clTextItalic, clTextUnderline, clTextNoWrap, clTruncate,
		clStack, clFlex, clGrid, clContainer, clTableWrap, clTable, clTableHead, clTableThBase, clTableTh, clTableTd, clTableRow, clTableTdC,
		clTableThSort, clTableSortBtn, clTableRowAlt, clTableTdStrong, clDetailList, clDetailHeader, clDetailTitle, clDetailDescription,
		clDetailItems, clDetailRow, clDetailRowSeparated, clDetailTerm,
		clDetailTermDescription, clDetailValue,
		clCard, clCardFrame, clCardSectioned, clCardBorder,
		clCardPadNone, clCardPadSmall, clCardPadDefault, clCardPadMedium, clCardPadLarge,
		clCardShadowSmall, clCardShadowMedium, clCardShadowLarge,
		clCardClickable, clCardHoverable, clCardTitle, clCardDesc,
		clCardHeader, clCardFooter, clCardImageVertical, clCardImageHorizontal,
		clCardHorizontal, clCardVertical,
		clBreadcrumb, clBreadcrumbSep, clBreadcrumbCur,
		clSidebarRootAdmin, clSidebarRootContent, clSidebarWidthCollapsed,
		clSidebarWidthExpanded, clSidebarDisabled, clSidebarInner,
		clSidebarColumnAdmin, clSidebarColumnContent,
		clSidebarBrandAdmin, clSidebarBrandContent, clSidebarBrandLink, clSidebarBrandText,
		clSidebarNavWrapAdmin, clSidebarNavWrapContent, clSidebarNavAdmin, clSidebarNavContent,
		clSidebarLinkAdmin, clSidebarLinkContent,
		clSidebarLinkPadExpanded, clSidebarLinkPadCollapsed,
		clSidebarLinkActiveAdmin, clSidebarLinkIdleAdmin,
		clSidebarLinkActiveContent, clSidebarLinkIdleContent,
		clSidebarItemDisabled, clSidebarSection, clSidebarSectionHeader,
		clSidebarSectionHeaderAdmin, clSidebarSectionHeaderContent,
		clSidebarSectionGlyph, clSidebarSectionList,
		clSidebarPrefixAdmin, clSidebarPrefixContent,
		clSidebarLabelVisible, clSidebarLabelHidden, clSidebarLabelContent,
		clSidebarNestedGroup, clSidebarNestedIndent,
		clSidebarFooterAdmin, clSidebarFooterContent,
		clPagination, clPageBtn, clPageIdle, clPageCur, clTabsRoot, clTabsRootHorizontal, clTabsRootVertical,
		clTabsListBase, clTabsListHorizontal, clTabsListVertical,
		clTabsListUnderlineHorizontal, clTabsListUnderlineVertical,
		clTabsButtonBase, clTabsButtonPills, clTabsButtonUnderlineHorizontal,
		clTabsButtonUnderlineVertical, clTabsPillsActive, clTabsPillsIdle,
		clTabsUnderlineActive, clTabsUnderlineIdle, clTabsDisabled, clTabsIcon,
		clTabsBadge, clTabsPanels, clTabsPanel, clTabsLazy, clTabsLazyLabel,
	}
	for _, m := range []map[string]style.ClassList{
		clButtonVariant, clButtonTone, clButtonSize, clButtonLoadingVariant, clButtonLoadingTone,
		clBadgeVariant, clBadgeTone, clBadgeSize, clBadgeDotTone, clAlertVariant, clIconSize, clIconTone, clInputSize, clInputTone, clSpinnerSize, clSpinnerTone, clDetailValueTone,
		clSkeletonBlockSize, clSkeletonLineSize, clSkeletonCircleSize,
		clTextTransform, clModalPanelSize,
	} {
		for _, cl := range m {
			out = append(out, cl)
		}
	}
	for _, cl := range clHeadingLevel {
		out = append(out, cl)
	}
	// Enumerated one-offs composed inline by renderers.
	for _, sz := range clTextSize {
		out = append(out, style.New().FontSize(sz))
	}
	for _, w := range clTextWeight {
		out = append(out, style.New().FontWeight(w))
	}
	for _, c := range clTextColor {
		out = append(out, style.New().TextColor(c))
	}
	for _, align := range clTextAlign {
		out = append(out, style.New().TextAlign(align))
	}
	for lines := 1; lines <= 6; lines++ {
		out = append(out, style.New().LineClamp(lines))
	}
	for _, s := range clGapScale {
		out = append(out, style.New().Gap(s))
	}
	for _, n := range []int{1, 2, 3, 4, 6, 12} {
		out = append(out, style.New().GridCols(n))
	}
	out = append(out,
		style.New().Items(style.ItemsStart), style.New().Items(style.ItemsCenter),
		style.New().Items(style.ItemsEnd), style.New().Items(style.ItemsStretch),
		style.New().Justify(style.JustifyStart), style.New().Justify(style.JustifyCenter),
		style.New().Justify(style.JustifyEnd), style.New().Justify(style.JustifyBetween),
		style.New().FlexDir(style.FlexRow), style.New().FlexDir(style.FlexCol), style.New().FlexWrap(),
		style.New().MaxWScaled(style.MaxWSM), style.New().MaxWScaled(style.MaxWMD),
		style.New().MaxWScaled(style.MaxWLG), style.New().MaxWScaled(style.MaxWXL),
		style.New().MaxWScaled(style.MaxW2XL), style.New().MaxWScaled(style.MaxW4XL),
		style.New().MaxWScaled(style.MaxW7XL), style.New().MaxWScaled(style.MaxWFull),
	)
	return out
}
