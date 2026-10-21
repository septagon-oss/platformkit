package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
	g "maragu.dev/gomponents"
)

func TestResourceCountsEndWithTheTranslatedNoun(t *testing.T) {
	t.Parallel()
	catalog := catalogues()
	for _, nouns := range []struct {
		module, singular, plural, ptSingular, ptPlural string
	}{
		{"user", "User", "Users", "utilizador", "utilizadores"},
		{"task", "Task", "Tasks", "tarefa", "tarefas"},
	} {
		r := resource.Resource{
			Schema: entity.Schema{Module: nouns.module, Entity: nouns.module},
			Name:   entity.Name{One: nouns.singular, Other: nouns.plural},
			Screen: "/app/example",
		}
		for _, tag := range []string{"en", "pt-PT"} {
			selected := catalog.Select(tag)
			for _, count := range []int64{0, 1, 2, 5} {
				t.Run(fmt.Sprintf("%s/%s/%d", tag, nouns.module, count), func(t *testing.T) {
					one, other := strings.ToLower(nouns.singular), strings.ToLower(nouns.plural)
					if tag == "pt-PT" {
						one, other = nouns.ptSingular, nouns.ptPlural
					}
					noun := other
					if count == 1 {
						noun = one
					}
					rows := make([]map[string]any, count)
					for i := range rows {
						rows[i] = map[string]any{"id": fmt.Sprint(i + 1)}
					}
					view := resource.List(r, resource.Options{Locale: &selected}, rows, count, 1, "", false)
					var out strings.Builder
					if err := g.Group(view.Body).Render(&out); err != nil {
						t.Fatal(err)
					}
					want := fmt.Sprintf(">%d %s<", count, noun)
					if !strings.Contains(out.String(), want) {
						t.Errorf("%s count must be a complete text node %q; rendered:\n%s", tag, want, out.String())
					}
				})
			}
		}
	}
}
