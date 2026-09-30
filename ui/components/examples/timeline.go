package examples

import (
	"strings"
	"time"

	c "github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

func timelineExamples() []Example {
	var entries []Example
	for _, w := range []struct{ locale, label, actor, time, details, before, after, summary, second, date, field, old, new, redacted, unknown, terminal, empty, loading, failed, refused, offline, more, retry string }{
		{"en", "Activity", "Actor", "Time", "Details", "Before", "After", "The item was updated.", "The item was completed.", "29 September 2026, 12:00 UTC", "Title", "Draft", "Published", "Restricted", "Unknown actor", "Complete", "There is no activity yet.", "Loading activity", "Activity could not be loaded.", "You do not have access to this activity.", "Offline: this is the last confirmed activity.", "More activity", "Try again"},
		{"pt-PT", "Atividade", "Autor", "Hora", "Detalhes", "Antes", "Depois", "O item foi atualizado.", "O item foi concluído.", "29 de setembro de 2026, 12:00 UTC", "Título", "Rascunho", "Publicado", "Restrito", "Autor desconhecido", "Concluído", "Ainda não há atividade.", "A carregar atividade", "Não foi possível carregar a atividade.", "Não tem acesso a esta atividade.", "Sem ligação: esta é a última atividade confirmada.", "Mais atividade", "Tentar novamente"},
	} {
		for _, state := range []string{"default", "audit", "loading", "empty", "failed", "refused", "offline", "one", "many", "redacted", "absent-actor", "terminal", "long-copy", "more", "more-disabled"} {
			p := c.TimelineProps{ComponentProps: c.ComponentProps{Attrs: map[string]string{"lang": w.locale}}, Label: w.label, ActorLabel: w.actor, TimeLabel: w.time, DetailsLabel: w.details, BeforeLabel: w.before, AfterLabel: w.after,
				Items: []c.TimelineItem{{ID: "first", ActorText: "Alex", ActorAvatar: new(c.AvatarProps{Name: "Alex", Size: "sm"}), Time: c.TimeText{AtUTC: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC), Text: w.date}, Summary: w.summary, Changes: []c.Change{{Label: w.field, BeforeText: w.old, AfterText: w.new}}}}}
			slots := c.TimelineSlots{RetryAction: []g.Node{c.Button(c.ButtonProps{Label: w.retry, Href: "/activity", Size: "lg"})}}
			switch state {
			case "audit":
				p.Layout = "audit"
			case "loading", "empty", "failed", "refused":
				text := w.empty
				if state == "failed" {
					text = w.failed
				}
				if state == "refused" {
					text = w.refused
				}
				p.State = c.ContentState{Status: c.MediaStatus(state), Title: w.label, Text: text, LoadingLabel: w.loading}
				p.Items = nil
			case "offline":
				p.State = c.ContentState{Offline: true, OfflineText: w.offline}
			case "many":
				next := p.Items[0]
				next.ID = "second"
				next.Summary = w.second
				p.Items = append(p.Items, next)
			case "redacted":
				p.Items[0].Changes = []c.Change{{Label: w.field, Redacted: true, RedactedText: w.redacted}}
			case "absent-actor":
				p.Items[0].ActorText = w.unknown
				p.Items[0].ActorAvatar = nil
			case "terminal":
				p.Items[0].StatusLabel = w.terminal
				p.Items[0].Tone = "success"
			case "long-copy":
				p.Items[0].Summary = strings.Repeat(w.summary+" ", 10)
			case "more", "more-disabled":
				p.More = &c.ChoiceLink{Key: "next", Label: w.more, Href: "/activity?cursor=next", Disabled: state == "more-disabled"}
			}
			entries = append(entries, ExampleWithSlots(ExampleInfo{ID: "pk-ui.component.timeline/" + state + "-" + w.locale, ComponentID: "pk-ui.component.timeline", Group: "Timelines", Name: state + " / " + w.locale}, p, slots, c.TimelineWithSlots))
		}
	}
	return entries
}
