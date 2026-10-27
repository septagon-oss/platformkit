package components

import (
	"maps"

	"github.com/septagon-oss/platformkit/ui/style"
)

// ClassLists is every ClassList the renderers compose, base and variant alike:
// the shell's and the gallery's together. It is what a completeness check reads
// — every class any renderer can emit — while an application's stylesheet is
// composed from the two halves separately. See ShellClassLists.
func ClassLists() []style.ClassList {
	return append(ShellClassLists(), GalleryClassLists()...)
}

// GalleryClassLists are the classes of the components only the gallery page
// renders: the divider, the empty state, the skeletons, the modal and the tabs.
// A third of the stylesheet was these, downloaded by every person on every
// page of an application that renders none of them.
//
// The line is drawn at the component and not at the prop. A component the shell
// renders keeps every class its props can reach, however unlikely — a class
// with no rule is unstyled HTML and nothing notices — so what moves here is
// only what no shell page can produce at all.
func GalleryClassLists() []style.ClassList {
	out := []style.ClassList{
		clDividerH, clDividerV, clDividerText, clDividerTextLine, clDividerTextLabel,
		// Media's panels stay here while only the gallery can produce them. Card's
		// picture routes through Media too, so the first screen that passes a Status
		// — a cut-out still being cut, an artwork somebody may not look at — has to
		// move clMediaAbsent, clMediaFailed and clMediaRefused into the shell list
		// with it, or that panel arrives unstyled on the page that needed it.
		clMediaFigure, clMediaCaption, clMediaAbsent, clMediaFailed, clMediaRefused,
		clModalRoot, clModalCentered, clModalBottomSheet, clModalOverlay, clDetailPanel, clDetailFrame, clDetailBody,
		clModalPanel, clModalHeader, clModalTitleBlock, clModalTitle,
		clModalDescription, clModalBody, clModalFooter, clModalSeparator, clModalClose, clModalCancel,
		clTabsRoot, clTabsRootHorizontal, clTabsRootVertical,
		clTabsListBase, clTabsListHorizontal, clTabsListVertical,
		clTabsListUnderlineHorizontal, clTabsListUnderlineVertical,
		clTabsButtonBase, clTabsButtonPills, clTabsButtonUnderlineHorizontal,
		clTabsButtonUnderlineVertical, clTabsPillsActive, clTabsPillsIdle,
		clTabsUnderlineActive, clTabsUnderlineIdle, clTabsDisabled, clTabsIcon,
		clTabsBadge, clTabsPanels, clTabsPanel, clTabsLazy, clTabsLazyLabel,
	}
	for _, m := range []map[string]style.ClassList{
		clModalPanelSize, clAvatarSize, clDetailPlacement,
	} {
		for _, cl := range m {
			out = append(out, cl)
		}
	}
	return out
}

// ShellClassLists are the classes every other component can emit — the ones an
// application's own pages render. ui.Stylesheet is composed from these.
func ShellClassLists() []style.ClassList {
	out := shellBaseClassLists()
	out = shellVariantClassLists(out)
	out = shellInlineClassLists(out)
	return out
}

func shellBaseClassLists() []style.ClassList {
	out := []style.ClassList{
		clStandalone, clDisclosure, clDisclosureSummary, clDisclosureDescription, clDisclosureBody,
		clShell, clShellColumn, clShellHeader, clShellMain, clShellFooter, clShellFooterMeasure,
		clSkipLink,
		clToolbar, clToolbarCopy, clToolbarActions, clForm, clFormActions, clFormSentence,
		clConfirmDialog, clConfirmTitle, clConfirmMessage,
		clIcon, clFocusRing, clButtonBase, clButtonFull, clButtonIconOnly, clButtonDisabledLink,
		// A person, as read by the generated list: ui/resource composes a cell out of
		// these whenever a field says `ui:"present:person"`, so they are on every page
		// of an application that has people in it, not on the gallery alone.
		clAvatar, clAvatarInitials, clAvatarLink, clAvatarLabel,
		clBadgeBase, clBadgeDot, clBadgeCount, clBadgeRemove,
		clAlertBase, clAlertRegular, clAlertCompact, clAlertBordered,
		clAlertTitle, clAlertMessage, clAlertBody, clAlertIcon, clAlertActions, clAlertClose, clRecoveryAction,
		clFieldWrap, clFieldWrapFull, clLabel, clHelp, clFieldErr, clRequired,
		clInput, clInputNormal, clInputError, clInputReadOnly, clInputDisabled,
		clInputIconWrap, clInputIconStart, clInputIconEnd, clInputPadStart, clInputPadEnd,
		clSelectGrid, clSelectSize, clSelectIndicator,
		clTextareaManual, clTextareaAuto, clTextareaMeta,
		clTextareaSupporting, clTextareaCounter,
		clCheckbox, clCheckboxRoot, clCheckboxRootDisabled,
		clCheckboxInput, clCheckboxIndicator, clCheckboxIndicatorIdle,
		clCheckboxIndicatorActive, clCheckboxCheckmark, clCheckboxBar, clCheckboxLabel,
		clHeadingBase, clSpinner, clVideo, clProse,
		clLink, clTextItalic, clTextUnderline, clTextNoWrap, clTruncate,
		clFlex, clGrid, clContainer, clTableWrap, clTable, clTableHead, clTableThBase, clTableTh, clTableTd, clTableRow, clTableTdC,
		clTableThSort, clTableSortBtn, clTableRowLink, clTableRowAlt, clTableTdStrong, clTableSelection, clDataList, clDataCount, clDataHeading, clDataDisclosure,
		clStepSegments, clStepSegment, clStepSegmentComplete, clStepSegmentActive,
		clTableStacked, clTableStack, clTableStackItem, clTableStackValue, clTableStackSort, clTableStackSortRow,
		// Generated lists compose DataList, including its empty and loading states.
		clEmpty, clEmptyPad, clEmptyBordered, clEmptyCompact, clEmptyTitle, clEmptyDesc,
		clSkeleton, clSkeletonText, clSkeletonLine, clSkeletonLineLast,
		clDetailList, clDetailHeader, clDetailTitle, clDetailDescription,
		clDetailItems, clDetailRow, clDetailRowSeparated, clDetailTerm,
		clDetailTermDescription, clDetailValue,
		clCardFrame, clCardSectioned, clCardBorder,
		clCardPadNone, clCardPadSmall, clCardPadDefault, clCardPadMedium, clCardPadLarge,
		clCardShadowSmall, clCardShadowMedium, clCardShadowLarge,
		clCardClickable, clCardHoverable, clCardTitle, clCardDesc,
		clCardHeader, clCardFooter, clCardImageVertical, clCardImageHorizontal,
		clImageCover, clImageContain,
		clCardHorizontal, clCardVertical,
		clBreadcrumb, clBreadcrumbSep, clBreadcrumbCur, clBreadcrumbItem,
		clChromeContext,
		clSidebarRootAdmin, clSidebarRootContent, clSidebarWidthCollapsed,
		clSidebarDisclosure, clSidebarDisclosureSummary, clSidebarDisclosurePanel,
		clSidebarWidthExpanded, clSidebarDisabled, clSidebarInner,
		clSidebarColumnAdmin, clSidebarColumnContent,
		clSidebarBrandAdmin, clSidebarBrandContent,
		clSidebarBrandLinkAdmin, clSidebarBrandLinkContent,
		clSidebarBrandTextAdmin, clSidebarBrandTextContent,
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
		clPagination, clPageBtn, clPageIdle, clPageCur,
	}
	return out
}

func shellVariantClassLists(out []style.ClassList) []style.ClassList {
	for _, m := range []map[string]style.ClassList{
		clButtonVariant, clButtonTone, clButtonSize, clButtonLoadingVariant, clButtonLoadingTone,
		clBadgeVariant, clBadgeTone, clBadgeSize, clBadgeDotTone, clAlertVariant,
		clIconSize, clIconTone, clInputSize, clInputTone, clSpinnerSize, clSpinnerTone,
		clDetailValueTone, clTextTransform,
		clSkeletonBlockSize, clSkeletonLineSize, clSkeletonCircleSize,
	} {
		for _, cl := range m {
			out = append(out, cl)
		}
	}
	for _, cl := range clHeadingLevel {
		out = append(out, cl)
	}
	return out
}

func shellInlineClassLists(out []style.ClassList) []style.ClassList {
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
		out = append(out, style.New().Gap(s), style.New().MarginBottom(s))
	}
	for _, n := range []int{1, 2, 3, 4, 6, 12} {
		out = append(out, style.New().GridCols(n))
		for _, at := range []style.Breakpoint{style.BreakpointSM, style.BreakpointMD, style.BreakpointLG} {
			out = append(out, style.New().Breakpoint(at, func(c style.ClassList) style.ClassList { return c.GridCols(n) }))
		}
	}
	for justification := range maps.Values(justifyContent) {
		out = append(out, style.New().Justify(justification))
	}
	out = append(out,
		style.New().Items(style.ItemsStart), style.New().Items(style.ItemsCenter),
		style.New().Items(style.ItemsEnd), style.New().Items(style.ItemsStretch),
		style.New().FlexDir(style.FlexRow), style.New().FlexDir(style.FlexCol), style.New().FlexWrap(),
		style.New().MaxWScaled(style.MaxWSM), style.New().MaxWScaled(style.MaxWMD),
		style.New().MaxWScaled(style.MaxWLG), style.New().MaxWScaled(style.MaxWXL),
		style.New().MaxWScaled(style.MaxW2XL), style.New().MaxWScaled(style.MaxW4XL),
		style.New().MaxWScaled(style.MaxW7XL), style.New().MaxWScaled(style.MaxWFull),
	)
	return out
}
