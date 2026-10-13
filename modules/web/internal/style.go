package internal

import (
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/css"
	"github.com/septagon-oss/platformkit/ui/style"
)

var (
	clPage   = style.New().MinHeightScreen().Bg(style.SurfaceSecondary).TextColor(style.FgPrimary)
	clHeader = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).Justify(style.JustifyBetween).FlexWrap().Gap(style.S6).PaddingX(style.S6).PaddingY(style.S4).Bg(style.SurfacePrimary).BorderBottom(style.Border1).BorderColor(style.BorderPrimary)
	clBrand  = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).Gap(style.S3)
	clLogo   = style.New().Height(style.S8).Width(style.S8).ObjectContain()
	clTitle  = style.New().FontFamily(style.FontSerif).FontSize(style.TextXL).FontWeight(style.FontBold).Tracking(style.TrackingTight).TextColor(style.FgPrimary).NoUnderline()
	clNav    = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).FlexWrap().Gap(style.S4)
	clMain   = style.New().PaddingX(style.S6).PaddingY(style.S8)
	clFooter = style.New().PaddingX(style.S6).PaddingY(style.S8).FontSize(style.TextXS).TextColor(style.FgMuted).BorderTop(style.Border1).BorderColor(style.BorderPrimary)
	// clMeasure is the site's reading measure. The container is the page's width and not a
	// measure: an uncapped paragraph of this site runs to 156 characters of gate measure at
	// 1440px, which the design floor refuses above 75. The cap is the 64 characters the
	// floor's own prose measure counts, taken in rem rather than ch because rem holds there
	// whatever face the visitor has — a cap in ch widens with a font whose “0” is wide,
	// and the floor measures the line, not the unit. The footer line takes the foundation's
	// prose cap (clFooterCopy) at its own xs size rather than a second cap of this site's.
	clMeasure = style.New().MaxWScaled(style.MaxWLG)
	// clFooterCopy caps the footer line at the foundation's prose measure. See lists().
	clFooterCopy = style.New().MaxWScaled(style.MaxWProse)
)

func lists() []style.ClassList {
	return []style.ClassList{clPage, clHeader, clBrand, clLogo, clTitle, clNav, clMain, clFooter, clMeasure, clFooterCopy}
}

// prose scopes the shared typography to this site's own article hook.
func prose() *css.Sheet { return components.ProseStyleFor("[data-prose]") }
