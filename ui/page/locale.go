package page

import (
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"golang.org/x/text/message/catalog"
)

// Messages is the shared language-selection contract used by pages and workers.
type Messages = locale.Messages

// Formatter returns plain text; render it through g.Text or typed labels.
type Formatter = locale.Formatter

// Locale is the shared selection result, retained here for page callers.
type Locale = locale.Locale

// SelectLocale negotiates one request's language. Providers return a supported
// language and a formatter, including when no preference is supported.
func SelectLocale(messages Messages, preferences ...string) Locale {
	return locale.SelectLocale(messages, preferences...)
}

// FromCatalog adapts x/text's CLDR formatting and language negotiation. Compose
// all module catalogs before calling this and do not mutate them while serving.
// No process-wide catalog is read or written. Regional number formatting may be
// retained even when Language names a more general supported content language.
func FromCatalog(messages catalog.Catalog) Messages {
	return xtext.FromCatalog(messages)
}
