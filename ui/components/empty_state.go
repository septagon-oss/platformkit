package components

// empty_state.go renders the nothing-here panel: a heading, a sentence and the
// one action that fills it.

import (
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
	if p.Description != "" {
		children = append(children, h.P(h.Class(clEmptyDesc.Compile()), g.Raw("<!--pk-text:description-->"), g.Text(p.Description), g.Raw("<!--/pk-text:description-->")))
	}
	children = append(children, slots.Actions...)
	return h.Div(children...)
}
