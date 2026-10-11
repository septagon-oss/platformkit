package pkit_test

import (
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestConcurrentClosesReleaseOnlyTheirOwnLifecycles(t *testing.T) {
	deployment := buildDeployment(onOneDatabase(t), app.All)
	var holders []*pkit.Runtime
	for range 3 {
		runtime, err := pkit.NewApp("collect").Use(doors, desk).Build(t.Context(), deployment)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtime.Close() })
		holders = append(holders, runtime)
	}
	start := make(chan struct{})
	var calls sync.WaitGroup
	for _, runtime := range holders[:2] {
		for range 2 {
			calls.Go(func() {
				<-start
				if err := runtime.Close(); err != nil {
					t.Errorf("concurrent Close: %v", err)
				}
			})
		}
	}
	close(start)
	calls.Wait()
	next := pkit.NewApp("wishlist").Use(doors, desk)
	runtime, err := next.Build(t.Context(), deployment)
	if runtime != nil {
		_ = runtime.Close()
		t.Fatal("concurrent closes released the surviving lifecycle's database")
	}
	says(t, err, "collect")
	says(t, err, "T-0231")
	if err := holders[2].Close(); err != nil {
		t.Fatal(err)
	}
	runtime, err = next.Build(t.Context(), deployment)
	if err != nil {
		t.Fatalf("all lifecycles closed but their database is still held: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}
