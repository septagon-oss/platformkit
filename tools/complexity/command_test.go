package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBudgetRequiresExactlySevenIntegerCeilings(t *testing.T) {
	raw, _ := json.Marshal(zeroBudget())
	valid := string(raw)
	for _, value := range []string{"null", "-1", "-0", "1.5", "1e0", `"1"`, "true", "[]", "{}", "9223372036854775808"} {
		bad := strings.Replace(valid, `"cc_over_30":0`, `"cc_over_30":`+value, 1)
		if _, err := decodeBudget([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	for _, bad := range []string{"null", "[]", "{}", valid + " {}", strings.Replace(valid, `"cc_over_30":0,`, "", 1), strings.Replace(valid, "{", `{"unknown":0,`, 1), strings.Replace(valid, "{", `{"cc_over_30":0,`, 1)} {
		if _, err := decodeBudget([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	for _, good := range []string{valid, " \n" + strings.ReplaceAll(valid, ":0", ":9223372036854775807") + "\n"} {
		if _, err := decodeBudget([]byte(good)); err != nil {
			t.Fatal(err)
		}
	}
	r, base := fixture(t, 0)
	if err := os.Remove(filepath.Join(r.root, budgetFile)); err != nil {
		t.Fatal(err)
	}
	invoke(t, r, 2, "--check", "--base", base)
}

func TestCountsAtCeilingsPass(t *testing.T) {
	r, base := fixture(t, 0)
	invoke(t, r, 0, "--check", "--base", base)
}

func TestLowerCountRequiresLowerCeiling(t *testing.T) {
	r, base := fixture(t, 1)
	out := invoke(t, r, 1, "--check", "--base", base)
	if !strings.Contains(out, "LOWER funcs_over_100: count 0 < ceiling 1; set ceiling to 0") {
		t.Fatal(out)
	}
	putBudget(t, r, 0)
	invoke(t, r, 0, "--check", "--base", base)
}

func TestAcceptanceMutationsRefuseAtTheCommand(t *testing.T) {
	mutations := map[string]string{
		"funcs_over_100":        "package a\n" + sizedFunction("Large", 101),
		"cc_over_30":            "package a\nfunc Branches(){" + strings.Repeat("if true {};", 30) + "}\n",
		"files_over_800":        "package a\n" + strings.Repeat("\n", 800),
		"long_comment_blocks":   "package a\n" + strings.Repeat("// explanation\n", 15),
		"history_comment_lines": "package a\n// review round 7\n",
		"stutters":              "package foo\ntype FooThing int\n",
		"httpx_exports":         "package httpx\ntype NewThing int\n",
	}
	for key, text := range mutations {
		t.Run(key, func(t *testing.T) {
			r, base := fixture(t, 0)
			path := "added.go"
			if key == "httpx_exports" {
				path = "kit/httpx/added.go"
			}
			writeFile(t, r.root, path, text)
			out := invoke(t, r, 1, "--check", "--base", base)
			if !strings.Contains(out, fmt.Sprintf("OVER %s: count 1 > ceiling 0 (+1)", key)) || !strings.Contains(out, path+":") {
				t.Fatal(out)
			}
		})
	}
}

func TestCommandFlagsAndExitStatuses(t *testing.T) {
	r, base := fixture(t, -1)
	invoke(t, r, 0, "--help")
	invoke(t, r, 0)
	for _, args := range [][]string{{"--check"}, {"--history-only"}, {"--base", base}, {"--check", "--history-only", "--base", base}, {"--check", "--base", base, "--merge-base", "main"}, {"--write"}, {"--unknown"}, {"--base="}, {"--merge-base="}, {"positional"}, {"--check", "--base", "HEAD"}} {
		invoke(t, r, 2, args...)
	}
	putBudget(t, r, 0)
	commit(t, r, "initialize")
	invoke(t, r, 0, "--history-only", "--base", base)
	invoke(t, r, 0, "--check", "--merge-base", "main")
}
