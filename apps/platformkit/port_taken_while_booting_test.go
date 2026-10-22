package main

// The case behind a port that stays this case's while its application boots.
//
// freeAddr hands a served case an address whose socket it has already released, and
// the application binds that number only once it has migrated an installation. Until
// this package's process held a claim beside the port, a second run of this same
// package in another checkout could choose the same number and bind it first: the case
// then waited on somebody else's answer at its own address, or died on a bind the
// kernel refused. What that run remembered about its choices was a map inside one
// process, which is no answer to a second process.

import (
	"errors"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestAPortTakenWhileBootingIsNotTheCasesPort asks whether a second harness can take
// the port this case was given while the case is on its way to booting, and then boots.
//
// One refusal, and the two observations that make it mean something: a run that asks
// for this case's port with a descriptor of its own is refused, a separate process is
// refused too, and a port nobody holds is still grantable — so the first refusal is
// the mechanism holding, not a lock that locks nothing. Then the application boots at
// that address and answers a real request there.
//
// A run that claims nothing is not merely reported: the case binds the address and
// holds it across the boot, as another worktree does with a port nobody held, and the
// case then dies waiting for an application that could not bind its own address.
func TestAPortTakenWhileBootingIsNotTheCasesPort(t *testing.T) {
	path, cfg := configure(t)
	addr := cfg.Server.Addr
	port := portOf(t, addr)

	other, err := claimPort(port)
	switch {
	case err == nil:
		other.release()
		t.Errorf("%s came to this case unheld: another run of this suite could take it before the application binds it", addr)
		stranger, err := net.Listen("tcp", addr)
		if err != nil {
			t.Fatalf("nothing would bind %s, so the window this case asks about cannot be shown: %v", addr, err)
		}
		defer stranger.Close()
	case errors.Is(err, errPortClaimHeld):
		// The port this case's configuration names is this case's, held since the
		// choice that handed it out.
	default:
		t.Fatalf("ask who holds the claim for %s: %v", addr, err)
	}

	t.Run("a separate process is refused the same claim", func(t *testing.T) {
		claimTheSamePortFromAnotherProcess(t, port)
	})
	t.Run("a port nobody holds is still claimable", func(t *testing.T) {
		// Without this, the refusal above would be consistent with a claim that can
		// never be granted to anybody. The number comes from an :0 bind, out of the
		// band this suite claims from, so it cannot step on a live claim.
		unclaimed := aPortNobodyHolds(t)
		for attempt := range 2 {
			claim, err := claimPort(unclaimed)
			if err != nil {
				t.Fatalf("claim port %d, which nobody holds (attempt %d): %v", unclaimed, attempt+1, err)
			}
			claim.release()
		}
	})

	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	// The address this case was handed serves this case's application: the liveness
	// route, asked of the configured address itself, which names no tenant and opens no
	// transaction — the same probe TestAnEmptyDatabaseBecomesAWorkingInstallation opens
	// with. A stranger's answer at this address would have ended the wait above.
	if code, body := do(t, cfg, nil, http.MethodGet, addr, "/health", ""); code != http.StatusOK {
		t.Errorf("GET /health at %s = %d %s, want 200", addr, code, body)
	}
}

// claimTheSamePortFromAnotherProcess asks the tool every shell has for the claim of one
// port, which is a second process asking: it is refused while this case holds it. A box
// without the tool says so and skips the subtest by name; the in-process refusal, which
// needs no tool, is asked by the case above and stands either way.
func claimTheSamePortFromAnotherProcess(t *testing.T, port int) {
	t.Helper()
	tool, err := exec.LookPath("flock")
	if err != nil {
		t.Skip("flock is not on this PATH, so no separate process can ask for the claim here")
	}
	path := portClaimPath(port)
	out, err := exec.Command(tool, "-n", path, "-c", "true").CombinedOutput()
	if err == nil {
		t.Errorf("%s granted a separate process the claim this case holds; its own words:\n%s", path, out)
		return
	}
	var refused *exec.ExitError
	if !errors.As(err, &refused) {
		t.Fatalf("ask %s for the claim at %s: %v", tool, path, err)
	}
}

// portOf is the number in a 127.0.0.1:<port> address this suite was given.
func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, text, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("the address %q this case was given carries no port: %v", addr, err)
	}
	port, err := strconv.Atoi(text)
	if err != nil {
		t.Fatalf("the port %q this case was given is not a number: %v", text, err)
	}
	return port
}

// aPortNobodyHolds is a port number, held by nothing: bound, read and released. Nothing
// is left listening, because it is the state a claim is asked about.
func aPortNobodyHolds(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a port nobody holds: %v", err)
	}
	defer l.Close()
	return portOf(t, l.Addr().String())
}
