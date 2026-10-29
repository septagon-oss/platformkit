// Package screens adapts authorized httpx resources to resolved HTML screens
// and the portable catalogue. Rendering stays in ui/resource; requests and
// guarded read/write closures stay here.
package screens

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	g "maragu.dev/gomponents"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// Options is what a shell decides about its generated screens. See
// resource.Options; Mount sets Locale per request from page.Request.
type Options = resource.Options

// described resolves localized declarations without caller-specific commands.
// Route availability remains separate from structural field editability.
func described(r httpx.Resource, o Options) resource.Resource {
	r.Commands = nil
	return resolvedResource(r, o)
}

func resolvedResource(r httpx.Resource, o Options) resource.Resource {
	r.Schema.Module = cmp.Or(r.Schema.Module, r.Module)
	r.Schema.Entity = cmp.Or(r.Schema.Entity, r.Entity)
	v := resource.Resource{CommandsOnly: r.CommandsOnly, Schema: r.Schema, Immutable: r.Immutable, Singleton: r.Singleton,
		Screen: r.Screen, Operations: screenOperations(r), Present: r.Present, API: r.Path, ScreenOwner: r.Module}
	text := func(key, fallback string) string { return o.Text(key, fallback, []any{}...) }
	language := "en"
	if o.Locale != nil {
		language = o.Locale.Language
	}
	v.Resolved = resolve(Localise(r, text), text, language)
	return v
}

// writeAvailable is structural: request authority is checked by the mounted guard.
func writeAvailable(r httpx.Resource, verb httpx.CRUD) bool {
	if !r.WriteAuth().Declared() || !r.Offers(verb) {
		return false
	}
	switch verb {
	case httpx.CRUDCreate:
		return !r.Singleton && r.Create != nil
	case httpx.CRUDUpdate:
		return r.Update != nil
	case httpx.CRUDDelete:
		return !r.Singleton && r.Delete != nil
	}
	return false
}

// mountedOperations is that same route table in the form the resource carries its
// offered verbs in, so a policy resolved from it is resolved from what answers.
func mountedOperations(r httpx.Resource) []httpx.CRUD {
	words := screenOperations(r)
	out := make([]httpx.CRUD, 0, len(words))
	for _, word := range words {
		out = append(out, httpx.CRUD(word))
	}
	return out
}

// screenOperations keeps an explicit empty set distinct from the all-verbs default.
func screenOperations(r httpx.Resource) []string {
	out := make([]string, 0, 5)
	for _, verb := range httpx.CRUDValues() {
		if verb == httpx.CRUDList || verb == httpx.CRUDRead {
			if r.Offers(verb) {
				out = append(out, string(verb))
			}
		} else if writeAvailable(r, verb) {
			out = append(out, string(verb))
		}
	}
	return out
}

// view is the whole of what a mounted screen knows: the resource's own shape, and
// the lifecycle routes *this caller* may use. It is the adapter's only place where an
// authorized httpx.Resource becomes something a renderer draws, which is also why a
// command appears on one person's record page and not another's.
//
// at is the list path, which is where a collection command's route is mounted and
// what the renderer needs to build an action without knowing how paths are derived.
func view(r httpx.Resource, ctx context.Context, at string, o Options) resource.Resource {
	r.Commands = r.CommandsFor(ctx)
	// A mounted screen is resolved against the routes this adapter mounts, not against
	// everything the resource declares: an unwired closure or an undeclared guard
	// answers no page, so a door in front of one is a refusal with a button on it. An
	// exported screen has no routes at all and keeps the declared reading.
	r.Operations = mountedOperations(r)
	v := resolvedResource(r, o)
	v.Commands = commands(r, r.Commands, at)
	return v
}

// commands carries only authorized callable routes into the pure renderer.
// Execution closures and guards remain with the HTTP adapter.
func commands(r httpx.Resource, available []httpx.Command, at string) []resource.Command {
	out := make([]resource.Command, 0, len(available))
	for _, c := range available {
		if c.Run == nil {
			continue
		}
		out = append(out, describedCommand(r, c, at))
	}
	return out
}

// listView, detailView and formView are the same renderers reached with a caller in
// hand. Mount uses them; the exported trio below is what a caller without a request
// renders — a design export, a golden file, a test — and so carries no commands,
// because "may this person resolve the task" has no answer where there is no person.
func listView(r httpx.Resource, ctx context.Context, o Options, at string, rows []map[string]any, total int64, pageNo int, sort string, writable bool) page.View {
	return resource.List(view(r, ctx, at, o), o, rows, total, pageNo, sort, writable)
}

func detailView(r httpx.Resource, ctx context.Context, o Options, at string, row map[string]any, writable bool) (page.View, error) {
	rendered := map[string]string{}
	for _, field := range r.Schema.Fields {
		if field.Widget != "richtext" {
			continue
		}
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return page.View{}, richtext.ErrMissing
		}
		source, _ := row[field.Name].(string)
		doc, err := richtext.Parse(source)
		if err != nil {
			return page.View{}, err
		}
		html, err := richtext.Render(ctx, tx, doc, r.RichTextFiles, richtext.Workspace)
		if err != nil {
			return page.View{}, err
		}
		rendered[field.Name] = html
	}
	locales, err := localeStates(ctx, r, row)
	if err != nil {
		return page.View{}, err
	}
	v := view(r, ctx, at, o)
	v.History = historyPanel(r, v, ctx, o, at, row)
	return resource.DetailRichText(v, o, row, writable, rendered, locales), nil
}

// localeStates reads translation standing in the request transaction.
// It uses the row's id so the same path handles singleton surfaces.
func localeStates(ctx context.Context, r httpx.Resource, row map[string]any) ([]entity.LocaleState, error) {
	if r.Locales == nil {
		return nil, nil
	}
	id, err := uuid.Parse(display.Text(row["id"]))
	if err != nil {
		return nil, fmt.Errorf("screens: the %s row's id is %q, which is no id: %w", r.Entity, display.Text(row["id"]), err)
	}
	return r.Locales(ctx, id)
}

// List is the list screen of a registered resource. See resource.List.
func List(r httpx.Resource, o Options, rows []map[string]any, total int64, pageNo int, sort string, writable bool) page.View {
	return resource.List(described(r, o), o, rows, total, pageNo, sort, writable)
}

// Detail is one row of a registered resource. See resource.Detail.
func Detail(r httpx.Resource, o Options, row map[string]any, writable bool) page.View {
	// No caller and no request, so no language question gets asked: a design
	// export and a golden file render the record, not its translation standing.
	return resource.DetailRichText(described(r, o), o, row, writable, nil, nil)
}

// Form is the create and edit screen of a registered resource. See resource.Form.
func Form(r httpx.Resource, o Options, action, title string, row map[string]any, errs map[string]string, detail string, create bool) page.View {
	return resource.Form(described(r, o), o, action, title, row, errs, detail, create)
}

// FormExample captures the form body Form serves, without its page chrome, for
// design export. See resource.FormExample.
func FormExample(id string, r httpx.Resource, o Options, action, title string, row map[string]any, errs map[string]string, detail string, create bool) examples.Example {
	return resource.FormExample(id, described(r, o), o, action, title, row, errs, detail, create)
}

// Control adapts an entity field to the portable form control. See resource.Control.
func Control(f entity.Field, value, fieldErr string, immutable bool) g.Node {
	return resource.Control(f, value, fieldErr, immutable)
}

// historyPanel reads the current row's version series under its existing guard.
// Unavailable history leaves the record readable; restore requires an authorized
// command and quotes the revision already displayed, as the text a count reaches
// this page in whether or not its form answers as steps.
func historyPanel(r httpx.Resource, v resource.Resource, ctx context.Context, o Options, at string, row map[string]any) *resource.HistoryPanel {
	if r.History == nil || r.History.Versions == nil {
		return nil
	}
	id, err := uuid.Parse(strings.TrimSpace(rest.Text(row["id"])))
	if err != nil {
		return nil
	}
	stored, err := r.History.Versions(ctx, id)
	if err != nil {
		return nil
	}
	out := &resource.HistoryPanel{
		Carries: r.History.RestoreVerb,
		Title:   o.Text("screens.history", "History"),
		Restore: o.Text("screens.history.restore", "Restore this version"),
	}
	for _, policy := range v.Resolved.Commands {
		if policy.Verb != r.History.RestoreVerb || !policy.Visible || policy.System {
			continue
		}
		for _, command := range v.Commands {
			if command.Verb == policy.Verb {
				out.Action = at + "/" + id.String() + "/" + policy.Verb
				out.Restore = policy.Label
				out.Resolved = new(policy)
				if policy.Confirmation != nil {
					out.Asking = policy.Confirmation.Body
					if policy.Confirmation.ConfirmLabel != "" {
						out.Restore = policy.Confirmation.ConfirmLabel
					}
				}
			}
		}
	}
	out.Condition = display.Text(row[entity.RevisionField])
	// What each version held, indexed by its number, because the comparison one
	// version offers is against the one before it and the series arrives newest
	// first. A number missing from the index is a version retention released, or the
	// beginning of the series: both answer "nothing to compare against".
	held := make(map[int64]map[string]any, len(stored))
	for _, v := range stored {
		document := map[string]any{}
		decoder := json.NewDecoder(strings.NewReader(string(v.Fields)))
		decoder.UseNumber()
		_ = decoder.Decode(&document)
		held[v.Version] = document
	}
	for _, version := range stored {
		item := resource.VersionRow{Number: version.Version, When: version.At.UTC().Format("2 Jan 2006, 15:04 UTC")}
		if from, restored := rest.RestoredVersion(version.Reason); restored {
			item.RestoredFrom = from
		}
		for _, name := range version.Changed {
			label := name
			for _, field := range v.Resolved.Fields {
				if field.Name == name {
					label = field.Label
					break
				}
			}
			item.Changes = append(item.Changes, resource.VersionChange{
				Label: label,
				Edits: valueEdits(held[version.Version-1], held[version.Version], name),
			})
		}
		out.Rows = append(out.Rows, item)
	}
	return out
}

// valueEdits is one field of one version as the comparison it is: the words the
// version before held, marked against the words this one left. Where no earlier
// version stands on this page there is nothing to mark, and the value is shown as one
// unchanged stretch, cut to the panel's line.
func valueEdits(before, after map[string]any, name string) []resource.VersionEdit {
	if before == nil {
		return []resource.VersionEdit{{Text: excerpt(after[name])}}
	}
	return wordEdits(rest.Text(before[name]), rest.Text(after[name]))
}

// excerpt is a stored value as one line of the panel. A version of a body is
// paragraphs long and the panel is a list of versions, so a line is cut; the whole
// document stays behind the versions route, which answers it in full.
func excerpt(value any) string {
	text := rest.Text(value)
	runes := []rune(text)
	const cut = 160
	if len(runes) <= cut {
		return text
	}
	return string(runes[:cut]) + "…"
}

// entryName shares the catalogue's translated noun with generated form headings.
func entryName(r httpx.Resource, o Options) string { return resolvedIdentity(r, o).Singular }

func resolvedIdentity(r httpx.Resource, o Options) present.ResolvedIdentity {
	text := func(key, fallback string) string { return o.Text(key, fallback, []any{}...) }
	return resolve(Localise(r, text), text, "").Identity
}

// subject uses the same resolved nouns as the rendered page.
func subject(r httpx.Resource, o Options, at string, record bool) page.Subject {
	identity := resolvedIdentity(r, o)
	return page.Subject{Singular: identity.Singular, Plural: identity.Plural, List: at, Record: record}
}
