package main

// ui/legible promises that what the number declines to count is never hidden: "a
// report prints them, because a number that quietly chose not to count something is
// not a measurement", and its README: "A report lists every exempt string". The gate's
// second line names how many strings were not copy. Both are the reviewer's only view
// of the exemption rule at work on the shipped pages.

import (
	"bufio"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var notCopyLine = regexp.MustCompile(`(\d+) strings not copy`)

func TestTheGateReportsTheStringsItDeclinedToCount(t *testing.T) {
	report := runGateForExemptions(t)

	// Reached through the line every run prints, fixed or not.
	match := notCopyLine.FindStringSubmatch(report)
	if match == nil {
		t.Fatalf("the gate printed no \"strings not copy\" figure:\n%s", report)
	}
	// Every record screen shows its row's id and every list a timestamp, which the
	// exemption rule declines to count, so a figure of zero is the wrong number.
	if n, _ := strconv.Atoi(match[1]); n == 0 {
		t.Errorf("the report says %q, yet every record page shows its row's id, an exempt string", match[0])
	}
	// And the strings themselves are listed, so what the number did not count stays
	// reviewable: the bootstrap administrator's address is on the user record page and
	// is exempt as an email.
	listed := false
	for _, line := range strings.Split(report, "\n") {
		if !strings.HasPrefix(line, "TEXT ") && strings.Contains(line, "GET /app/user/users/{id}") &&
			strings.Contains(line, adminEmail) {
			listed = true
		}
	}
	if !listed {
		t.Errorf("the report lists no exempt string; %s is on the user record page and is not named", adminEmail)
	}
}

func runGateForExemptions(t *testing.T) string {
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
