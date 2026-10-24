package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
)

func TestReferencesAreDeterministicTypedConstants(t *testing.T) {
	data, err := os.ReadFile("../../ui/screens/testdata/screens.json")
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := screen.ParseSnapshot(data)
	if err != nil {
		t.Fatal(err)
	}
	a, err := references(inventory)
	if err != nil {
		t.Fatal(err)
	}
	b, err := references(inventory)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("generation differs: %v", err)
	}
	if !bytes.Contains(a, []byte("ViewRef")) || !bytes.Contains(a, []byte("AnchorRef")) || !bytes.Contains(a, []byte(inventory.Snapshot().Hash)) {
		t.Fatal("generated references lost their types or contract hash")
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "references.go", a, 0); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidArtifactLeavesGeneratedOutputUntouched(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "screens.json"), filepath.Join(dir, "references.go")
	if err := os.WriteFile(in, []byte(`{"schemaVersion":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("preserved"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-in", in, "-out", out}); err == nil {
		t.Fatal("unsupported artifact was generated")
	}
	got, err := os.ReadFile(out)
	if err != nil || string(got) != "preserved" {
		t.Fatalf("output = %q, %v", got, err)
	}
}

func TestCheckRefusesMissingOrEditedReferencesWithoutRepairingThem(t *testing.T) {
	out := filepath.Join(t.TempDir(), "references.go")
	args := []string{"-in", "../../ui/screens/testdata/screens.json", "-out", out}
	if err := run(append(args, "-check")); err == nil {
		t.Fatal("accepted missing references")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("check wrote output: %v", err)
	}
	if err := run(args); err != nil {
		t.Fatal(err)
	}
	if err := run(append(args, "-check")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(append(args, "-check")); err == nil {
		t.Fatal("accepted edited references")
	}
	got, err := os.ReadFile(out)
	if err != nil || string(got) != "edited" {
		t.Fatalf("check repaired stale output: %q, %v", got, err)
	}
}
