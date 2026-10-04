package internal

import (
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/css"
	"github.com/septagon-oss/platformkit/ui/style"
)

var (
	clPage       = style.New().MinHeightScreen().Bg(style.SurfaceSecondary).TextColor(style.FgPrimary)
	clHeader     = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).Justify(style.JustifyBetween).FlexWrap().Gap(style.S6).PaddingX(style.S6).PaddingY(style.S4).Bg(style.SurfacePrimary).BorderBottom(style.Border1).BorderColor(style.BorderPrimary)
	clBrand      = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).Gap(style.S3)
	clLogo       = style.New().Height(style.S8).Width(style.S8).ObjectContain()
	clTitle      = style.New().FontFamily(style.FontSerif).FontSize(style.TextXL).FontWeight(style.FontBold).Tracking(style.TrackingTight).TextColor(style.FgPrimary).NoUnderline()
	clNav        = style.New().Display(style.DisplayFlex).Items(style.ItemsCenter).FlexWrap().Gap(style.S4)
	clMain       = style.New().PaddingX(style.S6).PaddingY(style.S8)
	clFooter     = style.New().PaddingX(style.S6).PaddingY(style.S8).FontSize(style.TextXS).TextColor(style.FgMuted).BorderTop(style.Border1).BorderColor(style.BorderPrimary)
	clFooterCopy = style.New().MaxWScaled(style.MaxWProse)
)

func lists() []style.ClassList {
	return []style.ClassList{clPage, clHeader, clBrand, clLogo, clTitle, clNav, clMain, clFooter, clFooterCopy}
}

// prose scopes the shared typography to this site's own article hook.
func prose() *css.Sheet { return components.ProseStyleFor("[data-prose]") }
