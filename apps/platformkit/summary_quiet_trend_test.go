package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

// summary_quiet_trend_test.go reads the task card's daily series for a desk on
// which nothing was created in the window. A quiet fortnight is fourteen named
// zeros; a series that is not there at all is the absence the document keeps for
// a count nobody could answer.

// taskCard is the task entry of one summary body, key by key as it was sent.
func taskCard(t *testing.T, body string) map[string]json.RawMessage {
	t.Helper()
	var document struct {
		Entries []map[string]json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal([]byte(body), &document); err != nil {
		t.Fatalf("the summary body is not the document: %v\n%s", err, body)
	}
	for _, e := range document.Entries {
		if string(e["module"]) == `"task"` && string(e["entity"]) == `"task"` {
			return e
		}
	}
	t.Fatalf("the summary names no task card: %s", body)
	return nil
}

func TestAQuietFortnightIsAFlatTrendAndNotAnAbsentOne(t *testing.T) {
	cfg, acme := started(t)
	// Twice: the first answer is read from the database, the second from what the
	// first one stored, and a shell is handed either.
	for _, read := range []string{"counted", "stored"} {
		code, body := do(t, cfg, acme, http.MethodGet, acmeHost, summaryPath, "")
		if code != http.StatusOK {
			t.Fatalf("GET %s (%s) = %d %s", summaryPath, read, code, body)
		}
		task := taskCard(t, body)
		raw, ok := task["trend"]
		if !ok {
			t.Errorf("the %s answer names no trend for tasks, which declare createdAt; a window with no rows in it is fourteen zeros, not unanswered: %s", read, body)
			continue
		}
		var trend struct {
			Field string `json:"field"`
			Days  []struct {
				Day   string `json:"day"`
				Count *int64 `json:"count"`
			} `json:"days"`
		}
		if err := json.Unmarshal(raw, &trend); err != nil {
			t.Fatalf("the %s trend is not a series: %v\n%s", read, err, raw)
		}
		if trend.Field != "createdAt" || len(trend.Days) != 14 {
			t.Errorf("the %s trend is %q over %d days, want createdAt over 14", read, trend.Field, len(trend.Days))
		}
		for _, day := range trend.Days {
			if day.Count == nil || *day.Count != 0 {
				t.Errorf("the %s trend answers %s as %v, want a printed 0", read, day.Day, day.Count)
			}
		}
	}
}
