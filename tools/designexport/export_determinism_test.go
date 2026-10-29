package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestTheGalleryExportIsTheSameBytesOnEveryRun pins that the rendering steps
// split out of the long functions keep a stable, ordered output.
func TestTheGalleryExportIsTheSameBytesOnEveryRun(t *testing.T) {
	var first, second bytes.Buffer
	if err := run(nil, strings.NewReader(""), &first); err != nil {
		t.Fatal(err)
	}
	if err := run(nil, strings.NewReader(""), &second); err != nil {
		t.Fatal(err)
	}
	if first.Len() == 0 || !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatalf("gallery export differs between runs (%d vs %d bytes)", first.Len(), second.Len())
	}
}
