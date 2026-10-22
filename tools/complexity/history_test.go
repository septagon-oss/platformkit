package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOrdinaryCeilingLoweringPassesHistory(t *testing.T) {
	r, base := fixture(t, 1)
	putBudget(t, r, 0)
	writeFile(t, r.root, "a.go", "package a\n")
	commit(t, r, "feat: remove debt")
	historyResult(t, r, base, 0)
	invoke(t, r, 0, "--check", "--base", base)
}

func TestEveryRaiseRequiresItsOwnApproval(t *testing.T) {
	messages := []string{
		"build(complexity): growth", "feat: growth\n\nOwner-approved: owner", "Build(complexity): growth\n\nOwner-approved: owner",
		"build(complexity): growth\n\nOwner-approved:   ",
		"build(complexity): growth\n\nOwner-approved: owner\nThis is body text, not a trailer.",
		"build(complexity): Owner-approved: owner", approved,
	}
	for _, message := range messages {
		t.Run(message, func(t *testing.T) {
			r, base := fixture(t, 10)
			commit(t, r, "chore: prior approval\n\nOwner-approved: owner")
			putBudget(t, r, 11)
			sha := commit(t, r, message)
			want := 1
			if message == approved {
				want = 0
			}
			out := historyResult(t, r, base, want)
			if want == 1 && (!strings.Contains(out, sha) || !strings.Contains(out, "10 -> 11")) {
				t.Fatal(out)
			}
		})
	}
}

func TestApprovedRaiseMustChangeOnlyComplexityBudget(t *testing.T) {
	for _, path := range []string{"a.go", "a_test.go", "README.md", "loc-budget.json", "renamed"} {
		t.Run(path, func(t *testing.T) {
			r, _ := fixture(t, 10)
			writeFile(t, r.root, "old", "content")
			base := commit(t, r, "old file")
			putBudget(t, r, 11)
			if path == "renamed" {
				git(t, r, "mv", "old", "new")
			} else {
				writeFile(t, r.root, path, "content")
			}
			commit(t, r, approved)
			out := historyResult(t, r, base, 1)
			if !strings.Contains(out, "must change only") {
				t.Fatal(out)
			}
		})
	}
}

func TestHistoryDoesNotHideIntermediateRaises(t *testing.T) {
	for _, values := range [][]int64{{11, 10}, {8, 9}} {
		r, base := fixture(t, 10)
		for _, n := range values {
			putBudget(t, r, n)
			commit(t, r, "feat: change")
		}
		historyResult(t, r, base, 1)
	}
}

func TestOrdinaryLoweringAfterApprovedRaisePasses(t *testing.T) {
	r, base := fixture(t, 10)
	putBudget(t, r, 12)
	commit(t, r, approved)
	putBudget(t, r, 11)
	writeFile(t, r.root, "note", "lowered")
	commit(t, r, "feat: remove one")
	historyResult(t, r, base, 0)
}

func TestWorkingBudgetCannotBorrowOldApproval(t *testing.T) {
	r, base := fixture(t, 10)
	putBudget(t, r, 12)
	commit(t, r, approved)
	putBudget(t, r, 13)
	historyResult(t, r, base, 1)
	git(t, r, "add", budgetFile)
	historyResult(t, r, base, 1)
	putBudget(t, r, 11)
	historyResult(t, r, base, 0)
}

func mergeFixture(t *testing.T, left, right, result int64, message string) (repository, string) {
	t.Helper()
	r, base := fixture(t, 10)
	git(t, r, "checkout", "-q", "-b", "side")
	putBudget(t, r, right)
	commit(t, r, approved)
	side := git(t, r, "rev-parse", "HEAD")
	git(t, r, "checkout", "-q", "main")
	putBudget(t, r, left)
	commit(t, r, approved)
	leftSHA := git(t, r, "rev-parse", "HEAD")
	putBudget(t, r, result)
	git(t, r, "add", budgetFile)
	tree := git(t, r, "write-tree")
	sha := git(t, r, "commit-tree", tree, "-p", leftSHA, "-p", side, "-m", message)
	git(t, r, "reset", "--hard", sha)
	return r, base
}

func TestMergeCarriesTheChangedParentBudget(t *testing.T) {
	for _, tc := range []struct {
		left, result int64
		want         int
	}{{12, 12, 0}, {9, 9, 0}, {9, 10, 1}} {
		r, base := mergeFixture(t, tc.left, 10, tc.result, "merge")
		historyResult(t, r, base, tc.want)
	}
}

func TestMergeCannotRestoreReturnedCeiling(t *testing.T) {
	for _, tc := range []struct {
		left, right, result int64
		want                int
	}{{12, 9, 9, 0}, {12, 9, 12, 1}, {12, 14, 12, 0}, {12, 14, 14, 1}} {
		r, base := mergeFixture(t, tc.left, tc.right, tc.result, approved)
		historyResult(t, r, base, tc.want)
		if tc.want == 0 {
			putBudget(t, r, 14)
			commit(t, r, approved)
			historyResult(t, r, base, 0)
		}
	}
}

func TestSideBranchHistoryIsInspectedOnce(t *testing.T) {
	r, base := fixture(t, 10)
	git(t, r, "checkout", "-q", "-b", "side")
	putBudget(t, r, 11)
	bad := commit(t, r, "feat: hidden raise")
	putBudget(t, r, 10)
	commit(t, r, "feat: back")
	git(t, r, "checkout", "-q", "main")
	git(t, r, "merge", "--no-ff", "side", "-m", "merge")
	out := historyResult(t, r, base, 1)
	if strings.Count(out, bad+": funcs_over_100") != 1 {
		t.Fatal(out)
	}
}

func TestOctopusUsesChangedParentMinimum(t *testing.T) {
	r, base := fixture(t, 10)
	var parents []string
	for _, n := range []int64{12, 14, 10} {
		git(t, r, "checkout", "-q", "--detach", base)
		putBudget(t, r, n)
		parents = append(parents, commit(t, r, approved))
	}
	putBudget(t, r, 12)
	git(t, r, "add", budgetFile)
	tree := git(t, r, "write-tree")
	args := []string{"commit-tree", tree, "-m", "octopus"}
	for _, parent := range parents {
		args = append(args, "-p", parent)
	}
	merged := git(t, r, args...)
	git(t, r, "reset", "--hard", merged)
	historyResult(t, r, base, 0)
	putBudget(t, r, 14)
	git(t, r, "add", budgetFile)
	args[1] = git(t, r, "write-tree")
	merged = git(t, r, args...)
	git(t, r, "reset", "--hard", merged)
	historyResult(t, r, base, 1)
	// Two opposite merges have both siblings as best common ancestors.
	first := git(t, r, "commit-tree", tree, "-p", parents[0], "-p", parents[1], "-m", "first")
	second := git(t, r, "commit-tree", tree, "-p", parents[1], "-p", parents[0], "-m", "second")
	cross := git(t, r, "commit-tree", tree, "-p", first, "-p", second, "-m", "cross")
	git(t, r, "reset", "--hard", cross)
	out := historyResult(t, r, base, 2)
	for _, parent := range parents[:2] {
		if !strings.Contains(out, parent) {
			t.Fatal(out)
		}
	}
}

func TestBudgetDeletionCannotRestartInitialization(t *testing.T) {
	r, base := fixture(t, 10)
	if err := os.Remove(filepath.Join(r.root, budgetFile)); err != nil {
		t.Fatal(err)
	}
	deleted := commit(t, r, approved)
	putBudget(t, r, 10)
	commit(t, r, approved)
	out := historyResult(t, r, base, 1)
	if !strings.Contains(out, deleted+": deleted established") {
		t.Fatal(out)
	}
	writeFile(t, r.root, budgetFile, `{"funcs_over_100":10}`)
	commit(t, r, approved)
	historyResult(t, r, base, 2)
}

func TestInitialBudgetUsesMeasuredBase(t *testing.T) {
	for _, seed := range []int64{0, 1, 2} {
		r, _ := fixture(t, -1)
		writeFile(t, r.root, "a.go", "package a\n"+sizedFunction("Big", 101))
		base := commit(t, r, "unbudgeted debt")
		putBudget(t, r, seed)
		if seed == 0 {
			writeFile(t, r.root, "a.go", "package a\n")
		}
		commit(t, r, approved)
		want := 0
		if seed == 2 {
			want = 1
		}
		invoke(t, r, want, "--check", "--base", base)
	}
	// A side branch from before introduction uses the trusted baseline ceiling.
	r, old := fixture(t, -1)
	putBudget(t, r, 1)
	base := commit(t, r, "introduce budget")
	git(t, r, "checkout", "-q", "-b", "old-side", old)
	commit(t, r, "pre-budget work")
	putBudget(t, r, 2)
	commit(t, r, approved)
	git(t, r, "checkout", "-q", "main")
	left := git(t, r, "rev-parse", "HEAD")
	right := git(t, r, "rev-parse", "old-side")
	putBudget(t, r, 2)
	git(t, r, "add", budgetFile)
	sha := git(t, r, "commit-tree", git(t, r, "write-tree"), "-p", left, "-p", right, "-m", "merge")
	git(t, r, "reset", "--hard", sha)
	historyResult(t, r, base, 0)
	if err := os.Remove(filepath.Join(r.root, budgetFile)); err != nil {
		t.Fatal(err)
	}
	commit(t, r, "remove budget")
	putBudget(t, r, 1)
	historyResult(t, r, base, 1)
}

func TestMissingHistoryFailsClosed(t *testing.T) {
	r, base := fixture(t, 0)
	invoke(t, r, 2, "--history-only", "--merge-base", "absent")
	invoke(t, r, 2, "--history-only", "--base", strings.Repeat("f", 40))
	git(t, r, "checkout", "-q", "-b", "other")
	other := commit(t, r, "other")
	git(t, r, "checkout", "-q", "main")
	invoke(t, r, 2, "--history-only", "--base", other)
	commit(t, r, "descendant")
	shallow := repository{filepath.Join(t.TempDir(), "shallow")}
	git(t, r, "clone", "--quiet", "--depth=1", "file://"+r.root, shallow.root)
	invoke(t, shallow, 2, "--history-only", "--base", base)
	shallowHead := git(t, shallow, "rev-parse", "HEAD")
	invoke(t, shallow, 0, "--history-only", "--base", shallowHead)
	// Inspecting the truncated commit itself must expose its missing edge.
	h := &history{repo: shallow, initial: budgetState{values: zeroBudget()}, budgets: map[string]budgetState{}}
	if _, err := h.transition(shallowHead); err == nil || !strings.Contains(err.Error(), "shallow history") {
		t.Fatalf("missing parent: %v", err)
	}
}

func TestRunIsReadOnlyAndRepeatable(t *testing.T) {
	r, base := fixture(t, 0)
	before := git(t, r, "status", "--porcelain=v1") + git(t, r, "show-ref") + git(t, r, "ls-files", "-s")
	one := invoke(t, r, 0, "--check", "--base", base)
	two := invoke(t, r, 0, "--check", "--base", base)
	after := git(t, r, "status", "--porcelain=v1") + git(t, r, "show-ref") + git(t, r, "ls-files", "-s")
	if one != two || before != after {
		t.Fatal("check changed output or repository state")
	}
	h, err := startHistory(r, base, "")
	if err != nil {
		t.Fatal(err)
	}
	current := commit(t, r, "move HEAD")
	if err := h.stable(); err == nil || !strings.Contains(err.Error(), base) || !strings.Contains(err.Error(), current) {
		t.Fatalf("HEAD drift: %v", err)
	}
}
