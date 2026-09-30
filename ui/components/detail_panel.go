package components

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// DetailPanelSlots uses the existing modal frame; actions remain native caller forms.
type DetailPanelSlots struct {
	Header, Body, Actions, EmptyAction, RetryAction []g.Node
}

func (p DetailPanelProps) Validate() error {
	for _, value := range []string{p.ID, p.Label, p.CloseLabel, p.ReturnHref} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("DetailPanel: ID, Label, CloseLabel and ReturnHref are required")
		}
	}
	if err := p.State.Validate(); err != nil {
		return err
	}
	switch p.Size {
	case "", "small", "medium", "large", "full":
	default:
		return fmt.Errorf("DetailPanel: unknown Size")
	}
	if p.State.ready() {
		if strings.TrimSpace(p.ItemID) == "" || strings.TrimSpace(p.Title) == "" {
			return fmt.Errorf("DetailPanel: ready requires ItemID and Title")
		}
	} else if p.ItemID != "" || p.Title != "" || p.Description != "" {
		return fmt.Errorf("DetailPanel: absent states must clear item content")
	}
	return nil
}

func (p DetailSheetProps) Validate() error {
	if err := p.DetailPanelProps.Validate(); err != nil {
		return err
	}
	switch p.Placement {
	case "", "auto", "bottom", "end":
		return nil
	}
	return fmt.Errorf("DetailSheet: unknown Placement")
}

func DetailSheet(p DetailSheetProps) g.Node { return DetailSheetWithSlots(p, DetailPanelSlots{}) }

func DetailSheetWithSlots(p DetailSheetProps, slots DetailPanelSlots) g.Node {
	if err := p.Validate(); err != nil {
		return g.NodeFunc(func(io.Writer) error { return err })
	}
	if err := validateDetailSlots(p.State, slots); err != nil {
		return g.NodeFunc(func(io.Writer) error { return err })
	}
	modal, content := detailFrame(p.DetailPanelProps, slots)
	modal.Open, modal.Placement = p.Open, cmp.Or(p.Placement, "auto")
	modal.CloseOnOverlay, modal.CloseOnEscape = p.CloseOnOverlay, p.CloseOnEscape
	modal.Attrs["data-detail-panel"] = "sheet"
	return ModalWithSlots(modal, content)
}

func SidePanel(p DetailPanelProps) g.Node { return SidePanelWithSlots(p, DetailPanelSlots{}) }

func SidePanelWithSlots(p DetailPanelProps, slots DetailPanelSlots) g.Node {
	if err := p.Validate(); err != nil {
		return g.NodeFunc(func(io.Writer) error { return err })
	}
	if err := validateDetailSlots(p.State, slots); err != nil {
		return g.NodeFunc(func(io.Writer) error { return err })
	}
	modal, content := detailFrame(p, slots)
	root := baseAttrs(modal.ComponentProps)
	root = append(root, classes(clDetailPanel.Compile(), p.Class), g.Attr("data-component", "side-panel"),
		g.Attr("data-detail-panel", "side"), g.Attr("aria-label", p.Label))
	return h.Aside(append(root, modalPanel(modal, content, false, modalSize(p.Size)))...)
}

func detailFrame(p DetailPanelProps, slots DetailPanelSlots) (ModalProps, ModalSlots) {
	props := p.ComponentProps
	props.Class, props.Disabled = "", false // Busy never disables Escape or the return link.
	props.Attrs = maps.Clone(props.Attrs)
	if props.Attrs == nil {
		props.Attrs = map[string]string{}
	}
	props.Attrs["data-detail-item"] = p.ItemID
	props.Attrs["aria-busy"] = boolText(p.Busy || p.State.Status == MediaLoading)
	modal := ModalProps{ComponentProps: props, Title: p.Title, Description: p.Description, AriaLabel: p.Label,
		CloseLabel: p.CloseLabel, Size: p.Size, ShowClose: new(false)}
	title := p.Title
	if !p.State.ready() {
		title = cmp.Or(p.State.Title, p.Label)
	}
	header := []g.Node{h.Div(h.Class(clModalTitleBlock.Compile()),
		h.H2(h.Class(clModalTitle.Compile()), h.ID(p.ID+"-title"), g.Attr("tabindex", "-1"), g.Text(title)),
		g.If(p.Description != "", h.P(h.Class(clModalDescription.Compile()), g.Text(p.Description))))}
	if p.State.ready() {
		header = append(header, slots.Header...)
	}
	header = append(header, recoveryAction(ButtonProps{Label: p.CloseLabel, Href: p.ReturnHref, Variant: "ghost",
		ComponentProps: ComponentProps{Attrs: map[string]string{"data-detail-close": ""}}}, false))
	body := []g.Node{contentStateNode(p.State, slots.EmptyAction, slots.RetryAction, Skeleton(SkeletonProps{Shape: "text", Lines: 3}))}
	var actions []g.Node
	if p.State.ready() {
		body = append(body, slots.Body...)
		actions = slots.Actions
	}
	return modal, ModalSlots{Header: header, Body: body, Footer: actions}
}

// Absent content must be cleared at the capture boundary as well as in HTML.
func validateDetailSlots(state ContentState, slots DetailPanelSlots) error {
	if !state.ready() && (len(slots.Header) != 0 || len(slots.Body) != 0 || len(slots.Actions) != 0) {
		return fmt.Errorf("DetailPanel: absent states must clear protected slots")
	}
	return nil
}
