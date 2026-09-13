// Package xtext adapts an explicitly supplied x/text message catalog to locale.
// It reads no process-global catalog and starts no background work.
package xtext

import (
	"github.com/septagon-oss/platformkit/kit/locale"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/message/catalog"
)

// FromCatalog adapts x/text's CLDR formatting and language negotiation. Compose
// all module catalogs before calling this and do not mutate them while serving.
// Regional number formatting may be retained even when Language names a more
// general supported content language. A nil or empty catalog panics.
func FromCatalog(messages catalog.Catalog) locale.Messages {
	if messages == nil || len(messages.Languages()) == 0 {
		panic("xtext: localization needs a nonempty catalog")
	}
	return catalogMessages{messages}
}

type catalogMessages struct{ catalog.Catalog }

func (m catalogMessages) Select(preferences ...string) locale.Locale {
	tag, index := language.MatchStrings(m.Matcher(), preferences...)
	return locale.Locale{Language: m.Languages()[index].String(),
		Formatter: catalogFormatter{message.NewPrinter(tag, message.Catalog(m.Catalog))}}
}

type catalogFormatter struct{ printer *message.Printer }

func (f catalogFormatter) Text(key, fallback string, args ...any) string {
	return f.printer.Sprintf(message.Key(key, fallback), args...)
}
