package module

import (
	"fmt"

	"github.com/septagon-oss/platformkit/kit/screen"
)

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
