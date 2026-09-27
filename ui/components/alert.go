package components

// alert.go renders the alert, its dismissal and the icon each tone defaults to.

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// Alert renders AlertProps with role="alert" for danger/warning and
// role="status" otherwise, so severity maps to interruption behavior.
func Alert(p AlertProps) g.Node {
	return AlertWithSlots(p, AlertSlots{})
}

// AlertSlots carries trusted Go adornments and actions around the message.
type AlertSlots struct {
	IconStart, Actions []g.Node
}

// AlertWithSlots retains the same status semantics and dismissal behavior as Alert.
func AlertWithSlots(p AlertProps, slots AlertSlots) g.Node {
	iconStart, actions := slots.IconStart, slots.Actions
	tone := p.Tone
	if tone == "" {
		tone = "info"
	}
	cl := clAlertBase.Merge(variantOr(clAlertVariant, tone, "info"))
	if p.Compact {
		cl = cl.Merge(clAlertCompact)
	} else {
		cl = cl.Merge(clAlertRegular)
	}
	if p.Bordered {
		cl = cl.Merge(clAlertBordered)
	}
	role := "status"
	live := "polite"
	if tone == "danger" || tone == "warning" {
		role = "alert"
		live = "assertive"
	}
	body := []g.Node{h.Class(clAlertBody.Compile())}
	if p.Title != "" {
		body = append(body, h.P(h.Class(clAlertTitle.Compile()), g.Raw("<!--pk-text:title-->"), g.Text(p.Title), g.Raw("<!--/pk-text:title-->")))
	}
	body = append(body, h.P(h.Class(clAlertMessage.Compile()), g.Raw("<!--pk-text:message-->"), g.Text(p.Message), g.Raw("<!--/pk-text:message-->")))

	var children []g.Node
	children = append(children, baseAttrs(p.ComponentProps)...)
	children = append(
		children,
		classes(cl.Compile(), p.Class),
		h.Role(role),
		g.Attr("aria-live", live),
		g.Attr("aria-atomic", "true"),
		g.Attr("data-component", "alert"),
		g.Attr("data-alert-tone", tone),
	)
	if p.Dismissible {
		children = append(
			children,
			g.Attr("data-controller", "alert"),
			g.Attr("data-alert-dismissible-value", "true"),
		)
	}
	if len(iconStart) == 0 {
		iconStart = []g.Node{Icon(IconProps{
			Name: defaultAlertIcon(tone),
			Size: "sm",
			Tone: tone,
		})}
	}
	if len(iconStart) > 0 {
		children = append(children, h.Span(
			h.Class(clAlertIcon.Compile()),
			g.Attr("data-alert-icon", ""),
			g.Group(iconStart),
		))
	}
	children = append(children, h.Div(body...))
	if len(actions) > 0 {
		children = append(children, h.Div(
			h.Class(clAlertActions.Compile()),
			g.Attr("data-alert-actions", ""),
			g.Group(actions),
		))
	}
	if p.Dismissible {
		children = append(children, h.Button(
			h.Type("button"),
			h.Class(clAlertClose.Compile()),
			g.Attr("data-action", "click->alert#dismiss"),
			g.Attr("data-alert-close", ""),
			g.Attr("aria-label", fallbackText(strings.TrimSpace(p.DismissLabel), "Dismiss notification")),
			glyph("x-mark"),
		))
	}
	return h.Div(children...)
}

func defaultAlertIcon(tone string) string {
	switch tone {
	case "success":
		return "check-circle"
	case "warning":
		return "exclamation-triangle"
	case "danger":
		return "x-circle"
	default:
		return "information-circle"
	}
}
