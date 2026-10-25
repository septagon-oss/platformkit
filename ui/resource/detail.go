package resource

import (
	"strconv"
	"time"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/document"
)

// Detail is one row: every field, in schema order, and the write affordances
// for a caller who may. The document's title is the row's own name, not the
// entity's: a browser tab, a bookmark and a history entry all read it, and
// eleven of them saying "Task" is eleven of them saying nothing.
func Detail(r Resource, o Options, row map[string]any, writable bool) document.View {
	return DetailRichText(r, o, row, writable, nil, nil)
}

// DetailRichText renders prose prepared by the request adapter in its tenant
// transaction, beside the record's standing in each language the tenant speaks
// besides its own.
func DetailRichText(r Resource, o Options, row map[string]any, writable bool, rendered map[string]string,
	locales []entity.LocaleState) document.View {
	at := r.Screen
	// A singleton is reached at its own path, which is the only place its API is
	// reached too: an id in this path would be an id nobody issued, and the row
	// map does not carry one.
	item := at
	if !r.Singleton {
		item = at + "/" + display.Text(row["id"])
	}
	named := r.rowName(row, readable(r.Schema.Fields))
	var actions []g.Node
	if writable {
		if hasControl(r.resolvedView("detail", writable), "action.edit") {
			actions = []g.Node{components.Button(components.ButtonProps{ComponentProps: components.ComponentProps{Attrs: r.anchor("detail", "action.edit")}, Label: o.Text("screens.edit", "Edit"), Href: item + "/edit"})}
		}
		// A singleton is not created and not removed, whatever Create and Delete
		// answer when they are asked. kit/rest makes those two refuse rather than
		// nil because the generator calls all five closures — which is right for a
		// closure and wrong for a door: a refusal is a sentence about a mistake
		// somebody just made, and an absent door is the same truth told before the
		// mistake. See kit/rest/singleton.go.
		if hasControl(r.resolvedView("detail", writable), "action.delete") {
			actions = append(actions, deleteForm(o, item, r.Schema.Entity, r.anchor("detail", "action.delete")))
		}
	}
	body := []g.Node{
		breadcrumb(o, display.Humanize(r.reading(o).Plural), at, named),
		components.Toolbar(components.ToolbarProps{Title: named}, actions...),
		localeStrip(locales),
		details(r, o.Words(r.Schema), row, rendered),
	}
	// Below the record rather than above it: what a person comes to read is the
	// row, and a command is what they may do to it once they have read it.
	body = append(body, commandFormsIn(o, o.Words(r.Schema), r.offeredCommands(), item, false, locales, row)...)
	body = append(body, historyDisclosure(o, r.History))
	return document.View{Title: named, Body: r.viewBody("detail", body)}
}

// offeredCommands is the commands to draw a form for: every one this caller may
// use, except the one the history panel carries. The panel is that command's
// control, per version, with the version it names in it; a second form above it
// asking for a version number by hand would be the same door twice, once usable
// and once a guess.
func (r Resource) offeredCommands() []Command {
	if r.History == nil || r.History.Carries == "" {
		return r.Commands
	}
	out := make([]Command, 0, len(r.Commands))
	for _, c := range r.Commands {
		if c.Verb != r.History.Carries {
			out = append(out, c)
		}
	}
	return out
}

// localeStrip shows each supplied language's completeness and freshness.
// Untouched languages remain visible at zero percent; localized status words
// distinguish missing translations from translations that need another look.
func localeStrip(locales []entity.LocaleState) g.Node {
	if len(locales) == 0 {
		return g.Raw("")
	}
	badges := make([]g.Node, 0, len(locales))
	for _, state := range locales {
		tone := "warning"
		if state.Percent() == 100 {
			tone = "success"
		}
		badges = append(badges, components.Badge(components.BadgeProps{
			Label: state.Locale + " " + strconv.Itoa(state.Percent()) + "% · " +
				display.Humanize(state.State()),
			Tone: tone, Size: "sm",
		}))
	}
	return components.Flex(components.FlexProps{Gap: "2", Wrap: true}, badges...)
}

// details is the detail screen: every field the author kept on a record, in schema
// order, as a description list. `hide:list` hides nothing here — it is about a table
// being readable, not about a field being secret — and `visibility:hidden` is the one
// declaration that is off this screen too, which readable applies.
func details(r Resource, words Words, row map[string]any, rendered map[string]string) g.Node {
	// One pass, in schema order: richtext becomes a prose section as it always
	// did, and every other readable field becomes a term beside its value.
	var prose []g.Node
	type read struct {
		field entity.Field
		item  components.DetailItem
	}
	var reads []read
	// instantsByLabel pairs a term with the instant behind it, so the value door below can hand
	// that one item a <time>. It is keyed by the term because DetailItem is a label and a string:
	// the term a person reads is the only identity an item has, and it is the same words the <dt>
	// carries. Two fields labelled alike would be two <dt>s nobody could tell apart anyway.
	instantsByLabel := map[string]components.TimeText{}
	for _, f := range readable(r.Schema.Fields) {
		if f.Widget == "richtext" {
			if html, ok := rendered[f.Name]; ok {
				prose = append(prose, h.Section(h.H2(g.Text(words.FieldLabel(f))), components.Prose(components.ProseProps{HTML: html})))
			} else {
				prose = append(prose, h.Section(h.H2(g.Text(words.FieldLabel(f))), h.P(g.Text(display.Text(row[f.Name])))))
			}
			continue
		}
		label := words.FieldLabel(f)
		value := words.Value(f, row[f.Name])
		if f.Type == entity.TypeTime {
			if when, ok := display.Instant(row[f.Name]); ok {
				instantsByLabel[label] = components.TimeText{AtUTC: when, Text: value}
			}
		}
		reads = append(reads, read{field: f,
			item: components.DetailItem{Label: label, Value: value}})
	}
	// Every block of the record opens its terms through the one list, and that list keeps the
	// same value door a list cell uses, so the record and the list state an instant identically
	// whichever section the field landed in. A time whose value is no instant never reaches the
	// map above: it keeps its text.
	list := func(items []components.DetailItem) g.Node {
		return components.DetailListWithSlots(components.DetailListProps{Items: items}, components.DetailListSlots{
			Value: func(item components.DetailItem) g.Node {
				when, ok := instantsByLabel[item.Label]
				if !ok {
					return nil
				}
				return timeElement(when)
			},
		})
	}
	if len(r.Present.Sections) == 0 {
		all := make([]components.DetailItem, 0, len(reads))
		for _, r := range reads {
			all = append(all, r.item)
		}
		return g.Group{list(all), g.Group(prose)}
	}
	// Declared sections are the blocks of the record screen, in the order the
	// author named them. A field naming none is drawn after every one of them, in
	// schema order: an undeclared field that drifted between two blocks somebody
	// placed would be the section list losing an argument it never entered.
	out := make([]g.Node, 0, len(r.Present.Sections)+1)
	taken := map[string]bool{}
	for _, sec := range r.Present.Sections {
		items := make([]components.DetailItem, 0, len(reads))
		for _, r := range reads {
			if r.field.Presentation.Section == sec.Key {
				items = append(items, r.item)
				taken[r.field.Name] = true
			}
		}
		out = append(out, h.Section(h.H2(g.Text(words.Section(sec).Label)), list(items)))
	}
	rest := make([]components.DetailItem, 0, len(reads))
	for _, r := range reads {
		if !taken[r.field.Name] {
			rest = append(rest, r.item)
		}
	}
	if len(rest) > 0 {
		out = append(out, list(rest))
	}
	return g.Group{g.Group(out), g.Group(prose)}
}

// timeElement is one instant as a screen states it: the element every generated
// screen uses, with the instant a machine can read and the exact moment — in the
// zone the server actually wrote it in — a person can. The words inside the
// element are UTC wall time until the reader's own browser says them again in the
// reader's own zone (see ui/assets/js/components.js); the title is what is true
// before that happens, and it stays true afterwards because datetime never moves.
//
// stamp is an instant spelled out with the zone it is stated in. A wall time with
// no zone on it is a guess the reader has to make for themselves.
func timeElement(when components.TimeText) g.Node {
	return components.Time(components.TimeProps{Instant: when, Title: stamp(when.AtUTC)})
}

// stamp is the exact moment with its zone designator: what a person hovering the
// cell is told before the browser has said the same moment in their own zone. It is
// the ISO-8601 instant — `Z`, not the word `UTC` — because a `title` is one of the
// attributes ui/legible reads as what a person reads, and a spelled zone is a word
// no catalogue owns; the same moment in the shape `datetime` carries is data in
// every language.
func stamp(at time.Time) string { return at.UTC().Format(time.RFC3339) }
