package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestJSONReportCarriesRefusalsAndFailsTheRun(t *testing.T) {
	root := tree(t, map[string]string{"a/a.go": "package a\n\n// settled in review 3\nfunc A() {}\n"})
	out, err := os.Create(filepath.Join(t.TempDir(), "out.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if code := run([]string{"--json", root}, out, os.Stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1 for a refused comment", code)
	}
	raw, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	var rep Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatalf("--json is not a Report: %v\n%s", err, raw)
	}
	if len(rep.Refused) != 1 || rep.Refused[0].Line != 3 || len(rep.Packages) != 1 {
		t.Fatalf("report = %+v, want one refusal at line 3 and one package", rep)
	}
}
