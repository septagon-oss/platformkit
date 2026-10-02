package xtext

import (
	"strings"

	"github.com/septagon-oss/platformkit/kit/locale"
	"golang.org/x/text/language"
)

// pseudoExtension is the BCP 47 region a pseudo-locale wears. "XA" is the code the
// industry reserves for exactly this: a locale whose copy is the source language
// written so that a string which never reached a catalogue stands out on the page.
// "ar-XA" is its right-to-left pair; this provider derives the one whose base
// language the catalogues already answer in.
const pseudoExtension = "xa"

// Pseudo adds a debugging language to a composition's messages.
//
// The tag is "<base>-XA" — "en-XA" beside an English source — and it is answered
// from the base language's own copy, run through one deterministic transform: every
// letter accented, the sentence bracketed so it reads longer than it is. Nothing
// about it is a fact about anybody's customers: it exists because the only reliable
// way to see an untranslated string on a page is to make every translated one
// look unlike the source it came from. A page rendered in the pseudo-locale that
// still says "Save" says it because no catalogue was ever asked.
//
// Because the transform runs over the finished sentence, it cannot tell a word the
// code supplied from one a person did: a tenant's own name arrives accented too.
// That is the honest limit of deriving copy rather than writing it, and it is why
// the pseudo-locale is a debugging language a composition opts into and never a
// language anybody's users are served in.
//
// The pseudo-locale joins Languages(), so a caller may ask for it, a tenant may
// declare it, and the answer is the same catalogue with its letters moved — not a
// second set of copy that can drift from the first.
func Pseudo(base Catalog, tag string) Catalog {
	pseudoTag, err := language.Parse(tag)
	if err != nil {
		panic("xtext: " + tag + " is not a language Pseudo can serve")
	}
	if region, _ := pseudoTag.Region(); region.String() != strings.ToUpper(pseudoExtension) {
		panic("xtext: " + tag + " is not a pseudo-locale: only the -" + strings.ToUpper(pseudoExtension) + " region is derived")
	}
	wanted, _ := pseudoTag.Base()
	for _, served := range base.Languages() {
		servedTag, err := language.Parse(served)
		if err != nil {
			continue
		}
		servedBase, _ := servedTag.Base()
		if servedBase == wanted && served != tag {
			return pseudoCatalog{Catalog: base, tag: tag, base: served}
		}
	}
	panic("xtext: " + tag + " derives from " + wanted.String() + ", which these catalogues do not answer in")
}

// pseudoCatalog is the same messages with one more language on offer.
type pseudoCatalog struct {
	Catalog
	tag  string
	base string
}

func (p pseudoCatalog) Languages() []string {
	return append(p.Catalog.Languages(), p.tag)
}

// Select answers in the pseudo-locale when the caller asked for it and the language
// the composition would otherwise answer in is the one it is derived from. The
// second condition is what keeps a Portuguese tenant from being rendered in accented
// English by a browser that happens to name the debugging tag first: the
// pseudo-locale rides on a language the reader is already being served, distorting
// that copy rather than choosing another.
func (p pseudoCatalog) Select(preferences ...string) locale.Locale {
	selected := p.Catalog.Select(preferences...)
	if !askedFor(preferences, p.tag) || !sameLanguage(selected.Language, p.base) {
		return selected
	}
	return locale.Locale{Language: p.tag, Formatter: pseudoFormatter{Formatter: selected.Formatter}}
}

// askedFor reports whether a preference list names the pseudo-locale ahead of every
// language it recognises. Entries are read in the order the caller put them, each
// Accept-Language line as one string, because the ranking belongs to the caller and
// rewriting it here would be this provider choosing a language instead.
func askedFor(preferences []string, tag string) bool {
	want, err := language.Parse(tag)
	if err != nil {
		return false
	}
	for _, preference := range preferences {
		for _, entry := range strings.Split(preference, ",") {
			name, _, _ := strings.Cut(entry, ";")
			asked, err := language.Parse(strings.TrimSpace(name))
			if err != nil {
				continue
			}
			if asked == want {
				return true
			}
			askedBase, _ := asked.Base()
			wantBase, _ := want.Base()
			if askedBase != wantBase {
				// Any other language named ahead of the pseudo-locale outranks it.
				return false
			}
		}
	}
	return false
}

// sameLanguage compares what a caller named with what a selection answered, by
// language and not by tag: en-GB and en answer in the same copy.
func sameLanguage(a, b string) bool {
	first, err := language.Parse(a)
	if err != nil {
		return false
	}
	second, err := language.Parse(b)
	if err != nil {
		return false
	}
	firstBase, _ := first.Base()
	secondBase, _ := second.Base()
	return firstBase == secondBase
}

// pseudoFormatter is another formatter's answers, accented and lengthened.
type pseudoFormatter struct{ locale.Formatter }

func (p pseudoFormatter) Text(key, fallback string, args ...any) string {
	return accent(p.Formatter.Text(key, fallback, args...))
}

// Plural asks the wrapped formatter for the language's own cardinal form and
// accents the sentence it picked, so a count selects the form the way every other
// language does and the result still looks like foreign copy.
func (p pseudoFormatter) Plural(key string, count int64, one, other string, args ...any) string {
	if plural, ok := p.Formatter.(locale.PluralFormatter); ok {
		return accent(plural.Plural(key, count, one, other, args...))
	}
	return accent(p.Formatter.Text(key, one))
}

// accent is the transform on one string: every ASCII letter replaced by an accented
// one of the same letter, the sentence bracketed so a reader sees where copy begins
// and a developer sees a box it no longer fits in. The arguments a sentence was
// built with are already inside it by then, which is why a tenant's own name arrives
// accented beside the words around it.
func accent(s string) string {
	var out strings.Builder
	out.Grow(len(s) + 8)
	out.WriteString("⟪")
	for _, r := range s {
		out.WriteString(letter(r))
	}
	out.WriteString("⟫")
	return out.String()
}

// letters maps each ASCII letter to one Latin letter with a diacritic, so the
// accented run reads as foreign copy at a glance and never as a word an
// unaccented-ASCII check could miss.
var letters = map[rune]string{
	'a': "à", 'b': "ƀ", 'c': "ç", 'd': "ð", 'e': "é", 'f': "ƒ", 'g': "ĝ",
	'h': "ħ", 'i': "ï", 'j': "ĵ", 'k': "ķ", 'l': "ł", 'm': "ɱ", 'n': "ñ",
	'o': "ö", 'p': "þ", 'q': "ɋ", 'r': "ŕ", 's': "š", 't': "ţ", 'u': "û",
	'v': "ṽ", 'w': "ŵ", 'x': "х", 'y': "ý", 'z': "ž",
}

func letter(r rune) string {
	lower := r
	if r >= 'A' && r <= 'Z' {
		lower = r + ('a' - 'A')
	}
	accented, known := letters[lower]
	if !known {
		return string(r)
	}
	if r != lower {
		return strings.ToUpper(accented)
	}
	return accented
}
