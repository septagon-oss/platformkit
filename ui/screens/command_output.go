package screens

import (
	"bytes"
	"context"
	"encoding/json"

	g "maragu.dev/gomponents"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// commandOutput keeps private rewind credentials in a no-store POST response.
// The token is submitted unchanged; no read refreshes its quoted revision.
func commandOutput(res httpx.Resource, c httpx.Command, ctx context.Context, id uuid.UUID, body []byte, o Options, at, next string) (page.View, error) {
	values, err := rest.CommandValues(body, c.Fields)
	if err != nil {
		return page.View{}, rest.Fault(err)
	}
	out, err := c.Execute(ctx, id, values)
	if err != nil {
		return page.View{}, rest.Fault(err)
	}
	encoded, err := json.Marshal(out.Result)
	if err != nil {
		return page.View{}, err
	}
	var row map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&row); err != nil {
		return page.View{}, err
	}
	items := []components.DetailItem{}
	for _, field := range c.Output {
		if field.Presentation.Hidden() {
			continue
		}
		if value, ok := row[field.Name]; ok {
			items = append(items, components.DetailItem{Label: display.FieldLabel(field), Value: display.Display(field, value)})
		}
	}
	nodes := []g.Node{components.Heading(components.HeadingProps{Level: 1, Text: c.Summary}),
		components.FormSentence(components.FormSentenceProps{Text: o.Text("screens.command_saved", "Your change has been saved.")}), components.DetailList(components.DetailListProps{Items: items})}
	if out.UndoToken != "" {
		for _, undo := range res.CommandsFor(ctx) {
			if undo.Verb != c.Undo || undo.Present.System {
				continue
			}
			action := resource.Command{Verb: undo.Verb, Collection: undo.Collection, Base: at}.Action(next)
			nodes = append(nodes, components.Form(components.FormProps{Action: action, Label: undo.Summary},
				components.Input(components.InputProps{Type: "hidden", Name: "token", Value: out.UndoToken}),
				components.Button(components.ButtonProps{Label: undo.Summary, Type: "submit", Variant: "outline"})))
			break
		}
	}
	nodes = append(nodes, components.Link(components.LinkProps{Href: next, Label: o.Text("screens.continue", "Continue")}))
	return page.View{Title: c.Summary, Sensitive: true, Body: []g.Node{components.Stack(components.StackProps{Gap: "6"}, nodes...)}}, nil
}
