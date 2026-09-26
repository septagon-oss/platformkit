package components

// spinner.go renders the busy indicator, which is audible: the label is a Props
// with the authored English as its default.

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/style"
)

// Spinner renders SpinnerProps; the label is announced, the rotation is
// decoration.
func Spinner(p SpinnerProps) g.Node {
	return spinnerWithAppearance(
		p,
		variantOr(clSpinnerTone, normalizeSpinnerTone(p.Tone), "brand"),
	)
}

func spinnerWithAppearance(p SpinnerProps, appearance style.ClassList) g.Node {
	cl := clSpinner.
		Merge(variantOr(clSpinnerSize, p.Size, "md")).
		Merge(appearance)
	label := p.Label
	if label == "" {
		label = "Loading"
	}
	return h.Span(
		classes(cl.Compile(), p.Class),
		h.Role("status"), g.Attr("aria-label", label),
	)
}

func normalizeSpinnerTone(tone string) string {
	switch strings.ToLower(strings.TrimSpace(tone)) {
	case "neutral", "success", "warning", "danger", "info", "brand":
		return strings.ToLower(strings.TrimSpace(tone))
	default:
		return "brand"
	}
}
