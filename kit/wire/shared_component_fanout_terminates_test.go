package wire_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/wire"
)

// A component that several properties reach is expanded once per path, so a layered fan-out
// multiplies the visits. The comparison must finish promptly and either compare in full or
// refuse; it must never return nothing for a pair whose leaf type differs.
func TestSharedComponentFanOutEitherComparesOrRefuses(t *testing.T) {
	t.Parallel()
	doc := func(leaf string) []byte {
		d := map[string]any{}
		_ = json.Unmarshal(baseline("openapi"), &d)
		schemas := map[string]any{"L0": map[string]any{"type": leaf}}
		for i := 1; i <= 12; i++ {
			schemas[fmt.Sprintf("L%d", i)] = map[string]any{"type": "object", "properties": map[string]any{
				"a": map[string]any{"$ref": fmt.Sprintf("#/components/schemas/L%d", i-1)},
				"b": map[string]any{"$ref": fmt.Sprintf("#/components/schemas/L%d", i-1)},
			}}
		}
		comps, _ := d["components"].(map[string]any)
		if comps == nil {
			comps = map[string]any{}
			d["components"] = comps
		}
		comps["schemas"] = schemas
		media := at(d, "paths", "/items", "post", "requestBody", "content", "application/json")
		media["schema"] = map[string]any{"$ref": "#/components/schemas/L12"}
		out, _ := json.Marshal(d)
		return out
	}
	start := time.Now()
	breaks := wire.Compare(doc("string"), doc("integer"))
	if len(breaks) == 0 {
		t.Fatal("a changed leaf under a shared fan-out compared as no break")
	}
	if took := time.Since(start); took > 20*time.Second {
		t.Fatalf("the comparison took %s", took)
	}
}
