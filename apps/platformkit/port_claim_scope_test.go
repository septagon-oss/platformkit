package main

import (
	"strings"
	"testing"
)

// TestAClaimIsKeyedByPortAloneAndLivesOutsideTheCheckoutsTmp holds one port and
// shows a neighbouring port stays claimable, and that the claim's file is not under
// this process's own $TMPDIR, so a claim in another worktree is seen here.
func TestAClaimIsKeyedByPortAloneAndLivesOutsideTheCheckoutsTmp(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	port := aPortNobodyHolds(t)
	held, err := claimPort(port)
	if err != nil {
		t.Fatalf("claim port %d: %v", port, err)
	}
	defer held.release()
	if path := portClaimPath(port); strings.HasPrefix(path, t.TempDir()) || strings.Contains(path, "worktrees") {
		t.Fatalf("claim path %q is private to a checkout", path)
	}
	other, err := claimPort(port + 1)
	if err != nil {
		t.Fatalf("claim of port %d refused while only %d is held: %v", port+1, port, err)
	}
	other.release()
}
