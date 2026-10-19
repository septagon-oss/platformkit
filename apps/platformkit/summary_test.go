package main

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// summary_test.go asks the workspace's counting door the four questions it is
// built to answer: what one tenant holds, what is due today, what one tenant
// cannot be shown, and what a caller with nothing to show looks like.

const summaryPath = "/api/v1/app/summary"

// started brings the reference composition up and returns the two signed-in
// administrators, one per tenant, plus the config every request is asked with.
func started(t *testing.T) (config.Config, *http.Client) {
	t.Helper()
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))
	return cfg, signIn(t, cfg, acmeHost, adminEmail, adminPass)
}

// read is the document, asked of one host by one caller.
func read(t *testing.T, cfg config.Config, client *http.Client, host string) screens.Summary {
	t.Helper()
	code, body := do(t, cfg, client, http.MethodGet, host, summaryPath, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s at %s = %d %s", summaryPath, host, code, body)
	}
	var out screens.Summary
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("the summary body is not the document: %v\n%s", err, body)
	}
	return out
}

// taskEntry is the task entry of one document, and the whole body if it is
// missing: every case here counts tasks, and a body with no task card answered
// nothing about the question.
func taskEntry(t *testing.T, sum screens.Summary) screens.SummaryEntry {
	t.Helper()
	for _, e := range sum.Entries {
		if e.Module == "task" && e.Entity == "task" {
			return e
		}
	}
	t.Fatalf("the summary names no task card among %d entries", len(sum.Entries))
	return screens.SummaryEntry{}
}

func answered(t *testing.T, n *int64, what string) int64 {
	t.Helper()
	if n == nil {
		t.Fatalf("the %s was not answered at all, which is not a number and not a zero", what)
	}
	return *n
}

// createTask opens one task at host, due at the instant named (or never, for "").
func createTask(t *testing.T, cfg config.Config, client *http.Client, host, title, dueAt string) {
	t.Helper()
	body := `{"title":` + strconv.Quote(title) + `}`
	if dueAt != "" {
		body = `{"title":` + strconv.Quote(title) + `,"dueAt":"` + dueAt + `"}`
	}
	if code, out := do(t, cfg, client, http.MethodPost, host, tasksPath, body); code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s", tasksPath, code, out)
	}
}

// askAt is the document read with an Accept-Language, which `do` never sets: the
// case below is about the one header that could change these bytes.
func askAt(t *testing.T, cfg config.Config, client *http.Client, host, language string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"http://"+cfg.Server.Addr+summaryPath, nil)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}
	req.Host = host
	req.Header.Set("Accept-Language", language)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", summaryPath, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the body: %v", err)
	}
	return resp.StatusCode, string(body)
}

// TestTheSummaryCountsTheTasksDueTodayAndNothingElse asks the one question the
// card wears: the total is every task the list would page through, and the
// highlight is the declared window alone — today's UTC day, and nothing that
// fell in it yesterday or falls in it tomorrow.
func TestTheSummaryCountsTheTasksDueTodayAndNothingElse(t *testing.T) {
	cfg, acme := started(t)
	// Noon, twice: an instant that is in today no matter when this test ran, and
	// one that is in it no matter what zone the server believes it is in.
	day := time.Now().UTC().Truncate(24 * time.Hour)
	createTask(t, cfg, acme, acmeHost, "due today", day.Add(12*time.Hour).Format(time.RFC3339))
	createTask(t, cfg, acme, acmeHost, "due tomorrow", day.Add(36*time.Hour).Format(time.RFC3339))
	createTask(t, cfg, acme, acmeHost, "never due", "")

	// One read, because a count the installation answered is believed for a few
	// seconds and a second request would be asking the cache, not the database.
	after := taskEntry(t, read(t, cfg, acme, acmeHost))
	if got := answered(t, after.Total, "total"); got != 3 {
		t.Errorf("three tasks answer a total of %d, want 3", got)
	}
	if got := answered(t, after.Highlight, "highlight"); got != 1 {
		t.Errorf("one task due today answers a highlight of %d, want 1 — a highlight equal to the total is a dropped filter, and a highlight counting tomorrow is a window off by a day", got)
	}

	// The trend is the document's promise, not the query's: fourteen days, oldest
	// first, and the last one is the day the request was answered in.
	trend := after.Trend
	if trend == nil {
		t.Fatalf("tasks declare a daily series and the body answers no trend at all")
	}
	if trend.Field != "createdAt" {
		t.Errorf("the series is of %q, want the declared createdAt", trend.Field)
	}
	if len(trend.Days) != screens.TrendDays {
		t.Fatalf("the series names %d days, want %d", len(trend.Days), screens.TrendDays)
	}
	if last := trend.Days[len(trend.Days)-1]; last.Day != day.Format("2006-01-02") {
		t.Errorf("the last day of the series is %s, want today %s", last.Day, day.Format("2006-01-02"))
	}
	if today := trend.Days[len(trend.Days)-1]; today.Count != 3 {
		t.Errorf("three tasks created today answer %d on today's bucket, want 3", today.Count)
	}
	if first := trend.Days[0]; first.Count != 0 {
		t.Errorf("the first day of an untouched window answers %d, want a zero on a named day", first.Count)
	}
}

// catalogueEntry is the pair the catalogue names an entry by, and whether it says
// the list route is there. `operations` is absent for a resource that hides no
// verb and empty for one that hides all five, so absence means "list" here and
// `[]` means "nothing".
type catalogueEntry struct {
	Module     string   `json:"module"`
	Entity     string   `json:"entity"`
	Operations []string `json:"operations,omitzero"`
}

// TestTheSummaryEntryIsTheCatalogEntry is the pair the two documents must keep:
// the same entries, decided by the same method, so no number exists for a card no
// shell was ever given and no card is drawn for which no number is offered.
func TestTheSummaryEntryIsTheCatalogEntry(t *testing.T) {
	cfg, acme := started(t)
	code, body := do(t, cfg, acme, http.MethodGet, acmeHost, "/api/v1/app/resources", "")
	if code != http.StatusOK {
		t.Fatalf("GET the catalog = %d %s", code, body)
	}
	var document struct {
		Resources []catalogueEntry `json:"resources"`
	}
	if err := json.Unmarshal([]byte(body), &document); err != nil {
		t.Fatalf("the catalog body is not the document: %v\n%s", err, body)
	}
	want := []string{}
	for _, e := range document.Resources {
		if e.Operations == nil || slices.Contains(e.Operations, "list") {
			want = append(want, e.Module+"/"+e.Entity)
		}
	}
	got := []string{}
	for _, e := range read(t, cfg, acme, acmeHost).Entries {
		got = append(got, e.Module+"/"+e.Entity)
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(want, got) {
		t.Errorf("the summary answers %v and the catalogue lists %v: the two documents disagree about which cards exist", got, want)
	}
}

// TestASummaryTotalCannotBeReadAcrossATenant writes at one tenant and asks the
// other, in the same database and the same table. There is no parameter with which
// to name a tenant, so the only way this fails is a query that escaped the scope
// the request's own transaction opened — and the same name asked at two hosts
// answering two different numbers is also what makes the cache key's tenant
// segment observable rather than assumed.
func TestASummaryTotalCannotBeReadAcrossATenant(t *testing.T) {
	cfg, acme := started(t)
	code, body := do(t, cfg, acme, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want the second tenant", tenantPath, code, body)
	}
	other := uuid.MustParse(field(t, body, "id"))
	provision(t, cfg, other, askGranterEmail)
	globex := signIn(t, cfg, globexHost, askGranterEmail, adminPass)

	createTask(t, cfg, acme, acmeHost, "acme one", "")
	createTask(t, cfg, acme, acmeHost, "acme two", "")
	for _, title := range []string{"globex one", "globex two", "globex three"} {
		createTask(t, cfg, globex, globexHost, title, "")
	}

	if got := answered(t, taskEntry(t, read(t, cfg, acme, acmeHost)).Total, "acme's total"); got != 2 {
		t.Errorf("acme answers %d rows, want its own two and never globex's three", got)
	}
	if got := answered(t, taskEntry(t, read(t, cfg, globex, globexHost)).Total, "globex's total"); got != 3 {
		t.Errorf("globex answers %d rows, want its own three", got)
	}
}

// TestTheSummaryNamesNothingToACallerWithNoCredential keeps the door where
// /connection is not: the connection document answers an anonymous visitor
// because it says nothing about anybody, and this one says what a person holds.
func TestTheSummaryNamesNothingToACallerWithNoCredential(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))

	code, body := do(t, cfg, nil, http.MethodGet, acmeHost, summaryPath, "")
	if code == http.StatusOK {
		t.Fatalf("an anonymous caller is handed the workspace's counts: %s", body)
	}
	if code != http.StatusUnauthorized && code != http.StatusForbidden {
		t.Errorf("GET %s with no credential = %d, want a refusal to identify the caller", summaryPath, code)
	}
}

// TestTheSummaryBodyIsTheSameBytesInEveryLanguage is the case that protects the
// cache key's right to name no language: nothing in this document is a word, so
// nothing about who is asking changes a byte of it — and the same totals at the
// same instant are what one tenant's cache entry may answer every person with.
func TestTheSummaryBodyIsTheSameBytesInEveryLanguage(t *testing.T) {
	cfg, acme := started(t)
	createTask(t, cfg, acme, acmeHost, "one task", "")
	_, english := do(t, cfg, acme, http.MethodGet, acmeHost, summaryPath, "")
	code, portuguese := askAt(t, cfg, acme, acmeHost, "pt-PT")
	if code != http.StatusOK {
		t.Fatalf("GET %s in pt-PT = %d %s", summaryPath, code, portuguese)
	}
	if english == portuguese {
		return
	}
	// The two bodies may differ in asOf alone, which is a clock and not a word.
	without := func(s string) string {
		var body map[string]any
		if err := json.Unmarshal([]byte(s), &body); err != nil {
			t.Fatalf("the summary body is not the document: %v\n%s", err, s)
		}
		delete(body, "asOf")
		out, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("the summary body will not re-encode: %v", err)
		}
		return string(out)
	}
	if without(english) != without(portuguese) {
		t.Errorf("the summary is worded differently per language:\n en %s\n pt %s", without(english), without(portuguese))
	}
}
