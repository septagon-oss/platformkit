package internal

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
)

func TestExpiredSAMLMetadataDoesNotMaskAnUnavailableProvider(t *testing.T) {
	first := authtest.NewSAMLIdP(t).MetadataXML(t)
	second := authtest.NewSAMLIdP(t).MetadataXML(t)
	var phase atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch phase.Load() {
		case 0:
			_, _ = w.Write([]byte(first))
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			_, _ = w.Write([]byte(second))
		}
	}))
	t.Cleanup(endpoint.Close)
	provider := NewSAMLProvider(Cookies{}, false, nil)
	cfg := contracts.SAMLProvider{MetadataURL: endpoint.URL}
	original, err := provider.idpMetadata(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	phase.Store(1)
	if cached, err := provider.idpMetadata(t.Context(), cfg); err != nil || cached != original {
		t.Fatalf("unexpired metadata should remain usable: descriptor=%v, error=%v", cached, err)
	}
	provider.mu.Lock()
	entry := provider.idps[endpoint.URL]
	entry.expiresAt = db.Now().Add(-time.Second)
	provider.idps[endpoint.URL] = entry
	provider.mu.Unlock()
	if stale, err := provider.idpMetadata(t.Context(), cfg); err == nil || stale != nil {
		t.Fatalf("expired metadata must not authorize an unavailable provider: descriptor=%v, error=%v", stale, err)
	}
	phase.Store(2)
	rotated, err := provider.idpMetadata(t.Context(), cfg)
	if err != nil {
		t.Fatalf("provider recovery: %v", err)
	}
	if rotated.EntityID == original.EntityID {
		t.Fatal("the recovered URL still resolves to the expired document")
	}
	if cached, err := provider.idpMetadata(t.Context(), cfg); err != nil || cached != rotated {
		t.Fatalf("the replacement document was not cached: descriptor=%v, error=%v", cached, err)
	}
}
