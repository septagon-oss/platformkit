package internal

import (
	"context"
	"runtime"
	"sync"
	"time"
)

// decode.go is the ceiling the pixel ceiling is not. contracts.ProcessImage
// bounds how large one frame may be; nothing bounds how many frames this process
// is holding at once, and a machine decoding forty forty-megapixel frames is not
// slower, it is gone. The mechanism is a buffered channel and runtime.GOMAXPROCS
// because the bound is one fixed weight: a weighted semaphore would say the same
// thing with an import and with an Acquire that can be held across a return.
//
// One gate serves the whole process, and the process is the whole of its extent
// because the resource being bounded is the machine and not the request: the
// frames decoding at once are cores and address space, and a process holding
// twice its cores in frames is not slower, it is gone. A process builds one
// service per runtime, and pkit lets one process build several runtimes, so the
// gate is a process value rather than one per service — see processDecode.
//
// It is held over one frame's decode and nothing else: no row is written
// inside a slot, and the round-trip that opens an object is asked for outside one.
// What is inside is the rest of that object arriving and the CPU that turns it into
// pixels, because the alternative — buffering the whole photograph before taking a
// slot — spends the memory this bound exists to protect in order to shorten a hold
// on a resource whose whole cost is that CPU. A pass that opened an object and then
// found the ceiling closed closes what it opened before it answers: the bound
// refuses the decode, not the file descriptor.

const (
	// decodeUploadWait is how long an accepted upload waits for a frame slot
	// before it answers 503. Long enough to sit behind a healthy decode of
	// somebody else's photograph, short enough that a saturated instance says so
	// inside the client's own patience rather than past it.
	decodeUploadWait = 5 * time.Second
	// decodeJobWait is the same wait at the background encode, which has the rest
	// of the tick to spend and no caller waiting on it. A miss there is not a
	// refusal: the row stays pending and the next round asks again.
	decodeJobWait = 30 * time.Second
)

// processDecode is the gate every file service of this process shares, opened the
// first time one asks. A service built from it cannot hand out a frame the process
// did not have: two runtimes in one process answer from the same GOMAXPROCS slots,
// which is the ceiling a deployment reads off the machine it is on.
var processDecode = sync.OnceValue(func() *decodeGate { return newDecodeFrames(0) })

// decodeGate is the process's ceiling on how many image frames are being decoded
// at once. The zero value is not usable; build one with newDecodeFrames.
type decodeGate struct{ slots chan struct{} }

// newDecodeFrames opens n slots, floor one, and takes runtime.GOMAXPROCS when n
// is not a number — the bound a deployment gets is the cores it has, which is the
// honest reading of "decode as fast as this machine is".
func newDecodeFrames(n int) *decodeGate {
	if n <= 0 {
		n = runtime.GOMAXPROCS(0)
	}
	if n < 1 {
		n = 1
	}
	return &decodeGate{slots: make(chan struct{}, n)}
}

// wait takes one slot and hands back the release that gives it up, or says no by
// d without having taken anything. A cancelled context answers no at once: the
// request that carried this work is gone, and queueing behind it serves nobody.
//
// It never queues past d and never touches the database, which is what makes it
// safe to hold across one frame's decode.
func (g *decodeGate) wait(ctx context.Context, d time.Duration) (release func(), ok bool) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case g.slots <- struct{}{}:
		return func() { <-g.slots }, true
	case <-timer.C:
		return nil, false
	case <-ctx.Done():
		return nil, false
	}
}

// held is how many slots are taken right now, which is the only way a test can
// ask what the ceiling did without timing a decode it does not control.
func (g *decodeGate) held() int { return len(g.slots) }
