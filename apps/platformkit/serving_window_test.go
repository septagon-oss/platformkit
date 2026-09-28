package main

// The witness review 12 left behind.
//
// waitServing is built from two observations: this application's own log says it
// is listening at this address, and the address keeps answering while the
// application is still running. TestAListeningStrangerIsNotTakenForThisApplication
// and TestTheServingSentenceNamesOneAddress watch the first observation and the
// settle window as a whole — deleting the window hands the case a stranger's 404,
// and they say so. What neither reaches is the second observation on its own:
// deleting the dial at the address (every poll read as "answering") leaves this
// package and the whole repository green, because the stranger's case is caught a
// door earlier, by the Run that comes back with a bind error.
//
// So the claim in the comment — "whatever else holds the address would answer a
// dial, so the sentence alone is not yet proof" — is a promise no case keeps. This
// is that case, and it asks the question the promise makes: an application that
// has said it is listening, that is still running, and whose address answers
// nobody, is not yet served. The wait may not return early just because the
// sentence arrived; it returns when the address does.

import (
	"net"
	"testing"
	"time"
)

// TestTheServingWaitKeepsAskingTheAddressUntilTheAddressAnswers runs the wait
// against exactly that application: the sentence already said, the process still
// running, and the address not answering yet. The listener is opened late on
// purpose — a wait that asked the address has to wait for it, and a wait that only
// counted the settle window from the sentence returns long before.
func TestTheServingWaitKeepsAskingTheAddressUntilTheAddressAnswers(t *testing.T) {
	addr := freeAddr(t)

	// The address begins answering later than the settle window is long: the only
	// thing that can carry the wait across that gap is asking the address. The
	// listener is the suite's own and lives until the case is over; nothing in the
	// goroutine below speaks to t, because a case that has finished has no ear.
	late := servingSettle + 1500*time.Millisecond
	opened := make(chan net.Listener, 1)
	go func() {
		time.Sleep(late)
		l, err := net.Listen("tcp", addr)
		if err != nil {
			opened <- nil
			return
		}
		opened <- l
	}()
	listening := make(chan struct{})
	close(listening) // the kernel's sentence has already been written
	stopped := make(chan struct{})
	var runErr error

	started := time.Now()
	waitServing(t, addr, listening, stopped, &runErr)
	waited := time.Since(started)

	// The wait answers through the clock, not through a sentence: it cannot have
	// been served at the address before the address began answering.
	if waited < late {
		t.Errorf("waitServing called %s served after %s, before anything answered there (it began answering at %s); the sentence alone closed the wait, so the address was never asked",
			addr, waited.Round(time.Millisecond), late)
	}

	// The address the case opened late is the case's own to close.
	select {
	case l := <-opened:
		if l == nil {
			t.Fatalf("nothing ever bound %s, so the wait had no answer to find there", addr)
		}
		_ = l.Close()
	case <-time.After(3 * time.Second):
		t.Fatalf("the listener for %s never arrived", addr)
	}
}
