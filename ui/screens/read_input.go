package screens

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
	g "maragu.dev/gomponents"
)

// InputEntry describes a non-storage query's schemas and actual API address.
type InputEntry struct {
	Fields     []entity.Field `json:"fields"`
	Output     []entity.Field `json:"output"`
	OutputList bool           `json:"outputList"`
	Path       string         `json:"path"`
}

func mountReadInput(router *httpx.Router, shell page.Shell, o Options, r httpx.Resource) {
	prefix := "screen-" + r.Module + "-" + r.Entity + "-input-"
	page.Serve(router, shell, page.Route{ID: prefix + "start", Method: "GET", Path: r.Path, Summary: r.Entity}, r.ReadAuth(),
		func(ctx context.Context, req page.Request, _ *page.Empty) (page.View, error) {
			if stepped(r, "input") {
				return formStepRequest(ctx, req, o, r, "input", uuid.Nil, nil, false)
			}
			return Form(r, localized(o, req), r.Screen, entryName(r, localized(o, req)), nil, nil, "", true), nil
		})
	for _, final := range []bool{false, true} {
		path, name := r.Path+"/steps", "navigate"
		if final {
			path, name = r.Path, "submit"
		} else if !stepped(r, "input") {
			continue
		}
		page.Serve(router, shell, page.Route{ID: prefix + name, Method: "POST", Path: path, Summary: r.Entity}, r.ReadAuth(),
			func(ctx context.Context, req page.Request, in *formInput) (page.View, error) {
				if stepped(r, "input") {
					return formStepRequest(ctx, req, o, r, "input", uuid.Nil, in.RawBody, final)
				}
				values, err := rest.CommandValues(in.RawBody, r.Schema.Fields)
				var result any
				if err == nil {
					result, err = r.InputRun(ctx, values)
				}
				if err != nil {
					return page.View{}, err
				}
				return readInputResult(r, localized(o, req), result)
			})
	}
	if stepped(r, "input") {
		page.Serve(router, shell, page.Route{ID: prefix + "restart", Method: "GET", Path: r.Path + "/steps", Summary: r.Entity}, r.ReadAuth(),
			func(context.Context, page.Request, *page.Empty) (page.View, error) {
				return page.View{}, httpx.SeeOther(r.Screen)
			})
	}
}

func readInputResult(r httpx.Resource, o Options, result any) (page.View, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return page.View{}, err
	}
	d := json.NewDecoder(bytes.NewReader(encoded))
	d.UseNumber()
	var rows []map[string]any
	if r.InputList {
		err = d.Decode(&rows)
	} else {
		var row map[string]any
		err = d.Decode(&row)
		rows = append(rows, row)
	}
	if err != nil {
		return page.View{}, err
	}
	title := entryName(r, o)
	nodes := []g.Node{components.Heading(components.HeadingProps{Level: 1, Text: title})}
	for _, row := range rows {
		nodes = append(nodes, outputDetails(r.InputOutput, row))
	}
	nodes = append(nodes, components.Link(components.LinkProps{Href: r.Screen, Label: o.Text("screens.continue", "Continue")}))
	return page.View{Title: title, Sensitive: true, Body: []g.Node{components.Stack(components.StackProps{Gap: "6"}, nodes...)}}, nil
}

func outputDetails(fields []entity.Field, row map[string]any) g.Node {
	items := []components.DetailItem{}
	for _, field := range fields {
		if field.Presentation.Hidden() {
			continue
		}
		if value, ok := row[field.Name]; ok {
			items = append(items, components.DetailItem{Label: display.FieldLabel(field), Value: display.Display(field, value)})
		}
	}
	return components.DetailList(components.DetailListProps{Items: items})
}
