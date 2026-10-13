package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestAnExportFailureNamesNoAddress covers the unchecked half of ready(): it prints a
// Report's error beside its message, and what was read for the collector's address
// ("cluster.local", "4317", …) was the message, never the error.
func TestAnExportFailureNamesNoAddress(t *testing.T) {
	e := &exported{}
	e.note(errors.New("dial otel-collector.observability.svc.cluster.local:4317: connect: connection refused"))
	msg, err := e.Report(context.Background())
	if err == nil || strings.Contains(msg, "4317") || strings.Contains(err.Error(), "cluster.local") {
		t.Fatalf("/ready prints %q and %v: a Report's error reaches the body through ready's own concatenation, and the exporter's names a host, a port and a namespace", msg, err)
	}
}
