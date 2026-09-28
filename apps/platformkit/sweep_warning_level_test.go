package main

// sweep_warning_level_test.go asks the one question about the hourly report that
// no other case can see: at what level it is written.
//
// TestTheHourlyWarningHasNothingToSayAboutWhatThisCompositionSeeds already
// requires the report to fire — it plants a dead grant, runs the composed
// auth-sweep job, and fails unless the record names ghost:read in the operator's
// administrator, which is why the silence it asks for afterwards means
// something. What it cannot see is the level: the recorder that case captures
// with answers Enabled for every level and matches records on the message alone,
// so a report that moved from WarnContext to DebugContext is the same record to
// it, and the whole suite stays green while the operator's log stops showing it.
// That is a mutation no case in the repository answers (round 11's Checked item
// 9 records measuring it), and the brief's "the warning stays" is about a line a
// person reading the installation's log does see.

import (
	"log/slog"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestTheSweepReportsADeadGrantAsAWarning(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)

	// Booted, not merely composed: internal.Service.warn reads the catalogue the
	// service was handed in Routes and says nothing at all when it is empty, so a
	// sweep run against an un-booted composition is silent for the wrong reason.
	start(t, cfg, c.modules, app.Options{
		Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans, Authenticate: c.auth.Authenticate,
		Role: app.All, Transport: memory.New(), Log: quiet(),
	})
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	acme := bootstrappedTenant(t, c, conn)
	plant(t, conn, acme, authcontracts.RoleAdmin, "ghost:read")

	logs := captureDefaultLog(t)
	sweep := authSweepJob(t, c.modules)
	logs.reset()
	if err := sweep.Run(t.Context(), conn); err != nil {
		t.Fatalf("auth-sweep: %v", err)
	}

	reported := logs.take(sweepWarningMessage)
	if len(reported) != 1 {
		t.Fatalf("the sweep reported the planted dead grant in %d records, want exactly one", len(reported))
	}
	record := reported[0]
	if record.Level != slog.LevelWarn {
		t.Errorf("the sweep reported the dead grant at level %s, want %s: a report the installation's log does not show is a warning that stopped",
			record.Level, slog.LevelWarn)
	}

	// The three things a person needs from the line are in the record, not only
	// in its sentence: which tenant, which role, which permission.
	var tenant, role string
	var permissions []string
	record.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "tenant":
			tenant = a.Value.String()
		case "role":
			role = a.Value.String()
		case "permissions":
			if list, ok := a.Value.Any().([]string); ok {
				permissions = list
			}
		}
		return true
	})
	if tenant != acme.Slug || role != authcontracts.RoleAdmin || !slices.Contains(permissions, "ghost:read") {
		t.Errorf("the warning named tenant=%q role=%q permissions=%v, want %s's %q and ghost:read",
			tenant, role, permissions, acme.Slug, authcontracts.RoleAdmin)
	}
}
