package components

// helpers.go holds the two helpers more than one component family needs: the
// variant lookup that turns a Props string into the class list styling it, and
// the ARIA boolean, which is the word "true" or "false" and never "1".

import (
	"github.com/septagon-oss/platformkit/ui/style"
)

func variantOr(m map[string]style.ClassList, key, fallback string) style.ClassList {
	if cl, ok := m[key]; ok {
		return cl
	}
	return m[fallback]
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
