package inference_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTheConformanceTargetSelectsItsNamedProviders(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\nprintf 'selected=%s\\n' \"$PLATFORMKIT_TEST_INFERENCE_PROVIDERS\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "make", "--no-print-directory", "inference-conformance", "PROVIDERS=ollama,hosted")
	cmd.Dir = "../.."
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "PLATFORMKIT_TEST_INFERENCE_PROVIDERS=") && !strings.HasPrefix(entry, "PATH=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("documented provider invocation failed before running conformance: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "selected=ollama,hosted") {
		t.Fatalf("conformance did not receive the named providers:\n%s", out)
	}
}
