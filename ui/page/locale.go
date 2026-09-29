package page

import (
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"golang.org/x/text/language"
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
// Matching is on the language a tag names, and the tag that goes on to the provider
// is the tenant's. That is what "this browser speaks Portuguese" means when the
// tenant serves pt-PT: en-GB is served by an en tenant, and so is every Portuguese a
// browser can name — bare pt, pt-BR, pt-MO — because a region says where a person's
// Portuguese comes from, not which tenant languages it is worth answering in. A
// tenant that declared Portuguese declares the language, and the region it is
// answered in is the one that declaration names. `de` names no language this tenant
// serves and is refused by the same rule. A request that asked only for languages
// this tenant does not serve is not answered in one of them — the list it brought is
// empty by the time this returns, and the default it ends with is the answer.
//
// Rewriting the surviving tag, rather than passing the caller's spelling on, is the
// half that makes the tenant's set a ceiling on the answer and not only on the input:
// a deployment may carry two regions of one language (xtext.Load reads every
// `<locale>.json` a source holds, so pt-BR.json beside pt-PT.json is a legal
// catalogue), the provider selects from the tags it is handed, and a pt-BR header kept
// whole would answer a tenant that declared only European Portuguese from the
// Brazilian file.
func TenantPreferences(r Request, preferences ...string) []string {
	supported := r.Tenant.Languages.Preferred()
	if len(supported) == 0 {
		return preferences
	}
	out := make([]string, 0, len(preferences)+len(supported))
	for _, preference := range preferences {
		kept := make([]string, 0, len(preferences)+1)
		for _, token := range strings.Split(preference, ",") {
			// One browser header line is a list, and each entry is asked about on its
			// own — but the list goes on as one string. The provider reads a single
			// header in the order its quality values put it; hand it a list of
			// separate entries and it takes the first that matches anything, so
			// filtering here would keep the tenant's set and throw away the
			// caller's ranking, and the same request written in the other order
			// would be answered in the other language.
			token = strings.TrimSpace(token)
			asked, quality, _ := strings.Cut(token, ";")
			tag := declared(supported, asked)
			if tag == "" {
				continue
			}
			// The caller's ranking stays on the entry — round 1's finding is that it
			// must — the caller's region does not.
			if quality != "" {
				tag += ";" + quality
			}
			kept = append(kept, tag)
		}
		if len(kept) == 0 {
			continue
		}
		joined := strings.Join(kept, ",")
		if _, _, err := language.ParseAcceptLanguage(joined); err != nil {
			// One broken quality value is not a reason to stop reading the rest: a
			// list this parser refuses is handed over entry by entry, which is the
			// shape the provider reads one at a time, so the junk costs the one
			// entry that carries it rather than the caller's whole ranking.
			out = append(out, kept...)
			continue
		}
		out = append(out, joined)
	}
	// The tenant's own set behind the caller's list, its default at their head:
	// these are candidates the caller never named, ranked below everything that
	// survived the filter above, and the default leads them so it is the one that
	// answers when none of the above matches — which is why a tenant's default is
	// never left out of its own set.
	for _, tag := range supported {
		if !slices.Contains(out, tag) {
			out = append(out, tag)
		}
	}
	return out
}

// declared is which tag of the tenant's own set answers the language half of a
// preference, or "" when the tenant declares no language the caller speaks.
//
// Comparing whole tags would keep the closest spelling of a language and refuse the
// further one — a tenant serving pt-PT would read "pt" as Portuguese and "pt-BR" as a
// foreign language, and the nearer dialect would be answered in the tenant's default
// while the bare language got the copy. Comparing languages keeps the question the
// filter actually asks: does this person speak a language this tenant is served in.
//
// The answer is one of the tenant's tags and never the caller's spelling of it,
// because the provider picks the copy from the tags it is given: hand it "pt-BR" over
// a deployment carrying both regions and the browser, not the tenant, has chosen which
// Portuguese the page is written in. An exact tag match wins first, so a tenant that
// declared two regions still answers each person in the region they named; failing
// that, the first declaration whose language matches answers, and Preferred() leads
// with the tenant's default, so a person who named no region gets the region the
// tenant defaults to rather than whichever declaration happens to be stored first.
//
// A tag this parser refuses has no language half at all and answers to nothing, which
// is what keeps a header written in junk out of a tenant's set rather than in it.
func declared(supported []string, preference string) string {
	asked, err := language.Parse(strings.TrimSpace(preference))
	if err != nil {
		return ""
	}
	want, _ := asked.Base()
	near := ""
	for _, tag := range supported {
		parsed, err := language.Parse(tag)
		if err != nil {
			continue
		}
		if parsed == asked {
			return tag
		}
		if near == "" {
			if base, _ := parsed.Base(); base == want {
				near = tag
			}
		}
	}
	return near
}

// FromCatalog adapts x/text's CLDR formatting and language negotiation. Compose
// all module catalogs before calling this and do not mutate them while serving.
// No process-wide catalog is read or written. Regional number formatting may be
// retained even when Language names a more general supported content language.
func FromCatalog(messages catalog.Catalog) Messages {
	return xtext.FromCatalog(messages)
}
