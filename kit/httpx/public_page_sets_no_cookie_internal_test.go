//go:build !race

package httpx

// Built where its rig is: public_page_allocations_internal_test.go, not under -race.

import (
	"net/http"
	"testing"
)

// TestAPublicPageSetsNoCookie pins that the cheaper chain still answers an
// anonymous page without issuing a session or any other cookie.
func TestAPublicPageSetsNoCookie(t *testing.T) {
	rig := wiredPublicPageRig(t)
	rig.run()
	if rig.writer.status != http.StatusOK {
		t.Fatalf("the public page answered %d, want 200", rig.writer.status)
	}
	if got := rig.writer.header.Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("a public page set cookies %q, want none", got)
	}
}
