package internal

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/components"
)

// perPage is a screenful. The API's own default is the same number, so a page
// of a list and a page of the collection route are the same page.
const perPage = crud.DefaultLimit

// screens generates the seven pages of one resource: the list, the detail, the
// two forms, the two writes and the delete.
//
// Everything about them comes from the resource — the path from its API path,
// the guards from its two permissions, the columns and the controls from its
// schema. Nothing here names a task or a user, which is the claim: adding an
// entity to this application adds seven screens and no code.
func (s *Shell) mountScreens(api *httpx.API, r httpx.Resource) {
	at := screenPath(r)
	id := "admin-" + r.Module + "-" + r.Entity + "-"
	// The write declaration comes off the resource rather than being rebuilt
	// here: a resource whose rows are the operator's — the price list — is
	// written at the operator's host and nowhere else, and a screen that
	// declared the bare permission would be a form a customer's wildcard could
	// use after the API refused it. See docs/adr/0008.
	read, write := httpx.Permission(r.Read), r.WriteAuth()
	title := strings.ToUpper(r.Entity[:1]) + r.Entity[1:]

	html(api, s, id+"list", http.MethodGet, at, "The "+r.Entity+" list", read,
		func(ctx context.Context, in *listInput) (*httpx.Page, error) {
			page := max(in.Page, 1)
			rows, total, err := r.List(ctx, crud.Query{
				Limit: perPage, Offset: (page - 1) * perPage, Sort: in.Sort,
			})
			if err != nil {
				return nil, err
			}
			return ok(s.page(ctx, title+"s",
				components.Toolbar(components.ToolbarProps{
					Title:    title + "s",
					Subtitle: plural(total, title),
				}, writable(ctx, r, components.Button(components.ButtonProps{
					Label: "New " + r.Entity, Href: at + "/new"}))...),
				table(r, at, rows, in.Sort),
				components.Pagination(components.PaginationProps{
					HTMXProps:   components.HTMXProps{Target: "body", Swap: "outerHTML", PushURL: "true"},
					CurrentPage: page, TotalPages: pages(total), BaseURL: at + "?sort=" + in.Sort,
				}),
			))
		})

	html(api, s, id+"new", http.MethodGet, at+"/new", "The new-"+r.Entity+" form", write,
		func(ctx context.Context, _ *emptyInput) (*httpx.Page, error) {
			return ok(s.form(ctx, r, at, "New "+r.Entity, at, nil, nil, "", true))
		})

	html(api, s, id+"create", http.MethodPost, at, "Create a "+r.Entity, write,
		func(ctx context.Context, in *formInput) (*httpx.Page, error) {
			// Immutable is refused here rather than dropped: this form does not
			// render those fields at all, so a value for one did not come from
			// it. See rest.Values.
			sent, err := rest.Values(in.RawBody, r.Schema.Fields, r.Immutable)
			if err == nil {
				var row map[string]any
				if row, err = r.Create(ctx, sent); err == nil {
					return nil, httpx.SeeOther(at + "/" + rest.Text(row["id"]))
				}
			}
			errs, detail := rest.FieldErrors(err, r.Schema.Fields)
			return unprocessable(s.form(ctx, r, at, "New "+r.Entity, at, sent, errs, detail, true))
		})

	html(api, s, id+"read", http.MethodGet, at+"/{id}", "One "+r.Entity, read,
		func(ctx context.Context, in *itemInput) (*httpx.Page, error) {
			row, err := r.Get(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			item := at + "/" + in.ID.String()
			// The document's title is the row's own name, not the entity's: a
			// browser tab, a bookmark and a history entry all read it, and
			// eleven of them saying "Task" is eleven of them saying nothing.
			named := label(row, r.Schema.Fields)
			return ok(s.page(ctx, named,
				breadcrumb(title+"s", at, named),
				components.Toolbar(components.ToolbarProps{Title: named},
					writable(ctx, r,
						components.Button(components.ButtonProps{Label: "Edit", Href: item + "/edit"}),
						deleteForm(item, r.Entity))...),
				details(r, row),
			))
		})

	html(api, s, id+"edit", http.MethodGet, at+"/{id}/edit", "The edit-"+r.Entity+" form", write,
		func(ctx context.Context, in *itemInput) (*httpx.Page, error) {
			row, err := r.Get(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			return ok(s.form(ctx, r, at, "Edit "+r.Entity, at+"/"+in.ID.String(), row, nil, "", false))
		})

	html(api, s, id+"update", http.MethodPost, at+"/{id}", "Update a "+r.Entity, write,
		func(ctx context.Context, in *itemFormInput) (*httpx.Page, error) {
			item := at + "/" + in.ID.String()
			sent, err := rest.Values(in.RawBody, r.Schema.Fields, nil)
			if err == nil {
				if _, err = r.Update(ctx, in.ID, rest.Writable(sent, r.Immutable)); err == nil {
					return nil, httpx.SeeOther(item)
				}
			}
			errs, detail := rest.FieldErrors(err, r.Schema.Fields)
			return unprocessable(s.form(ctx, r, at, "Edit "+r.Entity, item, sent, errs, detail, false))
		})

	html(api, s, id+"delete", http.MethodPost, at+"/{id}/delete", "Delete a "+r.Entity, write,
		func(ctx context.Context, in *itemInput) (*httpx.Page, error) {
			if err := r.Delete(ctx, in.ID); err != nil {
				return nil, err
			}
			return nil, httpx.SeeOther(at)
		})
}

// table is the list screen's rows: the field a row is known by first, as the
// link into it, then every other field the schema does not hide.
//
// The id is not a column. It is the row's identity and it is already the link's
// href; a table that leads with a UUID is a table nobody can read.
func table(r httpx.Resource, at string, rows []map[string]any, sort string) g.Node {
	primary := known(r.Schema.Fields)
	shown := []crud.Field{primary}
	for _, f := range r.Schema.Fields {
		if f.HideList || f.Name == primary.Name || f.Name == "id" {
			continue
		}
		shown = append(shown, f)
	}
	columns := make([]components.TableColumn, 0, len(shown))
	for i, f := range shown {
		columns = append(columns, components.TableColumn{
			Key: f.Name, Label: rest.FieldLabel(f), Sortable: f.Type != crud.TypeList, Primary: i == 0,
		})
	}
	out := make([]components.TableRow, 0, len(rows))
	for _, row := range rows {
		cells := map[string]any{}
		for _, f := range shown {
			cells[f.Name] = rest.Display(f, row[f.Name])
		}
		out = append(out, components.TableRow{ID: rest.Text(row["id"]), Cells: cells})
	}
	return components.TableWithSlots(components.TableProps{
		HTMXProps: components.HTMXProps{Target: "body", Swap: "outerHTML", PushURL: "true"},
		Sortable:  true, Columns: columns, Rows: out,
		EmptyText: "No " + r.Entity + "s yet.",
	}, components.TableSlots{
		// Sorting is a link the server answers, not a script that reorders what
		// is on the page: page two of a table sorted in the browser is page two
		// of the wrong order.
		SortURL: func(c components.TableColumn) string { return at + "?sort=" + next(sort, c.Key) },
		SortState: func(c components.TableColumn) string {
			if strings.TrimPrefix(sort, "-") != c.Key {
				return "none"
			}
			if direction(sort) == "desc" {
				return "descending"
			}
			return "ascending"
		},
		// The first column is the way in. A whole row that is a link cannot
		// hold a link of its own, and a row that is a click handler is not a
		// row a keyboard can reach.
		Cell: func(row components.TableRow, c components.TableColumn) g.Node {
			if !c.Primary {
				return nil
			}
			return components.Link(components.LinkProps{
				Label: rest.Text(row.Cells[c.Key]), Href: at + "/" + row.ID})
		},
	})
}

// details is the detail screen: every field, in schema order, as a description
// list. There is no hiding here — hide:list is about a table being readable,
// not about a field being secret, and a field a caller may not see is a field
// the entity's JSON does not carry.
func details(r httpx.Resource, row map[string]any) g.Node {
	items := make([]components.DetailItem, 0, len(r.Schema.Fields))
	for _, f := range r.Schema.Fields {
		items = append(items, components.DetailItem{Label: rest.FieldLabel(f), Value: rest.Display(f, row[f.Name])})
	}
	return components.DetailList(components.DetailListProps{Items: items})
}

// form is the create and edit screen: one control per writable field, derived
// from the field's type and its ui:"widget:" tag, with the fields a command
// owns shown read-only so a person can see them and not change them here.
func (s *Shell) form(ctx context.Context, r httpx.Resource, at, title, action string,
	row map[string]any, errs map[string]string, detail string, create bool,
) g.Node {
	body := []g.Node{}
	if detail != "" {
		body = append(body, components.Alert(components.AlertProps{
			Tone: "danger", Title: "That could not be saved", Message: detail, Bordered: true}))
	}
	for _, f := range r.Schema.Fields {
		if f.ReadOnly {
			continue
		}
		immutable := slices.Contains(r.Immutable, f.Name)
		// A row that does not exist yet cannot have a value for a field only a
		// command sets, and this form is not that command: a read-only control
		// with a note under it is furniture. rest.Values refuses one that is
		// submitted anyway, so hiding it is not the only thing stopping it.
		if create && immutable {
			continue
		}
		body = append(body, control(f, start(f, row, create), errs[f.Name], immutable))
	}
	body = append(body, components.FormActions(components.FormActionsProps{},
		components.Button(components.ButtonProps{Label: "Cancel", Variant: "secondary", Href: at}),
		components.Button(components.ButtonProps{Label: "Save", Type: "submit"})))
	return s.page(ctx, title,
		breadcrumb(rest.Humanize(r.Entity)+"s", at, title),
		components.Toolbar(components.ToolbarProps{Title: title}),
		components.Form(components.FormProps{
			ComponentProps: components.ComponentProps{ID: "screen-form"},
			HTMXProps: components.HTMXProps{
				Post: action, Target: "#screen-form", Swap: "outerHTML", Select: "#screen-form"},
			Action: action, Label: title,
		}, body...),
	)
}

// control is one field's input. The widget comes from the tag when the entity
// named one and from the type otherwise, which is the whole of "screens derive
// from schemas": a select exists because the struct says enum, not because
// somebody wrote a select.
func control(f crud.Field, value, fieldErr string, immutable bool) g.Node {
	label, name := rest.FieldLabel(f), f.Name
	base := components.InputProps{
		Name: name, Label: label, Value: value, Error: fieldErr,
		HelpText: hint(rest.FieldHelp(f), immutableNote(immutable)),
		Required: f.Required, ReadOnly: immutable, FullWidth: true,
	}
	switch {
	case len(f.Enum) > 0 || f.Widget == "select":
		options := make([]components.SelectOption, 0, len(f.Enum))
		for _, v := range f.Enum {
			options = append(options, components.SelectOption{Label: rest.Humanize(v), Value: v})
		}
		// A field that declares a default has no unchosen state to name, and
		// the default is already selected, so a "Choose a …" placeholder would
		// be an option that cannot be what happens.
		placeholder := ""
		if f.Default == "" {
			placeholder = "Choose a " + strings.ToLower(label)
		}
		return components.Select(components.SelectProps{
			ComponentProps: components.ComponentProps{Disabled: immutable},
			Name:           name, Label: label, Value: value, Error: fieldErr,
			Required: f.Required, Options: options, Placeholder: placeholder,
			HelpText: base.HelpText,
		})
	case f.Widget == "textarea" || f.Type == crud.TypeText:
		return components.Textarea(components.TextareaProps{
			ComponentProps: components.ComponentProps{Disabled: immutable},
			Name:           name, Label: label, Value: value, ErrorMessage: fieldErr,
			Required: f.Required, Rows: 5, FullWidth: true, HelperText: base.HelpText,
		})
	case f.Type == crud.TypeBool:
		return components.Checkbox(components.CheckboxProps{
			ComponentProps: components.ComponentProps{Disabled: immutable},
			Name:           name, Label: label, Value: "true", Checked: value == "true",
			HelpText: base.HelpText,
		})
	case f.Widget == "entity-picker":
		// There is no picker yet, and pretending otherwise would be a control
		// that looks like it searches and does not. It is the id, and the note
		// says so.
		base.HelpText = hint(base.HelpText, "The identifier of the related record. There is no picker for it yet.")
		return components.Input(base)
	case f.Type == crud.TypeList:
		base.HelpText = hint(base.HelpText, "Comma separated.")
		return components.Input(base)
	case f.Type == crud.TypeTime:
		base.Type = "datetime-local"
		if len(base.Value) >= 16 {
			base.Value = base.Value[:16] // an RFC 3339 instant is longer than the control accepts
		}
		return components.Input(base)
	case f.Type == crud.TypeInt:
		base.Type, base.Step = "number", "1"
		return components.Input(base)
	case f.Type == crud.TypeFloat:
		base.Type, base.Step = "number", "any"
		return components.Input(base)
	default:
		return components.Input(base)
	}
}

// deleteForm is the destructive action: a real form, so it works without
// JavaScript, carrying the attribute confirm.js opens the dialog on.
func deleteForm(item, entity string) g.Node {
	return components.Form(components.FormProps{Action: item + "/delete", Label: "Delete this " + entity},
		components.Button(components.ButtonProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{
				"data-confirm":       "This deletes the " + entity + ". It cannot be undone.",
				"data-confirm-label": "Delete",
			}},
			Label: "Delete", Type: "submit", Tone: "danger", Size: "md",
		}))
}

// writable is the write affordances, or none of them: a person who may not
// write this resource is not shown a form that would refuse them.
//
// It asks the resource the same question the route behind the form asks — and
// that question carries the operator flag, so a customer's administrator, whose
// wildcard is everything in their own tenant, is not offered the price list's
// New button at a tenant that is not the operator's. The button was there and
// the save was a 403, which is the shape of interface that teaches people the
// application is broken. See httpx.Resource.Writable and docs/adr/0008.
func writable(ctx context.Context, r httpx.Resource, actions ...g.Node) []g.Node {
	if !r.Writable(ctx) {
		return nil
	}
	return actions
}

func breadcrumb(collection, at, here string) g.Node {
	return components.Breadcrumb(components.BreadcrumbProps{Items: []components.BreadcrumbItem{
		{Label: "Dashboard", Href: adminRoot}, {Label: collection, Href: at}, {Label: here},
	}})
}

// screenPath mirrors the API path: /api/v1/task/tasks is served at
// /admin/task/tasks, which is what every module's nav entry already says.
func screenPath(r httpx.Resource) string {
	if rest, ok := strings.CutPrefix(r.Path, "/api/v1"); ok {
		return adminRoot + rest
	}
	return adminRoot + "/" + r.Module + "/" + r.Entity
}

func pages(total int64) int { return int((total + perPage - 1) / perPage) }

func plural(total int64, noun string) string {
	if total == 1 {
		return "1 " + strings.ToLower(noun)
	}
	return strconv.FormatInt(total, 10) + " " + strings.ToLower(noun) + "s"
}

func direction(sort string) string {
	if strings.HasPrefix(sort, "-") {
		return "desc"
	}
	return "asc"
}

// known is the field a row is recognised by: the first writable string the
// entity declares. A schema has no "this is the title" flag, and inventing one
// would be a tag every entity would have to remember; the field an entity leads
// with is the one it leads with.
func known(fields []crud.Field) crud.Field {
	for _, f := range fields {
		if !f.ReadOnly && f.Type == crud.TypeString {
			return f
		}
	}
	if len(fields) > 0 {
		return fields[0]
	}
	return crud.Field{Name: "id"}
}

// label is what one row is called.
func label(row map[string]any, fields []crud.Field) string {
	if v := rest.Text(row[known(fields).Name]); v != "" {
		return v
	}
	return rest.Text(row["id"])
}

// start is what a control opens with: what the row holds, or — on a create, for
// a field nobody has filled in — the default the entity declares, so the select
// a person leaves alone posts what the API would have stored anyway.
func start(f crud.Field, row map[string]any, create bool) string {
	if v, ok := row[f.Name]; ok {
		return rest.Text(v)
	}
	if create {
		return f.Default
	}
	return ""
}

// hint joins the notes under a control: the field's own doc, and whatever this
// form has to add about the control it chose for it.
func hint(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " ")
}

func immutableNote(immutable bool) string {
	if immutable {
		return "Changed by a command of its own, not by this form."
	}
	return ""
}

// next is the sort a header click asks for: the same field the other way round,
// or a new field ascending.
func next(sort, field string) string {
	if sort == field {
		return "-" + field
	}
	return field
}
