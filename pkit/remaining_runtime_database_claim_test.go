package pkit_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestDatabaseStaysClaimedUntilBothSuccessfulLifecyclesClose(t *testing.T) {
	deployment := buildDeployment(onOneDatabase(t), app.All)
	var holders []*pkit.Runtime
	for range 2 {
		runtime, err := pkit.NewApp("collect").Use(doors, desk).Build(t.Context(), deployment)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtime.Close() })
		holders = append(holders, runtime)
	}
	for range 2 {
		if err := holders[0].Close(); err != nil {
			t.Fatal(err)
		}
	}
	next := pkit.NewApp("wishlist").Use(doors, desk)
	runtime, err := next.Build(t.Context(), deployment)
	if runtime != nil {
		_ = runtime.Close()
		t.Fatal("closing one lifecycle released the other successful lifecycle's database")
	}
	says(t, err, "collect")
	says(t, err, "T-0231")
	if err := holders[1].Close(); err != nil {
		t.Fatal(err)
	}
	runtime, err = next.Build(t.Context(), deployment)
	if err != nil {
		t.Fatalf("both lifecycles closed but their database remains claimed: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}
