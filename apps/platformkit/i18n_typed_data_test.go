package main

// The brief that set the pseudo-locale gate up says what its number counts: the words a
// person reads that a translator can reach. "Data a person typed, ids and numbers are
// exempt by rule." A task's title is a sentence somebody typed into a form; no
// catalogue will ever hold it, so a report that lists it as untranslated copy hands a
// translator work that cannot be done and keeps the floor below what the copy deserves.

import (
	"bufio"
	"io"
	"os"
	"strings"
	"testing"
)

// typedRows are the sentences seed() types into the workspace before the walk, each a
// value of a field a person fills in (a task title, a content title and body, a plan
// name). None of them is copy.
var typedRows = []string{"Pump room inspection", "About us", "We fix chillers.", "Pro, billed monthly"}

func TestTheCoverageCountsNoWordAPersonTyped(t *testing.T) {
	report := runGateForTypedData(t)

	// Reached through what holds on any run, fixed or not: the gate printed its number,
	// and the record page the seeded task is read on is in the measured set — the gate
	// refuses a run whose set differs from the artefact, so the artefact names it.
	if !strings.Contains(report, "i18n coverage ") {
		t.Fatalf("the gate printed no coverage line, so it did not run:\n%s", report)
	}
	floor := readFloor(t)
	if _, ok := floor.Pages["GET /app/task/tasks/{id}"]; !ok {
		t.Fatalf("%s measures no task record page, so the typed title was never on a page", floorFile)
	}
	for _, line := range strings.Split(report, "\n") {
		if !strings.HasPrefix(line, "TEXT ") {
			continue
		}
		for _, typed := range typedRows {
			if strings.HasSuffix(line, "\""+typed+"\"") {
				t.Errorf("the gate counts what a person typed as untranslated copy: %s", line)
			}
		}
	}
}

// runGateForTypedData runs the gate as `make check-i18n` does and returns what it
// printed. The gate's report goes to standard output, which is the surface the brief's
// number is read from.
func runGateForTypedData(t *testing.T) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = writer
	var out strings.Builder
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&out, bufio.NewReader(reader))
		close(done)
	}()
	t.Run("gate", TestThePseudoLocaleGate)
	os.Stdout = saved
	_ = writer.Close()
	<-done
	return out.String()
}
