package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestAnExportFailureNamesNoAddress is review round 10's pin. ready() prints a Report's
// error beside its message, and round 3 read the message for the collector's address
// ("cluster.local", "4317", …) and never the error. The error is the unchecked half.
func TestAnExportFailureNamesNoAddress(t *testing.T) {
	e := &exported{}
	e.note(errors.New("dial otel-collector.observability.svc.cluster.local:4317: connect: connection refused"))
	msg, err := e.Report(context.Background())
	if err == nil || strings.Contains(msg, "4317") || strings.Contains(err.Error(), "cluster.local") {
		t.Fatalf("/ready prints %q and %v: a Report's error reaches the body through ready's own concatenation, and the exporter's names a host, a port and a namespace", msg, err)
	}
}
