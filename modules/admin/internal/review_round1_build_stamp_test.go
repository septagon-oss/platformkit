package internal

// The footer of the admin shell renders version(), and version() is the
// revision Go stamps into the binary's own build info — "a fact the toolchain
// records; a version string somebody bumps by hand is a version string that is
// wrong", as mount.go puts it. That makes the stamp a field this repository
// reads, so the flags the Makefile hands every `go` subprocess have to leave it
// alone: `make run` and `scripts/e2e.sh` both produce the binary a person then
// looks at.
//
// This case builds ./apps/platformkit exactly as a goal of the Makefile would —
// with the GOFLAGS that file exports, read out of the file rather than guessed —
// and asks the toolchain what it stamped. It reaches its assertion through what
// a stamped build prints (`go version -m` naming the revision `git rev-parse
// HEAD` names), not through the sentence an unstamped one puts in the footer.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestTheBinaryAMakefileGoalBuildsCarriesTheRevisionTheFooterRenders(t *testing.T) {
	root := repositoryRoot(t)

	// Reachability: only a checkout with history can be stamped at all. A source
	// tree without one is not the subject of this case.
	head, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Skipf("no git history at %s, so nothing could stamp a revision: %v", root, err)
	}
	revision := strings.TrimSpace(string(head))

	flags := makefileGOFLAGS(t, root)
	binary := filepath.Join(t.TempDir(), "platformkit")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./apps/platformkit")
	build.Dir = root
	build.Env = append(os.Environ(), "GOFLAGS="+flags, "GOTOOLCHAIN=local")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the app with the Makefile's GOFLAGS (%q) failed: %v\n%s", flags, err, out)
	}

	stamp := exec.CommandContext(ctx, "go", "version", "-m", binary)
	stamp.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	out, err := stamp.CombinedOutput()
	if err != nil {
		t.Fatalf("reading the build info of %s: %v\n%s", binary, err, out)
	}
	if want := "vcs.revision=" + revision; !strings.Contains(string(out), want) {
		t.Fatalf("a binary built with the Makefile's GOFLAGS (%q) carries no %s, so the admin footer "+
			"renders %q instead of the revision mount.go's version() promises\n%s",
			flags, want, "(development)", out)
	}
}

// repositoryRoot is the directory that holds both go.mod and the Makefile whose
// flags are under test.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, goMod := os.Stat(filepath.Join(dir, "go.mod"))
		_, makefile := os.Stat(filepath.Join(dir, "Makefile"))
		if goMod == nil && makefile == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no directory above %s holds both go.mod and Makefile", dir)
		}
		dir = parent
	}
}

// makefileGOFLAGS is the value every `go` subprocess of a Makefile goal inherits:
// the exported GOFLAGS assignments of that file, in order, with $(GOFLAGS)
// expanded to what the caller's environment holds. An absent assignment is the
// empty string, which is what a goal gets when nobody exports one.
func makefileGOFLAGS(t *testing.T, root string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	assignment := regexp.MustCompile(`^export\s+GOFLAGS\s*[:?+]?=\s*(.*)$`)
	value := ""
	for line := range strings.SplitSeq(string(body), "\n") {
		m := assignment.FindStringSubmatch(strings.TrimRight(line, " \t\r"))
		if m == nil {
			continue
		}
		value = strings.TrimSpace(strings.ReplaceAll(m[1], "$(GOFLAGS)", value))
	}
	return strings.TrimSpace(strings.ReplaceAll(value, "$(GOFLAGS)", os.Getenv("GOFLAGS")))
}
