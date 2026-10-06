// Package xtext adapts an explicitly supplied x/text message catalog to locale.
// It reads no process-global catalog and starts no background work.
package xtext

import (
	"fmt"
	"github.com/septagon-oss/platformkit/kit/locale"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/message/catalog"
	"strings"
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
		Formatter: catalogFormatter{printer: message.NewPrinter(tag, message.Catalog(m.Catalog)), tag: tag, catalog: m.Catalog}}
}

type catalogFormatter struct {
	printer *message.Printer
	tag     language.Tag
	catalog catalog.Catalog
	forms   map[string]map[string]string
}

func (f catalogFormatter) Text(key, fallback string, args ...any) string {
	return f.printer.Sprintf(message.Key(key, fallback), args...)
}

func (f catalogFormatter) Plural(key string, count int64, one, other string, args ...any) string {
	if forms := f.forms[key]; len(forms) > 0 {
		selected := forms[pluralForm(f.tag, count)]
		if selected == "" {
			selected = forms["other"]
		}
		if !strings.Contains(selected, "%") {
			return selected
		}
		return f.printer.Sprintf(selected, append([]any{count}, args...)...)
	}
	// The composed catalog answers this key in the selected language, so x/text
	// chooses the form by that language's CLDR cardinal rule and interpolates the
	// count: a plural message stored under the key wins over the caller's copy, as
	// it does for Text. Only a key the catalog does not carry falls through to the
	// readable text below, which is what a fallback is for.
	if carries(f.catalog, f.tag, key) {
		return f.printer.Sprintf(message.Key(key, other), append([]any{count}, args...)...)
	}
	selected := other
	if count == 1 {
		selected = one
	}
	if !strings.Contains(selected, "%") {
		return selected
	}
	return fmt.Sprintf(selected, append([]any{count}, args...)...)
}

// asks is the renderer a catalog context needs to be asked whether it carries a
// key: it renders nothing and takes no arguments, because only the error —
// catalog.ErrNotFound or not — is read out of the call.
type asks struct{}

func (asks) Arg(int) interface{} { return nil }
func (asks) Render(string)       {}

// carries says whether the composed catalog answers this key for this language,
// by the language's own entry or one it falls back to. `lookup` on a catalog is
// unexported, so the question is asked the way a formatter asks it: try to
// evaluate the key and see whether the catalog says it has it.
func carries(messages catalog.Catalog, tag language.Tag, key string) bool {
	if messages == nil || key == "" {
		return false
	}
	return messages.Context(tag, asks{}).Execute(key) != catalog.ErrNotFound
}
