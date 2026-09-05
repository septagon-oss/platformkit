package main

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestExportReportsWriterFailure(t *testing.T) {
	t.Parallel()
	if err := run(nil, failingWriter{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write failure was lost: %v", err)
	}
}

func TestExportRejectsArgumentsWithoutOutput(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	if err := run([]string{"--output=snapshot.json"}, &output); err == nil {
		t.Error("unexpected argument was accepted")
	}
	if output.Len() != 0 {
		t.Error("rejected invocation wrote a partial snapshot")
	}
}
