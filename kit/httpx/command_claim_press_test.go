package httpx_test

// The claim is the kernel's own bookkeeping, and a database that is merely slow must
// not be allowed to refuse a person's deliberate command for it. The two cases below
// are the pair: a press that failed where nothing could have landed is pressed again,
// and a press whose answer was never read is not, because the row it may have left is
// somebody's fence. The first is the failure `make e2e` measured on a loaded host —
// one INSERT over the whole two-second budget, answered 503 to a browser that had
// already decided the command was worth pressing again.

import (
	"context"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
)

// TestACommandClaimThatWaitedTooLongIsPressedForRatherThanRefused refuses the first
// press of the claim with the SQLSTATE a lock wait answers (55P03) and lets every
// later press through. The sequence is what makes that exactly once: nextval advances
// even when the transaction it advanced inside is rolled back, which is the only
// counter here that survives the abort the refusal causes. Without the second press the
// command is refused and nothing runs, which is the harm; with an unbounded press the
// case below (TestAnUnavailableCommandStoreRefusesWithoutRunningTheCommand) would spend
// six seconds saying the same thing it says in two.
func TestACommandClaimThatWaitedTooLongIsPressedForRatherThanRefused(t *testing.T) {
	_, router, f, runs, path := setupNote(t)
	f.exec(`CREATE SEQUENCE command_claim_presses`)
	f.exec(`CREATE FUNCTION refuse_the_first_command_claim() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF nextval('command_claim_presses') = 1 THEN
				RAISE EXCEPTION 'the wait for the claim row ran out' USING ERRCODE = '55P03';
			END IF;
			RETURN NEW;
		END $$`)
	f.exec(`CREATE TRIGGER refuse_the_first_command_claim BEFORE INSERT ON platformkit_idempotency
		FOR EACH ROW EXECUTE FUNCTION refuse_the_first_command_claim()`)

	res := send(t, router, path, keyA, "pressed once, pressed again")
	if res.Code != http.StatusOK {
		t.Fatalf("one slow claim refused the command with %d: %s", res.Code, res.Body)
	}
	if n := runs.Load(); n != 1 {
		t.Errorf("the handler ran %d times, want once", n)
	}
	if n := countRows(t, f, "notes"); n != 1 {
		t.Errorf("one command whose claim failed once wrote %d rows, want one", n)
	}
	var presses int64
	err := db.RunSystem(t.Context(), f.app, syscap.NewSystemToken("kit/httpx test: the idempotency boundary"),
		func(_ context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Raw("SELECT last_value FROM command_claim_presses").Row().Scan(&presses)
		})
	if err != nil {
		t.Fatalf("read the presses: %v", err)
	}
	if presses < 2 {
		t.Errorf("the claim was pressed %d times: the 55P03 was never pressed again", presses)
	}
}
