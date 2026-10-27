package saved

import (
	"net/http"
	"strconv"
	"time"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
)

// grid is the page a person returns to: what they kept, when they kept it, and the
// one control that gives a copy back. The shell's toolbar carries the page's heading,
// so this adds the copies in reading order, titled with the words they were kept under
// — text, never a link, because the record a copy named may be gone. They run in one
// column at every width: their number is the person's own, and a column per card would
// leave the page's shape to whatever one person happened to keep.
//
// The heading names the pair the filter selected: the pair's own `ListTitle` only
// when both halves of the pair are named, the generic words for everything else —
// no filter, half a filter, a pair nobody declared. Two kinds called `order` in two
// modules would otherwise share a heading one of them wrote.
func grid(req page.Request, s page.Shell, o Options, at At, got Page, f filters, perPage int) page.View {
	title := listTitle(req.Locale, f.Module, f.Kind)
	body := []g.Node{
		components.Breadcrumb(components.BreadcrumbProps{
			NavigationLabel: say(req.Locale, "saved.breadcrumb", "Breadcrumb"),
			Items:           []components.BreadcrumbItem{{Label: s.BackLabel, Href: o.Workspace}, {Label: title}},
		}),
		components.Toolbar(components.ToolbarProps{Title: title}),
	}
	switch {
	case got.Total == 0 && f.Module != "" && f.Kind != "":
		body = append(body, components.EmptyState(components.EmptyStateProps{
			Title: say(req.Locale, "saved.empty.filtered", "Nothing saved like this yet"), Bordered: true}))
	case got.Total == 0:
		body = append(body, components.EmptyState(components.EmptyStateProps{
			Title: say(req.Locale, "saved.empty", "Nothing saved yet"), Bordered: true}))
	default:
		cards := make([]g.Node, 0, len(got.Rows))
		for _, row := range got.Rows {
			cards = append(cards, card(req.Locale, at, row))
		}
		body = append(body,
			components.Grid(components.GridProps{Columns: "1", Gap: "4"}, cards...),
			pager(req.Locale, at, f, perPage, got))
	}
	return page.View{Title: title, Body: body, Status: http.StatusOK, Sensitive: true}
}

// card is one kept copy, with its give-back drawn quiet the way a record draws its
// own: a copy is one of many here, so the page's one filled action may not be whoever
// was kept first. A source that has gone is said beside the title in a different
// sentence from one that is merely unreachable, and both keep the removal — the copy
// still exists, and giving it back is still the keeper's to do.
func card(l *locale.Locale, at At, row Row) g.Node {
	nodes := []g.Node{}
	switch row.Availability {
	case AvailabilityUnavailable:
		nodes = append(nodes, h.P(g.Text(say(l, "saved.unavailable", "No longer available"))))
	case AvailabilityTemporary:
		nodes = append(nodes, h.P(g.Text(say(l, "saved.temporary", "Not reachable right now"))))
	}
	when := row.Captured.UTC().Format("2 Jan 2006, 15:04 UTC")
	nodes = append(nodes, components.Time(components.TimeProps{
		Instant: components.TimeText{AtUTC: row.Captured.UTC(), Text: when},
		Title:   row.Captured.UTC().Format(time.RFC3339),
	}))
	if row.Availability == AvailabilityAvailable && row.SourceURL != "" {
		nodes = append(nodes, components.Link(components.LinkProps{
			Label: say(l, "saved.open", "Open the record"), Href: row.SourceURL}))
	}
	label := pairWord(l, row.Module, row.Kind, "remove", say(l, "saved.remove", "Remove from saved"))
	nodes = append(nodes, components.Form(components.FormProps{Action: at.RemoveAt(row.ID), Label: label},
		h.Input(h.Type("hidden"), h.Name("expectedRevision"), h.Value(strconv.FormatInt(row.Revision, 10))),
		h.Input(h.Type("hidden"), h.Name("module"), h.Value(row.Module)),
		h.Input(h.Type("hidden"), h.Name("kind"), h.Value(row.Kind)),
		components.Button(components.ButtonProps{Label: label, Type: "submit", Size: "md", Variant: "outline"})))
	return components.Card(components.CardProps{Title: row.Title}, nodes...)
}

// pager is the way on to the rest of a list too long for one page. Only the pair
// filter and the offset are carried forward: a link that carried anything else
// would promise a filter this page does not read.
func pager(l *locale.Locale, at At, f filters, perPage int, got Page) g.Node {
	var nodes []g.Node
	if f.Offset > 0 {
		nodes = append(nodes, components.Link(components.LinkProps{
			Label: say(l, "saved.newer", "Newer copies"),
			Href:  gridAt(at, f.Module, f.Kind, max(f.Offset-perPage, 0))}))
	}
	if int64(f.Offset+len(got.Rows)) < got.Total {
		nodes = append(nodes, components.Link(components.LinkProps{
			Label: say(l, "saved.older", "Older copies"),
			Href:  gridAt(at, f.Module, f.Kind, f.Offset+perPage)}))
	}
	if len(nodes) == 0 {
		return g.Raw("")
	}
	return components.Flex(components.FlexProps{Gap: "4", Wrap: true}, nodes...)
}

// gridAt is the grid's address with the filters it reads, spelled the one way the
// redirect after a removal, the pager and the card's own removal form build it.
func gridAt(at At, module, kind string, offset int) string {
	if module == "" || kind == "" {
		return at.List + "?offset=" + strconv.Itoa(max(offset, 0))
	}
	return at.List + "?module=" + module + "&kind=" + kind + "&offset=" + strconv.Itoa(max(offset, 0))
}

// listTitle is a pair's own heading for its own copies, asked with the key its
// declaration's words are translated under, so the word on a record's control and
// the heading of the list over it come out of one table. Any other list — no pair
// named, half of one named — takes the generic words, including when a kind name
// happens to coincide across modules.
func listTitle(l *locale.Locale, module, kind string) string {
	generic := say(l, "saved.listTitle", "Saved items")
	if module == "" || kind == "" {
		return generic
	}
	return pairWord(l, module, kind, "listTitle", generic)
}

// pairWord is one declared saving word of one pair, asked through the derivation
// ui/resource states: `hints.<module>/<kind>.saving.<aspect>`. The pair of slugs is
// what the kept row carries, so a row of a pair whose module has since stopped
// declaring it is answered the generic word rather than nothing.
func pairWord(l *locale.Locale, module, kind, aspect, fallback string) string {
	if module == "" || kind == "" {
		return fallback
	}
	return say(l, "hints."+module+"/"+kind+".saving."+aspect, fallback)
}

func say(l *locale.Locale, key, fallback string) string {
	if l == nil {
		return fallback
	}
	return l.Text(key, fallback)
}
