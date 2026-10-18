package valkey

// TestTheSharedStoreIsACache says `make up` starts the valkey service the Makefile
// points PLATFORMKIT_TEST_VALKEY_URL at. If either half is missing, the only
// production adapter of kit/cache skips in every `make check` and every CI run,
// and the skip sends whoever reads it to a command that starts nothing.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestMakeUpStartsTheStoreThisSuiteReads holds the two lines the skip sentence
// promises: a valkey service in compose.yaml, and the Makefile exporting the
// address this suite reads.
func TestMakeUpStartsTheStoreThisSuiteReads(t *testing.T) {
	compose, err := os.ReadFile("../../../../compose.yaml")
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	if !regexp.MustCompile(`(?m)^  valkey:\s*$`).Match(compose) {
		t.Errorf("compose.yaml declares no valkey service; `make up` starts no store for TestTheSharedStoreIsACache to read")
	}
	makefile, err := os.ReadFile("../../../../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	if !strings.Contains(string(makefile), "PLATFORMKIT_TEST_VALKEY_URL") {
		t.Errorf("the Makefile never sets PLATFORMKIT_TEST_VALKEY_URL, so `make check` skips the Valkey adapter's conformance run")
	}
}
