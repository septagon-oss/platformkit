package pkit_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestTransportRefusalReleasesDatabaseAndEventClaims(t *testing.T) {
	cfg := onOneDatabase(t)
	deployment := buildDeployment(cfg, app.All)
	reachedTransport := false
	deployment.Transports.Memory = func() events.Transport {
		reachedTransport = true
		return nil
	}
	runtime, err := pkit.NewApp("collect").Use(doors, desk, postedModule[postedNumber]("ledger")).
		Build(t.Context(), deployment)
	if runtime != nil {
		defer runtime.Close()
		t.Fatal("failed transport returned a runtime")
	}
	if !reachedTransport {
		t.Fatalf("build did not reach the transport factory: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "transport") {
		t.Fatalf("missing transport refusal: %v", err)
	}
	admin := dbtest.Open(t, cfg.Database.MigrateURL)
	var count int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("failed boot emitted %d events", count)
	}
	// Both the database name and the event shape must be free after a boot
	// fails, even when that boot reached its transport after migrating.
	replacement, err := pkit.NewApp("replacement").Use(doors, desk, postedModule[postedText]("ledger")).
		Build(t.Context(), buildDeployment(cfg, app.All))
	if err != nil {
		t.Fatalf("failed boot retained a database or event claim: %v", err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
}
