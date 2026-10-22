//go:build !unix || aix

package main

import (
	"errors"
	"os"
)

// claimFile refuses, because this platform offers no flock(2) to claim with. The
// refusal is not errPortClaimHeld: served cases end at the choice and say why, rather
// than going on with a choice that stopped claiming — which is the fault this file
// pair exists to close.
func claimFile(*os.File) error {
	return errors.New("a port claim needs a Unix advisory lock, which this platform does not offer")
}
