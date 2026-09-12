//go:build unix && !aix

package source

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// Lock the stable module directory inode, not the file replaced by rename.
func lock(root string) (func(), error) {
	directory, err := os.Open(root)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(directory.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		directory.Close()
		return nil, fmt.Errorf("source: cannot acquire module writer lock: %w", err)
	}
	return func() {
		unix.Flock(int(directory.Fd()), unix.LOCK_UN)
		directory.Close()
	}, nil
}
