package pkit_test

// One process composes one application per database, and the record that says so
// (pkit/claims.go) holds the name and the composition, because the name alone does
// not say which application this is. A rolling restart repeats both and is recorded
// without a word; a second boot with the held name and a different list of modules
// is a second application wearing it, and it is refused while the first one holds
// the database. Closing the lifecycles that hold it is the way to change a
// composition, which is what a deployment that is about to run different modules
// does anyway.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestACompositionChangeWaitsForItsDatabase(t *testing.T) {
	cfg := onOneDatabase(t)
	ledger := postedModule[postedNumber]("ledger")
	live, err := pkit.NewApp("collect").Use(doors, desk, ledger).
		Build(t.Context(), buildDeployment(cfg, app.All))
	if err != nil {
		t.Fatalf("the first lifecycle was refused: %v", err)
	}
	defer live.Close()

	again, err := pkit.NewApp("collect").Use(doors, desk, ledger).
		Build(t.Context(), buildDeployment(cfg, app.All))
	if err != nil {
		t.Fatalf("a second lifecycle of the same composition was refused: %v", err)
	}
	defer again.Close()

	changed, err := pkit.NewApp("collect").Use(doors, desk).
		Build(t.Context(), buildDeployment(cfg, app.All))
	if changed != nil {
		defer changed.Close()
	}
	if changed != nil || err == nil {
		t.Fatalf("a different composition started under the name holding the database: runtime=%v, err=%v", changed, err)
	}
	says(t, err, "collect")
	says(t, err, "doors, desk, ledger")
	says(t, err, "closes the lifecycle holding it first")

	// The refusal took nothing: with both lifecycles released, the composition that
	// met it is the next one on the database.
	if err := again.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := live.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	restarted, err := pkit.NewApp("collect").Use(doors, desk).
		Build(t.Context(), buildDeployment(cfg, app.All))
	if err != nil {
		t.Fatalf("the changed composition was refused after the database came free: %v", err)
	}
	defer restarted.Close()
}
