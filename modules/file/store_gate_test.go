package file_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The S3 adapter's cases (s3_test.go) fail rather than skip when no object store
// answers at PLATFORMKIT_TEST_S3_ENDPOINT, on this repository's own precedent for
// the NATS transport: a suite that quietly skips the store it ships proves nothing.
// The consequence of that choice belongs in the files that run the gate, so this
// case asks each workflow that runs `make check` the question the choice raises:
// does it put an object store where the suite dials, or name one in its own
// environment? A gate that cannot start the suite it ships is a red tick that says
// nothing about the change, and `make check` is the gate every pull request owes.
//
// `make up` starts the store (compose.yaml's s3 service), so a workstation passes;
// the CI jobs stand up their own services rather than the compose stack, which is
// why the answer has to be read in each workflow file rather than assumed from the
// Makefile's default.
func TestEveryWorkflowThatRunsMakeCheckNamesAnObjectStore(t *testing.T) {
	workflows, err := filepath.Glob("../../.github/workflows/*.yml")
	if err != nil {
		t.Fatalf("glob .github/workflows: %v", err)
	}
	gitea, err := filepath.Glob("../../.gitea/workflows/*.yml")
	if err != nil {
		t.Fatalf("glob .gitea/workflows: %v", err)
	}
	workflows = append(workflows, gitea...)
	if len(workflows) == 0 {
		t.Fatal("no workflow files found to read")
	}

	runsCheck := regexp.MustCompile(`(?m)^\s*run:\s*make\s+check(\s|$)`)
	// Three shapes answer the question: a service or step keyed s3, an image that
	// speaks S3, or an explicit endpoint the job starts itself. Anything narrower
	// (a bare "s3" substring) is a coincidence of somebody's commit SHA.
	namedService := regexp.MustCompile(`(?m)^\s*(-\s+)?s3:\s*$`)
	storeImage := regexp.MustCompile(`(?i)seaweedfs|minio/minio|quay\.io/minio`)
	namedEndpoint := regexp.MustCompile(`PLATFORMKIT_TEST_S3_ENDPOINT`)

	checked := 0
	for _, path := range workflows {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(raw)
		if !runsCheck.MatchString(text) {
			continue
		}
		checked++
		if namedService.MatchString(text) || storeImage.MatchString(text) || namedEndpoint.MatchString(text) {
			continue
		}
		t.Errorf("%s runs `make check` and names no object store: modules/file's S3 adapter's cases "+
			"fail rather than skip without one, so this job's make check is red. Start the store the "+
			"way the job starts NATS (compose.yaml's s3 service is the stack's own answer), or set "+
			"PLATFORMKIT_TEST_S3_ENDPOINT to an endpoint this job brings up.", path)
	}
	if checked == 0 {
		t.Fatal("no workflow runs `make check`, which is not a claim this case can make about this repository")
	}
	t.Logf("checked %d workflow(s) that run make check", checked)
}

// TestTheMakefileExportsTheEndpointTheStoreGateReads keeps the two halves of that
// answer honest with each other: the gate above reads the endpoint name this case
// asserts the Makefile exports, so a rename cannot leave one file answering for the
// other.
func TestTheMakefileExportsTheEndpointTheStoreGateReads(t *testing.T) {
	raw, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("read the Makefile: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "export PLATFORMKIT_TEST_S3_ENDPOINT") {
		t.Error("the Makefile does not export PLATFORMKIT_TEST_S3_ENDPOINT; the adapter's cases read it from the environment")
	}
}
