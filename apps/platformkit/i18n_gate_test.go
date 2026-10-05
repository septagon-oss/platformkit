package main

// The pseudo-locale gate: every document this application serves, rendered in a
// locale that marks what a catalogue answered, and the share of the words a person
// reads that a translator can reach.
//
// How it works, in the order it happens:
//
//  1. The application is composed exactly as main.go composes it, with one change:
//     the copy its shells format with is wrapped in kit/locale/providers/pseudo. The
//     wrap is reversible, so the page a person would read is on the wire with a mark
//     around every sentence that came from a catalogue.
//  2. The routes are read off the running composition — every Mounted() route the
//     kernel declared as a document (`Page`), never a list written here, because a
//     list drifts and a measurement boundary must not.
//  3. Each one is requested with Accept: text/html and Accept-Language: pt-PT, which
//     asks two questions at once: did this string reach a key, and does the one
//     language this deployment ships answer that key.
//  4. ui/legible extracts what a person reads, exempts what is data by shape, and is
//     handed the values this file typed through the workspace API — a task's title and
//     a page's body are sentences somebody typed, and no catalogue can hold them, so
//     they belong in neither number and `MarkDatum` is the only rule that can say so.
//  5. The numbers are compared with testdata/i18n-coverage.json, which is both the
//     floor and the set: coverage may only rise, untranslated may only fall, and the
//     pages measured may not quietly change.
//
// Every string is then printed: `TEXT` for one that went around a catalogue and `DATA`
// for one the number declined to count, with the rule that declined it. Nothing the
// measurement chose not to count is left out of the report.
//
// It issues no write: every request is a GET. The rows it reads were seeded through
// the workspace API before the walk, which is the only thing here that touches data.

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/pseudo"
	"github.com/septagon-oss/platformkit/ui/legible"
)

// floorFile is the checked-in measurement: the floor, the page set, and the artefact
// decision 0075's i18n row reads. UPDATE_I18N_FLOOR=1 rewrites it, and the diff is
// the review — the same idiom as UPDATE_GOLDEN for the OpenAPI document.
const floorFile = "testdata/i18n-coverage.json"

// floorVersion refuses an artefact this build cannot read rather than guessing at it.
const floorVersion = 1

// coverage is one page's measurement as a pair of integers, the numerator and the
// denominator of what a person reads: no float is ever stored or compared, so 41/43 and
// 82/86 are the same coverage and 40/43 is a fall.
type coverage struct {
	Wrapped  int `json:"wrapped"`
	Readable int `json:"readable"`
}

// percent is only ever printed. Every decision above it uses the two integers.
func (c coverage) percent() string {
	if c.Readable == 0 {
		return "n/a"
	}
	return strconv.FormatFloat(100*float64(c.Wrapped)/float64(c.Readable), 'f', 1, 64) + "%"
}

// report is the whole measurement. `pages` is both the floor and the set of pages
// the gate measured; `untranslated` is the count of distinct keys some page asked a
// catalogue for, which the copy owners work through.
type report struct {
	Version      int                 `json:"version"`
	MeasuredUTC  string              `json:"measured_utc"`
	Pages        map[string]coverage `json:"pages"`
	Untranslated int                 `json:"untranslated"`
}

func TestThePseudoLocaleGate(t *testing.T) {
	wanted := readFloor(t)
	path, cfg := configure(t)
	install(t, path)

	installed := catalogues()
	rec := pseudo.NewRecorder()
	c := composeCopy(cfg, installed, pseudo.Wrap(installed, rec))
	var mounted []httpx.MountedRoute
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	// The composition's own catalog mount, wrapped to keep the *httpx.API it is
	// handed: Mounted() is where the set of documents comes from, and this hook is
	// the one place a test can reach the API the composition registered.
	own := options.WorkspaceCatalog
	options.WorkspaceCatalog = func(api *httpx.API) {
		own(api)
		mounted = api.Mounted()
	}
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	seeds := seed(t, cfg, admin)

	measured := map[string]coverage{}
	var skips, unreachable, declined []string
	say := func(page string, collected []legible.String) {
		measured[page] = count(collected)
		copy, data := legible.Report(page, collected)
		unreachable = append(unreachable, copy...)
		declined = append(declined, data...)
	}
	for _, route := range documentRoutes(mounted) {
		page := pageName(route)
		at, ok := instantiate(route.Path, seeds)
		if !ok {
			skips = append(skips, page+" — no row to ask for, and an id made up here "+
				"would measure a refusal page and call it the record")
			continue
		}
		client := admin
		if route.Auth == "public" {
			client = nil
		}
		rec.Begin(page)
		code, contentType, body := get(t, cfg, client, at)
		// What the composition declared is not what it answered. A route whose answer
		// is a value, or a refusal, takes no part in the number — and says why, so a
		// page disappearing from the set is a line in the report and not a silent win.
		if !strings.HasPrefix(contentType, "text/html") {
			rec.End()
			skips = append(skips, fmt.Sprintf("%s — answered %d %s, which is not a document",
				page, code, contentType))
			continue
		}
		if code >= http.StatusBadRequest {
			rec.End()
			skips = append(skips, fmt.Sprintf("%s — answered %d where a document was expected", page, code))
			continue
		}
		collected, err := legible.Scan(body, pseudo.Wrapped)
		if err != nil {
			t.Errorf("%s: %v", page, err)
			rec.End()
			continue
		}
		say(page, legible.MarkDatum(collected, seeds.typed))
		rec.End()
	}
	for _, fault := range refusalDocs(t, admin) {
		rec.Begin(fault.Page)
		code, contentType, body := get(t, cfg, fault.Client, fault.Path)
		if !strings.HasPrefix(contentType, "text/html") {
			rec.End()
			skips = append(skips, fmt.Sprintf("%s — answered %s", fault.Page, contentType))
			continue
		}
		collected, err := legible.Scan(body, pseudo.Wrapped)
		if err != nil {
			t.Errorf("%s: %v", fault.Page, err)
			rec.End()
			continue
		}
		say(fault.Page, legible.MarkDatum(collected, seeds.typed))
		if code < http.StatusBadRequest {
			t.Errorf("%s: %s answered %d, which is not a refusal", fault.Page, fault.Path, code)
		}
		rec.End()
	}

	now := report{Version: floorVersion, MeasuredUTC: time.Now().UTC().Format(time.RFC3339),
		Pages: measured, Untranslated: len(rec.Unanswered())}
	if os.Getenv("UPDATE_I18N_FLOOR") == "1" {
		writeFloor(t, now)
	}
	check(t, wanted, now, unreachable, declined, skips)
}

// check prints the number on every run, passing included, then refuses what fell.
// t.Log is not evidence: `make check` runs the suite through gotestsum, whose format
// drops the output of a passing test, so the report is printed and the target passes
// -v.
func check(t *testing.T, wanted report, got report, unreachable, declined, skips []string) {
	t.Helper()
	totals := func(pages map[string]coverage) coverage {
		var out coverage
		for _, c := range pages {
			out.Wrapped += c.Wrapped
			out.Readable += c.Readable
		}
		return out
	}
	// The overall number is the sum over pages, not the mean of ratios, so a page
	// with four strings cannot out-vote the gallery. Both numbers in the artefact are
	// counts of strings, never a share: 41/43 and 82/86 are the same coverage.
	want, have := totals(wanted.Pages), totals(got.Pages)
	fmt.Printf("i18n coverage %d/%d (%s) over %d documents; floor %d/%d (%s); untranslated keys %d\n",
		have.Wrapped, have.Readable, have.percent(), len(got.Pages),
		want.Wrapped, want.Readable, want.percent(), got.Untranslated)
	fmt.Printf("i18n coverage measured %s; %d pages skipped, %d strings not copy\n",
		got.MeasuredUTC, len(skips), len(declined))
	// Every page's own two integers, on every run, passing included: the aggregate is a
	// number a change can hide inside, and the reviewer of a regeneration has to be
	// able to read which page moved without re-running the walk with a flag on.
	for _, page := range slices.Sorted(maps.Keys(got.Pages)) {
		measured, floor := got.Pages[page], wanted.Pages[page]
		fmt.Printf("i18n coverage per page %s %d/%d (%s); floor %d/%d (%s)\n",
			page, measured.Wrapped, measured.Readable, measured.percent(),
			floor.Wrapped, floor.Readable, floor.percent())
	}

	if wanted.Version != floorVersion && os.Getenv("UPDATE_I18N_FLOOR") != "1" {
		t.Errorf("%s records version %d and this build reads %d; regenerate it rather than guessing at a shape",
			floorFile, wanted.Version, floorVersion)
	}
	var added, gone []string
	for page := range got.Pages {
		if _, ok := wanted.Pages[page]; !ok {
			added = append(added, page)
		}
	}
	for page := range wanted.Pages {
		if _, ok := got.Pages[page]; !ok {
			gone = append(gone, page)
		}
	}
	if len(added) > 0 || len(gone) > 0 {
		sort.Strings(added)
		sort.Strings(gone)
		t.Errorf("%s names a different set of pages than this run measured: %s added, %s disappeared. "+
			"The set is the measurement boundary, so a change to it is a regeneration and a review, not a "+
			"silence. UPDATE_I18N_FLOOR=1 rewrites it.\nadded: %v\ndisappeared: %v",
			floorFile, plural(len(added), "page"), plural(len(gone), "page"), added, gone)
	}
	for page, have := range got.Pages {
		if have.Readable == 0 {
			t.Errorf("%s answered a document from which no string was read: a served page with an "+
				"empty denominator is a broken scan or the wrong document, never 100%% coverage", page)
		}
	}
	for page, floor := range wanted.Pages {
		have, ok := got.Pages[page]
		if !ok {
			continue
		}
		// Cross-multiplied: same coverage or better passes (41/43 measured at 82/86),
		// one string lost fails (41/43 measured at 40/43).
		if int64(have.Wrapped)*int64(floor.Readable) < int64(floor.Wrapped)*int64(have.Readable) {
			t.Errorf("%s covers %s of its copy and the floor recorded %s: coverage may only rise. "+
				"The fix is the one that names the words — wrap the string in Text and give the key to the "+
				"catalogue that owns it — not a regenerated floor inside the change that lowered it.",
				page, ratio(have), ratio(floor))
		}
	}
	// The whole application, on the same cross-multiplication and for the same reason.
	// Each page's own ratio cannot see this one: copy added to a page already at zero
	// leaves that page where it was while lowering the share of the words a person
	// reads that a translator can reach — and the pages that sit at zero today are
	// exactly where an untranslated string is cheapest to add.
	if int64(have.Wrapped)*int64(want.Readable) < int64(want.Wrapped)*int64(have.Readable) {
		t.Errorf("this tree covers %s of the copy a person reads across %d documents and the floor recorded %s: "+
			"coverage may only rise. Every page holding its own ratio is no defence — the total is the "+
			"number a reader of this application gets, and it fell.",
			ratio(have), len(got.Pages), ratio(want))
	}
	if got.Untranslated > wanted.Untranslated {
		t.Errorf("this tree asks %d keys no catalogue answers and the floor recorded %d: untranslated copy "+
			"may only fall. The list of keys, with the page each was asked on, is printed below.",
			got.Untranslated, wanted.Untranslated)
	}
	for _, skip := range skips {
		fmt.Println("SKIP", skip)
	}
	for _, line := range declined {
		fmt.Println(line)
	}
	for _, line := range unreachable {
		fmt.Println(line)
	}
}

// documentRoutes is the set the gate renders: every route the composition declared
// as a document, answered with GET, and not a wildcard tree — an asset tree is bytes,
// not a page. POST documents are left out because every one of them either redirects
// or re-renders the form its GET route already rendered, and reaching them would mean
// this gate wrote, which it never does.
func documentRoutes(mounted []httpx.MountedRoute) []httpx.MountedRoute {
	var out []httpx.MountedRoute
	for _, route := range mounted {
		if !route.Page || route.Method != http.MethodGet || strings.Contains(route.Path, "*") {
			continue
		}
		out = append(out, route)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Method+" "+out[i].Path < out[j].Method+" "+out[j].Path
	})
	return out
}

// pageName names one route the way the artefact names it: `METHOD path`, with the
// path pattern kept, so two runs over different rows agree on the key.
func pageName(route httpx.MountedRoute) string { return route.Method + " " + route.Path }

// seeds are the rows the walk needs, created through the workspace API. An id this
// file invented would answer a refusal page and be counted as the record screen.
type seeds struct {
	ids   map[string]string
	slug  string
	typed []string
}

// What seed types into each field, named once so the request body and the list of
// values handed to legible.MarkDatum cannot drift apart. Every one of these is a
// sentence a person typed into a form: a task's title, a page's title and body, a
// plan's name. They are data, they appear on the record screens the walk renders, and
// no catalogue will ever hold any of them — so they belong in neither number, and the
// report says which of them it declined to count, and why.
const (
	seededTaskTitle = "Pump room inspection"
	seededPlanName  = "Pro, billed monthly"
	seededPageSlug  = "about-us"
	seededPageTitle = "About us"
	seededPageBody  = "We fix chillers."
)

// instantiate fills one path pattern from a row of the resource it names. `{slug}` is
// the public site's one published page.
func instantiate(pattern string, seeds seeds) (string, bool) {
	if !strings.Contains(pattern, "{") {
		return pattern, true
	}
	if strings.Contains(pattern, "{slug}") {
		if seeds.slug == "" {
			return "", false
		}
		return strings.Replace(pattern, "{slug}", seeds.slug, 1), true
	}
	for prefix, id := range seeds.ids {
		if strings.HasPrefix(pattern, prefix) && id != "" {
			return strings.Replace(pattern, "{id}", id, 1), true
		}
	}
	return "", false
}

// refusalDoc is one refusal document and the request that gets that verdict.
type refusalDoc struct {
	Page   string
	Path   string
	Client *http.Client
}

// refusalDocs are the documents no route answers: the refusal the kernel renders for
// a verdict a person meets. They are induced, each by a request that gets that verdict
// and nothing else.
//
// Three verdicts are deliberately absent. No 403 is inducible from outside: the admin
// shell answers an unauthorised GET of a screen with the shell itself, HTTP 200, and
// shows its own "Permission denied" banner in the browser, so the sentences exist on a
// served page (and are counted there) but no address answers 403. The 500 and the 503
// are reachable only through an in-process shell, and
// ui/page/fault_outage_language_test.go renders those and asserts the same boolean —
// that every sentence on them came from a catalogue — without pretending a request
// reached them.
func refusalDocs(t *testing.T, admin *http.Client) []refusalDoc {
	t.Helper()
	return []refusalDoc{
		{Page: "FAULT 404", Path: "/app/no-such-page", Client: admin},
		// The ask door answers POST and the confirmation answers GET, so a GET of the
		// door itself is the one method this composition refuses as 405.
		{Page: "FAULT 405", Path: "/app/access-request", Client: admin},
	}
}

// get asks for one document the way a browser does: a document, in the one language
// this deployment ships copy for.
func get(t *testing.T, cfg config.Config, client *http.Client, at string) (int, string, []byte) {
	t.Helper()
	if client == nil {
		client = &http.Client{}
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+cfg.Server.Addr+at, nil)
	if err != nil {
		t.Fatalf("request %s: %v", at, err)
	}
	req.Host = acmeHost
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "pt-PT")
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", at, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header.Get("Content-Type"), body
}

// count is the page's two integers: the denominator is what a person reads that is copy
// — an exempt string is data, in neither number — and the numerator is the part of that
// a catalogue reached. Exempt strings are never hidden: the run prints the split.
func count(collected []legible.String) coverage {
	wrapped := len(collected) - len(legible.Violations(collected)) - len(legible.Exempted(collected))
	return coverage{Wrapped: wrapped, Readable: wrapped + len(legible.Violations(collected))}
}

// seed creates the rows the record screens need: one per resource that has a record
// screen, plus one published page for the site's `/{slug}`. It writes through the
// same doors a person does, before the walk, and nothing after it.
func seed(t *testing.T, cfg config.Config, admin *http.Client) seeds {
	t.Helper()
	out := seeds{ids: map[string]string{}}
	created := func(path, body string) string {
		code, answer := do(t, cfg, admin, http.MethodPost, acmeHost, path, body)
		if code != http.StatusCreated {
			t.Logf("seed POST %s = %d %s; the pages behind it are reported as skipped", path, code, answer)
			return ""
		}
		return field(t, answer, "id")
	}
	out.ids["/app/task/tasks/"] = created(tasksPath, `{"title":"`+seededTaskTitle+`","priority":"high"}`)
	// The plan is written where the composition says a plan is written — the ops door —
	// and read back through the tenant's own, which is the page a person is shown.
	out.ids["/app/billing/plans/"] = created("/api/v1/ops"+strings.TrimPrefix(plansPath, "/api/v1"),
		`{"code":"pro-monthly","name":"`+seededPlanName+`","priceCents":2900,"currency":"EUR","active":true}`)
	content := created(contentPath,
		`{"slug":"`+seededPageSlug+`","title":"`+seededPageTitle+`","body":"`+seededPageBody+`","kind":"page"}`)
	out.ids["/app/content/contents/"] = content
	if content != "" {
		// The public site answers a page only once it is published, and publishing is
		// the module's own verb door.
		if code, answer := do(t, cfg, admin, http.MethodPost, acmeHost,
			contentPath+"/"+content+"/publish", `{}`); code != http.StatusOK {
			t.Logf("publish = %d %s", code, answer)
		} else {
			out.slug = seededPageSlug
		}
	}
	// The bootstrap created one person, and that person is the row the user record
	// screen needs.
	if code, answer := do(t, cfg, admin, http.MethodGet, acmeHost, usersPath, ""); code == http.StatusOK {
		var list struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		if json.Unmarshal([]byte(answer), &list) == nil && len(list.Items) > 0 {
			out.ids["/app/user/users/"] = list.Items[0].ID
		}
	}
	out.typed = []string{seededTaskTitle, seededPlanName, seededPageSlug, seededPageTitle, seededPageBody}
	return out
}

func readFloor(t *testing.T) report {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(".", floorFile))
	if err != nil {
		if os.Getenv("UPDATE_I18N_FLOOR") == "1" {
			// The first run has no floor to compare with, which is the brief's own
			// first step: it records the current number and every later change to
			// lower it fails against it.
			return report{Version: floorVersion, Pages: map[string]coverage{}}
		}
		t.Fatalf("read the i18n floor: %v — the first run writes it with UPDATE_I18N_FLOOR=1", err)
	}
	var out report
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("%s is not the artefact this gate reads: %v", floorFile, err)
	}
	if out.Pages == nil {
		t.Fatalf("%s carries no pages: the floor and the set are the same map", floorFile)
	}
	return out
}

func writeFloor(t *testing.T, got report) {
	t.Helper()
	body, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("encode the floor: %v", err)
	}
	if err := os.WriteFile(floorFile, append(body, '\n'), 0o644); err != nil {
		t.Fatalf("write the floor: %v", err)
	}
	t.Logf("rewrote %s at this run's measurement", floorFile)
}

func ratio(c coverage) string {
	return c.percent() + " (" + strconv.Itoa(c.Wrapped) + "/" + strconv.Itoa(c.Readable) + ")"
}

func plural(n int, word string) string {
	if n == 1 {
		return strconv.Itoa(n) + " " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

func quote(text string) string { return "\"" + strings.Join(strings.Fields(text), " ") + "\"" }
