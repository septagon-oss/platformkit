package pkit_test

// One process composes one application per database. The arrangement `Server.Host`
// refuses — two applications hosted by one process — has a wider shape than one
// `Server` value: a second `Server`, or a bare `App.Build`, names the same database
// as far away as the first one's record of it. These cases are that wider shape:
// who is refused, who is named, and who is left free to build afterwards.

import (
	"context"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestABuildOfAnotherApplicationOnAHeldDatabaseIsRefusedBeforeItOpens(t *testing.T) {
	cfg := onOneDatabase(t)
	holder, err := pkit.NewApp("collect").Use(doors, desk).Build(t.Context(), buildDeployment(cfg, app.All))
	if err != nil {
		t.Fatalf("the first application on a database was refused: %v", err)
	}
	defer holder.Close()

	second := pkit.NewApp("wishlist").Use(doors, desk)
	rt, err := second.Build(t.Context(), buildDeployment(cfg, app.All))
	if rt != nil {
		defer rt.Close()
		t.Fatal("a second application built on a database this process already has")
	}
	says(t, err, "collect")
	says(t, err, "wishlist")
	says(t, err, "T-0231")

	// The refusal is answered above the connection, so the application that met it
	// met nothing else: once the database is no longer held, this same App builds.
	if err := holder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	rt, err = second.Build(t.Context(), buildDeployment(cfg, app.All))
	if err != nil {
		t.Fatalf("the refused application could not build once the database was free: %v", err)
	}
	defer rt.Close()
}

func TestASecondLifecycleOfTheHoldingApplicationIsNotASecondApplication(t *testing.T) {
	// The claim is one application's, not one App value's: a process may hold a
	// database from two Server values, and a check keyed by anything narrower would
	// refuse the restart it is standing through.
	cfg := onOneDatabase(t)
	first, err := pkit.NewApp("collect").Use(doors, desk).Build(t.Context(), buildDeployment(cfg, app.All))
	if err != nil {
		t.Fatalf("the first lifecycle was refused: %v", err)
	}
	defer first.Close()
	again, err := pkit.NewApp("collect").Use(doors, desk).Build(t.Context(), buildDeployment(cfg, app.All))
	if err != nil {
		t.Fatalf("a second lifecycle of the application already holding the database: %v", err)
	}
	defer again.Close()
}

func TestAReleasedDatabaseIsClaimedByTheNextApplicationWithoutAWord(t *testing.T) {
	cfg := onOneDatabase(t)
	served, err := pkit.NewServer().Deploy(buildDeployment(cfg, app.All)).
		Host(pkit.NewApp("collect").Use(doors, desk), pkit.Tenant("Acme", "acme.test")).
		Build(t.Context())
	if err != nil {
		t.Fatalf("the serving process was refused: %v", err)
	}
	if _, err := pkit.NewServer().Deploy(buildDeployment(cfg, app.All)).
		Host(pkit.NewApp("wishlist").Use(doors, desk), pkit.Tenant("Globex", "globex.test")).
		Build(t.Context()); err == nil {
		t.Fatal("a second application built while the process that hosted the first was still serving")
	}
	if err := served.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	next, err := pkit.NewServer().Deploy(buildDeployment(cfg, app.All)).
		Host(pkit.NewApp("wishlist").Use(doors, desk), pkit.Tenant("Globex", "globex.test")).
		Build(t.Context())
	if err != nil {
		t.Fatalf("the next application was refused after the holding process closed: %v", err)
	}
	defer next.Close()
}

func TestTwoDatabasesAreTwoClaims(t *testing.T) {
	// The record is keyed by the database, so the two halves of a test suite — and
	// an installation that gives each app its own schema — answer separately. A key
	// too broad here would refuse one application's own second database. The second
	// case is a subtest because that is what gives it its own schema of the test
	// database, which is what "another database" means here.
	first := onOneDatabase(t)
	one, err := pkit.NewApp("collect").Use(doors, desk).Build(t.Context(), buildDeployment(first, app.All))
	if err != nil {
		t.Fatalf("the first database was refused: %v", err)
	}
	defer one.Close()
	t.Run("second", func(t *testing.T) {
		other, err := pkit.NewApp("wishlist").Use(doors, desk).
			Build(t.Context(), buildDeployment(onOneDatabase(t), app.All))
		if err != nil {
			t.Fatalf("a different database was refused as if it were the first: %v", err)
		}
		defer other.Close()
	})
}

func TestARunThatReturnedLeavesItsDatabaseToTheNextApplication(t *testing.T) {
	cfg := onOneDatabase(t)
	cfg.Server.Addr = freeAddr(t)
	// A Run refused at the engine's first connect never held a database, so this one
	// has to reach the point of serving before it is stopped: the release under test
	// is the one a live process makes.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ran := make(chan error, 1)
	go func() {
		ran <- pkit.NewApp("collect").Use(doors, desk).Run(ctx, buildDeployment(cfg, app.All), app.All)
	}()
	deadline := time.Now().Add(answersWithin)
	for !answered(t, cfg.Server.Addr) {
		select {
		case early := <-ran:
			t.Fatalf("Run returned %v without serving anything", early)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("Run served nothing within %s", answersWithin)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-ran:
		if err != nil {
			t.Fatalf("Run returned after its context was cancelled: %v", err)
		}
	case <-time.After(answersWithin):
		t.Fatal("Run did not return when its context was cancelled")
	}
	next, err := pkit.NewApp("wishlist").Use(doors, desk).Build(t.Context(), buildDeployment(cfg, app.All))
	if err != nil {
		t.Fatalf("another application after a Run returned: %v", err)
	}
	defer next.Close()
}
