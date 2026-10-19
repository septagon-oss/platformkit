package app

import (
	"testing"
	"time"
)

func TestAPermissionStreamReleasesTheAuthorizersTransaction(t *testing.T) {
	conn, _, _ := openStreamRoute(t, time.Hour)
	// Admission has completed before resync reaches the client. Let its final
	// commit finish before checking a pool used by no other request or job.
	time.Sleep(100 * time.Millisecond)
	if held := conn.Stats().InUse; held != 0 {
		t.Errorf("permission stream holds %d pool connections after admission; want 0", held)
	}
}
