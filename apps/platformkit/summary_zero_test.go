package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

// summary_zero_test.go reads the counting door's bytes for a tenant that holds no
// task yet: an empty desk is an answer, and the card prints it.

func TestAnEmptyTaskDeskIsCountedAsZeroAndNotLeftUnanswered(t *testing.T) {
	cfg, acme := started(t)
	code, body := do(t, cfg, acme, http.MethodGet, acmeHost, summaryPath, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", summaryPath, code, body)
	}
	var document struct {
		Entries []map[string]json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal([]byte(body), &document); err != nil {
		t.Fatalf("the summary body is not the document: %v\n%s", err, body)
	}
	var task map[string]json.RawMessage
	for _, e := range document.Entries {
		if string(e["module"]) == `"task"` && string(e["entity"]) == `"task"` {
			task = e
		}
	}
	if task == nil {
		t.Fatalf("the summary names no task card: %s", body)
	}
	for _, key := range []string{"total", "highlight"} {
		if got, ok := task[key]; !ok || string(got) != "0" {
			t.Errorf("an empty desk answers %s as %q (present %v), want the number 0", key, got, ok)
		}
	}
}
