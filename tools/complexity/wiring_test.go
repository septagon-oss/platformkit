package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func wiringProblem(makefile, workflow string) error {
	for _, target := range []string{"check", "check-push"} {
		line := regexp.MustCompile(`(?m)^` + target + `:([^\n]*)`).FindStringSubmatch(makefile)
		if len(line) != 2 || !strings.Contains(" "+strings.Split(line[1], "#")[0]+" ", " check-complexity ") {
			return fmt.Errorf("%s lacks check-complexity", target)
		}
	}
	if !strings.Contains(makefile, `go run ./tools/complexity --check --merge-base "$(COMPLEXITY_BASE)"`) {
		return fmt.Errorf("recipe lacks full check")
	}
	for _, name := range []string{"make check", "Push tier (make check-push)"} {
		start := strings.Index(workflow, "      - name: "+name+"\n")
		if start < 0 {
			return fmt.Errorf("missing step %s", name)
		}
		step := workflow[start:]
		if end := strings.Index(step[1:], "\n      - name:"); end >= 0 {
			step = step[:end+1]
		}
		if !strings.Contains(step, "          COMPLEXITY_BASE: ${{ github.event.pull_request.base.sha || github.event.before }}") {
			return fmt.Errorf("%s lacks event base", name)
		}
	}
	if !regexp.MustCompile(`(?m)^          fetch-depth: 0$`).MatchString(strings.Split(workflow, "      - name: make check\n")[0]) {
		return fmt.Errorf("missing full history")
	}
	return nil
}

func TestComplexityGateIsWired(t *testing.T) {
	root := filepath.Join("..", "..")
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := os.ReadFile(filepath.Join(root, ".gitea/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := wiringProblem(string(makefile), string(workflow)); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"check", "check-push"} {
		cmd := exec.CommandContext(t.Context(), "make", "-n", target, "COMPLEXITY_BASE=fixture-base")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("make: %v %s", err, out)
		}
		if !strings.Contains(string(out), `go run ./tools/complexity --check --merge-base "fixture-base"`) {
			t.Fatal(string(out))
		}
	}
	for _, target := range []string{"check", "check-push"} {
		line := regexp.MustCompile(`(?m)^` + target + `:[^\n]*`).FindString(string(makefile))
		broken := strings.Replace(string(makefile), line, strings.Replace(line, " check-complexity", "", 1), 1)
		if err := wiringProblem(broken, string(workflow)); err == nil {
			t.Fatalf("accepted missing %s prerequisite", target)
		}
	}
	for _, needle := range []string{`--check --merge-base`, "fetch-depth: 0", "COMPLEXITY_BASE: ${{ github.event.pull_request.base.sha || github.event.before }}"} {
		input := string(workflow)
		if strings.Contains(string(makefile), needle) {
			input = string(makefile)
		}
		for offset := 0; ; {
			i := strings.Index(input[offset:], needle)
			if i < 0 {
				break
			}
			i += offset
			broken := input[:i] + input[i+len(needle):]
			m, w := string(makefile), string(workflow)
			if input == m {
				m = broken
			} else {
				w = broken
			}
			if err := wiringProblem(m, w); err == nil {
				t.Fatalf("accepted removal of %s at %d", needle, i)
			}
			if needle == "fetch-depth: 0" {
				break
			}
			offset = i + len(needle)
		}
	}
}
