package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, path, text string) {
	t.Helper()
	name := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, r repository, args ...string) string {
	t.Helper()
	out, err := r.git(nil, args...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, r repository, message string) string {
	t.Helper()
	git(t, r, "add", "-A")
	git(t, r, "-c", "core.hooksPath=/dev/null", "commit", "-q", "--allow-empty", "-m", message)
	return git(t, r, "rev-parse", "HEAD")
}

func putBudget(t *testing.T, r repository, n int64) {
	t.Helper()
	b := zeroBudget()
	b["funcs_over_100"] = n
	encoded, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, r.root, budgetFile, string(encoded)+"\n")
}

func fixture(t *testing.T, n int64) (repository, string) {
	t.Helper()
	r := repository{t.TempDir()}
	git(t, r, "init", "-q", "-b", "main")
	git(t, r, "config", "user.name", "Complexity test")
	git(t, r, "config", "user.email", "complexity@example.invalid")
	git(t, r, "config", "commit.gpgsign", "false")
	writeFile(t, r.root, "go.mod", "module example.test/fixture\n\ngo 1.26.6\n")
	writeFile(t, r.root, "kit/httpx/doc.go", "package httpx\n")
	if n >= 0 {
		putBudget(t, r, n)
	}
	return r, commit(t, r, "base")
}

func invoke(t *testing.T, r repository, want int, args ...string) string {
	t.Helper()
	var out, stderr bytes.Buffer
	code := run(append([]string{"--root", r.root}, args...), &out, &stderr)
	text := out.String() + stderr.String()
	if code != want {
		t.Fatalf("%v: exit %d want %d\n%s", args, code, want, text)
	}
	if code != 0 && strings.Contains(text, "complexity OK") {
		t.Fatalf("false success: %s", text)
	}
	return text
}

func historyResult(t *testing.T, r repository, base string, want int) string {
	t.Helper()
	return invoke(t, r, want, "--history-only", "--base", base)
}

const approved = "build(complexity): price growth\n\nOwner-approved: fixture owner"
