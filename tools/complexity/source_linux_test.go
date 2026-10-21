package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSelectedFIFORefusesWithoutWaitingForAWriter(t *testing.T) {
	if root := os.Getenv("COMPLEXITY_FIFO_FIXTURE"); root != "" {
		os.Exit(run([]string{"--root", root}, os.Stdout, os.Stderr))
	}
	r, _ := fixture(t, 0)
	writeFile(t, r.root, "blocked.go", "package a\n")
	git(t, r, "add", "blocked.go")
	name := filepath.Join(r.root, "blocked.go")
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(name, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSelectedFIFORefusesWithoutWaitingForAWriter$")
	cmd.Env = append(os.Environ(), "COMPLEXITY_FIFO_FIXTURE="+r.root)
	out, err := cmd.CombinedOutput()
	exit, ok := errors.AsType[*exec.ExitError](err)
	if ctx.Err() != nil || !ok || exit.ExitCode() != 2 || !strings.Contains(string(out), "not a regular file") {
		t.Fatalf("FIFO must refuse before opening: %v, context %v, output %s", err, ctx.Err(), out)
	}
}
