// Package pseudo supplies the pseudo-locale a translation gate renders with.
//
// Every sentence it returns is the delegate's own copy, marked so a reader of a
// rendered document can see at a glance which words went through a catalogue and
// which somebody wrote in Go: the text is wrapped in ⟦…⟧ and its vowels accented.
// The transformation is reversible and length-preserving, so a page can be checked
// by machine and un-read by a person, and nothing about it changes how the page
// lays out.
//
// This is the standard technique — Android's `en-XA`, Chromium's pseudolocales — and
// the reason it exists: the number that matters is the share of the words a person
// reads that a translator can reach, and no source scan can answer that when most
// call sites build their key at run time. Only the shape of the copy is pseudo; the
// language a person is served in never is, because Wrap returns
// [locale.Messages], not a catalogue, and so reaches no `Languages()` set, no
// tenant's supported list and no `?lang=` choice. Compose it in a test.
package pseudo

import (
	"fmt"
	"strings"

	"github.com/septagon-oss/platformkit/kit/locale"
)

// Tag is the pseudo-locale's BCP-47 subtag. `language.Parse` accepts it, which is
// what lets a rendered document declare `lang="en-XA"` truthfully: the copy in it
// really is written in this locale and in no other.
const Tag = "en-XA"

// carrier is the optional half of a delegate that can say whether a key stands
// behind its own answer. [xtext]'s catalogue implements it and a plain provider
// does not, so a delegate that cannot say is asked no question and the recorder
// falls back to comparing the answer with the text the call site passed.
type carrier interface {
	Carries(language, key string) bool
}

// messages marks every formatter the delegate selects and records every ask.
type messages struct {
	delegate locale.Messages
	rec      *Recorder
}

// Wrap answers every selection as the pseudo-locale: the delegate's own copy,
// marked. The delegate is asked with the caller's own preferences, so the copy
// under the wrap is the copy the real negotiation would have produced — a gate can
// send `Accept-Language: pt-PT` and learn both whether a string reached a key and
// whether pt-PT carries it. A nil delegate panics at composition, which is
// [locale.SelectLocale]'s existing refusal reached one layer earlier.
//
// rec may be nil, in which case nothing is recorded and the wrap is all there is.
func Wrap(delegate locale.Messages, rec *Recorder) locale.Messages {
	if delegate == nil {
		panic("pseudo: localization needs a delegate to mark")
	}
	return messages{delegate: delegate, rec: rec}
}

func (m messages) Select(preferences ...string) locale.Locale {
	selected := m.delegate.Select(preferences...)
	if selected.Language == "" || selected.Formatter == nil {
		panic("pseudo: the delegate returned an incomplete locale")
	}
	return locale.Locale{Language: Tag,
		Formatter: formatter{language: selected.Language, delegate: selected.Formatter,
			carrier: m.delegate, rec: m.rec}}
}

// formatter marks the copy its delegate answers with and says what happened.
type formatter struct {
	language string
	delegate locale.Formatter
	carrier  any
	rec      *Recorder
}

// Text marks the delegate's formatted answer, so the arguments a sentence
// interpolates travel inside the wrap with the sentence around them.
func (f formatter) Text(key, fallback string, args ...any) string {
	answer := f.delegate.Text(key, fallback, args...)
	if f.rec != nil {
		f.rec.ask(f.carrier, key, f.language, answer, fallback, args)
	}
	return Mark(answer)
}

// open and shut delimit marked copy. They are punctuation nobody writes in a
// sentence, and a payload that holds one of its own doubles it, as RFC 4180
// quotes a quote.
const (
	open = "⟦"
	shut = "⟧"
)

// doubling turns each delimiter into the pair that means itself.
var doubling = strings.NewReplacer(open, open+open, shut, shut+shut)

// clear reserves the accented characters the accent pass is about to hand out, so
// the two passes together are a bijection over the runes they touch: a payload
// that already reads `á` ends up as `ạ`, and the un-mark turns it back.
var clear = strings.NewReplacer(
	"á", "ạ", "à", "ȧ", "é", "ẹ", "í", "ḭ", "ó", "ọ", "ú", "ṵ",
	"Á", "Ạ", "À", "Ȧ", "É", "Ẹ", "Í", "Ḭ", "Ó", "Ọ", "Ú", "Ṳ")

// accent is the vowel map a reader sees. Every output is one rune, so marking
// never changes a string's length and never pushes a layout over its edge.
var accent = strings.NewReplacer(
	"a", "á", "e", "é", "i", "í", "o", "ó", "u", "ú",
	"A", "Á", "E", "É", "I", "Í", "O", "Ó", "U", "Ú")

// undoAccent and undoClear are the two passes inverted, applied in the other order.
var (
	undoAccent = strings.NewReplacer(
		"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u",
		"Á", "A", "É", "E", "Í", "I", "Ó", "O", "Ú", "U")
	undoClear = strings.NewReplacer(
		"ạ", "á", "ȧ", "à", "ẹ", "é", "ḭ", "í", "ọ", "ó", "ṵ", "ú",
		"Ạ", "Á", "Ȧ", "À", "Ẹ", "É", "Ḭ", "Í", "Ọ", "Ó", "Ṳ", "Ú")
	undoubling = strings.NewReplacer(open+open, open, shut+shut, shut)
)

// Mark wraps its payload in the pseudo-locale's delimiters and accents it. It is
// the whole transformation, and [Unmark] is its exact inverse for every string.
func Mark(payload string) string {
	return open + accent.Replace(clear.Replace(doubling.Replace(payload))) + shut
}

// Unmark reads marked copy back. A string Mark did not produce is returned as
// found: the inverse is a tool for the gate, not a second refusal layer.
func Unmark(marked string) string {
	if !Wrapped(marked) {
		return marked
	}
	body := marked[len(open) : len(marked)-len(shut)]
	return undoClear.Replace(undoAccent.Replace(undoubling.Replace(body)))
}

// Wrapped says whether a string is marked copy: delimiting pair present, and
// nothing inside the pair that is not either ordinary text or a doubled
// delimiter. A string that merely looks accented is not wrapped, which is the
// point — the gate counts the delimiters and nothing else.
func Wrapped(text string) bool {
	if len(text) < len(open)+len(shut) ||
		!strings.HasPrefix(text, open) || !strings.HasSuffix(text, shut) {
		return false
	}
	body := text[len(open) : len(text)-len(shut)]
	// Doubling is the only way a delimiter belongs inside the payload, so a scan
	// that removes every pair and still finds one means the pair closed early.
	for i := 0; i < len(body); {
		if strings.HasPrefix(body[i:], open+open) {
			i += 2 * len(open)
			continue
		}
		if strings.HasPrefix(body[i:], shut+shut) {
			i += 2 * len(shut)
			continue
		}
		if strings.HasPrefix(body[i:], open) || strings.HasPrefix(body[i:], shut) {
			return false
		}
		i++
	}
	return true
}

// answered says whether the delegate's copy came from an entry rather than from
// the readable text the call site passed. A delegate that can say does; one that
// cannot is asked the comparison instead, which over-counts work for a sentence
// whose translation happens to read exactly like its English and under-counts it
// for a sentence whose arguments hide the difference.
func answered(delegate any, language, key, answer, fallback string, args []any) bool {
	if c, ok := delegate.(carrier); ok {
		return c.Carries(language, key)
	}
	want := fallback
	if len(args) > 0 {
		want = fmt.Sprintf(fallback, args...)
	}
	return Unmark(answer) != want
}
