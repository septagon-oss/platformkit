// Package exampletest checks that documented examples execute in Go's runner.
package exampletest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Execute reruns only examples in this test binary and requires one RUN/PASS
// pair per name. The child inherits the environment and TestMain, and is waited
// on for at most fifteen minutes, within the parent's deadline. Forced exit
// fails the test and may leave TestMain's database for manual cleanup.
func Execute(t *testing.T, names ...string) {
	t.Helper()
	limit := 15 * time.Minute
	if deadline, ok := t.Deadline(); ok {
		limit = min(limit, time.Until(deadline)-10*time.Second)
	}
	if limit <= 0 {
		t.Fatal("not enough test time remains to execute examples")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), limit)
	defer cancel()
	child := exec.CommandContext(ctx, binary, "-test.run=^Example", "-test.v=true", "-test.count=1", "-test.timeout="+limit.String())
	child.WaitDelay = time.Second
	output, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("example child failed: %v (context: %v); forced exit may leave its fixture database; output: %q", err, ctx.Err(), output)
	}
	if err := checkFrames(string(output), names); err != nil {
		t.Fatalf("examples did not execute: %v; output: %q", err, output)
	}
}

func checkFrames(output string, names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("at least one example name is required")
	}
	states := make(map[string]int, len(names))
	for _, name := range names {
		if !strings.HasPrefix(name, "Example") {
			return fmt.Errorf("invalid example name %q", name)
		}
		if _, exists := states[name]; exists {
			return fmt.Errorf("duplicate required example %s", name)
		}
		states[name] = 0
	}
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		state, required := states[fields[2]]
		if !required {
			continue
		}
		switch {
		case fields[0] == "===" && fields[1] == "RUN" && len(fields) == 3 && state == 0:
			states[fields[2]] = 1
		case fields[0] == "---" && fields[1] == "PASS:" && len(fields) == 4 && state == 1:
			if _, err := time.ParseDuration(strings.TrimSuffix(strings.TrimPrefix(fields[3], "("), ")")); err != nil || !strings.HasPrefix(fields[3], "(") || !strings.HasSuffix(fields[3], ")") {
				return fmt.Errorf("malformed completion: %q", line)
			}
			states[fields[2]] = 2
		default:
			return fmt.Errorf("unexpected example frame: %q", line)
		}
	}
	for _, name := range names {
		if states[name] != 2 {
			return fmt.Errorf("%s has no complete RUN/PASS pair", name)
		}
	}
	return nil
}
