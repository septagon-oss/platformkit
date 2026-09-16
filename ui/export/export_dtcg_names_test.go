package export

import (
	"net/url"
	"strings"
	"testing"
)

func TestDTCGNameEncodingIsReversibleAndCollisionFree(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, name := range []string{"0.5", "0%2E5", "0%252E5", "$root", "{a.b}", "a/b~c", "日本語", "--brand"} {
		encoded := dtcgName(name)
		decoded, err := url.PathUnescape(encoded)
		if err != nil || decoded != name || seen[encoded] || strings.ContainsAny(encoded, ".{}$") {
			t.Fatalf("ambiguous or invalid DTCG name %q -> %q -> %q: %v", name, encoded, decoded, err)
		}
		seen[encoded] = true
	}
}
