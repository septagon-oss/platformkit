package internal

import (
	"context"
	"testing"
	"time"
)

// TestTheDecodeGateBoundsHowManyFramesDecodeAtOnce asks the one question the gate
// exists to answer: never more than N frames at once, and a caller that cannot
// get in is refused rather than queued without limit. The release is what makes
// the third waiter possible, so it is checked by taking it.
func TestTheDecodeGateBoundsHowManyFramesDecodeAtOnce(t *testing.T) {
	ctx := t.Context()
	gate := newDecodeFrames(2)
	first, ok := gate.wait(ctx, time.Second)
	if !ok || gate.held() != 1 {
		t.Fatalf("the first slot: ok=%v held=%d, want it taken", ok, gate.held())
	}
	second, ok := gate.wait(ctx, time.Second)
	if !ok || gate.held() != 2 {
		first()
		t.Fatalf("the second slot: ok=%v held=%d, want two taken", ok, gate.held())
	}
	// The ceiling is closed. A third decode is refused inside its own budget and
	// takes nothing on the way out — an over-budget waiter that queued anyway is
	// the unbounded behaviour this gate replaced.
	if _, ok := gate.wait(ctx, 10*time.Millisecond); ok {
		first()
		second()
		t.Fatal("a third frame was admitted past the ceiling")
	}
	if gate.held() != 2 {
		first()
		second()
		t.Fatalf("the refused waiter left %d slots taken, want the two it found", gate.held())
	}
	second()
	if gate.held() != 1 {
		first()
		t.Fatalf("a released slot left %d taken, want one", gate.held())
	}
	third, ok := gate.wait(ctx, time.Second)
	if !ok {
		t.Fatal("a slot was not handed on after a release")
	}
	first()
	third()
	if gate.held() != 0 {
		t.Errorf("every frame went and %d slots are still taken", gate.held())
	}
}

// TestACancelledDecodeWaiterLeavesTheSlotFree checks the leak this mechanism can
// have: the request whose work was queued is gone, and a slot spent on it is a
// slot nobody is waiting for any more.
func TestACancelledDecodeWaiterLeavesTheSlotFree(t *testing.T) {
	gate := newDecodeFrames(1)
	taken, ok := gate.wait(t.Context(), time.Second)
	if !ok {
		t.Fatal("the only slot was not taken")
	}
	defer taken()
	gone, cancel := context.WithCancel(t.Context())
	cancel()
	if _, ok := gate.wait(gone, time.Minute); ok {
		t.Fatal("a cancelled caller was admitted")
	}
	if gate.held() != 1 {
		t.Fatalf("the cancelled caller changed the gate to %d taken, want the one it found", gate.held())
	}
}

// TestTheDecodeGateDefaultsToTheCoresItIsOn pins the derived bound: a deployment
// sets no number for this, so the number it gets is the machine's, and a machine
// with no usable count still decodes one frame at a time rather than none.
func TestTheDecodeGateDefaultsToTheCoresItIsOn(t *testing.T) {
	if gate := newDecodeFrames(0); cap(gate.slots) < 1 {
		t.Errorf("the derived ceiling is %d slots, want at least one", cap(gate.slots))
	}
	if gate := newDecodeFrames(-8); cap(gate.slots) < 1 {
		t.Errorf("a negative ceiling is %d slots, want a machine's worth, at least one", cap(gate.slots))
	}
}
