package resource_test

// Two of the sentences a generated record page says are a phrase and a value: which
// version a restore read in order to be written, and which language a command is
// about to write. Each is one message, so where the value sits, and what holds it
// off the words around it, belongs to the copy of the language the person reads —
// not to a `+` beside the renderer, which would speak Go's order in every language.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// spokenRecord draws one record page carrying a version series and one command
// offered per language. The copy is the catalogue this package ships, because a
// hand-written table answers a key without its arguments and so cannot show where a
// language put a value inside its own sentence.
func spokenRecord(t *testing.T, preference string, r resource.Resource, locales []entity.LocaleState) string {
	t.Helper()
	options := opts
	if preference != "" {
		selected := locale.SelectLocale(
			xtext.Load("en", xtext.Source{FS: resource.Catalogues(), Name: "ui/resource"}), preference)
		options.Locale = &selected
	}
	return render(t, resource.DetailRichText(r, options,
		map[string]any{"id": "11111111-1111-1111-1111-111111111111", "title": "A line"},
		true, nil, locales).Body)
}

func restoredVersions() resource.Resource {
	r := lines()
	r.History = &resource.HistoryPanel{
		Title: "History", Restore: "Restore", Action: "/app/line/lines/one/restore",
		Rows: []resource.VersionRow{{Number: 4, RestoredFrom: 3}},
	}
	return r
}

// TestARestoredVersionIsNamedInsideItsOwnSentence: the panel says which version this
// write read, and the version number is an argument of that sentence rather than a
// "v" some Go code stuck onto the end of it. The reader who asked in Portuguese gets
// the Portuguese order of the same fact; the reader no catalogue answers gets the
// sentence this package was written in, finished.
func TestARestoredVersionIsNamedInsideItsOwnSentence(t *testing.T) {
	source := spokenRecord(t, "", restoredVersions(), nil)
	if !strings.Contains(source, "Restored from v3") {
		t.Errorf("the restored line is no longer the sentence this package wrote it in:\n%s", source)
	}
	pt := spokenRecord(t, "pt-PT", restoredVersions(), nil)
	if !strings.Contains(pt, "Restaurada desde a versão 3") {
		t.Errorf("the restored line lost the wording its own language gave the version:\n%s", pt)
	}
	for _, page := range []string{source, pt} {
		if strings.Contains(page, "%!") || strings.Contains(page, "%[") {
			t.Errorf("a formatting verb reached the version history:\n%s", page)
		}
	}
}

// TestALanguageScopedCommandNamesItsLanguageInsideItsOwnSentence: a command the
// screen offers once per language names the language it is about to write, in the
// form's accessible name — the sentence a screen reader announces before the person
// presses anything. Which side the language sits on is the copy's decision, so the
// two renderings differ where the two catalogues differ and agree where they do not.
func TestALanguageScopedCommandNamesItsLanguageInsideItsOwnSentence(t *testing.T) {
	r := lines()
	r.Commands = []resource.Command{{
		Verb: "assign", Label: "Assign", Base: r.Screen,
		Fields: []entity.Field{{Name: "expected", Elem: entity.TypeInt, Keys: []entity.MapKey{
			{Name: "title", From: entity.KeyFromRevision, Widget: "hidden"},
		}}},
	}}
	locales := []entity.LocaleState{{Locale: "pt-PT", Revisions: map[string]int64{"title": 7}}}
	source := spokenRecord(t, "", r, locales)
	if !strings.Contains(source, `Assign — pt-PT`) {
		t.Errorf("a language-scoped command's name is no longer the sentence this package wrote it in:\n%s", source)
	}
	pt := spokenRecord(t, "pt-PT", r, locales)
	if !strings.Contains(pt, `Assign (pt-PT)`) {
		t.Errorf("the command's name lost the shape its own language gave the language it names:\n%s", pt)
	}
	if strings.Contains(pt, `Assign — pt-PT`) {
		t.Errorf("the command's name kept Go's join where the catalogue had a shape of its own:\n%s", pt)
	}
	for _, page := range []string{source, pt} {
		if strings.Contains(page, "%!") || strings.Contains(page, "%[") {
			t.Errorf("a formatting verb reached a command's accessible name:\n%s", page)
		}
	}
}
