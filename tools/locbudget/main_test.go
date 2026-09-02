package main

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBucketCountsMatchingTrackedFiles(t *testing.T) {
	// "deleted/gone.go" is tracked but absent from the working tree, the state
	// git reports between a `rm` and its commit. Count must skip it, not fail.
	files := []string{"a/x.go", "a/x_test.go", "b/y.go", "docs/z.md", "deleted/gone.go"}
	contents := map[string]string{
		"a/x.go":      "package a\n// one\n// two\n",
		"a/x_test.go": "package a\n",
		"b/y.go":      "package b\nimport _ \"maragu.dev/gomponents\"\n",
		"docs/z.md":   "# z\n",
	}
	dir := t.TempDir()
	for _, f := range files {
		content, ok := contents[f]
		if !ok {
			continue
		}
		writeFile(t, dir, f, content)
	}
	b := Bucket{Name: "go_prod", Suffixes: []string{".go"}, ExcludeSuffixes: []string{"_test.go"}}
	got, err := b.Count(dir, files)
	if err != nil {
		t.Fatal(err)
	}
	if got != 5 {
		t.Fatalf("go_prod: want 5 lines, got %d", got)
	}
	ui := Bucket{Name: "module_ui", Suffixes: []string{".go"}, ExcludeSuffixes: []string{"_test.go"}, Contains: "maragu.dev/gomponents"}
	got, err = ui.Count(dir, files)
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("module_ui: want 2 lines, got %d", got)
	}
	under := Bucket{Name: "docs", Paths: []string{"docs/"}, Suffixes: []string{".md"}}
	got, err = under.Count(dir, files)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("docs: want 1 line, got %d", got)
	}
	// ExcludePaths drops b/y.go, leaving only a/x.go's 3 lines.
	excluded := Bucket{Name: "go_not_b", Suffixes: []string{".go"}, ExcludeSuffixes: []string{"_test.go"}, ExcludePaths: []string{"b/"}}
	got, err = excluded.Count(dir, files)
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Fatalf("go_not_b: want 3 lines, got %d", got)
	}
	// A bucket matching only the tracked-but-absent file counts zero, no error.
	absent := Bucket{Name: "absent", Paths: []string{"deleted/"}, Suffixes: []string{".go"}}
	got, err = absent.Count(dir, files)
	if err != nil {
		t.Fatalf("tracked-but-absent file must not error: %v", err)
	}
	if got != 0 {
		t.Fatalf("absent: want 0 lines, got %d", got)
	}
}

func TestCheckFailsWhenOverMax(t *testing.T) {
	budget := Budget{Buckets: []Bucket{{Name: "x", Max: 10}}}
	counts := map[string]int{"x": 11}
	if errs := budget.Check(counts); len(errs) != 1 {
		t.Fatalf("want 1 violation, got %d", len(errs))
	}
	counts["x"] = 10
	if errs := budget.Check(counts); len(errs) != 0 {
		t.Fatalf("want 0 violations, got %d", len(errs))
	}
}

func TestWriteOnlyRatchetsDown(t *testing.T) {
	budget := Budget{Buckets: []Bucket{{Name: "x", Max: 10}, {Name: "y", Max: 10}}}
	budget.Ratchet(map[string]int{"x": 7, "y": 12})
	if budget.Buckets[0].Max != 7 {
		t.Fatalf("x: want 7, got %d", budget.Buckets[0].Max)
	}
	if budget.Buckets[1].Max != 10 {
		t.Fatalf("y: want 10 (never raised), got %d", budget.Buckets[1].Max)
	}
}

func TestHasReportsKnownBuckets(t *testing.T) {
	budget := Budget{Buckets: []Bucket{{Name: "go_prod", Suffixes: []string{".go"}}}}
	if !budget.Has("go_prod") {
		t.Fatal("go_prod: want known")
	}
	if budget.Has("go_prd") {
		t.Fatal("go_prd: a typo must not resolve to a bucket")
	}
}

func TestValidateRejectsUnusableBuckets(t *testing.T) {
	good := Bucket{Name: "go_prod", Suffixes: []string{".go"}}
	tests := []struct {
		name    string
		buckets []Bucket
		want    string
	}{
		{"valid", []Bucket{good}, ""},
		{"no suffixes", []Bucket{{Name: "x"}}, "can never match"},
		{"empty name", []Bucket{{Suffixes: []string{".go"}}}, "empty name"},
		{"duplicate names", []Bucket{good, {Name: "go_prod", Suffixes: []string{".md"}}}, "duplicate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Budget{Buckets: tt.buckets}.Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want an error containing %q, got nil", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want an error containing %q, got %v", tt.want, err)
			}
		})
	}
}

// TestSourceFilesCountsWhatIsNotCommittedYet. A budget that only saw the index
// would pass on the commit that broke it and fail on the next one.
func TestSourceFilesCountsWhatIsNotCommittedYet(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	writeFile(t, dir, ".gitignore", "ignored.go\n")
	writeFile(t, dir, "committed.go", "package a\n")
	git("add", ".gitignore", "committed.go")
	git("-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-qm", "seed")
	writeFile(t, dir, "new.go", "package a\n")
	writeFile(t, dir, "ignored.go", "package a\n")

	got, err := sourceFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{".gitignore": true, "committed.go": true, "new.go": true}
	if len(got) != len(want) {
		t.Fatalf("sourceFiles = %v, want %v", got, slices.Sorted(maps.Keys(want)))
	}
	for _, f := range got {
		if !want[f] {
			t.Errorf("sourceFiles listed %q", f)
		}
	}
}

// TestDirSuffixesSelectTestSupportPackages. A module's fake and its conformance
// suite are production files by every other measure — no _test.go suffix, they
// compile into anything that imports them — and they are test support all the
// same. The package name is what says so, so that is what the budget matches.
func TestDirSuffixesSelectTestSupportPackages(t *testing.T) {
	files := []string{
		"modules/task/internal/service.go",
		"modules/task/contracts/tasktest/fake.go",
		"modules/task/contracts/tasktest/fake_test.go",
	}
	dir := t.TempDir()
	writeFile(t, dir, files[0], "package internal\n// one\n")
	writeFile(t, dir, files[1], "package tasktest\n")
	writeFile(t, dir, files[2], "package tasktest_test\n")

	support := Bucket{Name: "go_testsupport", Suffixes: []string{".go"},
		ExcludeSuffixes: []string{"_test.go"}, DirSuffixes: []string{"test"}}
	got, err := support.Count(dir, files)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Errorf("go_testsupport counted %d lines, want the fake's one", got)
	}

	prod := Bucket{Name: "modules", Paths: []string{"modules/"}, Suffixes: []string{".go"},
		ExcludeSuffixes: []string{"_test.go"}, ExcludeDirSuffixes: []string{"test"}}
	got, err = prod.Count(dir, files)
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Errorf("modules counted %d lines, want the service's two", got)
	}
}
