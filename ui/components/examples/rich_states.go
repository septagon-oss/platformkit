package examples

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	c "github.com/septagon-oss/platformkit/ui/components"
)

// These are invocations in Gallery, not another catalog. Copy belongs here,
// outside the renderers, just as a product supplies its request's messages.
func richStateExamples() []Example {
	var entries []Example
	for _, words := range []struct {
		locale, emptyTitle, emptyText, create, retry, failed, offline, refused, saved, dismiss, urgent, passive, loading string
	}{
		{"en", "No items yet", "Create the first item to get started.", "Create item", "Try again", "The items could not be loaded.", "You are offline. This is the last confirmed result.", "You do not have access to these items.", "Your changes have been saved.", "Dismiss notice", "Your changes were not saved.", "Your draft is available on this device.", "Loading items"},
		{"pt-PT", "Ainda não há itens", "Crie o primeiro item para começar.", "Criar item", "Tentar novamente", "Não foi possível carregar os itens.", "Está sem ligação. Este é o último resultado confirmado.", "Não tem acesso a estes itens.", "As suas alterações foram guardadas.", "Fechar aviso", "As suas alterações não foram guardadas.", "O seu rascunho está disponível neste dispositivo.", "A carregar itens"},
	} {
		info := func(component, state string) ExampleInfo {
			id := "pk-ui.component." + component
			return ExampleInfo{ID: id + "/" + state + "-" + words.locale, ComponentID: id,
				Group: "Shared states", Name: component + " / " + state + " / " + words.locale}
		}
		base := c.ComponentProps{Attrs: map[string]string{"lang": words.locale}}
		empty := func(state string, p c.EmptyStateProps) Example {
			p.ComponentProps = base
			return ExampleWithSlots(info("empty-state", state), p, c.EmptyStateSlots{}, c.EmptyStateWithSlots)
		}
		notice := func(state string, p c.NoticeProps) Example {
			p.ComponentProps = base
			return ExampleWithSlots(info("notice", state), p, c.NoticeSlots{}, c.NoticeWithSlots)
		}
		entries = append(entries,
			empty("default", c.EmptyStateProps{Title: words.emptyTitle, Text: words.emptyText}),
			empty("with-action", c.EmptyStateProps{Title: words.emptyTitle, Text: words.emptyText,
				Action: new(c.ButtonProps{Label: words.create, Href: "/items/new"})}),
			empty("without-action", c.EmptyStateProps{Title: words.emptyTitle, Text: words.emptyText}),
			empty("long-copy", c.EmptyStateProps{Title: words.emptyTitle, Text: strings.Repeat(words.emptyText+" ", 6)}),
			empty("legacy-description", c.EmptyStateProps{Title: words.emptyTitle, Description: words.emptyText}),
			empty("canonical-text", c.EmptyStateProps{Title: words.emptyTitle, Description: "legacy", Text: words.emptyText}),
			notice("failed-retry", c.NoticeProps{Text: words.failed, Action: new(c.ButtonProps{Label: words.retry, Href: "/items"})}),
			notice("retry-pending", c.NoticeProps{Text: words.failed, Action: new(c.ButtonProps{Label: words.retry, Loading: true, Href: "/items"})}),
			notice("offline", c.NoticeProps{Text: words.offline, Tone: "warning"}),
			notice("refused", c.NoticeProps{Text: words.refused, Tone: "info"}),
			notice("success", c.NoticeProps{Text: words.saved, Tone: "ok"}),
			notice("dismissible", c.NoticeProps{Text: words.saved, Tone: "ok", Dismissible: true, DismissLabel: words.dismiss}),
			notice("urgent", c.NoticeProps{Text: words.urgent, Live: "assertive"}),
			notice("passive", c.NoticeProps{Text: words.passive, Tone: "info", Live: "off"}),
			notice("long-copy", c.NoticeProps{Text: strings.Repeat(words.failed+" ", 6),
				Action: new(c.ButtonProps{Label: words.retry, Href: "/items"}), Dismissible: true, DismissLabel: words.dismiss}),
		)
		// A single named busy region owns the loading announcement; the reused
		// skeleton and its cells remain decorative, including with motion reduced.
		for _, state := range []string{"default", "geometry", "reduced-motion"} {
			for _, table := range []bool{false, true} {
				component := "skeleton"
				var skeleton g.Node = ExampleOf(ExampleInfo{ID: "placeholder", ComponentID: "pk-ui.component.skeleton"}, c.SkeletonProps{Shape: "text", Lines: 3}, c.Skeleton).Node
				if table {
					component = "table-skeleton"
					skeleton = ExampleOf(ExampleInfo{ID: "placeholder", ComponentID: "pk-ui.component.tableskeleton"}, c.TableSkeletonProps{Columns: 3, Rows: 3}, c.TableSkeleton).Node
				}
				frame := info(component, state)
				frame.ComponentID = "pk-ui.component.stack"
				entries = append(entries, ExampleWithChildren(frame, c.StackProps{ComponentProps: c.ComponentProps{
					Attrs: map[string]string{"lang": words.locale, "role": "status", "aria-live": "polite", "aria-busy": "true", "aria-label": words.loading},
				}, Gap: "3"}, []g.Node{h.Span(g.Text(words.loading)), skeleton}, c.Stack))
			}
		}
	}
	return entries
}
