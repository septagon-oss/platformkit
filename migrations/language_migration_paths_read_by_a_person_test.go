package migrations_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestTheLanguageMigrationPathsAReaderIsGivenAreFilesOfThisTree checks what a reader is told
// against what the tree holds. Module READMEs and a contract's doc comment name these files by
// path, and the composition decides which versions are free, so a file that moves must take its
// citations with it. Each of the three must exist, land in the order the prose states, be named
// by at least one document, and be named by no path this tree does not hold. No version number
// is pinned: a case that pinned one would tax every branch merging a main that took it.
func TestTheLanguageMigrationPathsAReaderIsGivenAreFilesOfThisTree(t *testing.T) {
	const root = ".."
	names := []string{"preferred_locale", "user_language_revision", "tenant_locale_self_service"}
	cited := regexp.MustCompile(`[\w./-]*\d{4,}_(?:` + strings.Join(names, "|") + `)\.up\.sql`)
	version, seen := map[string]int{}, map[string]int{}
	for _, n := range names {
		version[n], seen[n] = -1, 0
	}
	walk := func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		for _, n := range names {
			if strings.HasSuffix(d.Name(), "_"+n+".up.sql") {
				version[n], _ = strconv.Atoi(strings.SplitN(strings.TrimLeft(d.Name(), "0"), "_", 2)[0])
			}
		}
		if !strings.HasSuffix(d.Name(), ".md") && !(strings.HasSuffix(d.Name(), ".go") && !strings.HasSuffix(d.Name(), "_test.go")) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, cite := range cited.FindAllString(string(src), -1) {
			if _, err := os.Stat(filepath.Join(root, cite)); err != nil {
				t.Errorf("%s cites %s, which this tree does not hold", path, cite)
			}
			for _, n := range names {
				if strings.HasSuffix(cite, "_"+n+".up.sql") {
					seen[n]++
				}
			}
		}
		return nil
	}
	if err := filepath.WalkDir(root, walk); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if version[n] < 0 {
			t.Errorf("no file of this tree is the language migration %q", n)
		}
		if seen[n] == 0 {
			t.Errorf("nothing a reader is handed names the language migration %q; its SQL is undocumented", n)
		}
	}
	if version["preferred_locale"] >= version["user_language_revision"] ||
		version["user_language_revision"] >= version["tenant_locale_self_service"] {
		t.Errorf("the language migrations apply out of order: the choice is %d, its revision counter %d, the tenant's own rule %d",
			version["preferred_locale"], version["user_language_revision"], version["tenant_locale_self_service"])
	}
}
