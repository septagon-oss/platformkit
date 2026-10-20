package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestBrowserJourneyUsesTheConfiguredMailpitPort(t *testing.T) {
	body, err := os.ReadFile("../../scripts/e2e.sh")
	if err != nil {
		t.Fatal(err)
	}
	// Execute the script's derivation of the catcher's addresses with the same exported
	// port Compose receives, without bootstrapping an application for this
	// configuration-only assertion. The inbox URL is built from the API address and
	// that from the host, so the lines run together rather than the last one alone.
	text := string(body)
	start, end := strings.Index(text, "mail_host="), strings.Index(text, "mail_url=")
	if start < 0 || end < start {
		t.Fatal("e2e fixture no longer derives its inbox URL; exercise its new configuration entry point")
	}
	assignment := text[start : end+strings.Index(text[end:], "\n")]
	for _, tc := range []struct{ name, override, want string }{
		{"compose port", "", "http://localhost:56116"},
		{"explicit inbox URL", "http://inbox.example.test:9000", "http://inbox.example.test:9000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), "bash", "-c", assignment+"\nprintf '%s' \"$mail_url\"")
			cmd.Env = []string{"PLATFORMKIT_MAIL_UI_PORT=56116", "PLATFORMKIT_E2E_MAIL_URL=" + tc.override}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("evaluate the inbox address: %v: %s", err, out)
			}
			if string(out) != tc.want {
				t.Errorf("browser journey reads %q, want %q", out, tc.want)
			}
		})
	}
}
