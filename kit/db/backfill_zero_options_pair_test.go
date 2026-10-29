package db_test

// Backfill is the door every rehearsal script and every operator's shell command
// still spells, and BackfillWithOptions documents its zero value as the run that
// door performs. The pair is read at a drain rather than at a refusal: the rows the
// data file placed, the history it left and the queue it emptied, schema against
// schema. A zero value that quietly became "the deployment's own budget" would move
// a drain's patience without moving a sentence anywhere.

import (
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
)

// pairSchema is the history half alone, so the owner has history and its data file is
// left to the drain — which is where planOwner leaves one, and the only shape where a
// drain door has work to do at all.
func pairSchema() fstest.MapFS {
	files := pairFiles()
	delete(files, "000002_place.up.sql")
	return files
}

// TestBackfillDrainsWhatZeroOptionsDrain: the zero MigrateOptions is Backfill's run —
// same rows placed, same ledger rows and checksums, and no cursor left in the queue.
func TestBackfillDrainsWhatZeroOptionsDrain(t *testing.T) {
	schema := db.MigrationSource{Owner: "pair", Files: pairSchema()}
	source := db.MigrationSource{Owner: "pair", Files: pairFiles()}

	first, firstAdmin := freshSchema(t)
	if err := db.Migrate(t.Context(), first, schema); err != nil {
		t.Fatal(err)
	}
	if err := db.Backfill(t.Context(), first, source); err != nil {
		t.Fatal(err)
	}
	want, wantLedger, wantQueue := drained(t, firstAdmin), ledger(t, firstAdmin), queue(t, firstAdmin)
	if want == 0 {
		t.Fatal("the fixture drained no rows; the pair would prove nothing")
	}
	if wantQueue != 0 {
		t.Fatalf("Backfill left %d cursor rows; the pair assumes a finished queue", wantQueue)
	}

	second, secondAdmin := freshSchema(t)
	if err := db.Migrate(t.Context(), second, schema); err != nil {
		t.Fatal(err)
	}
	if err := db.BackfillWithOptions(t.Context(), second, db.MigrateOptions{}, source); err != nil {
		t.Fatal(err)
	}
	if got := drained(t, secondAdmin); got != want {
		t.Errorf("Backfill placed %d rows and BackfillWithOptions with the zero options %d", want, got)
	}
	if got := ledger(t, secondAdmin); got != wantLedger {
		t.Errorf("the two drains left different history:\n%s---\n%s", wantLedger, got)
	}
	if got := queue(t, secondAdmin); got != 0 {
		t.Errorf("the zero-options drain left %d cursor rows behind, where Backfill leaves %d", got, wantQueue)
	}
}
