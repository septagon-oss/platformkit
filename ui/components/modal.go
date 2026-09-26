package components

// modal.go renders the dialog: the overlay, the panel, its accessible name and
// the buttons and form a caller puts inside it. The interaction contract is the
// shared runtime controller, addressed by the data attributes written here.

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

func defaultTrue(value *bool) bool {
	return value == nil || *value
}

// ModalSlots is the trusted Go composition seam for rich dialog regions.
// Portable clients use ModalProps body/footer strings; server-rendered Go
// applications use these slots without recreating modal chrome or behavior.
type ModalSlots struct {
	Header []g.Node
	Body   []g.Node
	Footer []g.Node
}

// Modal renders a portable modal contract.
func Modal(p ModalProps) g.Node {
	return ModalWithSlots(p, ModalSlots{})
}

// ModalWithSlots renders the canonical modal root. Deferred roots deliberately
// contain no panel: HTMX swaps a ModalPanelWithSlots response into the root and
// the shared htmx-modal controller opens it after the swap.
func ModalWithSlots(p ModalProps, slots ModalSlots) g.Node {
	size := modalSize(p.Size)
	open := p.Open && !p.Hidden
	state := "closed"
	if open {
		state = "open"
	}
	centered := defaultTrue(p.Centered)
	closeOnEscape := defaultTrue(p.CloseOnEscape)
	clearOnClose := p.Deferred
	if p.ClearOnClose != nil {
		clearOnClose = *p.ClearOnClose
	}

	rootClass := clModalRoot.Merge(clModalCentered)
	if !centered {
		rootClass = clModalRoot.Merge(clModalBottomSheet)
	}
	rootProps := p.ComponentProps
	rootProps.Class = ""
	rootProps.Disabled = false
	rootProps.Hidden = false
	root := baseAttrs(rootProps)
	root = append(root,
		classes(rootClass.Compile(), p.Class),
		g.Attr("data-component", "modal"),
		g.Attr("data-controller", "htmx-modal"),
		g.Attr("data-htmx-modal-open-value", boolText(open)),
		g.Attr("data-htmx-modal-close-on-escape-value", boolText(closeOnEscape)),
		g.Attr("data-htmx-modal-clear-on-close-value", boolText(clearOnClose)),
		g.Attr("data-state", state),
		g.Attr("aria-hidden", boolText(!open)),
		g.Attr("tabindex", "-1"),
	)
	if p.Disabled {
		root = append(root, g.Attr("aria-disabled", "true"))
	}
	if !open {
		root = append(root, g.Attr("hidden"), h.Style("display:none"))
	}

	actions := make([]string, 0, 2)
	if p.OpenOnSwap {
		actions = append(actions, "htmx:afterSettle->htmx-modal#show")
	}
	if p.Deferred && defaultTrue(p.CloseOnOverlay) && !p.Disabled {
		actions = append(actions, "click->htmx-modal#backdropClick")
	}
	if len(actions) > 0 {
		root = append(root, g.Attr("data-action", strings.Join(actions, " ")))
	}
	if p.Deferred {
		return h.Dialog(root...)
	}

	root = append(root, h.Role("dialog"), g.Attr("aria-modal", "true"))
	titleID := modalTitleID(p, slots)
	accessibleName := modalAccessibleName(p)
	if titleID != "" && strings.TrimSpace(p.AriaLabel) == "" {
		root = append(root, g.Attr("aria-labelledby", titleID))
	} else {
		root = append(root, g.Attr("aria-label", accessibleName))
	}
	if defaultTrue(p.ShowOverlay) {
		overlay := []g.Node{
			h.Class(clModalOverlay.Compile()),
			g.Attr("data-modal-backdrop", ""),
			g.Attr("aria-hidden", "true"),
		}
		if defaultTrue(p.CloseOnOverlay) && !p.Disabled {
			overlay = append(overlay, g.Attr("data-action", "click->htmx-modal#close"))
		}
		root = append(root, h.Div(overlay...))
	}
	root = append(root, modalPanel(p, slots, false, size))
	if open {
		root = append(root, g.Attr("open"))
	}
	return h.Dialog(root...)
}

// ModalPanelWithSlots renders the panel fragment returned by an HTMX endpoint.
// It carries the accessible name the native deferred dialog adopts when opened.
func ModalPanelWithSlots(p ModalProps, slots ModalSlots) g.Node {
	return modalPanel(p, slots, true, modalSize(p.Size))
}

// ModalPanel renders a server-loaded modal panel with one rich body node.
func ModalPanel(p ModalProps, body g.Node) g.Node {
	return ModalPanelWithSlots(p, ModalSlots{Body: []g.Node{body}})
}

func modalPanel(p ModalProps, slots ModalSlots, standalone bool, size string) g.Node {
	closable := defaultTrue(p.Closable)
	showClose := defaultTrue(p.ShowClose)
	titleID := modalTitleID(p, slots)
	closeLabel := strings.TrimSpace(p.CloseLabel)
	if closeLabel == "" {
		closeLabel = "Close"
	}

	headerContent := slots.Header
	if len(headerContent) == 0 && (p.Title != "" || p.Description != "") {
		text := make([]g.Node, 0, 2)
		if p.Title != "" {
			title := []g.Node{h.Class(clModalTitle.Compile()), g.Text(p.Title)}
			if titleID != "" {
				title = append([]g.Node{h.ID(titleID)}, title...)
			}
			text = append(text, h.H2(title...))
		}
		if p.Description != "" {
			text = append(text, h.P(h.Class(clModalDescription.Compile()), g.Text(p.Description)))
		}
		headerContent = []g.Node{h.Div(h.Class(clModalTitleBlock.Compile()), g.Group(text))}
	}
	header := append([]g.Node(nil), headerContent...)
	if closable && showClose {
		header = append(header, ModalCloseButton(closeLabel, ""))
	}

	body := slots.Body
	if len(body) == 0 && p.Body != "" {
		body = []g.Node{g.Text(p.Body)}
	}
	footer := slots.Footer
	if len(footer) == 0 && p.Footer != "" {
		footer = []g.Node{g.Text(p.Footer)}
	}

	panel := []g.Node{
		h.Class(clModalPanel.Merge(clModalPanelSize[size]).Compile()),
		g.Attr("data-modal-panel", ""),
		g.Attr("data-action", "click->htmx-modal#stopPropagation"),
		g.Attr("tabindex", "-1"),
	}
	if standalone {
		panel = append(panel, h.Role("dialog"), g.Attr("aria-modal", "true"))
		if titleID != "" && strings.TrimSpace(p.AriaLabel) == "" {
			panel = append(panel, g.Attr("aria-labelledby", titleID))
		} else {
			panel = append(panel, g.Attr("aria-label", modalAccessibleName(p)))
		}
	}
	if len(header) > 0 {
		panel = append(panel, h.Div(h.Class(clModalHeader.Compile()), g.Group(header)))
		panel = append(panel, modalSeparator("header"))
	}
	panel = append(panel, h.Div(
		h.Class(clModalBody.Compile()),
		g.Attr("data-modal-body", ""),
		g.Group(body),
	))
	if len(footer) > 0 {
		panel = append(panel, modalSeparator("footer"))
		panel = append(panel, h.Div(
			h.Class(clModalFooter.Compile()),
			g.Attr("data-modal-footer", ""),
			g.Group(footer),
		))
	}
	return h.Div(panel...)
}

// modalSeparator is explicit structure rather than a side-border utility so
// browser and native design renderers receive the same one-pixel boundary.
func modalSeparator(section string) g.Node {
	return h.Div(
		h.Class(clModalSeparator.Compile()),
		g.Attr("data-modal-separator", section),
		g.Attr("aria-hidden", "true"),
	)
}

func modalSize(value string) string {
	size := strings.ToLower(strings.TrimSpace(value))
	if size == "fullscreen" {
		size = "full"
	}
	if _, ok := clModalPanelSize[size]; !ok {
		return "medium"
	}
	return size
}

func modalTitleID(p ModalProps, slots ModalSlots) string {
	if p.ID == "" || p.Title == "" || len(slots.Header) > 0 {
		return ""
	}
	return p.ID + "-title"
}

func modalAccessibleName(p ModalProps) string {
	if label := strings.TrimSpace(p.AriaLabel); label != "" {
		return label
	}
	if title := strings.TrimSpace(p.Title); title != "" {
		return title
	}
	return "Dialog"
}

// ModalCloseButton creates the canonical controller-backed icon close action.
func ModalCloseButton(label, class string) g.Node {
	if strings.TrimSpace(label) == "" {
		label = "Close"
	}
	return h.Button(
		h.Type("button"), classes(clModalClose.Compile(), class),
		g.Attr("data-action", "click->htmx-modal#close"),
		g.Attr("data-modal-close", ""), g.Attr("aria-label", label),
		Icon(IconProps{Name: "x", Size: "md"}),
	)
}

// ModalCancelButton creates a text dismissal action for modal footers.
func ModalCancelButton(label, class string) g.Node {
	if strings.TrimSpace(label) == "" {
		label = "Cancel"
	}
	return h.Button(
		h.Type("button"), classes(clModalCancel.Compile(), class),
		g.Attr("data-action", "click->htmx-modal#close"),
		g.Attr("data-modal-cancel", ""), g.Text(label),
	)
}

// ModalForm closes its owning modal after a successful HTMX request.
func ModalForm(attrs ...g.Node) g.Node {
	nodes := make([]g.Node, 0, len(attrs)+1)
	nodes = append(nodes, attrs...)
	nodes = append(nodes, g.Attr("data-action", "htmx:afterRequest->htmx-modal#closeOnSuccess"))
	return h.Form(nodes...)
}
