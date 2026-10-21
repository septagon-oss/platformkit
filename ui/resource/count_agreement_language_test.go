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

// TestThePortugueseCountEndsWithItsNoun reads the list's count line through the
// catalogue this package ships, in pt-PT. The noun is the only word in that line
// that can agree with the number and with its own gender, because the count
// selects it; a fixed word after it cannot do either, and a Portuguese reader of a
// one-row list or of a feminine noun meets the disagreement on every page. So the
// line is the number and the noun, and nothing after the noun: "1 utilizador",
// "3 tarefas".
func TestThePortugueseCountEndsWithItsNoun(t *testing.T) {
	t.Parallel()
	catalog := xtext.Load("en", xtext.Source{Name: "resource", FS: resource.Catalogues()})
	pt := catalog.Select("pt-PT")
	for _, c := range []struct {
		module, entity string
		name           entity.Name
		total          int64
		want           string
	}{
		{"people", "person", entity.Name{One: "Utilizador", Other: "Utilizadores"}, 1, "1 utilizador"},
		{"people", "person", entity.Name{One: "Utilizador", Other: "Utilizadores"}, 5, "5 utilizadores"},
		{"work", "chore", entity.Name{One: "Tarefa", Other: "Tarefas"}, 1, "1 tarefa"},
		{"work", "chore", entity.Name{One: "Tarefa", Other: "Tarefas"}, 3, "3 tarefas"},
	} {
		t.Run(fmt.Sprintf("%s/%d", c.entity, c.total), func(t *testing.T) {
			r := resource.Resource{
				Schema: entity.Schema{Module: c.module, Entity: c.entity, Fields: []entity.Field{
					{Name: "title", Type: entity.TypeString},
				}},
				Name: c.name, Screen: "/app/" + c.module + "/" + c.entity,
			}
			row := map[string]any{"id": "1", "title": "Primeiro"}
			var out strings.Builder
			if err := g.Group(resource.List(r, resource.Options{Locale: &pt}, []map[string]any{row}, c.total, 1, "", false).Body).Render(&out); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), ">"+c.want+"<") {
				line := "(no count line)"
				if at := strings.Index(out.String(), ">"+strings.Fields(c.want)[0]+" "); at >= 0 {
					line, _, _ = strings.Cut(out.String()[at+1:], "<")
				}
				t.Errorf("a pt-PT list of %d %s must say %q as its count, and says %q", c.total, c.entity, c.want, line)
			}
		})
	}
}
