package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestTheTokenFlagIsAcceptedAndWritesBytes fails, rather than skips, if run
// rejects --tokens, so the determinism case beside it cannot go silent.
func TestTheTokenFlagIsAcceptedAndWritesBytes(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"--tokens", "both"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("run rejected --tokens both: %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("token export wrote no bytes")
	}
}
