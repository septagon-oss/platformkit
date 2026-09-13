package page

import (
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/message/catalog"
)

// Messages selects a request-local formatter from a composition's immutable
// messages. Preferences are ordered: explicit choice, browser, then default.
// Implementations must support concurrent selection without global state.
type Messages interface {
	Select(preferences ...string) Locale
}

// Formatter translates a namespaced key, using readable source text when it is
// missing. Render the result through g.Text or typed labels, never g.Raw.
// The provider owns interpolation and plural rules.
type Formatter interface {
	Text(key, fallback string, args ...any) string
}

// Locale contains one render's supported content language and formatter.
type Locale struct {
	Language string
	Formatter
}

// SelectLocale negotiates one request's language. Providers return a supported
// language and a formatter, including when no preference is supported.
func SelectLocale(messages Messages, preferences ...string) Locale {
	if messages == nil {
		panic("page: localization needs messages")
	}
	locale := messages.Select(preferences...)
	if locale.Language == "" || locale.Formatter == nil {
		panic("page: localization provider returned an incomplete locale")
	}
	return locale
}

// FromCatalog adapts x/text's CLDR formatting and language negotiation. Compose
// all module catalogs before calling this and do not mutate them while serving.
// No process-wide catalog is read or written. Regional number formatting may be
// retained even when Language names a more general supported content language.
func FromCatalog(messages catalog.Catalog) Messages {
	if messages == nil || len(messages.Languages()) == 0 {
		panic("page: localization needs a nonempty catalog")
	}
	return catalogMessages{messages}
}

type catalogMessages struct{ catalog.Catalog }

func (m catalogMessages) Select(preferences ...string) Locale {
	tag, index := language.MatchStrings(m.Matcher(), preferences...)
	return Locale{Language: m.Languages()[index].String(),
		Formatter: catalogFormatter{message.NewPrinter(tag, message.Catalog(m.Catalog))}}
}

type catalogFormatter struct{ printer *message.Printer }

func (f catalogFormatter) Text(key, fallback string, args ...any) string {
	return f.printer.Sprintf(message.Key(key, fallback), args...)
}
