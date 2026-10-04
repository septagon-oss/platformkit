package pseudo_test

import (
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/pseudo"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
)

// fake is the two-key catalogue the conformance cases are asked of: one key pt-PT
// answers, one it does not, one whose Portuguese happens to read exactly like the
// English, and one that interpolates an argument. The service the gate runs against
// is apps/platformkit's own composition over the real thirteen files, and it makes
// these same four decisions there — which is why the cases are written here first.
func fake() xtext.Catalog {
	return xtext.Load("en", xtext.Source{FS: fstest.MapFS{
		"pt-PT.json": mapFile([]byte(`{"task.title":{"translation":"Tarefa"},` +
			`"audit.trail":{"translation":"Trail"},` +
			`"file.size":{"translation":"%[1]d MB"}}`)),
	}, Name: "fake"})
}

func TestEveryMarkedSentenceIsTheDelegatesOwn(t *testing.T) {
	rec := pseudo.NewRecorder()
	messages := pseudo.Wrap(fake(), rec)
	locale.SelectLocale(messages, "pt-PT")
	rendered := messages.Select("pt-PT").Text("task.title", "Task")
	if want := "⟦Táréfá⟧"; rendered != want {
		t.Errorf("marked = %q, want %q", rendered, want)
	}
	if back := pseudo.Unmark(rendered); back != "Tarefa" {
		t.Errorf("un-marking read back %q, want the copy the catalogue holds", back)
	}
	if asked := rec.Asks(); len(asked) != 1 || !asked[0].Answered || asked[0].Language != "pt-PT" {
		t.Errorf("recorded = %+v, want one answered ask in pt-PT", asked)
	}
}

// TestThePseudoTagNeverBecomesALanguageAPersonIsServedIn is the risk that decides
// the whole shape of this provider: a page may declare lang="en-XA" and be measured,
// while the deployment goes on answering people in the languages its files are
// written in.
func TestThePseudoTagNeverBecomesALanguageAPersonIsServedIn(t *testing.T) {
	rec := pseudo.NewRecorder()
	messages := pseudo.Wrap(fake(), rec)
	selected := messages.Select("pt-PT")
	if selected.Language != pseudo.Tag {
		t.Errorf("language = %q, want the pseudo tag", selected.Language)
	}
	if got := fake().Languages(); strings.Join(got, " ") != "en pt-PT" {
		t.Errorf("the catalogue answers %v; wrapping must not add a language", got)
	}
	for _, language := range fake().Languages() {
		if language == pseudo.Tag {
			t.Errorf("Languages() carries %q, which no person may be served in", language)
		}
	}
}

// TestMarkIsItsOwnInverse covers every class of payload the transformation touches.
func TestMarkIsItsOwnInverse(t *testing.T) {
	for _, payload := range []string{
		"Task", "", "⟦", "⟧", "⟦⟧", "á ready", "à é í ó ú Á É Í Ó Ú",
		"%s un-substituted", "a", "Delete task 4b2a9c1d", "1.234,50 EUR",
		"⏶ mixed ⟦⟧ ⟪brackets⏫", "pt-PT", "  padded  ",
	} {
		marked := pseudo.Mark(payload)
		if !pseudo.Wrapped(marked) {
			t.Errorf("Mark(%q) = %q, which Wrapped does not recognise", payload, marked)
		}
		if back := pseudo.Unmark(marked); back != payload {
			t.Errorf("Unmark(Mark(%q)) = %q; the map stopped being a bijection", payload, back)
		}
		if len(marked) < len(payload) {
			t.Errorf("Mark(%q) = %q, which is shorter than what it wraps", payload, marked)
		}
	}
}

// TestNothingThatOnlyLooksAccentedCounts as marked: the gate counts the delimiters
// and nothing else, so a page that arrived with accents of its own is copy that went
// around a catalogue.
func TestNothingThatOnlyLooksAccentedCounts(t *testing.T) {
	for _, text := range []string{"", "⟦unclosed", "unclosed⟧", "áá", "⟦a⟧⟦b⟧", "⟦⟦⟧⟧", "Task"} {
		if pseudo.Wrapped(text) {
			t.Errorf("Wrapped(%q) says marked copy, which this gate would then not count", text)
		}
	}
	for _, text := range []string{"⟦⟧", "⟦ ⟧", "⟦⟦⟦⟧⟧⟧", "⟦á⟧"} {
		if !pseudo.Wrapped(text) {
			t.Errorf("Wrapped(%q) says unmarked copy", text)
		}
	}
}

func TestUnansweredKeysAreTheOnesNoEntryAnswered(t *testing.T) {
	rec := pseudo.NewRecorder()
	formatter := pseudo.Wrap(fake(), rec).Select("pt-PT").Formatter
	formatter.Text("task.title", "Task")
	formatter.Text("task.due", "Due")
	formatter.Text("audit.trail", "Trail")
	formatter.Text("file.size", "%d MB", 4)

	var keys []string
	for _, ask := range rec.Unanswered() {
		keys = append(keys, ask.Key)
	}
	if strings.Join(keys, " ") != "task.due" {
		t.Errorf("unanswered = %v, want only the key no catalogue answers; "+
			"an answer that reads like its source is the imprecision this records", keys)
	}
	if got := rec.UnansweredKeys(); len(got) != 1 || got[0] != "task.due" {
		t.Errorf("UnansweredKeys() = %v", got)
	}
}

// TestAProviderThatCannotSayFallsBackToTheComparison covers the delegate that holds
// no Carries: the recorder then compares, which is the documented approximation.
func TestAProviderThatCannotSayFallsBackToTheComparison(t *testing.T) {
	rec := pseudo.NewRecorder()
	formatter := pseudo.Wrap(silent{}, rec).Select("pt-PT").Formatter
	formatter.Text("task.title", "Task")
	formatter.Text("task.due", "Due")
	if asks := rec.Asks(); len(asks) != 2 || !asks[0].Answered || asks[1].Answered {
		t.Errorf("asks = %+v, want the sentence the delegate re-worded answered and the "+
			"one it echoed not answered", asks)
	}
}

type silent struct{}

func (silent) Select(...string) locale.Locale {
	return locale.Locale{Language: "pt-PT", Formatter: silentFormatter{}}
}

type silentFormatter struct{}

// Text re-words one key and echoes the rest, which is all a delegate with no
// Carries can say about whether an entry stood behind what it was asked.
func (silentFormatter) Text(key, fallback string, args ...any) string {
	if key == "task.title" {
		return "Tarefa"
	}
	return fallback
}

func TestOnePageAtATime(t *testing.T) {
	rec := pseudo.NewRecorder()
	rec.Begin("GET /app")
	defer func() {
		if recover() == nil {
			t.Error("a second Begin over an open page returned, so asks would be attributed " +
				"to whichever page last asked")
		}
	}()
	rec.Begin("GET /app/admin/login")
}

func TestEndsWithNoPageOpenRefuses(t *testing.T) {
	rec := pseudo.NewRecorder()
	defer func() {
		if recover() == nil {
			t.Error("End with no page open returned")
		}
	}()
	rec.End()
}

func TestAsksAreRecordedFromAnyGoroutine(t *testing.T) {
	rec := pseudo.NewRecorder()
	rec.Begin("GET /app")
	defer rec.End()
	formatter := pseudo.Wrap(fake(), rec).Select("pt-PT").Formatter
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			formatter.Text("task.title", "Task")
		}()
	}
	wg.Wait()
	asks := rec.Asks()
	if len(asks) != 8 {
		t.Fatalf("recorded %d asks from 8 calls", len(asks))
	}
	for _, ask := range asks {
		if ask.Page != "GET /app" {
			t.Errorf("ask %+v belongs to no open page", ask)
		}
	}
}

func TestWrapRefusesADelegateThatIsNotThere(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Wrap(nil) returned; an unwrapped page would report itself as measured")
		}
	}()
	pseudo.Wrap(nil, nil)
}

// TestNoSourceMayShipThePseudoTag is xtext's refusal: `language.Parse` accepts the
// tag, so without the refusal a stray file would make a deployment answer people in
// the locale its test harness renders with.
func TestNoSourceMayShipThePseudoTag(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("Load accepted a catalogue written in the pseudo-locale")
		}
		if message, ok := recovered.(string); !ok || !strings.Contains(message, "pseudo-locale") {
			t.Fatalf("recovered %v; want the refusal that names the pseudo-locale", recovered)
		}
	}()
	xtext.Load("en", xtext.Source{FS: fstest.MapFS{
		"pt-PT.json": mapFile([]byte(`{"a":{"translation":"A"}}`)),
		"en-XA.json": mapFile([]byte(`{"a":{"translation":"⟦Á⟧"}}`)),
	}, Name: "stray"})
}

// mapFile is one catalogue file, spelled once: an fstest.MapFS value is a
// *fstest.MapFile, and a case that builds its own catalogue would rather say Data.
func mapFile(body []byte) *fstest.MapFile { return &fstest.MapFile{Data: body} }
