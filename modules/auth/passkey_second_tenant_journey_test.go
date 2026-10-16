package auth_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A browser journey that refuses tenant A's passkey at tenant B's host has to
// open a page at a host other than the suite's own: a navigation to an absolute
// address, from a spec that drives a virtual authenticator. Asking Chromium for
// an rpId the page's origin cannot claim is refused by the browser before any
// server is asked, so it proves nothing about a second tenant.
var secondHost = regexp.MustCompile("\\.goto\\(\\s*[^'\"`/\\s)]|\\.goto\\(\\s*[`'\"]https?://|\\.goto\\(\\s*`\\$\\{|baseURL:\\s*[a-zA-Z`'\"]")

func TestPasskeyBrowserJourneyVisitsASecondTenantsHost(t *testing.T) {
	root := filepath.Join("..", "..", "e2e")
	var crossed []string
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
		if !strings.HasSuffix(path, ".spec.ts") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		if strings.Contains(text, "WebAuthn.addVirtualAuthenticator") && secondHost.MatchString(text) {
			crossed = append(crossed, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read the browser journeys: %v", err)
	}
	if len(crossed) == 0 {
		t.Fatal("no passkey browser journey opens a page at a second tenant's host; the brief's cross-tenant refusal is not walked in a browser")
	}
}
