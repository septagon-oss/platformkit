package screens

import (
	"encoding/json"
	"strings"
	"testing"
)

// summary_absence_test.go holds the document to the one distinction a shell draws
// from it: a count the installation answered as zero is printed, and a count
// nobody answered is not there at all.

func TestAnUnansweredCountIsAbsentAndAZeroCountIsPrinted(t *testing.T) {
	zero := int64(0)
	answeredZero, err := json.Marshal(SummaryEntry{Module: "task", Entity: "task", Total: &zero, Highlight: &zero})
	if err != nil {
		t.Fatalf("encode an answered entry: %v", err)
	}
	for _, key := range []string{`"total":0`, `"highlight":0`} {
		if !strings.Contains(string(answeredZero), key) {
			t.Errorf("an entry answered with zero encodes as %s, want %s in it", answeredZero, key)
		}
	}
	unanswered, err := json.Marshal(SummaryEntry{Module: "task", Entity: "task"})
	if err != nil {
		t.Fatalf("encode an unanswered entry: %v", err)
	}
	for _, key := range []string{`"total"`, `"highlight"`, `"trend"`} {
		if strings.Contains(string(unanswered), key) {
			t.Errorf("an entry nobody answered encodes as %s, which names %s", unanswered, key)
		}
	}
}
