package main

import (
	"regexp"
	"strings"
	"testing"
)

// The tenant's name is a value somebody typed — the operator who created the tenant
// gave it, and `bootstrap --name` is that door here. No catalogue can hold it, so a
// line that is the name, or a translated heading joined to the name by the frame's
// separator, is not copy that went around a catalogue.
func TestTheCoverageCountsNoTenantNameAsCopy(t *testing.T) {
	report := runGateForTypedData(t)
	if !strings.Contains(report, "i18n coverage ") {
		t.Fatalf("the gate printed no coverage line, so it did not run:\n%s", report)
	}
	floor := readFloor(t)
	if _, ok := floor.Pages["GET /app/task/tasks"]; !ok {
		t.Fatalf("%s measures no task list, so no framed page was read", floorFile)
	}
	translatedTitle := regexp.MustCompile(`"⟦[^"]*⟧ · Acme Corporation"$`)
	for _, line := range strings.Split(report, "\n") {
		if !strings.HasPrefix(line, "TEXT ") {
			continue
		}
		if strings.HasSuffix(line, `"Acme Corporation"`) {
			t.Errorf("the gate counts the tenant's name as untranslated copy: %s", line)
		}
		if translatedTitle.MatchString(line) {
			t.Errorf("the gate counts a translated title as untranslated because the tenant's name follows it: %s", line)
		}
	}
}
