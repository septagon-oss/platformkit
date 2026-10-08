package examples_test

import (
	"bytes"
	"encoding/json"
	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"strings"
	"testing"
)

func TestSharedExactInt64PortableRoundTrip(t *testing.T) {
	for _, id := range []string{"pk-ui.component.quantity-input/overflow-boundary-en", "pk-ui.component.cart/default-en", "pk-ui.component.slot-picker/default-en"} {
		t.Run(id, func(t *testing.T) {
			for _, example := range examples.Gallery() {
				if example.ID != id {
					continue
				}
				before, err := example.Describe()
				if err != nil {
					t.Fatal(err)
				}
				var props map[string]any
				if err := json.Unmarshal(before.Props, &props); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(id, "quantity-input") {
					if props["value"] != "9007199254740993" {
						t.Fatalf("lost precision: %s", before.Props)
					}
					for _, bad := range []string{`{"value":9007199254740993}`, `{"value":"9223372036854775808"}`, `{"value":"01"}`, `{"value":"-0"}`} {
						if _, err := example.WithProps([]byte(bad)); err == nil {
							t.Fatalf("accepted %s", bad)
						}
					}
				}
				reapplied, err := example.WithProps(before.Props)
				if err != nil {
					t.Fatal(err)
				}
				after, err := reapplied.Describe()
				if err != nil || !bytes.Equal(before.Props, after.Props) || before.HTML != after.HTML {
					t.Fatalf("round trip changed output: %v", err)
				}
				return
			}
			t.Fatal("missing example")
		})
	}
	money := c.MoneyText{Money: c.Money{Minor: 9007199254740993, Currency: "EUR"}, Text: "exact", AccessibleText: "exact"}
	e := examples.ExampleOf(examples.ExampleInfo{ID: "exact", ComponentID: "pk-ui.component.buy-bar"}, c.BuyBarProps{Label: "Total", Total: &money, SummaryText: "Exact total"}, c.BuyBar)
	d, err := e.Describe()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d.Props), `"minor":"9007199254740993"`) {
		t.Fatalf("money not exact: %s", d.Props)
	}
}
