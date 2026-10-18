package pkit_test

import (
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestClosingAnOldRuntimeCannotReleaseItsReplacementsDatabase(t *testing.T) {
	cfg := onOneDatabase(t)
	deployment := buildDeployment(cfg, app.All)
	old, err := pkit.NewApp("collect").Use(doors, desk).Build(t.Context(), deployment)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = old.Close() })
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	replacement, err := pkit.NewApp("wishlist").Use(doors, desk).Build(t.Context(), deployment)
	if err != nil {
		t.Fatalf("closed runtime retained its database: %v", err)
	}
	t.Cleanup(func() { _ = replacement.Close() })
	var calls sync.WaitGroup
	for range 2 {
		calls.Go(func() {
			if err := old.Close(); err != nil {
				t.Errorf("repeated Close: %v", err)
			}
		})
	}
	calls.Wait()
	next := pkit.NewApp("shop").Use(doors, desk)
	runtime, err := next.Build(t.Context(), deployment)
	if runtime != nil {
		_ = runtime.Close()
		t.Fatal("closing the old runtime released its replacement's database")
	}
	says(t, err, "wishlist")
	says(t, err, "T-0231")
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
	runtime, err = next.Build(t.Context(), deployment)
	if err != nil {
		t.Fatalf("replacement closed but its database is still held: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}
