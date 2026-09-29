package page

import (
	"slices"
	"strings"

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

// Served is the preference list one request is answered from: what it asked for,
// restricted to what the tenant it belongs to is served in, with that tenant's own
// default at the end so a request nothing matches is answered in the language the
// tenant chose rather than the catalog's.
//
// The tenant's list arrives through Request.Tenant, which is the host resolution's
// own answer — the same query that says who this request belongs to says what it is
// read in — so an unresolved host, and every guard that refuses before one exists,
// has no list to restrict by and is negotiated from what it asked for. That is not a
// hole: with no tenant there is no tenant preference to honour, and the deployment's
// catalog is the only declaration left standing.
//
// Matching is on the tag and its preceding tags, which is what "this browser speaks
// Portuguese" means when the tenant serves pt-PT: en-GB is served by an en tenant
// and pt is served by pt-PT, while de is served by neither. A request that asked
// only for languages this tenant does not serve is not answered in one of them — the
// list it brought is empty by the time this returns, and the default it ends with is
// the answer.
func TenantPreferences(r Request, preferences ...string) []string {
	supported := r.Tenant.Languages.Preferred()
	if len(supported) == 0 {
		return preferences
	}
	out := make([]string, 0, len(preferences)+len(supported))
	for _, preference := range preferences {
		for _, token := range strings.Split(preference, ",") {
			// One browser header line is a list, and each entry is asked about on its
			// own. The quality value stays on what is passed on: the provider reads
			// it, and a list sorted by hand here would be a second negotiation.
			token = strings.TrimSpace(token)
			asked := strings.SplitN(token, ";", 2)[0]
			if asked != "" && speaks(supported, asked) {
				out = append(out, token)
			}
		}
	}
	// The tenant's default last, and its whole set with it: a preference list is
	// what the provider negotiates over, and the languages this tenant never
	// mentioned should not become candidates. The default is first in the tenant's
	// own list, so it is the one that answers when none of the above does.
	for _, tag := range supported {
		if !slices.Contains(out, tag) {
			out = append(out, tag)
		}
	}
	return out
}

// speaks is whether one of the supported tags covers a preference, either exactly or
// by being the broader tag the preference is a region of.
func speaks(supported []string, preference string) bool {
	if slices.Contains(supported, preference) {
		return true
	}
	for _, tag := range supported {
		if strings.HasPrefix(preference, tag+"-") || strings.HasPrefix(tag, preference+"-") {
			return true
		}
	}
	return false
}

// FromCatalog adapts x/text's CLDR formatting and language negotiation. Compose
// all module catalogs before calling this and do not mutate them while serving.
// No process-wide catalog is read or written. Regional number formatting may be
// retained even when Language names a more general supported content language.
func FromCatalog(messages catalog.Catalog) Messages {
	return xtext.FromCatalog(messages)
}
