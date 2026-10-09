package pkit_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestFailedRestartPreservesTheServingApplicationsDatabaseClaim(t *testing.T) {
	cfg := onOneDatabase(t)
	deployment := buildDeployment(cfg, app.All)
	serving, err := pkit.NewApp("collect").Use(doors, desk).Build(t.Context(), deployment)
	if err != nil {
		t.Fatal(err)
	}
	defer serving.Close()
	reachedTransport := false
	deployment.Transports.Memory = func() events.Transport {
		reachedTransport = true
		return nil
	}
	restart, err := pkit.NewApp("collect").Use(doors, desk).Build(t.Context(), deployment)
	if restart != nil {
		defer restart.Close()
		t.Fatal("failed restart returned a runtime")
	}
	if !reachedTransport || err == nil {
		t.Fatalf("restart did not reach and refuse its transport: %v", err)
	}
	// The failed restart may release only its own hold. The serving lifecycle
	// must still prevent another application from taking this database.
	next := pkit.NewApp("wishlist").Use(doors, desk)
	runtime, err := next.Build(t.Context(), buildDeployment(cfg, app.All))
	if runtime != nil {
		defer runtime.Close()
		t.Fatal("failed restart released the serving application's database")
	}
	says(t, err, "collect")
	says(t, err, "T-0231")
	if err := serving.Close(); err != nil {
		t.Fatal(err)
	}
	runtime, err = next.Build(t.Context(), buildDeployment(cfg, app.All))
	if err != nil {
		t.Fatalf("the last lifecycle closed but its database is still held: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}
