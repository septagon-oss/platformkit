package main

// Reviewer's case for the third review of T-0024 (three surfaces by path).
// Reviewer: a fresh pi session, 2026-09-21.
//
// kit/httpx/aliases.go states its own second half and names the test that holds it:
//
//	"The test over there (§TestTheAliasRedirectsAndNeverServes) checks that every row
//	 redirects the way the table says, and the reference application
//	 (§TestTheOldAddressesOfTheReferenceApp) asks its running server whether the address
//	 each row points at actually answers — … Without that second half a row can name an
//	 address nothing serves, and the person holding the bookmark gets a redirect into a 404."
//
// No test of that name exists in the repository (`git grep TestTheOldAddressesOfTheReferenceApp`
// finds the two comments that cite it and nothing else), and the failure it was cited to
// prevent is real in the composition this repository ships. The rows for auth's public
// inquiry doors point at addresses that are mounted only when a deployment turns
// registration on, and this application does not: the bookmark is redirected and then
// refused. The rows are not wrong for an installation that composes registration — which is
// exactly why the half has to be a test against a running composition rather than a reading
// of the table: whether a target answers is a fact about the composition, and the table
// cannot see it.
//
// The case takes its rows out of aliases.go itself — the same way
// TestEveryAddressAShippedExampleNavigatesToIsStillServed takes its addresses out of the
// shipped component reference — so a row added, changed or removed is read by the case
// without editing it. A prefix row is asked one level deeper than its own root, because what
// the alias is for is the thing beneath it: the shell's stylesheet, a generated screen, the
// catalog document. Its own root is either a directory (which a mounted tree has refused by
// design since this branch) or a namespace nobody bookmarks.
//
// The fix has two passing branches and the case does not care which is taken: enable
// registration in the composition these rows are vouched for, or drop the rows that point at
// a door this composition does not mount, which is what aliases.go says it did for
// /api/v1/tenant/tenants and the reason it gives for that absence.

import (
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// aliasRow is one row of the migration table, read off the file that is its owner.
type aliasRow struct{ from, to string }

// aliasRows reads kit/httpx/aliases.go and evaluates its two constants. The table is
// unexported and stays unexported: nothing but this claim needs it at runtime, and the
// claim is about a source file.
func aliasRows(t *testing.T) []aliasRow {
	t.Helper()
	src, err := os.ReadFile("../../kit/httpx/aliases.go")
	if err != nil {
		t.Fatalf("read the migration table: %v", err)
	}
	// apiRoot and AppRoot are the two prefixes the rows compose their addresses from. They
	// are declared beside the surface table itself, and they are read here rather than
	// restated, so the case cannot drift from the kernel.
	consts := map[string]string{}
	prefixes, err := os.ReadFile("../../kit/httpx/surfaces.go")
	if err != nil {
		t.Fatalf("read the surface prefixes: %v", err)
	}
	for _, m := range regexp.MustCompile(`(?m)^\t(AppRoot|apiRoot)\s*=\s*"([^"]*)"`).FindAllStringSubmatch(string(prefixes), -1) {
		consts[m[1]] = m[2]
	}
	if consts["AppRoot"] == "" || consts["apiRoot"] == "" {
		t.Fatal(`the migration table no longer names AppRoot and apiRoot; read it and update how this case reads it`)
	}

	// eval turns one side of a row into the address it means: a literal, or a prefix
	// constant added to a literal.
	eval := func(expr string) string {
		expr = strings.TrimSpace(expr)
		if at, tail, cut := strings.Cut(expr, "+"); cut {
			return consts[strings.TrimSpace(at)] + strings.Trim(strings.TrimSpace(tail), `"`)
		}
		return strings.Trim(expr, `"`)
	}

	var rows []aliasRow
	for _, m := range regexp.MustCompile(`(?m)^\t\{(".*?"|apiRoot \+ ".*?"|AppRoot \+ ".*?"), (".*?"|apiRoot \+ ".*?"|AppRoot \+ ".*?")\},`).FindAllStringSubmatch(string(src), -1) {
		rows = append(rows, aliasRow{eval(m[1]), eval(m[2])})
	}
	if len(rows) < 10 {
		t.Fatalf("the case read %d rows out of the migration table, which is fewer than the table holds; the rows have "+
			"changed shape and this case is now reading nothing: %v", len(rows), rows)
	}
	return rows
}

// aliasConcrete names the address the case dials beneath a row, where the row's own root is
// not an address anybody holds: a prefix row's namespace (the shell's stylesheet, a generated
// screen, the catalog document), or a door that reads one thing by id and so answers nothing
// at its collection address. TestPinnedAddresses asks the same shape of question — the public
// file door one level deeper than its prefix, "not-an-id" for the id — and for the same
// reason: that the door is mounted is the claim, and only what is mounted answers it.
// Every other row is dialled as itself.
var aliasConcrete = map[string]string{
	"/admin":                 "/task/tasks",
	"/admin/assets":          "/app.css",
	"/api/v1/admin":          "/resources",
	"/api/v1/content/public": "/not-an-id",
	"/api/v1/file/public":    "/not-an-id",
}

func TestEveryAliasRowOfTheReferenceApplicationLeadsSomewhereThatAnswers(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	dial := func(method, at string) (int, string, string) {
		t.Helper()
		req, err := http.NewRequest(method, "http://"+cfg.Server.Addr+at, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = acmeHost
		// No redirect following: the alias's own hop is the thing being asserted about, and
		// a client that followed it would see only the destination's answer.
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, at, err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header.Get("Location"), string(body)
	}

	// The table is live before anything else is asserted: /admin, the general row's own
	// subject, is shown answering a redirect rather than a refusal.
	if code, to, body := dial(http.MethodGet, "/admin"); code != http.StatusFound {
		t.Fatalf("GET /admin = %d %q %s; this case is about a table that redirects", code, to, trimHTML(body))
	}

	// Nothing mounted anywhere is what this says. A target that answers 405 (a read door the
	// case asked with the wrong verb), 303 (a door that wants a session first) or 200 is a
	// target that is mounted and serving.
	const unmounted = "nothing is served at this address"

	for _, row := range aliasRows(t) {
		old := row.from + aliasConcrete[row.from]
		code, to, body := dial(http.MethodGet, old)
		if code != http.StatusFound {
			t.Errorf("%s: GET %s = %d %q, want the table's redirect; the row reads {%q, %q}: %s",
				row.from, old, code, to, row.from, row.to, trimHTML(body))
			continue
		}
		if want := row.to + strings.TrimPrefix(old, row.from); to != want {
			t.Errorf("%s: the table sends this address to %q and the running server sends it to %q; the case reads the "+
				"table and the server answers to it, so one of the two has moved", row.from, want, to)
		}
		dest, destBody := "", ""
		if destCode, _, b := dial(http.MethodGet, to); destCode == http.StatusNotFound && strings.Contains(b, unmounted) {
			dest, destBody = to, b
		} else {
			continue
		}
		t.Errorf("the bookmark of %s is redirected to %s, which answers 404 %q: a redirect into a 404 is what "+
			"aliases.go names as worse than the 404 the person would have been given, because it makes it look as "+
			"though the installation broke rather than as though the link aged. Either this composition mounts that "+
			"door or the row does not belong in the table it is written to answer for it.", old, dest, trimHTML(destBody))
	}
}
