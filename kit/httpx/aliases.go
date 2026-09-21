package httpx

import "strings"

// aliases.go is the one release in which an old address still means something.
//
// The surfaces moved the generated shell from /admin to /app and the catalog
// from /api/v1/admin/resources to /api/v1/app/resources, and the public face
// took its own prefix under the API. A person with a bookmark, a native build
// already installed, and a link somebody emailed all still hold the old
// address. Each row below answers it with a redirect and nothing else.
//
// Two rules hold the table honest:
//
//   - An alias is a redirect, never a second mount. A screen served at two
//     addresses gives one form two namespaces (ui/forms.Namespace is derived
//     from the path), which is how a controller starts writing to the wrong
//     page — so the alias is spent ahead of the router, and no route exists at
//     the old path to serve a body from.
//   - Never a 301. A permanent redirect a browser caches outlives the release
//     that removes the row, and the person then gets a 404 from a cached answer
//     with nothing in the application left to fix. 302 for a safe method and
//     307 for the rest, so the method and the body survive and nothing is
//     remembered past this visit.
//
// Removal: every row here is deleted in v1.3.0, one release after they shipped,
// as README.md and CHANGELOG.md say. Two tests hold the table, one per half of
// what a row claims: §TestTheAliasRedirectsAndNeverServes, in this package,
// checks that every row redirects the way the table says and that nothing is
// served at the old address besides the redirect. The other half belongs to the
// composition and no fixture here can prove it — the doors an alias aims at are
// mounted by modules this package may not import — so
// §TestEveryAliasRowOfTheReferenceApplicationLeadsSomewhereThatAnswers asks the
// reference application's running server, row by row, whether the address each
// row points at actually answers. Without that half a row can name an address
// nothing serves, and the person holding the bookmark gets a redirect into a 404
// — which is what makes a row about an optional door a decision rather than a
// detail, and why the rows below say which composition they are written for.
//
// What is deliberately absent: /api/v1/tenant/tenants. Those routes moved to the
// control plane, and a control plane does not announce its new address by
// keeping the old one open — the old path answers 404 at a tenant host because
// nothing is mounted there, and 404 at the installation host because the alias
// table does not vouch for it either.

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
	for _, a := range aliasTable {
		if path == a.from {
			return a.to, true
		}
		if rest, ok := strings.CutPrefix(path, a.from+"/"); ok {
			return a.to + "/" + rest, true
		}
	}
	return "", false
}
