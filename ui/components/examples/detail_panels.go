package examples

import (
	"strings"

	c "github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

func detailPanelExamples() []Example {
	var entries []Example
	for _, w := range []struct{ locale, label, title, body, close, save, empty, loading, failed, refused, offline, retry, conflict, field string }{
		{"en", "Item details", "Item Alpha", "Review the information before continuing.", "Return to items", "Save changes", "Select an item to see its details.", "Loading details", "The details could not be loaded.", "You do not have access to this item.", "Offline: this is the last confirmed result.", "Try again", "This item changed. Reload before saving.", "Notes"},
		{"pt-PT", "Detalhes do item", "Item Alfa", "Reveja a informação antes de continuar.", "Voltar aos itens", "Guardar alterações", "Selecione um item para ver os detalhes.", "A carregar detalhes", "Não foi possível carregar os detalhes.", "Não tem acesso a este item.", "Sem ligação: este é o último resultado confirmado.", "Tentar novamente", "Este item mudou. Atualize antes de guardar.", "Notas"},
	} {
		for _, kind := range []string{"detail-sheet", "side-panel"} {
			for _, state := range []string{"default", "closed", "bottom", "end", "loading", "empty", "failed", "refused", "offline", "long-content", "pending", "conflict", "read-only", "restricted"} {
				if kind == "side-panel" && (state == "bottom" || state == "end" || state == "restricted") {
					continue
				}
				p := c.DetailPanelProps{ComponentProps: c.ComponentProps{ID: "detail-" + w.locale, Attrs: map[string]string{"lang": w.locale, "data-detail-return-focus": "list-heading"}},
					ItemID: "a", Title: w.title, Label: w.label, CloseLabel: w.close, ReturnHref: "/items"}
				slots := c.DetailPanelSlots{Body: []g.Node{c.Text(c.TextProps{Content: w.body, Size: "sm"})}}
				action := c.ButtonProps{Label: w.save, Type: "submit", Size: "lg", ComponentProps: c.ComponentProps{Attrs: map[string]string{"form": "detail-form"}}}
				switch state {
				case "loading", "empty", "failed", "refused":
					text := w.empty
					if state == "failed" {
						text = w.failed
					}
					if state == "refused" {
						text = w.refused
					}
					p.State = c.ContentState{Status: c.MediaStatus(state), Title: w.label, Text: text, LoadingLabel: w.loading}
					p.ItemID, p.Title = "", ""
					slots.Body = nil
					slots.RetryAction = []g.Node{c.Button(c.ButtonProps{Label: w.retry, Href: "/items/a", Size: "lg"})}
				case "offline":
					p.State = c.ContentState{Offline: true, OfflineText: w.offline}
				case "long-content":
					slots.Body = []g.Node{c.Text(c.TextProps{Content: strings.Repeat(w.body+" ", 90), Size: "sm"})}
				case "pending":
					p.Busy, action.Loading, action.Disabled = true, true, true
				case "conflict":
					slots.Body = append(slots.Body, c.Notice(c.NoticeProps{Text: w.conflict, Tone: "warning", Action: new(c.ButtonProps{Label: w.retry, Href: "/items/a"})}))
				}
				if p.State.Status == "" && state != "read-only" && state != "conflict" {
					slots.Body = append(slots.Body, c.Form(c.FormProps{ComponentProps: c.ComponentProps{ID: "detail-form"}, Action: "/items/a", Label: w.label},
						c.Input(c.InputProps{ComponentProps: c.ComponentProps{ID: "detail-note"}, Name: "notes", Label: w.field, Size: "lg"})))
					slots.Actions = []g.Node{c.Button(action)}
				}
				info := ExampleInfo{ID: "pk-ui.component." + kind + "/" + state + "-" + w.locale, ComponentID: "pk-ui.component." + kind, Group: "Detail panels", Name: kind + " / " + state + " / " + w.locale}
				if kind == "detail-sheet" {
					sheet := c.DetailSheetProps{DetailPanelProps: p, Open: state != "closed"}
					if state == "bottom" || state == "end" {
						sheet.Placement = state
					}
					if state == "restricted" {
						sheet.CloseOnOverlay, sheet.CloseOnEscape = new(false), new(false)
					}
					entries = append(entries, ExampleWithSlots(info, sheet, slots, c.DetailSheetWithSlots))
				} else {
					p.Hidden = state == "closed"
					entries = append(entries, ExampleWithSlots(info, p, slots, c.SidePanelWithSlots))
				}
			}
		}
	}
	return entries
}
