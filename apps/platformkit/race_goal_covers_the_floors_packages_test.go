package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The administrators' floor is proven by racing pairs: two transactions
// emptying the last two administering roles, a deactivation against an
// emptying write, two administrators standing down at once. Those cases live
// in three packages, and they are only evidence of a lock that works when the
// race detector runs them. The race goal must therefore keep naming all three,
// and must state a per-package bound of its own, because go test's default
// clock has already stopped a working suite before the floor's cases finished.
func TestTheRaceGoalRunsThePackagesThatHoldTheFloorsRacingPairs(t *testing.T) {
	makefile, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(makefile), "\n")
	patterns := racePackages(t, lines)
	for _, pkg := range []string{"./modules/user/internal", "./modules/auth/internal", "./apps/platformkit"} {
		if !covered(pkg, patterns) {
			t.Errorf("RACE_PACKAGES %q does not cover %s, where the floor's racing pairs live", patterns, pkg)
		}
	}
	recipe := raceRecipe(t, lines)
	if !strings.Contains(recipe, "$(RACE_PACKAGES)") {
		t.Errorf("the check-race recipe %q does not run $(RACE_PACKAGES)", recipe)
	}
	bound := regexp.MustCompile(`-timeout=(\S+)`).FindStringSubmatch(recipe)
	if bound == nil {
		t.Fatalf("the check-race recipe %q states no per-package -timeout", recipe)
	}
	d, err := time.ParseDuration(bound[1])
	if err != nil || d <= 0 {
		t.Fatalf("the check-race bound %q is not a positive duration: %v", bound[1], err)
	}
}

// racePackages returns the words assigned to RACE_PACKAGES, joining the
// backslash-continued lines the assignment spans.
func racePackages(t *testing.T, lines []string) []string {
	t.Helper()
	for i, line := range lines {
		if !strings.HasPrefix(line, "RACE_PACKAGES") {
			continue
		}
		_, value, _ := strings.Cut(line, "=")
		for strings.HasSuffix(strings.TrimSpace(value), "\\") && i+1 < len(lines) {
			value = strings.TrimSuffix(strings.TrimSpace(value), "\\") + " " + lines[i+1]
			i++
		}
		return strings.Fields(value)
	}
	t.Fatal("the Makefile assigns no RACE_PACKAGES")
	return nil
}

// raceRecipe returns the go test line under the check-race target.
func raceRecipe(t *testing.T, lines []string) string {
	t.Helper()
	for i, line := range lines {
		if !strings.HasPrefix(line, "check-race:") {
			continue
		}
		for _, recipe := range lines[i+1:] {
			if !strings.HasPrefix(recipe, "\t") {
				break
			}
			if strings.Contains(recipe, "go test") && strings.Contains(recipe, "-race") {
				return strings.TrimSpace(recipe)
			}
		}
		t.Fatal("the check-race target runs no go test -race line")
	}
	t.Fatal("the Makefile has no check-race target")
	return ""
}

// covered reports whether a package path is matched by one of the Makefile's
// patterns: the path itself, or a parent followed by /... .
func covered(pkg string, patterns []string) bool {
	for _, p := range patterns {
		if p == pkg {
			return true
		}
		if parent, ok := strings.CutSuffix(p, "/..."); ok && (pkg == parent || strings.HasPrefix(pkg, parent+"/")) {
			return true
		}
	}
	return false
}
