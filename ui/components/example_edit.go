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
	location, err := e.locate(path)
	if err != nil {
		return Example{}, err
	}
	edited, err := location.target.WithProps(patch)
	if err != nil {
		return Example{}, err
	}
	return location.rebuild(edited)
}

// At resolves an exact source occurrence without rendering. Like WithPropsAt,
// it refuses ambiguous or unsupported ancestor inputs, but permits a directly
// declared child that is currently suppressed. The result remains bound to the
// original capture; subsequent edits return new captures, not mutable handles.
func (e Example) At(path []string) (Example, error) {
	location, err := e.locate(path)
	if err != nil {
		return Example{}, err
	}
	return location.target, nil
}

// WithReplacementAt replaces one typed invocation with another of the same
// declared interface. It keeps the destination's ExampleInfo and copies the
// replacement's complete props, slots and renderer; it never merges overrides.
// Both captures must be valid. Paths and owning slots follow WithPropsAt's
// non-rendering rules. Trusted nodes and closures are not cloned or inspected.
func (e Example) WithReplacementAt(path []string, replacement Example) (Example, error) {
	location, err := e.locate(path)
	if err != nil {
		return Example{}, err
	}
	if err := replacement.validateCapture(); err != nil {
		return Example{}, err
	}
	if location.target.render == nil || replacement.render == nil {
		return Example{}, fmt.Errorf("replacement requires two typed source captures")
	}
	record, err := newExampleRecord(replacement, replacement.bound, map[*exampleNode]bool{})
	if err != nil {
		return Example{}, err
	}
	if !location.record.description.SameInterface(record.description) {
		return Example{}, fmt.Errorf("replacement %q has a different source interface from %q", replacement.ID, path)
	}
	replacement.ExampleInfo = location.target.ExampleInfo
	return location.rebuild(replacement.capture())
}

type exampleLocation struct {
	target Example
	record *exampleRecord
	owners []exampleOwner
}

type exampleOwner struct {
	example Example
	slot    string
	nodes   []g.Node
	index   int
}

func (e Example) locate(path []string) (exampleLocation, error) {
	if err := e.validateCapture(); err != nil {
		return exampleLocation{}, err
	}
	if len(path) == 0 || path[0] != e.ID {
		return exampleLocation{}, fmt.Errorf("example path must begin with exact root identity %q", e.ID)
	}
	// Reuse the source projection's portable-schema, cycle and local-identity
	// checks, without executing a constructor or collecting rendered spans.
	record, err := newExampleRecord(e, e.bound, map[*exampleNode]bool{})
	if err != nil {
		return exampleLocation{}, err
	}
	location := exampleLocation{target: e, record: record}
	for _, id := range path[1:] {
		owner, child, err := location.target.ownedChild(location.record, id)
		if err != nil {
			return exampleLocation{}, err
		}
		location.owners = append(location.owners, owner)
		// Private captures retain no preceding Node chain. Restore the exact
		// capture before applying the ordinary public edit guards.
		location.target = child.node.example
		location.target.Node, location.target.bound = child.node, child.node
		if err := location.target.validateCapture(); err != nil {
			return exampleLocation{}, err
		}
		location.record = child
	}
	return location, nil
}

func (e Example) ownedChild(record *exampleRecord, id string) (exampleOwner, *exampleRecord, error) {
	if len(record.description.OpaqueSlots) != 0 {
		return exampleOwner{}, nil, fmt.Errorf("example %q has opaque slot inputs %q", e.ID, record.description.OpaqueSlots)
	}
	child, exists := record.byID[id]
	if !exists || child.node == nil || child.slot == "" {
		return exampleOwner{}, nil, fmt.Errorf("example %q has no directly owned child %q", e.ID, id)
	}
	fields, err := exampleFields(e.slots.Type())
	if err != nil {
		return exampleOwner{}, nil, err
	}
	slots := copyExampleValue(e.slots)
	var nodes []g.Node
	for _, field := range fields {
		value := exampleFieldValue(slots, field.index)
		if field.typ != exampleNodeType && field.typ != exampleNodesType {
			if !value.IsZero() {
				return exampleOwner{}, nil, fmt.Errorf("example %q has nonzero unsupported slot %q", e.ID, field.name)
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
		return exampleOwner{}, nil, fmt.Errorf("example %q lost the declared slot ownership of %q", e.ID, id)
	}
	return exampleOwner{e, child.slot, nodes, index}, child, nil
}

func (location exampleLocation) rebuild(edited Example) (Example, error) {
	for _, owner := range slices.Backward(location.owners) {
		owner.nodes[owner.index] = edited.Node
		var err error
		edited, err = owner.example.WithSlot(owner.slot, owner.nodes...)
		if err != nil {
			return Example{}, err
		}
	}
	return edited, nil
}
