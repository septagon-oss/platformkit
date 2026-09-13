// Package locale supplies language selection and message formatting contracts
// for web pages, workers and other Go applications. Providers own interpolation,
// plural rules and language negotiation; this package starts no services.
package locale

// Messages selects a render-local formatter from a composition's immutable
// messages. Preferences are ordered: explicit choice, browser, then default.
// Implementations must support concurrent selection without global state.
type Messages interface {
	Select(preferences ...string) Locale
}

// Formatter translates a namespaced key, using readable source text when it is
// missing. Text is plain text: HTML renderers must escape it. The provider owns
// interpolation and plural rules.
type Formatter interface {
	Text(key, fallback string, args ...any) string
}

// Locale contains one render's supported content language and formatter.
type Locale struct {
	Language string
	Formatter
}

// SelectLocale negotiates one render's language. Providers return a supported
// language and a formatter, including when no preference is supported. Missing
// or incomplete providers panic because they are composition errors.
func SelectLocale(messages Messages, preferences ...string) Locale {
	if messages == nil {
		panic("locale: localization needs messages")
	}
	selected := messages.Select(preferences...)
	if selected.Language == "" || selected.Formatter == nil {
		panic("locale: localization provider returned an incomplete locale")
	}
	return selected
}
