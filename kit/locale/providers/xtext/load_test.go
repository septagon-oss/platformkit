package xtext_test

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
)

func ExampleLoad() {
	messages := xtext.Load("en", xtext.Source{
		Name: "task",
		FS: fstest.MapFS{"en.json": &fstest.MapFile{Data: []byte(
			`{"task.assigned": {"translation": "Assigned to %s", "plural": {"": "Assigned to %s"}}}`)},
			"pt-PT.json": &fstest.MapFile{Data: []byte(
				`{"task.assigned": {"translation": "Atribuída a %s", "plural": {"": "Atribuída a %s"}}}`)},
		},
	})
	selected := locale.SelectLocale(messages, "pt-PT")
	fmt.Println(selected.Language)
	fmt.Println(selected.Text("task.assigned", "Assigned to %s", "João"))
	// Output:
	// pt-PT
	// Atribuída a João
}

// files is one catalogue pair, written the way a module's messages directory is.
func files(english, portuguese string) fstest.MapFS {
	return fstest.MapFS{"en.json": &fstest.MapFile{Data: []byte(english)},
		"pt-PT.json": &fstest.MapFile{Data: []byte(portuguese)}}
}

// The merge order is the argument order, and it is the whole of how a product
// re-words a module's sentence and a client re-words the product's.
func TestALaterSourceAnswersForAKeyAnEarlierOneCarries(t *testing.T) {
	t.Parallel()
	messages := xtext.Load("en",
		xtext.Source{Name: "module", FS: files(
			`{"screens.new": {"translation": "New %s"}}`,
			`{"screens.new": {"translation": "Novo %s"}}`)},
		xtext.Source{Name: "product", FS: files(
			`{"screens.new": {"translation": "Add a %s"}}`,
			`{"screens.new": {"translation": "Adicionar %s"}}`)},
	)

	for _, want := range []struct{ preference, text string }{
		{"en", "Add a note"},
		{"pt-PT", "Adicionar note"},
	} {
		selected := locale.SelectLocale(messages, want.preference)
		if got := selected.Text("screens.new", "New %s", "note"); got != want.text {
			t.Errorf("%s said %q, want %q", want.preference, got, want.text)
		}
	}
}

// The kernel refuses a request in a sentence the kernel chose. A product that
// could re-word fault.* could re-word what a refusal to write says, which is
// why the prefix is owned and the composition is refused rather than ignored.
func TestASourceCannotAnswerForAKeyAnEarlierSourceOwns(t *testing.T) {
	t.Parallel()
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("a catalogue answering for a key the kernel owns was accepted")
		}
		if reason, ok := recovered.(string); !ok || !strings.Contains(reason, "fault.CSRF_ORIGIN") ||
			!strings.Contains(reason, "ui/page") {
			t.Fatalf("the refusal said %v, which names neither the key nor its owner", recovered)
		}
	}()
	xtext.Load("en",
		xtext.Source{Name: "ui/page", Owns: []string{"fault."}, FS: files(
			`{"fault.CSRF_ORIGIN": {"translation": "Nothing was written."}}`,
			`{"fault.CSRF_ORIGIN": {"translation": "Nada foi escrito."}}`)},
		xtext.Source{Name: "collect", FS: files(
			`{"fault.CSRF_ORIGIN": {"translation": "Your session ended."}}`,
			`{"fault.CSRF_ORIGIN": {"translation": "A sua sessão terminou."}}`)},
	)
}

// A product may re-word the shell's own copy while the kernel keeps its
// refusals: ownership is per prefix, not per source.
func TestTheSameSourceStillAnswersForWhatItDoesNotOwn(t *testing.T) {
	t.Parallel()
	messages := xtext.Load("en",
		xtext.Source{Name: "ui/page", Owns: []string{"fault."}, FS: files(
			`{"fault.CSRF_ORIGIN": {"translation": "Nothing was written."}}`,
			`{"fault.CSRF_ORIGIN": {"translation": "Nada foi escrito."}}`)},
		xtext.Source{Name: "collect", FS: files(
			`{"collect.cart": {"translation": "%d items"}}`,
			`{"collect.cart": {"translation": "%d artigos"}}`)},
	)
	pt := locale.SelectLocale(messages, "pt-PT")
	if got := pt.Text("collect.cart", "%d items", 3); got != "3 artigos" {
		t.Errorf("the unowned key said %q", got)
	}
	if got := pt.Text("fault.CSRF_ORIGIN", "Nothing was written."); got != "Nada foi escrito." {
		t.Errorf("the owned key said %q", got)
	}
}

// The conditions a catalogue has to meet are refused at composition, in front of
// whoever can fix the file, and not as an English page in front of a person who
// asked in Portuguese. Each of these is a different way a catalogue can be
// wrong, so each is refused for its own reason.
func TestLoadRefusesACatalogueNobodyCouldAnswerFrom(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		sources []xtext.Source
		want    string
	}{
		{
			name: "a key one locale answers for and the next does not",
			sources: []xtext.Source{{Name: "module", FS: files(
				`{"a.one": {"translation": "One"}, "a.two": {"translation": "Two"}}`,
				`{"a.one": {"translation": "Um"}}`)}},
			want: `no copy for "a.two"`,
		},
		{
			name:    "a copy that is only spaces",
			sources: []xtext.Source{{Name: "module", FS: files(`{"a.one": {"translation": "One"}}`, `{"a.one": {"translation": "   "}}`)}},
			want:    "has no copy for a.one",
		},
		{
			name:    "a translation that dropped an argument",
			sources: []xtext.Source{{Name: "module", FS: files(`{"a.one": {"translation": "%d of %s"}}`, `{"a.one": {"translation": "%d de"}}`)}},
			want:    "leaves the sentence short of one",
		},
		{
			name:    "a translation that reordered the arguments",
			sources: []xtext.Source{{Name: "module", FS: files(`{"a.one": {"translation": "%d %s"}}`, `{"a.one": {"translation": "%s %d"}}`)}},
			want:    "leaves the sentence short of one",
		},
		{
			name:    "the two spellings of one message disagreeing",
			sources: []xtext.Source{{Name: "module", FS: files(`{"a.one": {"translation": "One", "plural": {"": "One"}}}`, `{"a.one": {"translation": "Um", "plural": {"": "Uma"}}}`)}},
			want:    "the one copy a reader gets",
		},
		{
			name:    "a plural form no selector here could choose",
			sources: []xtext.Source{{Name: "module", FS: files(`{"a.one": {"translation": "One"}}`, `{"a.one#one": {"translation": "Um"}, "a.one#other": {"translation": "Uns"}}`)}},
			want:    "a plural key needs a selector",
		},
		{
			name:    "a file named for something that is not a language",
			sources: []xtext.Source{{Name: "module", FS: fstest.MapFS{"translation.json": &fstest.MapFile{Data: []byte(`{}`)}, "pt-PT.json": &fstest.MapFile{Data: []byte(`{}`)}, "en.json": &fstest.MapFile{Data: []byte(`{}`)}}}},
			want:    "not a supported language tag",
		},
		{
			name:    "a file that is not gotext JSON",
			sources: []xtext.Source{{Name: "module", FS: files(`{"a.one": "One"}`, `{"a.one": {"translation": "Um"}}`)}},
			want:    "is not gotext JSON",
		},
		{
			name:    "a source with no catalogues at all",
			sources: []xtext.Source{{Name: "module", FS: fstest.MapFS{"README.md": &fstest.MapFile{}}}},
			want:    "carries no <locale>.json catalogues",
		},
		{
			name:    "a catalogue directory that lists files it cannot open",
			sources: []xtext.Source{{Name: "module", FS: halfRead{}}},
			want:    "cannot be read",
		},
		{
			name:    "a catalogue nobody can list is the empty catalogue it looks like",
			sources: []xtext.Source{{Name: "module", FS: unreadable{}}},
			want:    "carries no <locale>.json catalogues",
		},
		{
			name:    "a source that names no files",
			sources: []xtext.Source{{Name: "module"}},
			want:    "is a catalog source with no files",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				recovered := recover()
				if recovered == nil {
					t.Fatalf("%v was accepted", c.sources)
				}
				if reason, ok := recovered.(string); !ok || !strings.Contains(reason, c.want) {
					t.Fatalf("the refusal said %v, want it to say %q", recovered, c.want)
				}
			}()
			xtext.Load("en", c.sources...)
		})
	}
}

// The source language is the language the code is written in, so it is the one
// locale a catalogue may not ship: every key then answers from the readable
// text its call site passes, and the translations still answer in theirs.
func TestTheSourceLanguageNeedsNoFileOfItsOwn(t *testing.T) {
	t.Parallel()
	messages := xtext.Load("en", xtext.Source{
		Name: "module",
		FS: fstest.MapFS{"pt-PT.json": &fstest.MapFile{Data: []byte(
			`{"screens.delete": {"translation": "Eliminar"}}`)},
		}})

	english := locale.SelectLocale(messages, "en-GB,en;q=0.9")
	if english.Language != "en" {
		t.Errorf("an English browser was answered in %q", english.Language)
	}
	if got := english.Text("screens.delete", "Delete"); got != "Delete" {
		t.Errorf("the source language said %q, want the sentence the call site wrote", got)
	}
	if got := locale.SelectLocale(messages, "pt-PT").Text("screens.delete", "Delete"); got != "Eliminar" {
		t.Errorf("the translation said %q", got)
	}
}

// A browser that speaks none of the deployment's languages is answered in the
// source language, which is the one language that always has something to say.
func TestAnUnsupportedPreferenceIsAnsweredInTheSourceLanguage(t *testing.T) {
	t.Parallel()
	messages := xtext.Load("en", xtext.Source{Name: "module", FS: files(
		`{"a.one": {"translation": "One"}}`, `{"a.one": {"translation": "Um"}}`)})
	for _, preference := range []string{"de-DE,de;q=0.9", "", "garbage"} {
		if got := locale.SelectLocale(messages, preference); got.Language != "en" {
			t.Errorf("%q selected %q, want the source language", preference, got.Language)
		}
	}
	if got := locale.SelectLocale(messages, "pt"); got.Language != "pt-PT" {
		t.Errorf("a preference for pt selected %q, want the deployment's Portuguese", got.Language)
	}
}

// unreadable is an catalogue directory that cannot even say what is in it — a
// mount that is there and a reader that cannot use it. fs.Glob keeps no error,
// so what the loader sees is a source with no catalogues, which it refuses.
type unreadable struct{}

func (unreadable) Open(string) (fs.File, error) {
	return nil, errors.New("simulated read failure")
}

// halfRead lists its catalogues and then cannot open any of them, which is the
// difference between an empty directory and a directory that went.
type halfRead struct{}

func (halfRead) Open(string) (fs.File, error) {
	return nil, errors.New("simulated read failure")
}

func (halfRead) ReadDir(string) ([]fs.DirEntry, error) {
	return []fs.DirEntry{named("en.json"), named("pt-PT.json")}, nil
}

// named is the one part of a directory entry a catalogue reader asks for.
type named string

func (n named) Name() string             { return string(n) }
func (named) IsDir() bool                { return false }
func (named) Type() fs.FileMode          { return 0 }
func (named) Info() (fs.FileInfo, error) { return nil, errors.New("not a file") }
