package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The local command must pass the mail sink to the application process. The
// stand-in go executable exits immediately, so this checks the actual Make
// recipe without starting a server or writing a local configuration file.
func TestLocalRunPassesMailConfigurationToApplication(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	gotFile := filepath.Join(bin, "mail-environment")
	goScript := "#!/bin/sh\nprintf '%s\\n%s\\n%s\\n' \"${PLATFORMKIT_MAIL_HOST-}\" \"${PLATFORMKIT_MAIL_PORT-}\" \"${PLATFORMKIT_MAIL_FROM-}\" > \"$PKIT_CAPTURE\"\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(goScript), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "make", "-C", root, "-o", "config.yaml", "run")
	for _, setting := range os.Environ() {
		if strings.HasPrefix(setting, "PLATFORMKIT_MAIL_") || strings.HasPrefix(setting, "PATH=") || strings.HasPrefix(setting, "PKIT_CAPTURE=") {
			continue
		}
		cmd.Env = append(cmd.Env, setting)
	}
	cmd.Env = append(cmd.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "PKIT_CAPTURE="+gotFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make run through a stand-in Go executable: %v\n%s", err, out)
	}
	got, err := os.ReadFile(gotFile)
	if err != nil {
		t.Fatalf("the application command did not run: %v", err)
	}
	if string(got) != "127.0.0.1\n1025\nplatformkit@localhost\n" {
		t.Errorf("the application received mail host, port and sender as %q; make run must pass all three", got)
	}
}
