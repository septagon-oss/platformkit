package view_test

import (
	"testing"
	"time"
)

// TestACalendarDayIsCutInTheZoneTheCallerLoaded keeps a day field on the zone the
// caller passed and an instant field on UTC, with a zone the host need not have.
// State.Zone is the location the caller loaded and this package loads none, so the
// day a page prints is cut by that zone alone: Build reads no /usr/share/zoneinfo,
// and a zone built here is the same kind of *time.Location as a loaded one.
// A zone carried in a name only a zone database can resolve leaves this rule guarded
// by a case that goes quiet wherever that database is absent, which is the host a
// page is built on, not the one a person typed the zone name into.
func TestACalendarDayIsCutInTheZoneTheCallerLoaded(t *testing.T) {
	t.Parallel()

	// A fixed +05:00 zone: the day after UTC's, with no zone database to consult.
	east := time.FixedZone("east", 5*3600)
	row := map[string]any{"id": "t1", "title": "n", "dueAt": "2026-07-01T23:30:00Z", "dayAt": "2026-07-01T23:30:00Z"}

	inZone := recordCells(t, domainInput(row, east))
	if got, want := inZone["dayAt"].Value, "2026-07-02"; got != want {
		t.Errorf("the day by a zone at +05:00 is %v, want %s, which is that zone's own calendar day", got, want)
	}
	if got, want := inZone["dueAt"].Value, "2026-07-01T23:30:00.000000Z"; got != want {
		t.Errorf("the instant by that same zone is %v, want %s: an instant is the moment itself, which no zone moves", got, want)
	}
	if page := build(t, domainInput(row, east)); page.Zone != "east" {
		t.Fatalf("the page names zone %q, want the zone the caller gave, so this case compares a day cut in that zone", page.Zone)
	}

	utc := recordCells(t, domainInput(row, nil))
	if got, want := utc["dayAt"].Value, "2026-07-01"; got != want {
		t.Errorf("the day with no zone loaded is %v, want %s: nil Zone is UTC", got, want)
	}
	if got, want := utc["dueAt"].Value, "2026-07-01T23:30:00.000000Z"; got != want {
		t.Errorf("the instant with no zone loaded is %v, want %s", got, want)
	}
}
