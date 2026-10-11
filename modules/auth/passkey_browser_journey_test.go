package auth_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPasskeyBrowserJourneyUsesVirtualAuthenticator(t *testing.T) {
	root := filepath.Join("..", "..", "e2e")
	var journey, authenticator bool
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".ts") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".spec.ts") && strings.Contains(strings.ToLower(string(body)), "passkey") {
			journey = true
		}
		if strings.Contains(string(body), "WebAuthn.addVirtualAuthenticator") {
			authenticator = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read the browser journeys: %v", err)
	}
	if !journey || !authenticator {
		t.Fatalf("passkey browser journey: Playwright spec=%t, CDP virtual authenticator=%t; want both", journey, authenticator)
	}
}
