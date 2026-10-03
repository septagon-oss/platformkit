package internal

// The build stamp has two halves, and this is the one no other case in this
// repository holds.
//
// The Makefile stripped the revision the admin shell's footer
// renders (mount.go's version()). The cure is in two places: `-buildvcs=false`
// now sits on the two gate commands that throw their output away and nowhere
// else — which binary_makefile_goal_builds_carries_revision_footer_test.go guards — and the Makefile pins
// its compiler to go.mod's `toolchain` line rather than its `go` line, because
// go1.26 does not accept a git worktree's `.git` *file* as a VCS root
// (go.dev/issue/58218, fixed in go1.27) and stamps the first parent directory
// with a `.git` of its own instead, or fails the build outright.
//
// The flags case cannot see that second half: it
// passes under go1.26.6 in a clone-shaped checkout, so a pin reverted to the
// `go` line is green in CI and red only on a contributor's linked worktree —
// the ordinary way to keep two branches buildable.
//
// So this case asks the Makefile which toolchain every goal's `go` subprocess
// inherits, and then asks that toolchain to build something inside a linked
// worktree of a throwaway repository: one commit, `git worktree add`, a one-file
// main package. It asserts what a stamped build prints — `vcs.revision` equal to
// that worktree's own HEAD — never the "(development)" sentence an unstamped one
// puts in the footer.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTheToolchainTheMakefileSelectsIdentifiesALinkedWorktree(t *testing.T) {
	root := repositoryRoot(t)
	toolchain := makefileGOTOOLCHAIN(t, root)

	// Reachability: a git that cannot add a linked worktree cannot set the
	// subject of this case up, which is not the same as a toolchain that
	// misreads one.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("no git on PATH, so no linked worktree to build in: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fixture := t.TempDir()
	upstream := filepath.Join(fixture, "upstream")
	linked := filepath.Join(fixture, "linked")
	if err := os.MkdirAll(upstream, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(upstream, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/stamp\n\ngo 1.26\n")
	write("main.go", "package main\n\nfunc main() {}\n")

	git := func(dir string, arguments ...string) string {
		t.Helper()
		command := exec.CommandContext(ctx, "git", arguments...)
		command.Dir = dir
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=stamper", "GIT_AUTHOR_EMAIL=stamper@example.test",
			"GIT_COMMITTER_NAME=stamper", "GIT_COMMITTER_EMAIL=stamper@example.test")
		out, err := command.CombinedOutput()
		if err != nil {
			t.Skipf("git %s in %s: %v\n%s", strings.Join(arguments, " "), dir, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(upstream, "init", "-q", "-b", "main")
	git(upstream, "add", "-A")
	git(upstream, "commit", "-q", "-m", "the checkout a build has to identify")
	git(upstream, "worktree", "add", "-q", "-b", "linked", linked)

	// The subject: a checkout whose .git is a file rather than a directory.
	if info, err := os.Stat(filepath.Join(linked, ".git")); err != nil || info.IsDir() {
		t.Skipf("git did not give %s a .git file, so this checkout is not a linked worktree (err %v)", linked, err)
	}
	revision := git(linked, "rev-parse", "HEAD")

	binary := filepath.Join(t.TempDir(), "stamp")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	build.Dir = linked
	build.Env = append(os.Environ(), "GOTOOLCHAIN="+toolchain, "GOFLAGS=", "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the toolchain the Makefile selects (GOTOOLCHAIN=%s) cannot build inside a linked "+
			"worktree: %v\n%s", toolchain, err, out)
	}

	stamp := exec.CommandContext(ctx, "go", "version", "-m", binary)
	stamp.Env = append(os.Environ(), "GOTOOLCHAIN="+toolchain, "GOFLAGS=")
	out, err := stamp.CombinedOutput()
	if err != nil {
		t.Fatalf("reading the build info of %s: %v\n%s", binary, err, out)
	}
	if want := "vcs.revision=" + revision; !strings.Contains(string(out), want) {
		t.Fatalf("the toolchain the Makefile selects (GOTOOLCHAIN=%s) built a binary in a linked "+
			"worktree that carries no %s, so `make run`, `make e2e` and `make rehearse` stamp another "+
			"checkout's revision or nothing at all, and mount.go's version() has no revision to render "+
			"(go.dev/issue/58218)\n%s", toolchain, want, out)
	}
}

// makefileGOTOOLCHAIN is the GOTOOLCHAIN every `go` subprocess of a Makefile
// goal inherits, read from that file by Make itself rather than by a regular
// expression: the value is a $(shell) expansion of go.mod, so only Make can say
// what it comes to. An empty export is "local", which is what a goal gets when
// nobody pins one.
func makefileGOTOOLCHAIN(t *testing.T, root string) string {
	t.Helper()
	ask := filepath.Join(t.TempDir(), "gotoolchain.mk")
	body := "include " + filepath.Join(root, "Makefile") + "\n" +
		"print-gotoolchain:\n\t@printf '%s' '$(GOTOOLCHAIN)'\n"
	if err := os.WriteFile(ask, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("make", "--no-print-directory", "-C", root, "-f", ask,
		"print-gotoolchain")
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("asking the Makefile for the toolchain it selects: %v\n%s", err, out)
	}
	if value := strings.TrimSpace(string(out)); value != "" {
		return value
	}
	return "local"
}
