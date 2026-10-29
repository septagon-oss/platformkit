package httpx

import (
	"sort"
	"strings"
)

// aliases.go is the one release in which an old address still means something: a person
// with a bookmark, an installed native build or a link somebody emailed all still hold
// the address the surfaces moved off. Each row below answers it with a redirect.
//
// An alias is a redirect and never a second mount, never a 301, and a row names the
// composition it is written for: the doors a row aims at are mounted by modules this
// package may not import, so a row about an optional door is a decision rather than a
// detail. README.md, "One release of the old addresses" holds both rules, the removal
// release, what is deliberately absent and how each row is proven to lead somewhere.

// aliasTable is the migration table: an old address, or an old subtree, and
// where it goes. Longest prefix wins, so a specific row is not shadowed by a
// general one beneath it.
var aliasTable = []struct{ from, to string }{
	// The four pages the admin module owns. The old shell root doubled as that
	// module's namespace — /admin/login was its sign-in page and /admin/assets
	// its files, not a module called login — while /admin/<module>/<entity> was
	// a generated screen. One row per owned page, above the general one, or the
	// general row sends a bookmark of the sign-in page to a screen of that name.
	{"/admin/login", AppRoot + "/admin/login"},
	{"/admin/health", AppRoot + "/admin/health"},
	{"/admin/assets", AppRoot + "/admin/assets"},
	{"/admin/_gallery", AppRoot + "/admin/_gallery"},
	{"/admin", AppRoot},
	{apiRoot + "/admin", apiRoot + "/app"},
	{apiRoot + "/content/public", apiRoot + "/public/content/contents"},
	{apiRoot + "/file/public", apiRoot + "/public/file/files"},
	{apiRoot + "/site/settings/public", apiRoot + "/public/site/settings"},
	{apiRoot + "/auth/password/forgot", apiRoot + "/public/auth/password/forgot"},
	// The three inquiry doors of signup. They are in this table because this
	// repository's composition mounts them — apps/platformkit/modules.go turns
	// password-first signup on — and an alias that aimed at a door nobody
	// mounted is the redirect into a 404 this file calls worse than the 404. A
	// composition that leaves signup off takes these three rows out with it, and
	// the case named above is what notices: it asks the running server rather
	// than reading the table.
	{apiRoot + "/auth/register", apiRoot + "/public/auth/register"},
	{apiRoot + "/auth/resend-verification", apiRoot + "/public/auth/resend-verification"},
	{apiRoot + "/auth/verify-email", apiRoot + "/public/auth/verify-email"},
}

// alias is the whole of the table: the new address of an old one, if it has
// one. The match is an exact address or a path segment prefix — never a bare
// string prefix, which would send /administrator somewhere it never was.
func alias(path string) (string, bool) {
	return lookup(aliasTable, path)
}

// Alias adds one row to this API's own migration table: a module's moved
// address (module.Module.Moved), registered by kit/app before any route is
// served. The kernel's rows above are asked first, then this composition's,
// longest From first, so a module's specific row is not shadowed by its general
// one. The same two rules hold: a redirect and never a second mount — the
// surface gate refuses a route mounted at a moved address — and never a 301.
func (a *API) Alias(from, to string) {
	a.moved = append(a.moved, struct{ from, to string }{strings.TrimSuffix(from, "/"), strings.TrimSuffix(to, "/")})
	sort.SliceStable(a.moved, func(i, j int) bool { return len(a.moved[i].from) > len(a.moved[j].from) })
}

// alias is the kernel's table, then this composition's own rows.
func (a *API) alias(path string) (string, bool) {
	if to, ok := alias(path); ok {
		return to, true
	}
	return lookup(a.moved, path)
}

func lookup(table []struct{ from, to string }, path string) (string, bool) {
	for _, a := range table {
		if path == a.from {
			return a.to, true
		}
		if rest, ok := strings.CutPrefix(path, a.from+"/"); ok {
			return a.to + "/" + rest, true
		}
	}
	return "", false
}
