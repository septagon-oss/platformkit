package limit

import "testing"

// MaxWindow is the sweep's own horizon and nothing else: a window a caller
// configures up to it opened no earlier than the row Purge deletes.
func TestTheLongestWindowIsTheOnePurgeKeeps(t *testing.T) {
	if MaxWindow != keep {
		t.Fatalf("MaxWindow = %s, keep = %s; the export names the sweep's horizon", MaxWindow, keep)
	}
	if MaxWindow <= 0 {
		t.Fatalf("MaxWindow = %s; a horizon is a positive duration", MaxWindow)
	}
}
