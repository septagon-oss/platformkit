package components

import (
	"encoding/json"
	"fmt"
	"slices"

	g "maragu.dev/gomponents"
)

// WithPropsAt edits one captured invocation by its exact root ID followed by
// source-local child IDs. It does not render: directly declared but suppressed
// children can be edited. Callers needing a visible edit must separately check
// rendering evidence before and after the change; this is not persistence.
//
// Traversed ancestors must have only directly bound Node/[]Node inputs and zero
// values in other advertised slots. Hidden fields and closures are not inspected.
// The target retains its slots. Only its owning slot chain is immutably rebuilt
// through WithProps and WithSlot; no JSON value constructs a node.
func (e Example) WithPropsAt(path []string, patch json.RawMessage) (Example, error) {
	if err := e.validateCapture(); err != nil {
		return Example{}, err
	}
	if len(path) == 0 || path[0] != e.ID {
		return Example{}, fmt.Errorf("example path must begin with exact root identity %q", e.ID)
	}
	// Reuse the source projection's portable-schema, cycle and local-identity
	// checks, without executing a constructor or collecting rendered spans.
	record, err := newExampleRecord(e, e.bound, map[*exampleNode]bool{})
	if err != nil {
		return Example{}, err
	}
	return e.withPropsAt(record, path[1:], patch)
}

func (e Example) withPropsAt(record *exampleRecord, path []string, patch json.RawMessage) (Example, error) {
	if len(path) == 0 {
		return e.WithProps(patch)
	}
	if len(record.description.OpaqueSlots) != 0 {
		return Example{}, fmt.Errorf("example %q has opaque slot inputs %q", e.ID, record.description.OpaqueSlots)
	}
	child, exists := record.byID[path[0]]
	if !exists || child.node == nil || child.slot == "" {
		return Example{}, fmt.Errorf("example %q has no directly owned child %q", e.ID, path[0])
	}
	fields, err := exampleFields(e.slots.Type())
	if err != nil {
		return Example{}, err
	}
	slots := copyExampleValue(e.slots)
	var nodes []g.Node
	for _, field := range fields {
		value := exampleFieldValue(slots, field.index)
		if field.typ != exampleNodeType && field.typ != exampleNodesType {
			if !value.IsZero() {
				return Example{}, fmt.Errorf("example %q has nonzero unsupported slot %q", e.ID, field.name)
			}
			continue
		}
		if field.name != child.slot {
			continue
		}
		if field.typ == exampleNodesType {
			nodes = slices.Clone(value.Interface().([]g.Node))
		} else if !value.IsNil() {
			nodes = []g.Node{value.Interface().(g.Node)}
		}
	}
	index := slices.IndexFunc(nodes, func(node g.Node) bool {
		bound, ok := node.(*exampleNode)
		return ok && bound == child.node
	})
	if index < 0 {
		return Example{}, fmt.Errorf("example %q lost the declared slot ownership of %q", e.ID, path[0])
	}
	// Captures intentionally retain no preceding Example.Node chain. Restore a
	// view of this exact private capture before using the ordinary edit guards.
	nested := child.node.example
	nested.Node, nested.bound = child.node, child.node
	if err := nested.validateCapture(); err != nil {
		return Example{}, err
	}
	edited, err := nested.withPropsAt(child, path[1:], patch)
	if err != nil {
		return Example{}, err
	}
	nodes[index] = edited.Node
	return e.WithSlot(child.slot, nodes...)
}
