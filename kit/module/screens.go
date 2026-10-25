package module

import (
	"fmt"

	"github.com/septagon-oss/platformkit/kit/screen"
)

// views reports this module's own custom-screen descriptors: every ref parses, is
// owned by this module, and is named once. A module that names no view adds nothing.
func (v *validation) views(m Module) {
	if err := validateViews(m); err != nil {
		v.add("%v", err)
	}
}

// validateViews is the same question asked of one module, as an error rather than
// as diagnostics, so a caller that holds one module can refuse it on its own.
func validateViews(m Module) error {
	seen := map[screen.ViewRef]bool{}
	for _, v := range m.Views {
		if _, err := screen.ParseView(string(v.Ref())); err != nil {
			return fmt.Errorf("module %s: %w", m.Name, err)
		}
		if v.Ref().Owner() != m.Name || seen[v.Ref()] {
			return fmt.Errorf("module %s: duplicate or foreign view %s", m.Name, v.Ref())
		}
		seen[v.Ref()] = true
	}
	return nil
}
