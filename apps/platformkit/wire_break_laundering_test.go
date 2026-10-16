package main

// The headline gate: UPDATE_GOLDEN=1 must not write the
// golden the wire gate has just refused.
//
// Three places say this is so. wire_compatibility_test.go's header: "Nothing
// overrides a refusal, including UPDATE_GOLDEN=1". openapi_contract_test.go's
// UPDATE_GOLDEN branch: "The refusal runs before the rewrite. A break cannot be
// laundered by regenerating the file it breaks". The gate case this
// delivery says it wrote: "fails **despite** the flag — the flag cannot launder a
// break".
//
// It is the ordering that is wrong, not the rules. refuseWireBreak reports each
// problem with t.Error, which does not stop the test, and the branch falls through
// to os.WriteFile. So a break prints its B-rules and is written anyway; and the
// *next* run — the one `make check` does, with no flag — compares the served
// document against the broken golden the flag just wrote and passes. The command
// that proves this, at the commit the change is against:
//
// 	python3 - <<'EOF'                # rename the catalog's operationId in kit/app
// 	p='kit/app/app.go'; s=open(p).read()
// 	open(p,'w').write(s.replace('OperationID: "app-resources"','OperationID: "app-resourcez"',1))
// 	EOF
// 	UPDATE_GOLDEN=1 go test ./apps/platformkit -run TheOpenAPIDocumentIsTheCompositionServed -count=1   # FAILS, and writes
// 	go test     ./apps/platformkit -run TheOpenAPIDocumentIsTheCompositionServed -count=1               # ok
//
// git status then shows a committed contract with a renamed operationId and an
// operation the document no longer publishes, and make check is green over it. B1
// and B2 exist for exactly that rename.
//
// The case below asserts the correct behaviour, so it has a passing branch: with a
// golden that differs from the served document by a break, a run under
// UPDATE_GOLDEN=1 must refuse *and write nothing*, which is the only ordering that
// keeps the second run red too. It fails today because the file is rewritten.
//
// It is sequential on purpose (no t.Parallel): the case replaces
// apps/platformkit/testdata/openapi.json for its duration, and Go resumes a
// package's parallel tests only after its sequential ones have finished, so the two
// parallel readers of that golden cannot see the replaced file and its sequential
// readers cannot overlap with this one. The bytes are restored in Cleanup either
// way, so the delivery's own golden is untouched whichever way the assertion falls.

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// pinnedOperation is an address and an operationId the composition serves
// nowhere. A golden that names them disagrees with the served document by an
// address that went away, which is B1 and B2 — not by a gap the gate allows.
const pinnedOperation = "pinned-address"

func TestTheWireGateWritesNothingWhenItRefuses(t *testing.T) {
	golden := mustReadOpenAPIGolden(t)

	broken := wireMutated(t, golden, func(t *testing.T, doc map[string]any) {
		wireMap(doc["paths"])["/api/v1/app/"+pinnedOperation] = map[string]any{
			"get": map[string]any{
				"operationId": pinnedOperation,
				"summary":     "an address the composition does not serve",
				"tags":        []any{"kernel"},
				"responses":   map[string]any{},
			},
		}
	})

	// The pair is a break by the gate's own rules — the gate working, before the
	// fix and after it, and not the defect's output.
	var rules []string
	for _, problem := range breakingWireChanges(t, broken, golden) {
		rules = append(rules, problem)
	}
	joined := strings.Join(rules, "\n")
	if !strings.Contains(joined, "B1") || !strings.Contains(joined, "B2") {
		t.Fatalf("the mutated golden is not a B1/B2 break, so this case would prove nothing; the gate said: %v", rules)
	}

	if err := os.WriteFile(openapiGolden, broken, 0o644); err != nil {
		t.Fatalf("write the broken golden: %v", err)
	}
	t.Cleanup(func() {
		if err := os.WriteFile(openapiGolden, golden, 0o644); err != nil {
			t.Errorf("restore %s: %v", openapiGolden, err)
		}
	})

	cmd := exec.Command("go", "test", "./apps/platformkit", "-v",
		"-run", "TestTheOpenAPIDocumentIsTheCompositionServed", "-count=1")
	cmd.Dir = "../.." // the repository root, which is where ./apps/platformkit is
	cmd.Env = append(os.Environ(), "UPDATE_GOLDEN=1")
	out, err := cmd.CombinedOutput()
	// Reached through what the refusal prints, which is correct behaviour and not
	// the defect's: the case is expected to fail in both worlds, and only the file
	// on disk says whether the refusal was worth anything.
	if !strings.Contains(string(out), "--- FAIL: TestTheOpenAPIDocumentIsTheCompositionServed") {
		t.Fatalf("the run under UPDATE_GOLDEN=1 never reached the refusal it is judged on:\n%s", out)
	}
	if err == nil {
		t.Fatalf("the golden test passed under UPDATE_GOLDEN=1 against a golden that breaks B1 and B2:\n%s", out)
	}

	onDisk, err := os.ReadFile(openapiGolden)
	if err != nil {
		t.Fatalf("read %s after the refused run: %v", openapiGolden, err)
	}
	if !strings.Contains(string(onDisk), pinnedOperation) {
		t.Errorf("UPDATE_GOLDEN=1 wrote %s over a break it had just named (%s): the flag laundered the contract. The next run, the one make check runs, reads the served document against the broken golden this run wrote and finds them equal.",
			openapiGolden, strings.ReplaceAll(rules[0], "\n", " "))
	}
}
