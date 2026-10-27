package view_test

import (
	"encoding/json"
	"testing"
	"time"
)

// TestAPageBuiltFromAClockInANamedZoneStampsItsInstantInUTC keeps builtAt in UTC
// however the composition's clock reports itself. State.Now anchors every relative
// phrase on the page, and the page states the rule about it: builtAt is that instant
// in UTC. The caller owns the clock and this package loads no zone, so converting the
// instant is the builder's own work. Left unconverted, the page prints its stamp with
// the clock's offset while every instant inside its fields prints in UTC, spelling one
// moment two ways for the shell to reconcile.
func TestAPageBuiltFromAClockInANamedZoneStampsItsInstantInUTC(t *testing.T) {
	t.Parallel()

	// A fixed +05:00 zone, so the case needs no zone database and no host's tzdata.
	carrier := time.FixedZone("named", 5*3600)
	in := listInput()
	in.State.Zone = carrier
	in.State.Now = now.In(carrier)
	page := build(t, in)

	if page.Zone != "named" {
		t.Fatalf("the page names zone %q, want the zone the caller gave, so this case reads a page built in a named zone", page.Zone)
	}
	if !page.BuiltAt.Equal(now) {
		t.Fatalf("builtAt %s is not the instant the clock carried (%s), so this case compares another instant", page.BuiltAt, now)
	}
	if _, offset := page.BuiltAt.Zone(); offset != 0 {
		t.Errorf("builtAt %s carries a %d-second offset: the page stamps its instant in UTC wherever the clock wore it", page.BuiltAt, offset)
	}
	wanted := now.UTC().Format(time.RFC3339)
	var wire struct {
		BuiltAt string `json:"builtAt"`
	}
	if err := json.Unmarshal(bytes(t, page), &wire); err != nil {
		t.Fatalf("the page does not decode: %v", err)
	}
	if wire.BuiltAt != wanted {
		t.Errorf("the page answers \"builtAt\":%q on the wire, want the same instant in UTC (%q)", wire.BuiltAt, wanted)
	}
}
