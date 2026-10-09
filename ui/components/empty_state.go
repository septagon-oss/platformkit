package components

// empty_state.go renders the nothing-here panel: a heading, a sentence and the
// one action that fills it.

import (
	"cmp"
	"fmt"
	"io"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// EmptyState renders EmptyStateProps.
func EmptyState(p EmptyStateProps) g.Node {
	return EmptyStateWithSlots(p, EmptyStateSlots{})
}

// EmptyStateSlots carries trusted Go adornments and recovery actions.
type EmptyStateSlots struct {
	IconStart, Actions []g.Node
}

// EmptyStateWithSlots composes content through the existing empty-state renderer.
func EmptyStateWithSlots(p EmptyStateProps, slots EmptyStateSlots) g.Node {
	if err := p.Validate(); err != nil {
		return g.NodeFunc(func(io.Writer) error { return err })
	}
	if p.Action != nil && len(slots.Actions) != 0 {
		return g.NodeFunc(func(io.Writer) error { return fmt.Errorf("EmptyState: Action and Actions slot are exclusive") })
	}
	cl := clEmpty.Merge(clEmptyPad)
	if p.Compact {
		cl = clEmpty.Merge(clEmptyCompact)
	}
	if p.Bordered {
		cl = cl.Merge(clEmptyBordered)
	}
	var children []g.Node
	children = append(children, baseAttrs(p.ComponentProps)...)
	children = append(children, classes(cl.Compile(), p.Class))
	children = append(children, slots.IconStart...)
	children = append(children, h.P(h.Class(clEmptyTitle.Compile()), g.Raw("<!--pk-text:title-->"), g.Text(p.Title), g.Raw("<!--/pk-text:title-->")))
	text := cmp.Or(p.Text, p.Description)
	if text != "" {
		region := "description"
		if p.Text != "" {
			region = "text"
		}
		children = append(children, h.P(h.Class(clEmptyDesc.Compile()), g.Raw("<!--pk-text:"+region+"-->"), g.Text(text), g.Raw("<!--/pk-text:"+region+"-->")))
	}
	if p.Action != nil {
		children = append(children, recoveryAction(*p.Action, p.Disabled))
	}
	children = append(children, slots.Actions...)
	return h.Div(children...)
}

// Validate preserves legacy empty values while validating the new action contract.
func (p EmptyStateProps) Validate() error {
	return validateRecoveryAction(p.Action)
}
