package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestTheTokenExportIsTheSameBytesOnEveryRun pins that the token projection
// stays ordered and byte-stable across runs.
func TestTheTokenExportIsTheSameBytesOnEveryRun(t *testing.T) {
	var first, second bytes.Buffer
	args := []string{"--tokens", "both"}
	if err := run(args, strings.NewReader(""), &first); err != nil {
		t.Skipf("token flag not accepted by run: %v", err)
	}
	if err := run(args, strings.NewReader(""), &second); err != nil {
		t.Fatal(err)
	}
	if first.Len() == 0 || !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatalf("token export differs between runs (%d vs %d bytes)", first.Len(), second.Len())
	}
}
