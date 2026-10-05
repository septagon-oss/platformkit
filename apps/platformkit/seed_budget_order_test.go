package main

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestSeedCaseHasItsBudgetBeforeItLands(t *testing.T) {
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = "../.."
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	// Measure the commit that added this branch's case, rather than pinning a
	// whole-tree count against an unrelated change that may land later.
	feature := git("log", "-1", "--format=%H", "--", "apps/platformkit/seed_task_natural_key_refusal_test.go")
	if feature == "" {
		t.Fatal("the seed's natural-key case has no committed source")
	}
	parent := feature + "^"
	var budget struct {
		Buckets []struct {
			Name string `json:"name"`
			Max  int    `json:"max"`
		} `json:"buckets"`
	}
	if err := json.Unmarshal([]byte(git("show", parent+":loc-budget.json")), &budget); err != nil {
		t.Fatal(err)
	}
	limit := -1
	for _, bucket := range budget.Buckets {
		if bucket.Name == "go_test" {
			limit = bucket.Max
		}
	}
	if limit < 0 {
		t.Fatal("the preceding tree declares no go_test budget")
	}
	before := 0
	for line := range strings.SplitSeq(git("grep", "-c", "^", parent, "--", "*_test.go"), "\n") {
		at := strings.LastIndexByte(line, ':')
		if at < 0 {
			t.Fatalf("unreadable committed line count: %q", line)
		}
		count, err := strconv.Atoi(line[at+1:])
		if err != nil {
			t.Fatal(err)
		}
		before += count
	}
	delta := 0
	for line := range strings.SplitSeq(git("diff", "--numstat", parent, feature, "--", "*_test.go"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			t.Fatalf("unreadable source delta: %q", line)
		}
		added, addErr := strconv.Atoi(fields[0])
		removed, removeErr := strconv.Atoi(fields[1])
		if addErr != nil || removeErr != nil {
			t.Fatalf("non-text source delta: %q", line)
		}
		delta += added - removed
	}
	if before+delta > limit {
		t.Errorf("seed source commit %.7s adds %d test lines with %d already committed and a preceding ceiling of %d; price the budget before this source commit",
			feature, delta, before, limit)
	}
}
