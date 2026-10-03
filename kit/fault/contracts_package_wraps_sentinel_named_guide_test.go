package fault_test

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// The round-4 review's two cases about the caller list this package publishes.
// Both have a passing branch: name what the tree holds, and state the reachability
// the import graph runs.

// TestEveryContractsPackageThatWrapsTheSentinelIsNamedByAGuide: this package's
// guide says "Four `contracts/` packages of this repository wrap one of these
// refusals, and a guide that traced one has to name the rest", and the release
// note repeats the count. A module's test doubles live one directory deeper than
// the guard's glob — modules/<name>/contracts/<name>test — and they wrap the
// adapter's name too. Passes when the guide names every one of them, or when it
// says in words that the list excludes the modules' test doubles.
func TestEveryContractsPackageThatWrapsTheSentinelIsNamedByAGuide(t *testing.T) {
	guide, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read this package's guide: %v", err)
	}
	if strings.Contains(string(guide), "test double") {
		return // the guide says what its list covers and what it leaves out
	}
	wrap := regexp.MustCompile(`fmt\.Errorf\(.*crud\.Err`)
	root := filepath.Join("..", "..", "modules")
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return err
		}
		pkg := strings.TrimPrefix(filepath.Dir(path), filepath.Join("..", "..")+string(filepath.Separator))
		if !strings.Contains(pkg, "/contracts/") && !strings.Contains(pkg, "/domain/") &&
			!strings.HasSuffix(pkg, "/contracts") && !strings.HasSuffix(pkg, "/domain") {
			return nil
		}
		text, err := os.ReadFile(path)
		if err != nil || !wrap.Match(text) {
			return err
		}
		if !strings.Contains(string(guide), "`"+pkg+"`") {
			t.Errorf("%s wraps the adapter's refusal name and no package guide names it; the guide's Limits counts four and this one is not among them",
				pkg)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the modules' contracts packages: %v", err)
	}
}

// TestTheReleaseNoteStatesTheReachabilityTheGraphRuns: the release note explains
// why no closure moved by saying who reaches whom. It says "kit/crud still reaches
// modules/auth/contracts" and "kit/db reaches that package", both of which run the
// other way: no kit package imports a module, which scripts/check_imports.sh and
// the compiler both refuse. Passes when the note names the module as the one that
// reaches the kit package, as this package's own guide already does.
func TestTheReleaseNoteStatesTheReachabilityTheGraphRuns(t *testing.T) {
	note, err := os.ReadFile(filepath.Join("..", "..", "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read the release note: %v", err)
	}
	inverted := regexp.MustCompile("(?s)`kit/(crud|db)`[^.]{0,80}?reaches `?modules/")
	if m := inverted.Find(note); m != nil {
		t.Errorf("the release note says %q; go list -deps names no modules/ path in a kit package, so the reachability runs from the module to the kit package and the note has the arrow backwards", m)
	}
	for _, c := range []struct{ pkg, want string }{
		{"./kit/crud", "github.com/septagon-oss/platformkit/modules/auth/contracts"},
		{"./kit/db", "github.com/septagon-oss/platformkit/modules/auth/contracts"},
	} {
		for _, dep := range goListDeps(t, c.pkg) {
			if dep == c.want {
				t.Errorf("%s really does reach %s, so the note's direction holds after all", c.pkg, c.want)
			}
		}
	}
}

// TestTheGuideTeachesAWrapTheRendererCanRead: the guide tells a caller how to wrap.
// Its one example puts the sentinel last; all 84 wraps of these three values in
// this repository's non-test Go source put it first, and the order decides what a
// client reads. kit/rest trims the literal "crud: invalid: " off a problem detail
// to make the field message a form marks against a control (FieldErrors), and that
// trim only runs on the sentinel-first order. Measured through it, the example's
// order answers the person "a role name is at most 64 characters: crud: invalid"
// and the repository's own order answers "a role name is at most 64 characters"
// which is what modules/admin asserts a person is never shown. Passes when the
// example wraps %w first, as modules/auth/contracts/roles.go does.
func TestTheGuideTeachesAWrapTheRendererCanRead(t *testing.T) {
	guide, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read this package's guide: %v", err)
	}
	text := string(guide)
	quote := byte('"')
	at := strings.Index(text, "fmt.Errorf(")
	if at < 0 {
		t.Fatalf("the guide names no fmt.Errorf example to check")
	}
	tail := text[at:]
	open := strings.IndexByte(tail, quote)
	close := strings.IndexByte(tail[open+1:], quote)
	lit := tail[open+1 : open+1+close]
	if !strings.HasPrefix(lit, "%w") {
		t.Errorf("the guide teaches the wrap %q, with the refusal last; every one of the 84 wraps of these three values in this repository puts the refusal first, because kit/rest derives the message a client reads by trimming the literal %q off the front of the problem detail",
			lit, "crud: invalid: ")
	}
	fields := []crud.Field{{Name: "name"}, {Name: "title"}}
	_, detail := rest.FieldErrors(rest.Fault(fmt.Errorf("%w: a role name is at most 64 characters", fault.ErrInvalid)), fields)
	if strings.Contains(detail, "crud: invalid") {
		t.Errorf("a refusal wrapped the way this repository wraps it reached the person as %q; the marker is supposed to be trimmed off", detail)
	}
}

// goListDeps is the same call kit/fault/fault_test.go makes: from the module root,
// this module's own graph, whatever the machine has around it.
func goListDeps(t *testing.T, pkg string) []string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", pkg)
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}
	return strings.Fields(string(out))
}
