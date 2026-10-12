package internal

import (
	"context"
	"runtime"
	"time"
)

// decode.go is the ceiling the pixel ceiling is not. contracts.ProcessImage
// bounds how large one frame may be; nothing bounds how many frames this process
// is holding at once, and a machine decoding forty forty-megapixel frames is not
// slower, it is gone. The mechanism is a buffered channel and runtime.GOMAXPROCS
// because the bound is one fixed weight: a weighted semaphore would say the same
// thing with an import and with an Acquire that can be held across a return.
//
// One gate serves the whole process because one process builds one file service
// (pkit builds it once) and the resource being bounded is the machine, not the
// request. It is held over the CPU section alone — never across a store read,
// never across a row write — because a slot held while waiting on somebody
// else's latency is a slot that is doing nothing while it stops somebody else.

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
// safe to hold across the CPU section alone.
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
