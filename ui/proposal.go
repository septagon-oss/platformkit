package ui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/components"
)

// PropsProposal addresses one source invocation, never a native editor object.
// Path contains the root ID followed by definition-local child IDs. Props uses
// Example.WithProps's exact-case, top-level typed patch semantics.
type PropsProposal struct {
	BaseSHA256 string          `json:"baseSHA256"`
	Path       []string        `json:"path"`
	Props      json.RawMessage `json:"props"`
}

// UnmarshalJSON refuses missing, repeated and unknown fields, including casing
// aliases. A rejected request leaves its receiver unchanged.
func (p *PropsProposal) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') || !json.Valid(data) {
		return fmt.Errorf("props proposal requires one JSON object")
	}
	var value PropsProposal
	fields := map[string]any{"baseSHA256": &value.BaseSHA256, "path": &value.Path, "props": &value.Props}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := token.(string)
		field, found := fields[name]
		if !ok || !found {
			return fmt.Errorf("props proposal has unknown or repeated field %q", name)
		}
		if err := decoder.Decode(field); err != nil {
			return fmt.Errorf("props proposal %s: %w", name, err)
		}
		delete(fields, name)
	}
	if len(fields) != 0 || value.BaseSHA256 == "" || len(value.Path) == 0 {
		return fmt.Errorf("props proposal requires baseSHA256, a nonempty path and props")
	}
	*p = value
	return nil
}

// ErrStaleExport means the caller must obtain the current source snapshot.
var ErrStaleExport = errors.New("props proposal base differs from current source export")

// ProjectProps conditionally projects one edit against the caller's authoritative
// examples, palette and stylesheet. It renders the current export, checks the
// content revision, copies the affected inputs, and renders the candidate.
// Ownership checks cover observed source occurrences, not arbitrary HTML emitted
// through opaque callbacks or separate buffers.
// Rendering runs trusted synchronous Go constructors; arbitrary callback effects
// are not rolled back. Neither source files nor persistent state are saved or
// locked. A persistence owner must perform its own atomic revision check.
// Failure returns nil examples and a zero export, never an accepted partial edit.
func ProjectProps(theme design.Pair, examples []components.Example, proposal PropsProposal, extra ...Extra) ([]components.Example, DesignExport, error) {
	base, err := Export(theme, examples, extra...)
	if err != nil {
		return nil, DesignExport{}, err
	}
	if proposal.BaseSHA256 != base.SHA256 {
		return nil, DesignExport{}, ErrStaleExport
	}
	before, err := observedProposalTarget(base, proposal.Path)
	if err != nil {
		return nil, DesignExport{}, err
	}
	index := slices.IndexFunc(examples, func(example components.Example) bool { return example.ID == proposal.Path[0] })
	edited, err := examples[index].WithPropsAt(proposal.Path, proposal.Props)
	if err != nil {
		return nil, DesignExport{}, fmt.Errorf("props proposal %q: %w", proposal.Path, err)
	}
	candidate := slices.Clone(examples)
	candidate[index] = edited
	projected, err := Export(theme, candidate, extra...)
	if err != nil {
		return nil, DesignExport{}, err
	}
	after, err := observedProposalTarget(projected, proposal.Path)
	if err != nil {
		return nil, DesignExport{}, err
	}
	if before.ComponentID != after.ComponentID {
		return nil, DesignExport{}, fmt.Errorf("props proposal %q changed the source interface", proposal.Path)
	}
	return candidate, projected, nil
}

func observedProposalTarget(snapshot DesignExport, path []string) (*components.ExampleDescription, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("props proposal requires a source occurrence path")
	}
	index := slices.IndexFunc(snapshot.Examples, func(example components.ExampleDescription) bool { return example.ID == path[0] })
	if index < 0 {
		return nil, fmt.Errorf("props proposal has unknown root %q", path[0])
	}
	current := &snapshot.Examples[index]
	for _, id := range path[1:] {
		index := slices.IndexFunc(current.Children, func(child components.ChildOccurrence) bool { return child.Description.ID == id })
		if len(current.OpaqueSlots) != 0 || index < 0 {
			return nil, fmt.Errorf("props proposal %q requires unambiguous declared ownership", path)
		}
		child := &current.Children[index]
		if child.Slot == "" || child.Span == nil {
			return nil, fmt.Errorf("props proposal %q requires an observed, directly owned child", path)
		}
		current = &child.Description
	}
	return current, nil
}
