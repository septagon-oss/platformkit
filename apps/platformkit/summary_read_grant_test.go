package main

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// summary_read_grant_test.go asks the counting door as three people of one tenant,
// in the order that would leak if the cache key were trusted instead of the read
// guard: the administrator's read stores the tenant's task count first, and the
// people asking after it hold less.

// listedEntries is the set of list-bearing catalogue entries this caller is
// given, which is the set of cards the summary may answer for them.
func listedEntries(t *testing.T, cfg config.Config, as *http.Client) []string {
	t.Helper()
	code, body := do(t, cfg, as, http.MethodGet, acmeHost, "/api/v1/app/resources", "")
	if code != http.StatusOK {
		t.Fatalf("GET the catalog = %d %s", code, body)
	}
	var document struct {
		Resources []catalogueEntry `json:"resources"`
	}
	if err := json.Unmarshal([]byte(body), &document); err != nil {
		t.Fatalf("the catalog body is not the document: %v\n%s", err, body)
	}
	out := []string{}
	for _, e := range document.Resources {
		if e.Operations == nil || slices.Contains(e.Operations, "list") {
			out = append(out, e.Module+"/"+e.Entity)
		}
	}
	slices.Sort(out)
	return out
}

// summarised is the set of entries one summary names, in the same spelling.
func summarised(t *testing.T, cfg config.Config, as *http.Client) ([]string, map[string]*int64) {
	t.Helper()
	sum := read(t, cfg, as, acmeHost)
	names, totals := []string{}, map[string]*int64{}
	for _, e := range sum.Entries {
		names = append(names, e.Module+"/"+e.Entity)
		totals[e.Module+"/"+e.Entity] = e.Total
	}
	slices.Sort(names)
	return names, totals
}

// TestASummaryCountIsNamedOnlyToAPersonWhoMayReadIt primes the tenant's stored
// task count as the administrator, then asks as a member who holds no grant and
// as an observer who holds task:read. The member is named no task card and no
// card their own catalogue does not give them; the observer is named the count
// the administrator was.
func TestASummaryCountIsNamedOnlyToAPersonWhoMayReadIt(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	createTask(t, cfg, admin, acmeHost, "counted once", "")
	createTask(t, cfg, admin, acmeHost, "counted twice", "")
	_, primed := summarised(t, cfg, admin)
	stored := answered(t, primed["task/task"], "administrator's task total")
	if stored != 2 {
		t.Fatalf("two tasks answer the administrator a total of %d, want 2", stored)
	}

	acme := acmeTenant(t, cfg)
	people := map[string]*http.Client{}
	for _, role := range []string{authcontracts.RoleMember, "observer"} {
		email := role + ".summary@acme.localhost"
		provisionAs(t, cfg, c, acme, email, role)
		people[role] = signIn(t, cfg, acmeHost, email, adminPass)
	}

	member := people[authcontracts.RoleMember]
	names, totals := summarised(t, cfg, member)
	if total, named := totals["task/task"]; named {
		answer := "no number"
		if total != nil {
			answer = strconv.FormatInt(*total, 10)
		}
		t.Errorf("a member holding no task:read is named the task card, with %s", answer)
	}
	if want := listedEntries(t, cfg, member); !slices.Equal(names, want) {
		t.Errorf("the member's summary names %v and the member's catalogue lists %v", names, want)
	}

	observer := people["observer"]
	_, totals = summarised(t, cfg, observer)
	if got := answered(t, totals["task/task"], "observer's task total"); got != stored {
		t.Errorf("an observer holding task:read is answered %d tasks, want the %d the administrator was", got, stored)
	}
}
