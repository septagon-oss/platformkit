package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func sourcePaths(t *testing.T, r repository, revision string) []string {
	t.Helper()
	files, err := r.sources(revision)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range files {
		paths = append(paths, f.path)
	}
	return paths
}

func TestSourceSelection(t *testing.T) {
	r, _ := fixture(t, 0)
	writeFile(t, r.root, "tracked.go", "package a\n")
	writeFile(t, r.root, "deleted.go", "package a\n")
	commit(t, r, "track")
	writeFile(t, r.root, ".gitattributes", "deleted.go linguist-generated=invalid\n")
	writeFile(t, r.root, ".gitignore", "tracked.go\nignored.go\n")
	for _, path := range []string{"ignored.go", "new.go", "nested/vendor/x.go", "node_modules/x.go", "a/testdata/x.go", "a/x_test.go", "supporttest/x.go"} {
		writeFile(t, r.root, path, "package a\n")
	}
	if err := os.Remove(filepath.Join(r.root, "deleted.go")); err != nil {
		t.Fatal(err)
	}
	git(t, r, "add", "new.go")
	want := []string{"kit/httpx/doc.go", "new.go", "supporttest/x.go", "tracked.go"}
	if got := sourcePaths(t, r, ""); !slices.Equal(got, want) {
		t.Fatalf("%v want %v", got, want)
	}
	writeFile(t, r.root, "new.go", "package a\n"+sizedFunction("Big", 101))
	invoke(t, r, 1, "--check", "--base", git(t, r, "rev-parse", "HEAD"))
}

func TestGeneratedAttributes(t *testing.T) {
	r, _ := fixture(t, 0)
	writeFile(t, r.root, ".gitattributes", "set.go linguist-generated\ntrue.go linguist-generated=true\nfalse.go linguist-generated=false\nunset.go -linguist-generated\nnested/*.go linguist-generated=true\n")
	writeFile(t, r.root, "nested/.gitattributes", "keep.go linguist-generated=false\n")
	for _, name := range []string{"set.go", "true.go", "false.go", "unset.go", "plain.go", "header.go", "nested/skip.go", "nested/keep.go"} {
		writeFile(t, r.root, name, "// Code generated; DO NOT EDIT.\npackage a\n")
	}
	want := []string{"false.go", "header.go", "kit/httpx/doc.go", "nested/keep.go", "plain.go", "unset.go"}
	sha := commit(t, r, "attributes")
	for _, revision := range []string{"", sha} {
		if got := sourcePaths(t, r, revision); !slices.Equal(got, want) {
			t.Fatalf("%v want %v", got, want)
		}
	}
	writeFile(t, r.root, ".gitattributes", "plain.go linguist-generated=maybe\n")
	if _, err := r.sources(""); err == nil {
		t.Fatal("accepted unknown attribute value")
	}
	// The committed attributes must not follow a working-copy edit.
	if got := sourcePaths(t, r, sha); !slices.Equal(got, want) {
		t.Fatal(got)
	}
}

func TestHTTPGeneratedAndTaggedFiles(t *testing.T) {
	r, _ := fixture(t, 0)
	writeFile(t, r.root, ".gitattributes", "kit/httpx/generated.go linguist-generated=true\n")
	writeFile(t, r.root, "kit/httpx/generated.go", "invalid ignored Go")
	writeFile(t, r.root, "kit/httpx/tagged.go", "//go:build never_enabled_tag\n\npackage httpx\ntype Tagged int\n")
	files, err := r.sources("")
	if err != nil {
		t.Fatal(err)
	}
	m, err := measure(files)
	if err != nil {
		t.Fatal(err)
	}
	requireCount(t, m, "httpx_exports", 1)
}

func TestMeasurementRefusesIncompleteInput(t *testing.T) {
	for _, kind := range []string{"parse", "unreadable", "symlink", "parent-symlink", "missing-http"} {
		t.Run(kind, func(t *testing.T) {
			r, _ := fixture(t, 0)
			switch kind {
			case "parse":
				writeFile(t, r.root, "bad.go", "package")
			case "unreadable":
				writeFile(t, r.root, "bad.go", "package a\n")
				git(t, r, "add", "bad.go")
				if err := os.Remove(filepath.Join(r.root, "bad.go")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(r.root, "bad.go"), 0755); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("kit/httpx/doc.go", filepath.Join(r.root, "bad.go")); err != nil {
					t.Fatal(err)
				}
			case "parent-symlink":
				writeFile(t, r.root, "linked/a.go", "package a\n")
				git(t, r, "add", "linked/a.go")
				if err := os.RemoveAll(filepath.Join(r.root, "linked")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("kit/httpx", filepath.Join(r.root, "linked")); err != nil {
					t.Fatal(err)
				}
			case "missing-http":
				if err := os.RemoveAll(filepath.Join(r.root, "kit")); err != nil {
					t.Fatal(err)
				}
			}
			invoke(t, r, 2)
		})
	}
}
