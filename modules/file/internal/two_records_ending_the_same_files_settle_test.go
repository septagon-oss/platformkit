package internal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// TestTwoRecordsEndingTheSameTwoPicturesBothSave is the ending half of the
// save-wide lock pass. Two published records each show two shared pictures, in
// opposite field order, and both saves drop both. Ending a use locks its file
// exclusively, so a save that took those locks one field at a time would hold one
// picture while asking for the other; the pass that names the whole record first
// takes them in one ascending order. Both saves must commit, and with no public
// use left neither picture is served to a stranger.
func TestTwoRecordsEndingTheSameTwoPicturesBothSave(t *testing.T) {
	for i, err := range endTwoSharedPictures(t, true) {
		if err != nil {
			t.Errorf("record %d dropping both pictures: %v; both saves must commit", i, err)
		}
	}
}

// endTwoSharedPictures seeds two published records over two pictures — record 0
// shows f0 in body and f1 in summary, record 1 the other way round — then runs
// both records' saves that drop everything, each held open after its first field
// until the other has either finished its first field or is waiting on a lock.
// ordered says whether each save names its whole record to the ledger first, as
// kit/rest's richtext save does through RecordUses.OrderUses.
func endTwoSharedPictures(t *testing.T, ordered bool) [2]error {
	t.Helper()
	admin, conn := dbtest.Schema(t, file.Migrations)
	svc := internal.NewService(internal.NewLocal(t.TempDir()), 1<<20, 0, 0)
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), acme), 20*time.Second)
	defer cancel()

	var ids [2]uuid.UUID
	records := [2]uuid.UUID{uuid.New(), uuid.New()}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for i := range ids {
			row, err := svc.Upload(ctx, held(tx), uploaded("shared.png"))
			if err != nil {
				return err
			}
			ids[i] = row.ID
		}
		for r, record := range records {
			body, summary := useOf(record), useOf(record)
			summary.Field = "summary"
			if _, err := svc.SetUsesShown(ctx, tx, body, []uuid.UUID{ids[r]}, true); err != nil {
				return err
			}
			if _, err := svc.SetUsesShown(ctx, tx, summary, []uuid.UUID{ids[1-r]}, true); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	pids := make(chan int, 2)
	firstField := make(chan int, 2)
	carryOn := make(chan struct{})
	done := make(chan error, 2)
	for _, record := range records {
		go func() {
			done <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				var pid int
				if err := tx.DB().Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
					return err
				}
				pids <- pid
				if ordered {
					if err := svc.OrderUses(ctx, tx, useOf(record), nil); err != nil {
						return err
					}
				}
				body, summary := useOf(record), useOf(record)
				summary.Field = "summary"
				if _, err := svc.SetUsesShown(ctx, tx, body, nil, true); err != nil {
					return err
				}
				firstField <- pid
				select {
				case <-carryOn:
				case <-ctx.Done():
					return ctx.Err()
				}
				_, err := svc.SetUsesShown(ctx, tx, summary, nil, true)
				return err
			})
		}()
	}
	// Carry on once each save has either filed its first field or is queued on a
	// row lock the other holds: a pass that serialises the two saves is the
	// passing branch, and a barrier that waited for both first fields would wait
	// on it forever.
	started := map[int]bool{<-pids: false, <-pids: false}
	for {
		for drained := false; !drained; {
			select {
			case pid := <-firstField:
				started[pid] = true
			default:
				drained = true
			}
		}
		settled := 0
		for pid, filed := range started {
			if filed {
				settled++
				continue
			}
			var waiting bool
			if err := admin.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
				WHERE pid = $1 AND wait_event_type = 'Lock')`, pid).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting {
				settled++
			}
		}
		if settled == 2 {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("neither save reached its first field nor a lock wait")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(carryOn)
	var outcome [2]error
	for i := range outcome {
		outcome[i] = <-done
	}
	if outcome[0] == nil && outcome[1] == nil {
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			for _, id := range ids {
				if _, _, err := svc.Open(ctx, tx, id, true); !errors.Is(err, crud.ErrNotFound) {
					t.Errorf("a picture no published record shows any more, anonymously = %v, want not found", err)
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return outcome
}
