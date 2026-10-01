package main

import (
	"fmt"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestKernelResourceNamesUseCataloguePluralRules(t *testing.T) {
	resources := []struct {
		module, entity, enOne, enOther, ptOne, ptOther string
	}{
		{"user", "user", "User", "Users", "Utilizador", "Utilizadores"},
		{"task", "task", "Task", "Tasks", "Tarefa", "Tarefas"},
		{"content", "content", "Content item", "Content items", "Item de conteúdo", "Itens de conteúdo"},
		{"billing", "plan", "Plan", "Plans", "Plano", "Planos"},
		{"billing", "subscription", "Subscription", "Subscriptions", "Subscrição", "Subscrições"},
		{"site", "settings", "Setting", "Settings", "Definição", "Definições"},
	}
	cat := catalogues()
	for _, tag := range []string{"en", "pt-PT"} {
		selected := cat.Select(tag)
		for _, tt := range resources {
			name := entity.Name{One: tt.enOne, Other: tt.enOther}
			wantOne, wantOther := tt.enOne, tt.enOther
			if tag == "pt-PT" {
				wantOne, wantOther = tt.ptOne, tt.ptOther
			}
			r := resource.Resource{Schema: entity.Schema{Module: tt.module, Entity: tt.entity}, Name: name, Screen: "/app/example"}
			o := resource.Options{Workspace: "/app", Home: "Dashboard", Locale: &selected}
			for _, count := range []int64{0, 1, 2, 5} {
				t.Run(fmt.Sprintf("%s/%s/%s/%d", tag, tt.module, tt.entity, count), func(t *testing.T) {
					want := wantOther
					if count == 1 {
						want = wantOne
					}
					key := "resource." + tt.module + "." + tt.entity
					if got := selected.Plural(key, count, name.One, name.Other); got != want {
						t.Fatalf("plural name = %q, want %q", got, want)
					}
					list := resource.List(r, o, nil, count, 1, "", false)
					if list.Title != wantOther {
						t.Fatalf("list title = %q, want %q", list.Title, wantOther)
					}
					var html strings.Builder
					if err := g.Group(list.Body).Render(&html); err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(html.String(), fmt.Sprintf("%d %s", count, strings.ToLower(want))) {
						t.Fatalf("list count lacks %d %s", count, want)
					}
					if !strings.Contains(html.String(), strings.ToLower(wantOther)) {
						t.Fatalf("empty state lacks %s", wantOther)
					}
					// Detail and form breadcrumbs use the same declared collection name.
					for _, view := range []struct {
						title string
						body  []g.Node
					}{
						{title: "detail", body: resource.Detail(r, o, map[string]any{"id": "1"}, false).Body},
						{title: "form", body: resource.Form(r, o, "/app/example", "Edit", nil, nil, "", false).Body},
					} {
						html.Reset()
						if err := g.Group(view.body).Render(&html); err != nil {
							t.Fatal(err)
						}
						if !strings.Contains(html.String(), wantOther) {
							t.Errorf("%s breadcrumb lacks %s", view.title, wantOther)
						}
					}
				})
			}
		}
	}
}
