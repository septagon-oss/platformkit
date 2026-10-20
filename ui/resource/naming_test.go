package resource_test

// naming_test.go is the rendered answer to "Settingss" and "Contents". The rule
// lives in kit/entity/display and every one of these screens reaches it through
// that one door; what this file reads is the markup, so a renderer that goes
// around the rule with a stray + "s" fails here even while the unit table stays
// green.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// named is a resource whose entity is one of the words the catalogue already
// wrote as a set: mass nouns and words ending in s are the two shapes that came
// out doubled.
func named(entityName string) resource.Resource {
	return resource.Resource{
		Schema: entity.Schema{Module: "workspace", Entity: entityName, Path: "/api/v1/workspace/" + entityName,
			Fields: []entity.Field{{Name: "name", Type: entity.TypeString}, {Name: "note", Type: entity.TypeString}}},
		Screen: "/app/workspace/" + entityName,
	}
}

func TestASetTheCatalogueAlreadyNamedKeepsItsOwnName(t *testing.T) {
	t.Parallel()
	// The heading humanises the entity and the empty state says it as the schema
	// wrote it: "settings" is a set in both, and neither line adds a second s.
	for _, c := range []struct{ entity, want, empty, mustNot string }{
		{"settings", "Settings", "No settings yet.", "Settingss"},
		{"content", "Content", "No content yet.", "Contents"},
		{"news", "News", "No news yet.", "Newss"},
		{"note", "Notes", "No notes yet.", ""},
	} {
		out := render(t, resource.List(named(c.entity), opts, nil, 0, 1, "", false).Body)
		for _, want := range []string{">" + c.want + "<", c.empty} {
			if !strings.Contains(out, want) {
				t.Errorf("the list of %q lacks %q:\n%s", c.entity, want, out)
			}
		}
		if c.mustNot != "" && strings.Contains(out, c.mustNot) {
			t.Errorf("the list of %q says %q:\n%s", c.entity, c.mustNot, out)
		}
		// The region's accessible name is the same words as the heading, and the
		// breadcrumb's collection crumb on a record is them too.
		if !strings.Contains(out, `aria-label="`+c.want+`"`) {
			t.Errorf("the list of %q names its region after something other than %q:\n%s", c.entity, c.want, out)
		}
	}
}

func TestTheRecordTrailNamesTheSetNotTheWordPlusAnS(t *testing.T) {
	t.Parallel()
	row := map[string]any{"id": "7", "name": "Welcome"}
	for _, c := range []struct{ entity, want, mustNot string }{
		{"content", "Content", "Contents"},
		{"settings", "Settings", "Settingss"},
		{"note", "Notes", ""},
	} {
		detail := render(t, resource.Detail(named(c.entity), opts, row, true).Body)
		if !strings.Contains(detail, `href="/app/workspace/`+c.entity+`">`+c.want+"<") {
			t.Errorf("the record of %q has no %q crumb:\n%s", c.entity, c.want, detail)
		}
		if c.mustNot != "" && strings.Contains(detail, c.mustNot) {
			t.Errorf("the record of %q says %q:\n%s", c.entity, c.mustNot, detail)
		}
		form := render(t, resource.Form(named(c.entity), opts, "/app/workspace/"+c.entity+"/7/edit", "Edit", row, nil, "", false).Body)
		if !strings.Contains(form, `href="/app/workspace/`+c.entity+`">`+c.want+"<") {
			t.Errorf("the edit form of %q has no %q crumb:\n%s", c.entity, c.want, form)
		}
	}
}

// TestTheCountAndTheEmptyStateAgreeAboutTheNoun reads the two lines a list says
// about how much there is: the count under the title and the sentence when there
// is none. Both pluralise through the same rule, and the second one is the line
// the doubled "s" survived in after the first was fixed.
func TestTheCountAndTheEmptyStateAgreeAboutTheNoun(t *testing.T) {
	t.Parallel()
	one := render(t, resource.List(note(), opts, []map[string]any{{"id": "1", "title": "Buy milk"}}, 1, 1, "", false).Body)
	if !strings.Contains(one, "1 note") || strings.Contains(one, "1 notes") {
		t.Errorf("a list of one does not say so:\n%s", one)
	}
	none := render(t, resource.List(note(), opts, nil, 0, 1, "", false).Body)
	if !strings.Contains(none, "0 notes") || !strings.Contains(none, "No notes yet.") {
		t.Errorf("an empty list does not say so in both places:\n%s", none)
	}
	many := render(t, resource.List(named("settings"), opts, nil, 7, 1, "", false).Body)
	if !strings.Contains(many, "7 settings") || !strings.Contains(many, "No settings yet.") {
		t.Errorf("a mass noun was inflected:\n%s", many)
	}
}

// pt is the shipped catalogue, not a paraphrase of it: the entry is read from the
// file the package embeds, so this case moves when the translation moves and
// cannot drift from it. Portuguese noun morphology is the translation's business,
// which is why the entry takes the noun whole rather than appending its own "s".
func pt(t *testing.T) *locale.Locale {
	t.Helper()
	raw, err := fs.ReadFile(resource.Catalogues(), "pt-PT.json")
	if err != nil {
		t.Fatal(err)
	}
	var entries map[string]struct {
		Translation string `json:"translation"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	l := &locale.Locale{Language: "pt-PT", Formatter: catalogue(entries)}
	return l
}

type catalogue map[string]struct {
	Translation string `json:"translation"`
}

func (c catalogue) Text(key, fallback string, args ...any) string {
	entry, ok := c[key]
	if !ok || entry.Translation == "" {
		return fmt.Sprintf(fallback, args...)
	}
	return fmt.Sprintf(entry.Translation, args...)
}

func TestThePortugueseEmptyStateAddsNoSecondS(t *testing.T) {
	t.Parallel()
	language := pt(t)
	withLocale := resource.Options{Workspace: "/app", Home: "Painel", Locale: language}
	empty := render(t, resource.List(note(), withLocale, nil, 0, 1, "", false).Body)
	if !strings.Contains(empty, "Ainda sem notes.") {
		t.Errorf("the Portuguese empty state does not take the noun whole:\n%s", empty)
	}
	if strings.Contains(empty, "notess") {
		t.Errorf("the sentence doubles the noun's own s:\n%s", empty)
	}
	// The count keeps its Portuguese adjective, which is the word that carries the
	// number's agreement — not the interpolated noun.
	list := render(t, resource.List(note(), withLocale, []map[string]any{{"id": "1", "title": "Comprar"}}, 3, 1, "", false).Body)
	if !strings.Contains(list, "3 notes registados") {
		t.Errorf("the localized count moved:\n%s", list)
	}
}
