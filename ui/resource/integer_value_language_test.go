package resource_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/ui/resource"
	g "maragu.dev/gomponents"
)

func TestIntegerResourceValuesRenderInTheRequestsLocale(t *testing.T) {
	catalog := xtext.Load("en", xtext.Source{Name: "resource", FS: resource.Catalogues()})
	r := resource.Resource{
		Schema: entity.Schema{Module: "measurement", Entity: "measurement", Fields: []entity.Field{
			{Name: "amount", Type: entity.TypeInt},
		}},
		Name: entity.Name{One: "Measurement", Other: "Measurements"}, Screen: "/app/measurement/measurements",
	}
	for _, tag := range []string{"en", "pt-PT"} {
		for _, value := range []any{int(12345), int64(-12345), int64(9007199254740993), uint64(18446744073709551615)} {
			t.Run(fmt.Sprintf("%s/%T/%v", tag, value, value), func(t *testing.T) {
				loc := catalog.Select(tag)
				want := loc.Text("integer.expected", "%d", value)
				row := map[string]any{"id": "1", "amount": value}
				for _, view := range []struct {
					name string
					body []g.Node
				}{
					{"detail", resource.Detail(r, resource.Options{Locale: &loc}, row, false).Body},
					{"list", resource.List(r, resource.Options{Locale: &loc}, []map[string]any{row}, 1, 1, "", false).Body},
				} {
					var out strings.Builder
					if err := g.Group(view.body).Render(&out); err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(out.String(), ">"+want+"<") {
						t.Errorf("%s must render the exact localized integer %q for %T(%v)", view.name, want, value, value)
					}
					if _, tail, broken := strings.Cut(out.String(), "%!"); broken {
						marker, _, _ := strings.Cut(tail, "<")
						t.Errorf("%s exposes a Go formatting error: %%!%s", view.name, marker)
					}
				}
			})
		}
	}
}
