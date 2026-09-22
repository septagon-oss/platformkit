package fault_test

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// self is the one non-standard line a transaction-free package may list. It is
// written out rather than asked of go list, so renaming or moving the package
// fails this test instead of moving the goalpost with it.
const self = "github.com/septagon-oss/platformkit/kit/fault"

// TestLinksNothingButTheStandardLibrary is this package's reason to exist. The
// rule it serves lets a module import another module only a package whose
// transitive imports name no transaction, and the check for that rule is
// go list -deps: one non-standard line naming a storage adapter, or a web
// server, and every value package that needs to say "invalid" is back to
// importing the CRUD adapter for it.
//
// The command's own format string prints no line for a standard package, so a
// package that links nothing has exactly one line: itself.
func TestLinksNothingButTheStandardLibrary(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), "go", "list", "-deps",
		"-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./kit/fault")
	// ./kit/fault resolves from the module root; a test runs in its own
	// directory. GOWORK is off and GOFLAGS clear so the listing is this
	// module's own dependency graph, whatever the machine has around it.
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// Fail rather than skip: a gate that quietly skips when the go command
		// is missing from PATH would pass on the machine that needs it most.
		t.Fatalf("go list -deps ./kit/fault: %v\n%s", err, &stderr)
	}
	lines := strings.Fields(string(out))
	if len(lines) != 1 || lines[0] != self {
		t.Errorf("go list -deps ./kit/fault lists %d non-standard package(s), want only %s:\n%s",
			len(lines), self, strings.Join(lines, "\n"))
	}
}
