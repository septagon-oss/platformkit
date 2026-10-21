package resource_test

import (
	"bytes"
	"maps"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/document"
	"github.com/septagon-oss/platformkit/ui/forms"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func sharedArgumentCommands() []resource.Command {
	fields := []entity.Field{
		{Name: "lang", Type: entity.TypeString, Doc: "Language to write"},
		{Name: "fields", Type: entity.TypeList, Doc: "Fields to write"},
		{Name: "fields-help", Type: entity.TypeString, Doc: "Additional context"},
		{Name: "approved", Type: entity.TypeBool, Doc: "Approve this change"},
		{Name: "mode", Type: entity.TypeString, Enum: []string{"draft", "final"}, Doc: "Writing mode"},
		{Name: "notes", Type: entity.TypeString, Widget: "textarea", Doc: "Notes for the reader"},
	}
	commands := []resource.Command{
		{Verb: "review-translation", Fields: slices.Clone(fields)},
		{Verb: "suggest-translation", Fields: slices.Clone(fields)},
	}
	for _, verb := range []string{"translate-a", "translate_a"} {
		commands = append(commands, resource.Command{Verb: verb, Fields: append(slices.Clone(fields),
			entity.Field{Name: "values", Elem: entity.TypeString, Doc: "Translated text", Keys: []entity.MapKey{
				{Name: "title", Label: "Title"}, {Name: "body", Label: "Body", Widget: "textarea"},
			}},
			entity.Field{Name: "expected", Elem: entity.TypeInt, Keys: []entity.MapKey{
				{Name: "title", From: entity.KeyFromRevision, Widget: "hidden"},
				{Name: "body", From: entity.KeyFromRevision, Widget: "hidden"},
			}},
		)})
	}
	for i := range commands {
		commands[i].Label = commands[i].Verb
		commands[i].Base = "/app/note/notes"
	}
	return commands
}

func commandLocales() []entity.LocaleState {
	return []entity.LocaleState{
		{Locale: "pt-PT", Revisions: map[string]int64{"title": 7}},
		{Locale: "pt_PT", Revisions: map[string]int64{"title": 8}},
		{Locale: "any", Revisions: map[string]int64{"title": 9}},
		{},
	}
}

func commandDetail(commands []resource.Command, row string, locales []entity.LocaleState) document.View {
	return resource.DetailRichText(withCommands(commands...), opts,
		map[string]any{"id": row, "title": "A note"}, false, nil, locales)
}

func TestCommandControlsHaveUniqueIDsAndResolvedReferences(t *testing.T) {
	view := commandDetail(sharedArgumentCommands(), "one", commandLocales())
	ids := commandIdentities(t, view, 10, 92)
	for _, verb := range []string{"review-translation", "suggest-translation", "translate-a", "translate_a"} {
		if ids[[3]string{"/app/note/notes/one/" + verb, "", "lang"}] == "" {
			t.Errorf("command %s lost its ordinary language control", verb)
		}
	}
}

func TestCommandControlIDsRemainStableAcrossRenderingOrder(t *testing.T) {
	for _, collection := range []bool{false, true} {
		commands, locales := sharedArgumentCommands(), commandLocales()
		renderPage := func(row string) document.View {
			if collection {
				return resource.List(withCommands(commands...), opts, nil, 0, 1, "", false)
			}
			return commandDetail(commands, row, locales)
		}
		formCount, controlCount := 10, 92
		if collection {
			formCount, controlCount = 4, 32
			for i := range commands {
				commands[i].Collection = true
			}
		}
		first := commandIdentities(t, renderPage("row-a"), formCount, controlCount)
		slices.Reverse(commands)
		slices.Reverse(locales)
		for i := range commands {
			commands[i].Label = "Different display words"
			slices.Reverse(commands[i].Fields)
			for j := range commands[i].Fields {
				commands[i].Fields[j].Presentation.Label = "Another label"
				slices.Reverse(commands[i].Fields[j].Keys)
			}
		}
		if next := commandIdentities(t, renderPage("row-a"), formCount, controlCount); !maps.Equal(first, next) {
			t.Errorf("collection=%v: order or display copy changed control identities", collection)
		}
		if collection {
			for i := range commands {
				commands[i].Base = "/app/other/notes"
			}
		}
		other := commandIdentities(t, renderPage("row_a"), formCount, controlCount)
		firstIDs := slices.Collect(maps.Values(first))
		for key, id := range other {
			if slices.Contains(firstIDs, id) {
				t.Errorf("collection=%v: different record or base shares ID %s for %v", collection, id, key)
			}
		}
	}
}

func TestStandaloneControlsKeepTheirNameDerivedIDs(t *testing.T) {
	f := entity.Field{Name: "lang", Type: entity.TypeString, Doc: "Language"}
	for name, node := range map[string]g.Node{
		"resource": resource.Control(f, "pt-PT", "Choose a language", false),
		"forms": forms.Control(forms.ControlProps{Field: forms.Field{Definition: f},
			Value: "pt-PT", Error: "Choose a language"}),
		"component": components.Input(components.InputProps{Name: "lang", Label: "Language",
			Value: "pt-PT", Error: "Choose a language", HelpText: "Language"}),
	} {
		t.Run(name, func(t *testing.T) {
			out := render(t, []g.Node{node})
			for _, want := range []string{`id="pk-input-lang"`, `for="pk-input-lang"`,
				`id="pk-input-lang-error"`, `id="pk-input-lang-help"`,
				`aria-describedby="pk-input-lang-error pk-input-lang-help"`} {
				if !strings.Contains(out, want) {
					t.Errorf("standalone control lost %s", want)
				}
			}
		})
	}
}

// commandIdentities checks a complete document and indexes controls by their
// submitted identity, independently of the renderer's DOM ID representation.
func commandIdentities(t *testing.T, view document.View, wantForms, wantControls int) map[[3]string]string {
	t.Helper()
	out, err := document.Render(document.Document(document.Chrome{}, document.Request{}, view, document.Bare(view.Body)))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	labels := commandReferenceLabels(t, doc)
	identities := map[[3]string]string{}
	formCount, controls := 0, 0
	for form := range doc.Descendants() {
		if form.Type != html.ElementNode || form.Data != "form" {
			continue
		}
		formCount++
		lang := ""
		for n := range form.Descendants() {
			if commandAttribute(n, "name") == "lang" {
				lang = commandAttribute(n, "value")
				if lang != "" && commandAttribute(n, "type") != "hidden" {
					t.Errorf("known locale %q is no longer carried in a hidden control", lang)
				}
			}
		}
		for n := range form.Descendants() {
			if !slices.Contains([]string{"input", "select", "textarea"}, n.Data) {
				continue
			}
			controls++
			id, name := commandAttribute(n, "id"), commandAttribute(n, "name")
			if id == "" || labels[id] != 1 {
				t.Errorf("%s has no distinct ID and label: %q", name, id)
			}
			if commandAttribute(n, "type") != "hidden" && commandAttribute(n, "aria-describedby") == "" {
				t.Errorf("%s lost its description", name)
			}
			if strings.HasPrefix(name, "expected[") {
				want := "0"
				if name == "expected[title]" && lang != "" {
					want = map[string]string{"pt-PT": "7", "pt_PT": "8", "any": "9"}[lang]
				}
				if commandAttribute(n, "value") != want || commandAttribute(n, "type") != "hidden" {
					t.Errorf("%s in %s lost its hidden revision %s", name, lang, want)
				}
			}
			key := [3]string{commandAttribute(form, "action"), lang, name}
			if identities[key] != "" {
				t.Errorf("duplicate submitted control %v", key)
			}
			identities[key] = id
		}
	}
	if formCount != wantForms || controls != wantControls {
		t.Fatalf("rendered %d forms / %d controls; want %d / %d", formCount, controls, wantForms, wantControls)
	}
	return identities
}

func commandReferenceLabels(t *testing.T, doc *html.Node) map[string]int {
	t.Helper()
	ids := map[string]*html.Node{}
	for n := range doc.Descendants() {
		if id := commandAttribute(n, "id"); id != "" {
			if ids[id] != nil {
				t.Fatalf("duplicate document ID %q", id)
			}
			ids[id] = n
		}
	}
	labels := map[string]int{}
	for n := range doc.Descendants() {
		for _, attr := range []string{"for", "aria-describedby", "aria-errormessage"} {
			for ref := range strings.FieldsSeq(commandAttribute(n, attr)) {
				target := ids[ref]
				if target == nil || commandForm(target) != commandForm(n) {
					t.Fatalf("%s=%q does not resolve within its form", attr, ref)
				}
				if attr == "for" {
					labels[ref]++
				} else if !strings.HasPrefix(ref, commandAttribute(n, "id")+"-") {
					t.Errorf("%s refers to another control's feedback: %s", commandAttribute(n, "name"), ref)
				}
			}
		}
	}
	return labels
}

func commandAttribute(n *html.Node, name string) string {
	for _, attr := range n.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func commandForm(n *html.Node) *html.Node {
	for n != nil && n.Data != "form" {
		n = n.Parent
	}
	return n
}
