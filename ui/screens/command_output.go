package screens

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
	g "maragu.dev/gomponents"
)

func commandPolicy(res httpx.Resource, c httpx.Command, ctx context.Context, o Options, at string) (present.ResolvedCommand, error) {
	for _, policy := range view(res, ctx, at, o).Resolved.Commands {
		if policy.Verb == c.Verb {
			return policy, nil
		}
	}
	return present.ResolvedCommand{}, problem.New(http.StatusForbidden, "This command is unavailable")
}

func commandResponse(res httpx.Resource, c httpx.Command, ctx context.Context, id uuid.UUID, body []byte, o Options, at, next string) (page.View, error) {
	policy, err := commandPolicy(res, c, ctx, o, at)
	if err != nil {
		return page.View{}, err
	}
	if c.HasResult {
		return commandOutput(res, c, policy, ctx, id, body, o, at, next)
	}
	if err := perform(c, ctx, id, body); err != nil {
		return page.View{}, err
	}
	if policy.SuccessMessage == "" {
		return page.View{}, httpx.SeeOther(next)
	}
	return outputPage(policy, nil, o, next), nil
}

// commandOutput keeps private rewind credentials in a no-store POST response.
// The token is submitted unchanged; no read refreshes its quoted revision.
func commandOutput(res httpx.Resource, c httpx.Command, policy present.ResolvedCommand, ctx context.Context, id uuid.UUID, body []byte, o Options, at, next string) (page.View, error) {
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
	nodes := []g.Node{outputFields(res, c, row, o)}
	if out.UndoToken != "" {
		for _, undo := range res.CommandsFor(ctx) {
			if undo.Verb != c.Undo || undo.Run == nil {
				continue
			}
			up, err := commandPolicy(res, undo, ctx, o, at)
			if err != nil || !up.Visible || up.System {
				continue
			}
			action := resource.Command{Verb: undo.Verb, Collection: undo.Collection, Base: at}.Action(next)
			tone, variant := "", "outline"
			if up.Destructive {
				tone = "danger"
			}
			if up.Primary {
				variant = "primary"
			}
			nodes = append(nodes, components.Form(components.FormProps{Action: action, Label: up.Label},
				components.Input(components.InputProps{Type: "hidden", Name: "token", Value: out.UndoToken}),
				components.Button(components.ButtonProps{Label: up.Label, Type: "submit", Variant: variant, Tone: tone})))
			break
		}
	}
	return outputPage(policy, nodes, o, next), nil
}

func outputFields(res httpx.Resource, c httpx.Command, row map[string]any, o Options) g.Node {
	fields := o.Words(res.Schema).CommandFields(c.Verb, c.Output)
	schema := entity.Schema{Fields: fields}
	policy := present.Resolve(present.Input{Schema: schema, Text: resource.PolicyText(func(key, fallback string) string { return o.Text(key, fallback, []any{}...) })})
	items, nodes := []components.DetailItem{}, []g.Node{}
	for _, field := range policy.Fields {
		if !field.OnRecord {
			continue
		}
		items = append(items, components.DetailItem{Label: field.Label})
		nodes = append(nodes, resource.Value(field, row, o))
	}
	return components.DetailListWithSlots(components.DetailListProps{Items: items}, components.DetailListSlots{
		KeepEmpty: true, ValueAt: func(i int, _ components.DetailItem) g.Node { return nodes[i] },
	})
}

func outputPage(policy present.ResolvedCommand, nodes []g.Node, o Options, next string) page.View {
	body := []g.Node{components.Heading(components.HeadingProps{Level: 1, Text: policy.Label})}
	if policy.SuccessMessage != "" {
		body = append(body, components.FormSentence(components.FormSentenceProps{Text: policy.SuccessMessage}))
	}
	body = append(body, nodes...)
	body = append(body, components.Link(components.LinkProps{Href: next, Label: o.Text("screens.continue", "Continue")}))
	return page.View{Title: policy.Label, Sensitive: true, Body: []g.Node{components.Stack(components.StackProps{Gap: "6"}, body...)}}
}
