package page

import (
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/message/catalog"
)

// Locale contains one render's content language and message formatter. Format
// strings and interpolation stay with x/text; render its strings through g.Text
// or typed component labels, never g.Raw.
type Locale struct {
	Language string
	*message.Printer
}

// SelectLocale matches explicit preferences before an Accept-Language header.
// Unsupported or malformed preferences fall through to the next preference,
// then the catalog's configured fallback. Language names a supported content
// language; the printer may retain the visitor's regional number formatting.
//
// Compose namespaced messages into this catalog before serving requests and
// retain it as a read-only dependency. There is no process-wide default catalog.
// Parent-language translations and message.Key's source-text fallback are
// provided by x/text. A fallback language does not supply missing translations
// from an unrelated language; provide source text with every message.Key.
func SelectLocale(messages catalog.Catalog, preferences ...string) Locale {
	if messages == nil || len(messages.Languages()) == 0 {
		panic("page: localization needs a nonempty catalog")
	}
	tag, index := language.MatchStrings(messages.Matcher(), preferences...)
	return Locale{Language: messages.Languages()[index].String(),
		Printer: message.NewPrinter(tag, message.Catalog(messages))}
}
