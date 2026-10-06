package httpx_test

// The fence that spends a key whose COMMIT was never answered is only right because
// the commit next door is not fenced. A COMMIT Postgres answered with an error — the
// deferred constraint this transaction existed to defer — is a statement it rejected,
// the transaction is aborted, and nothing is in the database. A claim kept over that
// would spend a key on a command that provably never ran: the caller's retry would be
// refused for a day for work the database rolled back, which is the same fault as the
// double apply, aimed the other way. So this is the pair of
// TestALostCommitAcknowledgementDoesNotReleaseTheCommandKey, and the two together are
// the whole of what a failed commit may and may not do to a key.

import (
	"context"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/tenancy"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestACommitTheDatabaseRefusedReleasesTheCommandKey(t *testing.T) {
	api, router, f, runs, _ := setupNote(t)
	f.exec(`ALTER TABLE notes ADD CONSTRAINT notes_written_once UNIQUE (tenant_id, body)
		DEFERRABLE INITIALLY DEFERRED`)
	writeNote := note(runs)
	mount(t, api, "refused-commit-note", "/refused-notes", true,
		func(ctx context.Context, in *noteIn) (*noteOut, error) {
			out, err := writeNote(ctx, in)
			if err != nil {
				return nil, err
			}
			if runs.Load() == 1 {
				// The same note a second time in the same transaction. The constraint is
				// deferred, so no statement fails: the database refuses the COMMIT.
				tx, _ := httpx.TxFrom(ctx)
				tn, _ := tenancy.FromContext(ctx)
				if err := tx.DB().Exec("INSERT INTO notes (tenant_id, body) VALUES (?, ?)",
					tn.ID, in.Body.Text).Error; err != nil {
					return nil, err
				}
			}
			return out, nil
		})
	path := at(api, "/refused-notes")

	first := send(t, router, path, keyA, "charged once")
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("a command whose commit was refused answered %d: want 500: %s", first.Code, first.Body)
	}
	if n := countRows(t, f, "notes"); n != 0 {
		t.Fatalf("a refused commit left %d notes behind; want none", n)
	}
	if n := heldRows(t, f, true); n != 0 {
		t.Fatalf("a commit the database refused held %d claims; want none, because the work is not in the database", n)
	}
	retry := send(t, router, path, keyA, "charged once")
	if retry.Code != http.StatusOK {
		t.Errorf("the same key after a refused commit answered %d, want 200: %s", retry.Code, retry.Body)
	}
	if n := countRows(t, f, "notes"); n != 1 {
		t.Errorf("the retry committed %d notes; want one", n)
	}
}
