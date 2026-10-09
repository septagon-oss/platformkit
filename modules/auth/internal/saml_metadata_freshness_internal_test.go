package internal

// The metadata cache's two promises, asked of the cache itself: a copy fetched from a
// URL is answered until its lease runs out and fetched again after it, and a document
// too large to be a metadata document is never read into memory at all.
//
// These are asked in the package rather than at the door because the door cannot say
// them. Sign-in through a tenant configured by metadata URL takes an hour of wall clock
// to reach the moment its copy ages out — the lease is a hour because an hour is what an
// administrator experiences, not because a test wanted a number to wait for — and an
// oversized document is not one any identity provider is going to serve on purpose. What
// the sign-in does with a re-fetched document is the accepted sign-in case, which dials
// this same code through `idpMetadata`.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
)

// TestAMetadataCopyFetchedFromAURLIsFetchedAgainAfterItsLease: the entry that made a
// rotated certificate a lockout until restart. One provider, two reads, and the number of
// times its endpoint was dialled is the whole verdict.
func TestAMetadataCopyFetchedFromAURLIsFetchedAgainAfterItsLease(t *testing.T) {
	idp := authtest.NewSAMLIdP(t)
	cfg := contracts.SAMLProvider{MetadataURL: idp.MetadataURL()}

	fresh := NewSAMLProvider(Cookies{}, false, nil)
	for range 2 {
		if _, err := fresh.idpMetadata(t.Context(), cfg); err != nil {
			t.Fatalf("reading the provider's metadata: %v", err)
		}
	}
	if got := idp.MetadataHits(); got != 1 {
		t.Errorf("two reads inside the lease dialled the provider %d times, want 1: the cache is not a cache", got)
	}

	// A lease already run out is what this entry looks like an hour after it landed.
	// Waiting an hour would measure the clock and not the code.
	stale := NewSAMLProvider(Cookies{}, false, nil)
	stale.ttl = -time.Minute
	if _, err := stale.idpMetadata(t.Context(), cfg); err != nil {
		t.Fatalf("reading the provider's metadata the second time: %v", err)
	}
	before := idp.MetadataHits()
	if _, err := stale.idpMetadata(t.Context(), cfg); err != nil {
		t.Fatalf("reading the provider's metadata the third time: %v", err)
	}
	if got := idp.MetadataHits(); got != before+1 {
		t.Errorf("a copy whose lease ran out was answered %d reads on, want a fresh fetch: a tenant whose IdP rotated its certificate is refused until somebody restarts the process", got-before)
	}
}

// TestMetadataTooLargeToBeAMetadataDocumentIsNeverRead: the ceiling on an address an
// operator pasted. What the fetch is asked for is a few kilobytes of XML; a response that
// arrives at more than a megabyte is refused as the mistake it is rather than buffered to
// see whether it parses.
func TestMetadataTooLargeToBeAMetadataDocumentIsNeverRead(t *testing.T) {
	oversized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/samlmetadata+xml")
		// One byte past the ceiling: the case is the ceiling, not memory.
		w.Write([]byte(strings.Repeat("x", metadataMaxBytes+1)))
	}))
	t.Cleanup(oversized.Close)

	_, err := NewSAMLProvider(Cookies{}, false, nil).idpMetadata(t.Context(),
		contracts.SAMLProvider{MetadataURL: oversized.URL})
	if err == nil {
		t.Fatal("a metadata document one byte over its ceiling was read as a provider")
	}
	if !strings.Contains(err.Error(), "identity provider") {
		t.Errorf("the refusal says %q, want the one that names the provider as unreachable", err)
	}
}
