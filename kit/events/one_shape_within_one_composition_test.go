package events_test

// One process holds one shape per event name says nothing about where the two
// spellings came from: two compositions can disagree by each standing under its
// own, and one composition can disagree with itself by naming one event twice in
// one list. The second is the harder case, because nothing is standing to be
// disagreed with — a catalog that took whichever declaration arrived first would
// answer the boot "no clash" and then check every payload against one of two
// promises the composition made. Both doors refuse it, and refuse it having
// installed nothing.
//
// Two spellings of one document are one event, so the shape is compared as the
// projection rather than as the Go type, and an event named without a payload
// type twice is one unchecked promise rather than a disagreement.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/events"
)

type twiceNumber struct {
	Number int64 `json:"number"`
}

// twiceSameDocument is another Go type for the document twiceNumber describes.
type twiceSameDocument struct {
	Number int64 `json:"number"`
}

// twiceText spells the same name as a document that is not the same one.
type twiceText struct {
	Number string `json:"number"`
}

func TestOneCompositionCannotSpellOneEventTwoWays(t *testing.T) {
	standing, err := events.DeclareMore([]events.Declared{events.Declare[twiceNumber]("ledger.other")})
	if err != nil {
		t.Fatalf("install the composition this one is composed beside: %v", err)
	}
	t.Cleanup(standing)

	// The disagreement is inside the list: nothing standing refuses it, and the
	// shape the boot would have answered under is whichever entry arrived first.
	twoWays := []events.Declared{
		events.Declare[twiceNumber]("ledger.posted"),
		events.Declare[twiceText]("ledger.posted"),
	}
	if err := events.CheckDeclared(twoWays); err == nil ||
		!strings.Contains(err.Error(), "ledger.posted") ||
		!strings.Contains(err.Error(), "integer") || !strings.Contains(err.Error(), "string") {
		t.Errorf("a list that declares one event two ways was not refused by name and shape: %v", err)
	}
	release, err := events.DeclareMore(twoWays)
	if err == nil {
		if release != nil {
			t.Cleanup(release)
		}
		t.Fatal("DeclareMore installed a composition that promises one event two ways")
	}
	if !strings.Contains(err.Error(), "ledger.posted") {
		t.Errorf("the refusal did not name the event: %v", err)
	}
	// Nothing was installed by the refusal: a name the refused composition wrote is
	// one the next composition is free to mean something by, which is what an answer
	// that wrote no part of a contract has to look like.
	if err := events.CheckDeclared([]events.Declared{events.Declare[twiceText]("ledger.posted")}); err != nil {
		t.Errorf("the refused composition left a shape standing: %v", err)
	}

	// A manifest that says the same thing twice, in two types for one document or
	// with the payload left undescribed both times, declares one event, and a gate
	// that refused it would refuse the composition for a coincidence of naming.
	saidTwice := []events.Declared{
		{Name: "ledger.described"},
		{Name: "ledger.described"},
	}
	saidTwice = append(saidTwice,
		events.Declare[twiceNumber]("ledger.one"),
		events.Declare[twiceSameDocument]("ledger.one"))
	if err := events.CheckDeclared(saidTwice); err != nil {
		t.Errorf("a composition that declares one event twice in one shape was refused: %v", err)
	}
	installed, err := events.DeclareMore(saidTwice)
	if err != nil {
		t.Fatalf("a composition that declares one event twice in one shape could not start: %v", err)
	}
	t.Cleanup(installed)
}
