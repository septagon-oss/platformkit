package resource

import (
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

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
	for _, name := range names {
		f := r.field(name)
		read := value(f, row, o)
		node := read.node
		if raw := row[name]; raw != nil && scalarText(raw) != "" {
			definition, _ := fieldFor(r.Schema.Fields, name)
			definition.Present = f.Kind
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
