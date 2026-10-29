package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repositoryRoot resolves the foundation whose committed graph this case reads.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// TestEveryCommittedContractBaselineRowIsCarriedByTheRealGraph reads the graph Go
// reads in this repository and requires each row of scripts/imports-contract-baseline.txt
// to be carried by a production contract import that exists today. A row nothing
// carries is refused by the gate as contract-baseline-unused, so this is the same
// question the gate asks of the committed file, answered against the real tree
// rather than a fixture: the frozen surface the rows name is still the surface the
// contracts packages import. No count is asserted, so a repository that moves one
// edge out or records another keeps passing as long as its own rows are true.
func TestEveryCommittedContractBaselineRowIsCarriedByTheRealGraph(t *testing.T) {
	root := repositoryRoot(t)
	t.Setenv("GOWORK", "off")
	data, err := os.ReadFile(filepath.Join(root, "scripts", "imports-contract-baseline.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{root}, &stdout, &stderr, runGo); code != 0 {
		t.Fatalf("the committed tree returned %d\nstdout: %s\nstderr: %s", code, &stdout, &stderr)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || strings.HasPrefix(line, "#") {
			continue
		}
		want := "contract baseline " + fields[0] + " -> " + fields[1] + "; carried by production source"
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("row %s -> %s is not carried by production source in this build context\nstderr: %s", fields[0], fields[1], &stderr)
		}
	}
	if !strings.Contains(stdout.String(), "satisfy the module boundaries") {
		t.Errorf("stdout = %s", &stdout)
	}
}
