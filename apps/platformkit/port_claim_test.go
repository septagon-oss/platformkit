package main

// The claim that holds a chosen port for the case that chose it.
//
// freeAddr proves a port is free by binding it and releasing it, because the
// application under test binds that number itself. Between the release and that bind
// sits a migration of a whole installation, and a second run of this package — another
// checkout, the same host, the same minute — could be given the same number inside it:
// the record of what was chosen lived in a map inside one process.

// The claim answers that. One empty file per port number, in a directory every
// checkout resolves identically, locked exclusively without waiting and held open
// until the case ends. The kernel is the arbiter, and a run it refuses takes the next
// port. No lease and no sweep of stale claims are needed: a lock lives in an open file
// description, so a killed run leaves an empty file, which is the free state.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
)

// portClaimsHere is where claims live. os.TempDir() honours $TMPDIR, which a harness
// that runs several checkouts side by side keeps one per checkout, and a directory
// only one run can see claims nothing. /tmp is world-writable and sticky: every run
// may create a name in it and none may remove another's.
const portClaimsHere = "/tmp"

// errPortClaimHeld says somebody else holds this port now. It is the answer that sends
// the choice on to the next port. Anything else is the mechanism itself failing, which
// ends a case rather than being skipped over: a choice that quietly stopped claiming
// would be this suite sharing ports again with nothing to say so.
var errPortClaimHeld = errors.New("the port is claimed by another test process")

// portClaimPath is the claim's name for one port. The number alone keys it: this
// suite serves the loopback and nothing else, so a different spelling of the address
// is the same argument about the same socket.
func portClaimPath(port int) string {
	return portClaimsHere + "/platformkit-test-port-" + strconv.Itoa(port) + ".lock"
}

// portClaim is one held claim — the open file its lock lives in. Releasing it is a
// close, which is what lets go of an flock. The file is never removed: unlinking a
// name a peer holds locked leaves that peer holding a lock nobody else can reach and
// hands the next claimant a fresh inode, so one port has two holders. Unlinking is
// that fault, not a cure for anything.
type portClaim struct{ file *os.File }

// claimPort asks for the exclusive claim to one port and does not wait for it.
//
// A claim another process holds comes back as errPortClaimHeld with no claim, and so
// does a claim file this process may not open: a name that cannot be opened is a name
// somebody else reached first, and a second file for one port would hand two runs the
// same number. Every other failure comes back as an error, because it belongs to the
// directory rather than to the port.
func claimPort(port int) (*portClaim, error) {
	path := portClaimPath(port)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil, errPortClaimHeld
		}
		return nil, fmt.Errorf("open the claim for port %d at %s: %w", port, path, err)
	}
	// The creating mode above is masked by whoever wrote the file first, and a umask
	// that hides it from the group leaves a developer's own run unable to open a port
	// another developer's run chose. A file not ours to chmod is already open for us to
	// lock, and the open above said which of these two this is.
	_ = file.Chmod(0o644)
	if err := claimFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &portClaim{file: file}, nil
}

// release gives the port back, once. The choice registers it as the case's cleanup
// when it hands out the port and calls it itself when the port turns out unusable, and
// a second call has nothing left to give.
func (c *portClaim) release() {
	if c == nil || c.file == nil {
		return
	}
	file := c.file
	c.file = nil
	_ = file.Close()
}
