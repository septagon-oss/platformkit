package resource

import (
	"strconv"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/document"
)

// Detail renders one row's resolved title, blocks and authorized commands.
// Prepared rich text and locale standing are optional through DetailRichText.
func Detail(r Resource, o Options, row map[string]any, writable bool) document.View {
	return DetailRichText(r, o, row, writable, nil, nil)
}

// DetailRichText renders prose prepared by the request adapter in its tenant
// transaction, beside the record's standing in each language the tenant speaks
// besides its own.
func DetailRichText(r Resource, o Options, row map[string]any, writable bool, rendered map[string]string,
	locales []entity.LocaleState) document.View {
	r = r.policy(o)
	if err := r.validatePolicy(); err != nil {
		return document.View{Body: []g.Node{invalid(err)}}
	}
	at := r.Screen
	// A singleton is reached at its own path, which is the only place its API is
	// reached too: an id in this path would be an id nobody issued, and the row
	// map does not carry one.
	item := at
	if !r.Singleton {
		item = at + "/" + display.Text(row["id"])
	}
	named := present.Title(r.Resolved.Titles.RecordCandidates, row, display.Text(row["id"]), r.Resolved.Titles.EmptyTitle)
	var actions []g.Node
	if writable {
		if r.offers("update") {
			actions = []g.Node{components.Button(components.ButtonProps{ComponentProps: components.ComponentProps{Attrs: r.anchor("detail", "action.edit")}, Label: o.Text("screens.edit", "Edit"), Href: item + "/edit", Variant: r.primaryAction(false)})}
		}
		// A singleton is not created and not removed, whatever Create and Delete
		// answer when they are asked. kit/rest makes those two refuse rather than
		// nil because the generator calls all five closures — which is right for a
		// closure and wrong for a door: a refusal is a sentence about a mistake
		// somebody just made, and an absent door is the same truth told before the
		// mistake. See kit/rest/singleton.go.
		if !r.Singleton && r.offers("delete") {
			actions = append(actions, deleteForm(o, item, r.Schema.Entity, r.anchor("detail", "action.delete")))
		}
	}
	// Beside Edit rather than under it: keeping a copy is something a person does
	// to the record they are looking at, and a control they may hold without any
	// right to edit the record would be buried by a slot it does not belong to.
	actions = append(actions, savingControl(r, o, row)...)
	body := []g.Node{
		breadcrumb(o, r.collectionName(), at, named),
		components.Toolbar(components.ToolbarProps{Title: named}, actions...),
		localeStrip(locales),
		details(r, o, row, rendered),
	}
	// Below the record rather than above it: what a person comes to read is the
	// row, and a command is what they may do to it once they have read it.
	body = append(body, commandFormsIn(o, o.Words(r.Schema), r.offeredCommands(), item, false, locales, row)...)
	body = append(body, historyDisclosure(o, r.History))
	return document.View{Title: named, Language: r.Resolved.Language, Modules: sketchCommandModules(r), Body: r.viewBody("detail", body)}
}

// savingControl is the keep-a-copy control, its standing, and the link to the
// pair's kept list. Both halves of the answer have to be in hand: the entry's own
// declaration (`nil` for every resource that declares no savable pair, so nothing
// is drawn whatever the installation composed) and this caller's standing against
// this row (`nil` where this installation wired no saving service, where the copy
// could not be read, or where neither grant is held). A control that would be
// refused, and a control that posts a key the record never published, are both
// drawn as nothing.
//
// The two commands are two forms and never one ambiguous toggle: the pressed
// button saves again — which the service answers with the one copy it already
// holds — and removal is a separate form whose post quotes the copy's own
// revision. Both are plain POSTs that answer 303 back to the record, so the state
// a person reads after pressing is the state a read answered.
func savingControl(r Resource, o Options, row map[string]any) []g.Node {
	if r.Resolved == nil || r.Saving == nil {
		return nil
	}
	d, s := r.Resolved.Saving, r.Saving
	if !(s.CanSave || (s.Pressed && s.CanRemove)) {
		return nil
	}
	key, version := display.Text(row[d.KeyField]), display.Text(row[d.VersionField])
	if key == "" || version == "" {
		return nil
	}
	out := []g.Node{components.Form(components.FormProps{Action: s.SaveURL, Label: d.Words.Save},
		h.Input(h.Type("hidden"), h.Name("module"), h.Value(d.Module)),
		h.Input(h.Type("hidden"), h.Name("kind"), h.Value(d.Kind)),
		h.Input(h.Type("hidden"), h.Name("key"), h.Value(key)),
		h.Input(h.Type("hidden"), h.Name("expectedSourceVersion"), h.Value(version)),
		// The label never changes with the state: `aria-pressed` carries it. The
		// outline appearance is the record's rule, not a preference: its one filled
		// action is Edit, and one thing is left to do per view.
		components.Button(components.ButtonProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{
				"aria-pressed": strconv.FormatBool(s.Pressed),
			}},
			Label: d.Words.Save, Type: "submit", Size: "md", Variant: "outline",
		}))}
	if s.Pressed {
		out = append(out, components.FormSentence(components.FormSentenceProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{"id": "saving-standing", "aria-live": "polite"}},
			Text:           d.Words.Saved,
		}))
		if s.CanRemove && s.UnsaveURL != "" {
			// The give-back is quiet for the same reason the keep control is: drawn in
			// the delete's filled danger it would leave the person who kept a copy with
			// two identical buttons, one of which destroys the record.
			out = append(out, components.Form(components.FormProps{Action: s.UnsaveURL, Label: d.Words.Remove},
				h.Input(h.Type("hidden"), h.Name("expectedRevision"),
					h.Value(strconv.FormatInt(s.Revision, 10))),
				components.Button(components.ButtonProps{Label: d.Words.Remove, Type: "submit", Variant: "outline", Size: "md"})))
		}
	}
	if s.ListURL != "" {
		out = append(out, components.Link(components.LinkProps{Label: d.Words.ListTitle, Href: s.ListURL}))
	}
	return out
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

// localeStrip names each supplied translation's completion and freshness.
// Untouched languages remain visible; an empty set draws no strip.
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
func details(r Resource, o Options, row map[string]any, rendered map[string]string) g.Node {
	p := r.Resolved
	nodes := []g.Node{}
	if name := p.Titles.Cover; name != "" && r.field(name).OnRecord {
		f, _ := fieldFor(r.Schema.Fields, name)
		f.Present = r.field(name).Kind
		title := present.Title(p.Titles.RecordCandidates, row, display.Text(row["id"]), p.Titles.EmptyTitle)
		if node := compose(f, components.TableRow{Cells: row}, false, r.Screen, o, title, r.field(name).Reading.Missing); node != nil {
			nodes = append(nodes, node)
		}
	}
	if len(p.Layout.Overview) > 0 {
		nodes = append(nodes, detailBlock(r, o, row, rendered, p.Layout.Overview, "overview", ""))
	}
	for _, section := range p.Layout.Sections {
		nodes = append(nodes, detailBlock(r, o, row, rendered, section.Fields, section.Key, section.Label))
	}
	if len(p.Layout.Information) > 0 {
		nodes = append(nodes, detailBlock(r, o, row, rendered, p.Layout.Information, "record-information", ""))
	}
	return g.Group(nodes)
}

func detailBlock(r Resource, o Options, row map[string]any, rendered map[string]string, names []string, role, title string) g.Node {
	items := make([]components.DetailItem, 0, len(names))
	nodes := make([]g.Node, 0, len(names))
	words := o.Words(r.Schema)
	for _, name := range names {
		f := r.field(name)
		read := value(f, row, o)
		node := read.node
		definition, _ := fieldFor(r.Schema.Fields, name)
		definition.Present = f.Kind
		if definition.Type == entity.TypeSketch {
			node = sketchReading(row[name], f.Label, "full", words.text)
		}
		// A choice reads its selection whatever the value, an empty one included;
		// a media kind is drawn only where the author left something to draw.
		if raw := row[name]; definition.Choices != nil || (raw != nil && scalarText(raw) != "") {
			if media := compose(definition, components.TableRow{Cells: row}, false, r.Screen, o, f.Label, f.Reading.Missing); media != nil {
				node = media
			}
		}
		if f.Control == "richtext" {
			if html, ok := rendered[name]; ok {
				node = components.Prose(components.ProseProps{HTML: html})
			} else {
				node = components.FormSentence(components.FormSentenceProps{Text: read.text})
			}
		}
		items = append(items, components.DetailItem{Label: f.Label, Value: read.text, Tone: read.tone})
		nodes = append(nodes, node)
	}
	return components.DetailListWithSlots(components.DetailListProps{Title: title, SemanticRole: role, Items: items}, components.DetailListSlots{
		KeepEmpty: true, ValueAt: func(i int, _ components.DetailItem) g.Node { return nodes[i] },
	})
}
