package examples

import (
	"fmt"

	c "github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

func dataListExamples() []Example {
	var entries []Example
	for _, w := range []struct {
		locale, label, title, group, count, selectAll, clear, none, one, two, alpha, beta, unavailable, bulk, empty, loading, failed, refused, offline, create, retry, view, filter, sort, remove, overdue, page, previous, next, current string
	}{
		{"en", "Items", "Title", "Open", "12 items", "Select all items", "Clear selection", "None selected", "One selected", "Two selected", "Alpha", "Beta", "Unavailable", "Apply to selection", "There are no items to show.", "Loading items", "The items could not be loaded.", "You do not have access to these items.", "Offline: showing the last confirmed result.", "Create item", "Try again", "Saved view", "Active items", "Title order", "Remove filter", "Overdue", "Page %d", "Previous page", "Next page", "Page %d, current page"},
		{"pt-PT", "Itens", "Título", "Em curso", "12 itens", "Selecionar todos os itens", "Limpar seleção", "Nenhum selecionado", "Um selecionado", "Dois selecionados", "Alfa", "Beta", "Indisponível", "Aplicar à seleção", "Não há itens para mostrar.", "A carregar itens", "Não foi possível carregar os itens.", "Não tem acesso a estes itens.", "Sem ligação: a mostrar o último resultado confirmado.", "Criar item", "Tentar novamente", "Vista guardada", "Itens ativos", "Ordem por título", "Remover filtro", "Em atraso", "Página %d", "Página anterior", "Página seguinte", "Página %d, atual"},
	} {
		for _, state := range []string{"default", "loading", "empty", "failed", "refused", "offline", "offline-failed", "grouped", "ungrouped", "collapsed", "no-matches", "filter-chips", "sort-ascending", "sort-descending", "saved-view", "selection-none", "selection-some", "selection-all", "selection-stale", "row-disabled", "overdue", "pagination"} {
			p := c.DataListProps{ComponentProps: c.ComponentProps{ID: "list-" + w.locale, Attrs: map[string]string{"lang": w.locale}},
				Label: w.label, ResultKey: "page-one", ResultCountText: w.count,
				Columns: []c.TableColumn{{Key: "title", Label: w.title, Primary: true, Sortable: true}},
				Groups: []c.DataGroup{{Key: "open", Title: w.group, CountText: w.count, Rows: []c.DataRow{
					{TableRow: c.TableRow{ID: "a", Cells: map[string]any{"title": w.alpha}}, Href: "/items/a", Selectable: true, SelectionLabel: w.alpha},
					{TableRow: c.TableRow{ID: "b", Cells: map[string]any{"title": w.beta}}, Href: "/items/b", Selectable: true, SelectionLabel: w.beta},
					{TableRow: c.TableRow{ID: "c", Cells: map[string]any{"title": w.unavailable}}, SelectionLabel: w.unavailable, StatusLabel: w.unavailable},
				}}}, SelectionName: "selected", FormID: "bulk-" + w.locale, SelectAllLabel: w.selectAll,
				ClearSelectionLabel: w.clear, SelectionCountLabels: []string{w.none, w.one, w.two}}
			slots := c.DataListSlots{
				BulkActions: []g.Node{c.Form(c.FormProps{ComponentProps: c.ComponentProps{ID: p.FormID}, Action: "/items/bulk", Label: w.bulk},
					c.Button(c.ButtonProps{Label: w.bulk, Type: "submit", Size: "lg", Variant: "outline"}))},
				EmptyAction: []g.Node{c.Button(c.ButtonProps{Label: w.create, Href: "/items/new", Size: "lg"})},
				RetryAction: []g.Node{c.Button(c.ButtonProps{Label: w.retry, Href: "/items", Size: "lg"})},
			}
			switch state {
			case "loading", "empty", "failed", "refused", "no-matches", "offline-failed":
				status := c.MediaStatus(state)
				text := w.empty
				if state == "no-matches" {
					status = c.MediaEmpty
				}
				if state == "offline-failed" {
					status = c.MediaFailed
				}
				if status == c.MediaFailed {
					text = w.failed
				}
				if status == c.MediaRefused {
					text = w.refused
				}
				p.State = c.ContentState{Status: status, Title: w.label, Text: text, LoadingLabel: w.loading,
					Offline: state == "offline-failed", OfflineText: w.offline}
				p.Groups = nil
				if state == "loading" {
					p.LoadingLayout = &c.DataListLoading{Rows: 3, GroupHeading: true, BulkAction: true}
				}
			case "offline":
				p.State = c.ContentState{Offline: true, OfflineText: w.offline}
			case "ungrouped":
				p.Groups[0].Title = ""
			case "grouped", "collapsed":
				p.Groups = append(p.Groups, c.DataGroup{Key: "second", Title: w.unavailable, CountText: "1", Rows: p.Groups[0].Rows[2:]})
				p.Groups[0].Rows = p.Groups[0].Rows[:2]
				p.Groups[0].Collapsible, p.Groups[0].Collapsed = true, state == "collapsed"
			case "filter-chips":
				p.Filters = []c.ChoiceLink{{Key: "active", Label: w.filter, Href: "/items?active=true", Selected: true, RemoveHref: "/items", RemoveLabel: w.remove}}
			case "sort-ascending", "sort-descending":
				p.SortChoices = []c.ChoiceLink{{Key: "title", Label: w.sort, Href: "/items?sort=title", Selected: true}}
				slots.SortURL = func(c.TableColumn) string { return "/items?sort=-title" }
				slots.SortState = func(c.TableColumn) string {
					if state == "sort-descending" {
						return "descending"
					}
					return "ascending"
				}
			case "saved-view":
				p.Views = []c.ChoiceLink{{Key: "saved", Label: w.view, Href: "/items?view=saved", Selected: true}}
			case "selection-some":
				p.SelectedIDs = []string{"a"}
			case "selection-all":
				p.SelectedIDs = []string{"a", "b"}
			case "selection-stale":
				p.SelectedIDs = []string{"old-result"}
			case "overdue":
				p.Groups[0].Rows[0].StatusLabel, p.Groups[0].Rows[0].Tone = w.overdue, "warning"
			case "pagination":
				p.Pagination = &c.PaginationProps{CurrentPage: 2, TotalPages: 3, BaseURL: "/items?sort=title", NavigationLabel: w.label,
					PreviousLabel: w.previous, NextLabel: w.next, PageLabel: w.page, CurrentPageLabel: w.current}
			}
			if state == "failed" || state == "offline-failed" || state == "refused" {
				// An absent capture carries no earlier result: the failed read
				// keeps only its own retry control, the refusal keeps nothing.
				retry := slots.RetryAction
				p = c.DataListProps{ComponentProps: p.ComponentProps, Label: p.Label, State: p.State}
				slots = c.DataListSlots{}
				if state != "refused" {
					slots.RetryAction = retry
				}
			}
			entries = append(entries, ExampleWithSlots(ExampleInfo{ID: fmt.Sprintf("pk-ui.component.data-list/%s-%s", state, w.locale),
				ComponentID: "pk-ui.component.data-list", Group: "Data lists", Name: state + " / " + w.locale}, p, slots, c.DataListWithSlots))
		}
	}
	return entries
}
