package fault_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// capability is how a document names another module's value package:
// "auth/contracts", "calendar/contracts". The prefix is what has to exist here.
var capability = regexp.MustCompile(`([a-z][a-z0-9_-]*)/(contracts|events|domain)`)

// ADR 0009 keeps a private catalog's capabilities out of a public document: a
// published document may say "a module's value package", but naming a package in a
// catalog this repository does not publish is a disclosure the foundation cannot
// undo. The rule is mechanical, so the case is too — every "<capability>/…" in every
// markdown document the repository publishes has to name a module this tree holds.
// The scope is every document and not only the kit guides, because the narrower read
// was reproduced as a hole: a line naming `calendar/contracts` written into
// CHANGELOG.md, an ADR or a provider guide one directory deeper was invisible to a
// read of `kit/*/README.md` alone. Both halves of that are guards of this case: a
// walk that opened no kit guide, or fewer than thirty documents, proves nothing and
// says so rather than passing.
func TestEveryPublicDocumentNamesOnlyAModuleThisRepositoryHolds(t *testing.T) {
	docs, kitGuides := 0, 0
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "designexport" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		docs++
		if strings.Count(path, "/") == 4 && strings.HasPrefix(path, "../../kit/") && d.Name() == "README.md" {
			kitGuides++
		}
		for _, match := range capability.FindAllStringSubmatch(string(text), -1) {
			owner := match[1]
			if strings.Contains(" kit ui design modules apps tools ", " "+owner+" ") {
				continue // a directory of this repository, not a capability elsewhere
			}
			if _, err := os.Stat(filepath.Join("../..", "modules", owner)); err != nil {
				t.Errorf("%s names %q, a capability this repository does not hold; ADR 0009 keeps a private "+
					"catalog's names out of a document this repository publishes",
					path, match[0])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the published documents: %v", err)
	}
	if kitGuides == 0 {
		t.Fatal("no kit guide was read, so this case proves nothing about a kit guide")
	}
	if docs < 30 {
		t.Fatalf("only %d markdown documents were read, so this case proves nothing", docs)
	}
}

// wrapSentinel is a refusal written against the adapter's name — the call kit/fault
// exists to remove from a package that takes no transaction.
var wrapSentinel = regexp.MustCompile(`fmt\.Errorf\(.*crud\.Err`)

// TestTheGuideNamesEveryCallerThatStillWrapsTheAdapterName checks this package's
// guide against the tree. The guide's Limits section says
// "`modules/auth/contracts` is the one traced caller" and the release note repeats
// it. `modules/content/contracts/render.go` wraps `crud.ErrInvalid` as well, and the
// reason the guide gives for the caller it did trace — that the same package embeds
// `crud.Base`, so no closure moves — holds for it too. A guide that under-reports its
// callers is how a reuse inventory stops being one: name every caller, or drop the
// superlative (and this case with it, by moving the sentence out of Limits).
func TestTheGuideNamesEveryCallerThatStillWrapsTheAdapterName(t *testing.T) {
	guide, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read this package's guide: %v", err)
	}
	for _, pattern := range []string{"../../modules/*/contracts/*.go", "../../modules/*/domain/*.go"} {
		files, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			text, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if !wrapSentinel.Match(text) {
				continue
			}
			pkg := strings.TrimPrefix(filepath.Dir(file), "../../")
			if !strings.Contains(string(guide), "`"+pkg+"/`") && !strings.Contains(string(guide), "`"+pkg+"`") {
				t.Errorf("%s wraps the adapter's refusal name and no package guide names it: the guide that "+
					"calls one caller \"the one traced\" has to have traced the rest", pkg)
			}
		}
	}
}
