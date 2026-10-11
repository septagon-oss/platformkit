package page

// A reviewer's case, T-0111 review round 3 — the fault catalogue's second gate under
// mutation.
//
// `catalogue_test.go` says of itself: "the only thing hand-written is the status set —
// which is hand-written in faultKey too, and a fourth status added there without a
// sentence in the catalogue is what this test is for". It is not. That test builds the
// keys it looks for out of `faultKeys` plus three statuses it names itself, so the day
// somebody widens `faultKey`'s switch the new status is invisible to it from both
// directions: it is not in the raised list, and it is not in the file either, so
// neither half has anything to complain about. The mutation that shows it — adding
// `http.StatusServiceUnavailable` to that one `case` line, with no copy anywhere —
// leaves `go test ./ui/page` green.
//
// That is not academic while `kit/httpx` refuses with 503: `authorize.go` answers a
// navigating caller whose authorizer or plan lookup failed with
// `"authorization is temporarily unavailable"`, which carries no `": "`, so it is
// keyed by its verdict and not by a code, and the verdict is 503. Such a page is
// negotiated like any other — a tenant served in Portuguese is served `lang="pt-PT"` —
// and says the kernel's English sentence under it.
//
// So this file asks `faultKey` the question instead of a list: for every status code a
// response can carry, does this package key a sentence for it, and is that sentence in
// the catalogue the deployment ships? Nothing here is hand-written, which is what makes
// it the gate the comment describes: a status added to `faultKey` is checked the moment
// it is added, and a status nobody added cannot fail this file.

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"testing"
)

// statusFaultKeys is every key faultKey derives from a verdict alone — the ones no
// published refusal code covers, and the only ones a new status can appear under.
func statusFaultKeys(t *testing.T) []string {
	t.Helper()
	var keys []string
	for code := 100; code < 600; code++ {
		key, lookup := faultKey("", code)
		if !lookup {
			continue
		}
		if key != "fault."+strconv.Itoa(code) {
			// A verdict keyed by something other than its own number is a different
			// mechanism, and this file has no claim about it.
			t.Fatalf("faultKey answers %q for status %d, which is not the status spelled as a key: "+
				"this case reads the verdict-derived keys and would silently skip this one", key, code)
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		t.Fatal("no verdict is keyed by its status at all, which means this gate stopped looking")
	}
	return keys
}

// TestEveryVerdictKeyedByStatusIsAnsweredInTheCatalogueTheDeploymentShips.
func TestEveryVerdictKeyedByStatusIsAnsweredInTheCatalogueTheDeploymentShips(t *testing.T) {
	t.Parallel()
	body, err := catalogues.ReadFile("messages/pt-PT.json")
	if err != nil {
		t.Fatal(err)
	}
	var shipped map[string]struct {
		Translation string `json:"translation"`
	}
	if err := json.Unmarshal(body, &shipped); err != nil {
		t.Fatalf("messages/pt-PT.json is not gotext JSON: %v", err)
	}
	// The same three codes every case in this package speaks of, so the sentence this
	// file adds a check for is the sentence a person is actually shown.
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusInternalServerError} {
		key := "fault." + strconv.Itoa(status)
		if _, carried := shipped[key]; !carried {
			t.Fatalf("%q is no longer carried by messages/pt-PT.json, and every other case here assumes it is", key)
		}
	}
	for _, key := range statusFaultKeys(t) {
		if message, ok := shipped[key]; !ok || message.Translation == "" {
			t.Errorf("faultKey keys a refusal page's sentence for %q, which messages/pt-PT.json does not "+
				"carry, so a tenant served in Portuguese is shown the kernel's English sentence under a "+
				"page that declares pt-PT", key)
		}
	}
	// The other direction, so the catalogue cannot drift into copy nothing renders: a
	// `fault.<digits>` key nobody can ask for.
	carried := make([]string, 0, len(shipped))
	for key := range shipped {
		if len(key) > 6 && key[:6] == "fault." && allDigits(key[6:]) {
			carried = append(carried, key)
		}
	}
	for _, key := range carried {
		if !slices.Contains(statusFaultKeys(t), key) {
			t.Errorf("messages/pt-PT.json carries %q, which faultKey never asks for", key)
		}
	}
}

// allDigits is whether a key's tail is a status spelled out and nothing else.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
