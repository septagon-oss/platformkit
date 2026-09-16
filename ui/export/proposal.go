package export

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components/examples"
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
	var value PropsProposal
	fields := map[string]any{"baseSHA256": &value.BaseSHA256, "path": &value.Path, "props": &value.Props}
	if err := decodeProposal(data, fields); err != nil {
		return err
	}
	if value.BaseSHA256 == "" || len(value.Path) == 0 {
		return fmt.Errorf("props proposal requires baseSHA256, a nonempty path and props")
	}
	*p = value
	return nil
}

// Both proposal types use one exact-field decoder. Each caller owns its required
// fields and semantic validation; no JSON data can add a source capability.
func decodeProposal(data []byte, fields map[string]any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') || !json.Valid(data) {
		return fmt.Errorf("source proposal requires one JSON object")
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := token.(string)
		field, found := fields[name]
		if !ok || !found {
			return fmt.Errorf("source proposal has unknown or repeated field %q", name)
		}
		if err := decoder.Decode(field); err != nil {
			return fmt.Errorf("source proposal %s: %w", name, err)
		}
		delete(fields, name)
	}
	if len(fields) != 0 {
		return fmt.Errorf("source proposal is missing required fields")
	}
	return nil
}

// ErrStaleExport means the caller must obtain the current source snapshot.
var ErrStaleExport = errors.New("source proposal base differs from current source export")

// ProjectProps conditionally projects one edit against the caller's authoritative
// examples, palette and stylesheet. It renders the current export, checks the
// content revision, copies the affected inputs, and renders the candidate.
// Ownership checks cover observed source occurrences, not arbitrary HTML emitted
// through opaque callbacks or separate buffers.
// Rendering runs trusted synchronous Go constructors; arbitrary callback effects
// are not rolled back. Neither source files nor persistent state are saved or
// locked. A persistence owner must perform its own atomic revision check.
// Failure returns nil examples and a zero export, never an accepted partial edit.
func ProjectProps(theme design.Pair, captures []examples.Example, proposal PropsProposal, extra ...ui.Extra) ([]examples.Example, DesignExport, error) {
	return projectSource(theme, captures, proposal.BaseSHA256, proposal.Path, func(_ DesignExport, root examples.Example) (examples.Example, error) {
		return root.WithPropsAt(proposal.Path, proposal.Props)
	}, extra...)
}

// Projection owns the two render passes and the common freshness, observation
// and interface checks. The edit copies source inputs between those passes.
func projectSource(theme design.Pair, captures []examples.Example, baseSHA256 string, path []string, edit func(DesignExport, examples.Example) (examples.Example, error), extra ...ui.Extra) ([]examples.Example, DesignExport, error) {
	base, err := Export(theme, captures, extra...)
	if err != nil {
		return nil, DesignExport{}, err
	}
	if baseSHA256 != base.SHA256 {
		return nil, DesignExport{}, ErrStaleExport
	}
	before, err := observedProposalTarget(base, path)
	if err != nil {
		return nil, DesignExport{}, err
	}
	index := slices.IndexFunc(captures, func(example examples.Example) bool { return example.ID == path[0] })
	edited, err := edit(base, captures[index])
	if err != nil {
		return nil, DesignExport{}, fmt.Errorf("source proposal %q: %w", path, err)
	}
	candidate := slices.Clone(captures)
	candidate[index] = edited
	projected, err := Export(theme, candidate, extra...)
	if err != nil {
		return nil, DesignExport{}, err
	}
	after, err := observedProposalTarget(projected, path)
	if err != nil {
		return nil, DesignExport{}, err
	}
	if !before.SameInterface(*after) {
		return nil, DesignExport{}, fmt.Errorf("source proposal %q changed the source interface", path)
	}
	return candidate, projected, nil
}

func observedProposalTarget(snapshot DesignExport, path []string) (*examples.ExampleDescription, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("source proposal requires a source occurrence path")
	}
	index := slices.IndexFunc(snapshot.Examples, func(example examples.ExampleDescription) bool { return example.ID == path[0] })
	if index < 0 {
		return nil, fmt.Errorf("source proposal has unknown root %q", path[0])
	}
	current := &snapshot.Examples[index]
	for _, id := range path[1:] {
		index := slices.IndexFunc(current.Children, func(child examples.ChildOccurrence) bool { return child.Description.ID == id })
		if len(current.OpaqueSlots) != 0 || index < 0 {
			return nil, fmt.Errorf("source proposal %q requires unambiguous declared ownership", path)
		}
		child := &current.Children[index]
		if child.Slot == "" || child.Span == nil {
			return nil, fmt.Errorf("source proposal %q requires an observed, directly owned child", path)
		}
		current = &child.Description
	}
	return current, nil
}
