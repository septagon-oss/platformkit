package screen

import (
	"fmt"
	"strings"
)

// Symbol returns an injective Go identifier for a canonical view or anchor.
// Nonalphanumeric source bytes, including underscores, use _HH encoding.
func Symbol(ref string) (string, error) {
	view, anchor, anchored := strings.Cut(ref, "#")
	if anchored {
		if _, err := ParseAnchor(ref); err != nil {
			return "", err
		}
	} else if _, err := ParseView(ref); err != nil {
		return "", err
	}
	parts := strings.Split(view, "/")
	prefix := "View__"
	if anchored {
		prefix = "Anchor__"
		parts = append(parts, strings.Split(anchor, ".")...)
	}
	for i, part := range parts {
		decoded, err := decode(part)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for j := range len(decoded) {
			c := decoded[j]
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
				b.WriteByte(c)
			} else {
				fmt.Fprintf(&b, "_%02X", c)
			}
		}
		parts[i] = b.String()
	}
	return prefix + strings.Join(parts, "__"), nil
}
