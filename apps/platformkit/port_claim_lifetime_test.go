package main

import (
	"errors"
	"testing"
)

// TestAClaimedPortIsHeldUntilReleasedAndThenFree asks the claim for its two ends: a
// second claimant is refused while the first holds the port, and is granted it once
// the first has released, however many times release is called.
func TestAClaimedPortIsHeldUntilReleasedAndThenFree(t *testing.T) {
	port := aPortNobodyHolds(t)
	first, err := claimPort(port)
	if err != nil {
		t.Fatalf("claim port %d: %v", port, err)
	}
	if second, err := claimPort(port); !errors.Is(err, errPortClaimHeld) {
		second.release()
		t.Fatalf("second claim of port %d while held = %v, want errPortClaimHeld", port, err)
	}
	first.release()
	first.release()
	again, err := claimPort(port)
	if err != nil {
		t.Fatalf("claim port %d after release: %v", port, err)
	}
	again.release()
}
