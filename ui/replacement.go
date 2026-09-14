package ui

import (
	"fmt"
	"slices"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/components/examples"
)

// ReplacementProposal selects two invocations from the authoritative source
// snapshot. Both paths contain exact root and local child IDs, never provider
// IDs, slot names or indexes. ReplacementPath supplies the complete replacement
// inputs and renderer; Path supplies the retained destination metadata. There
// is no implicit override preservation, conversion or cross-interface coercion.
type ReplacementProposal struct {
	BaseSHA256      string   `json:"baseSHA256"`
	Path            []string `json:"path"`
	ReplacementPath []string `json:"replacementPath"`
}

// UnmarshalJSON refuses missing, repeated, unknown and casing-aliased fields.
// A rejected request leaves its receiver unchanged.
func (p *ReplacementProposal) UnmarshalJSON(data []byte) error {
	var value ReplacementProposal
	fields := map[string]any{"baseSHA256": &value.BaseSHA256, "path": &value.Path, "replacementPath": &value.ReplacementPath}
	if err := decodeProposal(data, fields); err != nil {
		return err
	}
	if value.BaseSHA256 == "" || len(value.Path) == 0 || len(value.ReplacementPath) == 0 {
		return fmt.Errorf("replacement proposal requires baseSHA256 and two nonempty source paths")
	}
	*p = value
	return nil
}

// ProjectReplacement conditionally replaces one typed source invocation through
// Example.WithReplacementAt. It shares ProjectProps's revision, ownership and
// two-render validation boundary, with the same trust and persistence limits.
// The selected source must also be directly owned and observed in the base.
// Paths may overlap: immutable copies always read the base, not earlier writes.
// Self/same-content replacements are permitted and can retain the export hash.
// Native adapters must separately prove correspondence and edit/save fidelity.
func ProjectReplacement(theme design.Pair, captures []examples.Example, proposal ReplacementProposal, extra ...Extra) ([]examples.Example, DesignExport, error) {
	return projectSource(theme, captures, proposal.BaseSHA256, proposal.Path, func(base DesignExport, root examples.Example) (examples.Example, error) {
		if _, err := observedProposalTarget(base, proposal.ReplacementPath); err != nil {
			return examples.Example{}, err
		}
		index := slices.IndexFunc(captures, func(example examples.Example) bool { return example.ID == proposal.ReplacementPath[0] })
		replacement, err := captures[index].At(proposal.ReplacementPath)
		if err != nil {
			return examples.Example{}, err
		}
		return root.WithReplacementAt(proposal.Path, replacement)
	}, extra...)
}
