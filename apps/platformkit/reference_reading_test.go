package main

// reference_reading_test.go is the acceptance line of T-0327: the five reference
// resources say how they read, every word they say is served in the request's
// language with English underneath it, and no word is ever served empty.
//
// The resources are the composition's own — the same values `rest.CheckReferences`
// is handed at boot and the same values the catalogue route serves — reached
// through an unwired API, because a reading hint is not a row: nothing here needs
// a database, and a case that decided the copy table only where a Postgres stands
// would be a case nobody runs before writing the copy.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// referenceResources mounts the composition's routes on an API with no
// connection behind it and returns what it registers.
func referenceResources(t *testing.T) []httpx.Resource {
	t.Helper()
	_, cfg := configure(t)
	c := compose(cfg)
	api, _ := httpx.New(httpx.Options{
		Unwired: true, Tenants: c.tenants, Authorize: c.auth,
		Authenticate: c.auth.Authenticate, Cache: cache.Memory("pkit"), Log: quiet(),
	})
	for _, m := range c.modules {
		if m.Routes != nil {
			m.Routes(api.Surfaces(m.Name))
		}
	}
	return api.Resources()
}

func resourceAt(t *testing.T, module, entity string) httpx.Resource {
	t.Helper()
	for _, r := range referenceResources(t) {
		if r.Schema.Module == module && r.Schema.Entity == entity {
			return r
		}
	}
	t.Fatalf("the composition registers no %s/%s", module, entity)
	return httpx.Resource{}
}

// words is the composition's own copy machinery, answered in one language.
func words(t *testing.T, language string) screens.Text {
	t.Helper()
	messages := page.Messages(catalogues())
	selected := page.SelectLocale(messages, language)
	if selected.Language != language {
		t.Fatalf("%s asked for %q and was answered %q", "catalogues()", language, selected.Language)
	}
	return func(key, fallback string) string { return selected.Formatter.Text(key, fallback) }
}

// TestTheReferenceTaskCatalogueReadsWellInEnglishAndPortuguese is the golden the
// brief asks for: the real `task/task` entry, in the two languages this
// deployment answers in, byte for byte.
func TestTheReferenceTaskCatalogueReadsWellInEnglishAndPortuguese(t *testing.T) {
	task := resourceAt(t, "task", "task")
	for _, language := range []string{"en", "pt-PT"} {
		entry := screens.Describe1(screens.Localise(task, words(t, language)), true)
		got, err := json.MarshalIndent(entry, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, '\n')
		golden := "testdata/catalog-task-" + language + ".json"
		if os.Getenv("UPDATE_GOLDEN") == "1" {
			if err := os.WriteFile(golden, got, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("%s: %v (write it with UPDATE_GOLDEN=1)", golden, err)
		}
		if string(want) != string(got) {
			t.Errorf("%s is stale; regenerate with UPDATE_GOLDEN=1 go test ./apps/platformkit -run TestTheReferenceTaskCatalogue\n--- committed ---\n%s\n--- served now ---\n%s", golden, want, got)
		}
	}
}

// TestTheTaskGoldenSaysWhatTheTableSays reads the two goldens back rather than
// trusting them: the English one carries the words the module declared, the
// Portuguese one carries different words for the same keys, and neither carries a
// hint the module did not declare.
func TestTheTaskGoldenSaysWhatTheTableSays(t *testing.T) {
	var en, pt map[string]any
	read := func(name string, into *map[string]any) {
		t.Helper()
		body, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, into); err != nil {
			t.Fatal(err)
		}
	}
	read("catalog-task-en.json", &en)
	read("catalog-task-pt-PT.json", &pt)
	present, ok := en["presentation"].(map[string]any)
	if !ok {
		t.Fatal("the English entry carries no presentation block")
	}
	for key, want := range map[string]string{
		"singular": "task", "plural": "tasks", "icon": "task",
		"primaryField": "title", "previewField": "description", "statusField": "status",
	} {
		if got := present[key]; got != want {
			t.Errorf("the English entry's %s = %v, want %q", key, got, want)
		}
	}
	if group, _ := present["group"].(map[string]any); group["key"] != "work" || group["label"] != "Work" {
		t.Errorf("the English entry's group = %v", present["group"])
	}
	if ptPresent, _ := pt["presentation"].(map[string]any); ptPresent["singular"] == present["singular"] {
		t.Error("the Portuguese entry answers the same singular as the English one: the copy table is not being read")
	}
	// `updatedAt` is sortable although no row shows it, and `description` is the
	// preview although it is off the list: the two asymmetries the table argues for.
	if sortable, _ := present["sortable"].([]any); len(sortable) != 3 {
		t.Errorf("sortable = %v, want the three names the table names", present["sortable"])
	}
	// A tone is drawn and never read, so it is the same bytes in both languages.
	for _, language := range []string{"en", "pt-PT"} {
		into := map[string]any{}
		read("catalog-task-"+language+".json", &into)
		for _, field := range into["fields"].([]any) {
			f, _ := field.(map[string]any)
			p, _ := f["presentation"].(map[string]any)
			if f["name"] == "status" {
				tones, _ := p["enumTones"].(map[string]any)
				if tones["in_progress"] != "warning" {
					t.Errorf("%s: status in_progress tones = %v", language, tones)
				}
			}
			if f["name"] == "assigneeId" && p["visibility"] != "detail" {
				t.Errorf("%s: assigneeId visibility = %v, want detail", language, p["visibility"])
			}
			if f["name"] == "source" && p["visibility"] != "hidden" {
				t.Errorf("%s: source visibility = %v, want hidden", language, p["visibility"])
			}
		}
	}
}

// TestNoReferenceHintStringIsEverEmpty is the acceptance line's other half: for
// each of the five resources the phone shows, in both languages, every declared
// word resolves to something, is not the key it was asked for, and is not empty.
func TestNoReferenceHintStringIsEverEmpty(t *testing.T) {
	type target struct{ module, entity string }
	five := []target{{"task", "task"}, {"content", "content"}, {"user", "user"}, {"billing", "plan"}, {"site", "settings"}}
	for _, language := range []string{"en", "pt-PT"} {
		text := words(t, language)
		for _, want := range five {
			entry := screens.Describe1(screens.Localise(resourceAt(t, want.module, want.entity), text), true)
			checked := 0
			need := func(where, got string) {
				t.Helper()
				checked++
				key := "hints." + want.module + "/" + want.entity
				switch {
				case strings.TrimSpace(got) == "":
					t.Errorf("%s/%s in %s: %s resolves empty", want.module, want.entity, language, where)
				case got == key || strings.HasPrefix(got, key+"."):
					t.Errorf("%s/%s in %s: %s resolves to its own key %q", want.module, want.entity, language, where, got)
				}
			}
			if entry.Presentation != nil {
				p := entry.Presentation
				for _, s := range []struct {
					where string
					value *string
				}{{"singular", p.Singular}, {"plural", p.Plural}} {
					if s.value != nil {
						need(s.where, *s.value)
					}
				}
				if p.Group != nil {
					need("group label", p.Group.Label)
				}
			}
			for _, field := range entry.Fields {
				if field.Presentation.Label != "" {
					need("field "+field.Name+" label", field.Presentation.Label)
				}
				if field.Presentation.Help != "" {
					need("field "+field.Name+" help", field.Presentation.Help)
				}
				for value, word := range field.Presentation.EnumLabels {
					need("enum "+field.Name+"."+value, word)
				}
			}
			for _, command := range entry.Commands {
				if command.Presentation != nil && command.Presentation.Label != nil {
					need("command "+command.Verb+" label", *command.Presentation.Label)
				}
				if command.Presentation != nil && command.Presentation.Confirmation != nil {
					c := command.Presentation.Confirmation
					need("command "+command.Verb+" title", c.Title)
					need("command "+command.Verb+" body", c.Body)
					need("command "+command.Verb+" confirmLabel", c.ConfirmLabel)
				}
			}
			if checked == 0 {
				t.Errorf("%s/%s declares no hint string at all in %s", want.module, want.entity, language)
			}
		}
	}
}

// TestAReferenceHintResolvesForTheRequestAndNotTheProcess is the copy case: the
// same registration answered twice, in Portuguese and then in English, answers
// twice in the language asked for — which is only true because Localise copies
// what it translates.
func TestAReferenceHintResolvesForTheRequestAndNotTheProcess(t *testing.T) {
	task := resourceAt(t, "task", "task")
	first := screens.Describe1(screens.Localise(task, words(t, "pt-PT")), true)
	second := screens.Describe1(screens.Localise(task, words(t, "en")), true)
	if first.Presentation.Singular == nil || *first.Presentation.Singular == "task" {
		t.Fatalf("the Portuguese entry kept the English singular: %+v", first.Presentation)
	}
	if second.Presentation.Singular == nil || *second.Presentation.Singular != "task" {
		t.Fatalf("the English entry did not fall back to the declared word: %+v", second.Presentation)
	}
	for _, field := range task.Schema.Fields {
		if field.Name == "dueAt" && field.Presentation.Label != "Due" {
			t.Errorf("the registration was written to: dueAt now reads %q", field.Presentation.Label)
		}
	}
}

// TestADeclaredHiddenFieldIsNotOfferedByAForm and its detail twin: a visibility a
// screen never consults is a word nobody reads, so the two are asserted where the
// form is drawn.
func TestADeclaredHiddenFieldIsNotOfferedByAFormAndADetailFieldIs(t *testing.T) {
	task := resourceAt(t, "task", "task")
	create, edit := renderForm(t, task, true), renderForm(t, task, false)
	for name, body := range map[string]string{"create": create, "edit": edit} {
		if strings.Contains(body, `name="source"`) || strings.Contains(body, `name="sourceRef"`) {
			t.Errorf("the %s form offers a `hidden` field", name)
		}
		if !strings.Contains(body, "Response due") {
			t.Errorf("the %s form does not carry the declared label of slaDeadline", name)
		}
	}
	// `assigneeId` is `detail`, and it is one of the four columns the Spec names
	// Immutable: a create may not set it (that is the assign command's), an edit
	// shows it read-only. `detail` keeping it on the screen is the point.
	if strings.Contains(edit, `name="assigneeId"`) == false {
		t.Error("the edit form does not offer `assigneeId`, which `detail` puts on a record")
	}
}

// renderForm is the create form's own markup, drawn with no locale composed, so
// what it says is what the declaration says.
func renderForm(t *testing.T, r httpx.Resource, create bool) string {
	t.Helper()
	action := "/api/v1/task/tasks/new"
	title := "New task"
	if !create {
		action, title = "/api/v1/task/tasks/1/edit", "Edit task"
	}
	example := screens.FormExample("task-form", r, screens.Options{}, action, title, nil, nil, "", create)
	var body strings.Builder
	if err := example.Node.Render(&body); err != nil {
		t.Fatal(err)
	}
	return body.String()
}

// TestADeclaredWordIsDrawnInTheRequestLanguageOnTheWebScreen is the other half of
// the same claim: the generated screen and the JSON document answer one request in
// one language. It reads the same copy table, through Options.Locale.
func TestADeclaredWordIsDrawnInTheRequestLanguageOnTheWebScreen(t *testing.T) {
	task := resourceAt(t, "task", "task")
	selected := page.SelectLocale(page.Messages(catalogues()), "pt-PT")
	options := screens.Options{Locale: &selected}
	example := screens.FormExample("task-form-pt", task, options,
		"/api/v1/task/tasks/new", "New task", nil, nil, "", true)
	var body strings.Builder
	if err := example.Node.Render(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.String(), "Resposta devida") {
		t.Errorf("the Portuguese form did not draw the declared label: %s", body.String())
	}
	if strings.Contains(body.String(), "Response due") {
		t.Error("the Portuguese form drew the English label beside the Portuguese one")
	}
}
