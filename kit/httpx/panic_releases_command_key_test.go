package httpx_test

import (
	"context"
	"net/http"
	"testing"
)

func TestAPanickedCommandReleasesItsKeyAfterRollback(t *testing.T) {
	api, router, f, runs, _ := setupNote(t)
	writeNote := note(runs)
	mount(t, api, "panic-note", "/panic-note", true, func(ctx context.Context, in *noteIn) (*noteOut, error) {
		out, err := writeNote(ctx, in)
		if err != nil {
			return nil, err
		}
		if runs.Load() == 1 {
			panic("the command failed before committing")
		}
		return out, nil
	})
	path := at(api, "/panic-note")
	first := send(t, router, path, keyA, "kept only on retry")
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("panicked command returned %d: %s", first.Code, first.Body)
	}
	if n := countRows(t, f, "notes"); n != 0 {
		t.Fatalf("panicked command committed %d notes", n)
	}
	if n := heldRows(t, f, true); n != 0 {
		t.Errorf("panicked command left %d claims after rollback; want none", n)
	}
	second := send(t, router, path, keyA, "kept only on retry")
	if second.Code != http.StatusOK {
		t.Errorf("retry after rollback returned %d: %s; want 200", second.Code, second.Body)
	}
	if n := countRows(t, f, "notes"); n != 1 {
		t.Errorf("retry committed %d notes; want one", n)
	}
}
