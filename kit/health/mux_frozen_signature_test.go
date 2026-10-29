package health

import (
	"log/slog"
	"net/http"
	"testing"
)

// TestMuxKeepsItsFrozenSignature fails to compile if Mux stops taking
// (log, checks, reports...) and returning an http.Handler.
func TestMuxKeepsItsFrozenSignature(t *testing.T) {
	var mux func(*slog.Logger, []Check, ...Report) http.Handler = Mux
	if mux == nil {
		t.Fatal("Mux is nil")
	}
}
