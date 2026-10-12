package main

import "testing"

// The refusal pages reach the catalogue for four of the five strings a person reads
// on them (the sentence, the heading, the reference's label and the way back; the
// fifth is the tab title). The floor is the number a later change may not lower, so
// it records what these pages already reach: a floor below it lets a change put two
// of those strings back into English with make check green.
func TestTheFloorHoldsTheRefusalPagesAtWhatTheyReach(t *testing.T) {
	floor := readFloor(t)
	reached := coverage{Wrapped: 4, Readable: 5}
	for _, page := range []string{"FAULT 404", "FAULT 405"} {
		have, ok := floor.Pages[page]
		if !ok {
			t.Errorf("%s records no %s", floorFile, page)
			continue
		}
		if int64(have.Wrapped)*int64(reached.Readable) < int64(reached.Wrapped)*int64(have.Readable) {
			t.Errorf("%s floors %s at %s while the page reaches %s: a change back to %s passes the gate",
				floorFile, page, ratio(have), ratio(reached), ratio(have))
		}
	}
}
