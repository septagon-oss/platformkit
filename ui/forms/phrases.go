package forms

import (
	"fmt"

	"github.com/septagon-oss/platformkit/kit/locale"
)

// phraser answers one piece of a form's chrome in the language the request
// negotiated: a catalogue key, the readable source text to fall back to when no
// catalogue holds it, and the values that phrase interpolates. Every sentence
// this package draws a person reads goes through it, so a phrase with a value in
// it is one message that travels whole — word order and punctuation belong to the
// copy that answers, never to Go.
type phraser func(key, fallback string, args ...any) string

// phrasesFor composes the two seams ControlProps and Options carry into one read
// path. A formatter answers every phrase with its provider in charge of
// interpolation, plural rules and regional number format. The older func is asked
// with the phrase already formatted, because a message that takes arguments has a
// key no catalogue written before it can hold, so that seam can only ever answer
// with source text — and source text left holding Go's verbs is a sentence a
// person reads with verbs in it. With neither seam composed, the source text
// stands as it always did.
func phrasesFor(format locale.Formatter, text func(key, fallback string) string) phraser {
	return func(key, fallback string, args ...any) string {
		if format != nil {
			return format.Text(key, fallback, args...)
		}
		line := sourceText(fallback, args)
		if text == nil {
			return line
		}
		return text(key, line)
	}
}

// sourceText is a fallback as the person would read it: formatted when the phrase
// asks for values, verbatim when it does not, because fmt appends "%!(EXTRA …)"
// to a sentence that names no verb.
func sourceText(fallback string, args []any) string {
	if len(args) == 0 {
		return fallback
	}
	return fmt.Sprintf(fallback, args...)
}
