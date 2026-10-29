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
	// Execute the script's inbox-address assignment with the same exported
	// port Compose receives, without bootstrapping an application for this
	// configuration-only assertion.
	var assignment string
	for line := range strings.SplitSeq(string(body), "\n") {
		if strings.HasPrefix(line, "mail_url=") {
			assignment = line
			break
		}
	}
	if assignment == "" {
		t.Fatal("e2e fixture no longer assigns its inbox URL; exercise its new configuration entry point")
	}
	for _, tc := range []struct{ name, override, want string }{
		{"compose port", "", "http://127.0.0.1:56116"},
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
