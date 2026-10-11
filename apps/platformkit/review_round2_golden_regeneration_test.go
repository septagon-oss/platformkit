package main

// Review 2's pin over the fix to review 1's finding 1.
//
// Round 1 refused the gate because UPDATE_GOLDEN=1 wrote a document it had just
// named broken. The cure (commit 9b89aa7) makes refuseWireBreak end the test after
// naming every rule. That leaves the other half of the branch's promise — the half
// a reader of openapi_contract_test.go:86 still reads ("the flag is the fix for it",
// and the stale message's own "run with UPDATE_GOLDEN=1") — with nothing behind it:
// no case in this repository ever runs the flag against a golden the composition
// merely outgrew, because the only way to do that is to break the checked-in file
// and run the gate for real.
//
// So the fix could be made worse without anything going red. Make the refusal
// unconditional — a t.Fatalf before the B-rules, say, or the write moved behind a
// second condition — and every case here stays green: TestTheOpenAPIDocumentIsThe
// CompositionServed only runs the flag branch when the flag is set, and make check
// never sets it. What breaks is the next delivery that adds an address: it finds the
// document stale, discovers the flag refuses to fix stale, and has no way to move the
// contract at all. That is the same failure the delivery's own header names for the
// other direction — "an escape hatch a tired person can pull is the same failure as a
// golden nobody regenerates" — arrived at from the opposite side.
//
// The case below runs the flag against a golden that differs from the served
// document only additively, and asserts the two things the branch promises about it:
// the pair is stale and not broken (the gate's own rules say so, which is correct
// behaviour in both worlds), and the run passes and leaves the golden holding exactly
// the bytes the composition serves. It is reached through the refusal's absence and
// the file's bytes, never through anything a defect printed.

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// reviewPinStalePath is an address the reference composition serves and that
// deviceContractPaths does not name, so a golden missing it disagrees with the
// served document additively — the pair the flag exists to reconcile.
const reviewPinStalePath = "/api/v1/auth/password/reset"

func TestTheFlagStillRegeneratesADocumentThatIsMerelyStale(t *testing.T) {
	golden := mustReadOpenAPIGolden(t)

	stale := wireMutated(t, golden, func(t *testing.T, doc map[string]any) {
		paths := wireMap(doc["paths"])
		if _, ok := paths[reviewPinStalePath]; !ok {
			t.Fatalf("%s is not in the checked-in contract, so deleting it would not make the pair stale; pick an address the composition serves", reviewPinStalePath)
		}
		delete(paths, reviewPinStalePath)
	})

	// The pair is stale and not broken, said by the gate's own rules rather than by
	// this case's opinion. breakingWireChanges compares a golden against the served
	// document, and the checked-in golden *is* the served document — which is what
	// the no-flag branch of TestTheOpenAPIDocumentIsTheCompositionServed asserts on
	// every run of make check — so the pristine bytes stand in for what the
	// composition will serve.
	if problems := breakingWireChanges(t, stale, golden); len(problems) > 0 {
		t.Fatalf("the mutated golden is a break, not a stale pair, so the flag refusing it would be correct; the gate said: %v", problems)
	}

	if err := os.WriteFile(openapiGolden, stale, 0o644); err != nil {
		t.Fatalf("write the stale golden: %v", err)
	}
	t.Cleanup(func() {
		if err := os.WriteFile(openapiGolden, golden, 0o644); err != nil {
			t.Errorf("restore %s: %v", openapiGolden, err)
		}
	})

	cmd := exec.Command("go", "test", "./apps/platformkit",
		"-run", "TestTheOpenAPIDocumentIsTheCompositionServed", "-count=1")
	cmd.Dir = "../.." // the repository root, which is where ./apps/platformkit is
	cmd.Env = append(os.Environ(), "UPDATE_GOLDEN=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("UPDATE_GOLDEN=1 refused a document that is only stale, so a delivery that adds an address has no way to move the contract:\n%s\n(first difference the no-flag run would report: byte %d)",
			out, firstDifference(stale, golden))
	}

	onDisk, err := os.ReadFile(openapiGolden)
	if err != nil {
		t.Fatalf("read %s after the regeneration: %v", openapiGolden, err)
	}
	if string(onDisk) != string(golden) {
		t.Errorf("UPDATE_GOLDEN=1 ran over a stale golden and left %d bytes where the composition serves %d; the rewrite is not the served document, first difference at byte %d",
			len(onDisk), len(golden), firstDifference(onDisk, golden))
	}
	if strings.Contains(string(out), "B1 (breaking)") || strings.Contains(string(out), "is stale") {
		t.Errorf("the regeneration over a merely-stale pair reported a refusal or a staleness message anyway:\n%s", out)
	}
}
