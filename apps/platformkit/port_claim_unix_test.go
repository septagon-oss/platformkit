//go:build unix && !aix

package main

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// claimFile takes the exclusive flock(2) over an open file without waiting for it. A
// lock of this kind belongs to an open file description rather than to a process, so
// a second descriptor in this process is refused as honestly as a second process is —
// which is what lets a case stand in for another run of the suite. POSIX record locks
// would not be, and are not used.
func claimFile(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, syscall.EWOULDBLOCK), errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return errPortClaimHeld
	default:
		return err
	}
}
