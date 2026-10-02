package change_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// operations are the six doors modules/change/internal/handler.go registers. Their ids
// are the ones the module writes on each huma.Operation, so this list is the module's
// own claim about its surface and not something a consumer would have to guess.
var operations = []string{
	"change-proposal-list", "change-proposal-read", "change-proposal-propose",
	"change-proposal-review", "change-proposal-apply", "change-proposal-withdraw",
}

// TestTheReferenceAppMountsTheSixProposalOperations is the reachability half of the
// promise modules/change/README.md makes in its first paragraph: the module exists so
// that a person's write can be put forward by one account and applied only after a
// different one says yes. A state machine no request can reach is a package, not a
// capability, and the two things a person can do with an unreachable object — propose
// and decide — are the whole of what the kernel would be holding on somebody else's
// behalf.
//
// The reference application's published document is the door a person actually has, so
// the case asks the document. It goes green on its own once apps/platformkit composes
// change.Module with its subject bindings and the document is regenerated; nothing
// about it asks for a particular composition, only that some composition exists.
// fromTree walks up from the test's working directory to the repository root, so the
// case reads the document where it lives rather than from a path that depends on which
// package directory go test happened to start in.
func fromTree(t *testing.T, rel string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for d := dir; ; {
		candidate := d + "/" + rel
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(d)
		if parent == d {
			t.Fatalf("%s is not in this tree", rel)
		}
		d = parent
	}
}

func TestTheReferenceAppMountsTheSixProposalOperations(t *testing.T) {
	raw, err := os.ReadFile(fromTree(t, "apps/platformkit/testdata/openapi.json"))
	if err != nil {
		t.Fatalf("read the published API: %v", err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the published API is not readable: %v", err)
	}
	found := map[string]bool{}
	for _, methods := range doc.Paths {
		for _, op := range methods {
			found[op.OperationID] = true
		}
	}
	for _, want := range operations {
		if !found[want] {
			t.Errorf("the reference app publishes no %q operation, so nobody can reach the state machine this module owns", want)
		}
	}
}
