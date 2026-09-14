package examples

import (
	"bytes"
	"cmp"
	"slices"
)

// SameInterface compares source-generated component contracts, not arbitrary
// JSON Schema equivalence or native editor capabilities. Identity, editability,
// canonical schema bytes and every named slot declaration must agree. Slot
// declaration order, invocation metadata, values and content are not interfaces.
// Equal read-only descriptions remain read-only; equality grants no edit right.
func (d ExampleDescription) SameInterface(other ExampleDescription) bool {
	if d.ComponentID != other.ComponentID || d.PropsEditable != other.PropsEditable || !bytes.Equal(d.Schema, other.Schema) {
		return false
	}
	left, right := slices.Clone(d.Slots), slices.Clone(other.Slots)
	byName := func(a, b SlotDescription) int { return cmp.Compare(a.Name, b.Name) }
	slices.SortFunc(left, byName)
	slices.SortFunc(right, byName)
	return slices.Equal(left, right)
}
