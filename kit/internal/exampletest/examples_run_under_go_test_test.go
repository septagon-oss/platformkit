package exampletest

import (
	"os/exec"
	"strings"
	"testing"
)

func TestEachListedPackageRunsAnExampleUnderGoTest(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns go test")
	}
	for _, pkg := range []string{"db", "events", "httpx", "rest", "crud", "cache", "jobs", "tenancy"} {
		out, err := exec.Command("go", "test", "-count=1", "-run", "^Example", "-v", "../../"+pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", pkg, err, out)
		}
		if !strings.Contains(string(out), "--- PASS: Example") {
			t.Errorf("%s: no example executed and passed:\n%s", pkg, out)
		}
	}
}
