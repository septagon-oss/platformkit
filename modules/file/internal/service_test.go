package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

var errRollback = errors.New("rolled back on purpose")

const outbox = "platformkit_outbox"

// TestServiceConforms runs the same suite the fake runs, against the real
// service, a real Postgres, a real tenant transaction and a real directory.
func TestServiceConforms(t *testing.T) {
	filetest.RunService(t, func(t *testing.T, run func(filetest.Fixture)) {
		_, conn := dbtest.Schema(t)
		dir := t.TempDir()
		store := internal.NewLocal(dir)
		svc := internal.NewService(store, filetest.Limit)
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			run(filetest.Fixture{Ctx: ctx, Tx: tx, Service: svc, Storage: store,
				Keys:      func() []string { return keysUnder(t, dir) },
				Published: func() []string { return published(t, tx) }})
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("the case's transaction: %v", err)
		}
	})
}

// keysUnder is every blob on disk, which is what the suite's Keys is for.
func keysUnder(t *testing.T, dir string) []string {
	t.Helper()
	var keys []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		keys = append(keys, d.Name())
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return keys
}

func published(t *testing.T, tx db.Tx[db.Tenant]) []string {
	t.Helper()
	var names []string
	if err := tx.DB().Raw(`SELECT name FROM ` + outbox + ` ORDER BY created_at, id`).Scan(&names).Error; err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	return names
}

// TestTheBytesGoBeforeTheRowAndTheRowGoesBeforeTheBytes is the module's whole
// design in one test.
//
// An upload that rolls back leaves bytes nobody references, which costs disk. A
// delete that rolls back leaves the row and the bytes, which costs nothing at
// all — and that asymmetry is the reason the two orders are different: the
// alternative to each is a row that points at nothing, which is a download that
// fails forever.
func TestTheBytesGoBeforeTheRowAndTheRowGoesBeforeTheBytes(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	dir := t.TempDir()
	svc := internal.NewService(internal.NewLocal(dir), filetest.Limit)

	// An upload in a transaction that then fails.
	var orphan string
	_ = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		f, err := svc.Upload(ctx, tx, contracts.Upload{
			Name: "notes.txt", ContentType: "text/plain", Declared: -1,
			Body: strings.NewReader("hello"),
		})
		if err != nil {
			return err
		}
		orphan = f.StorageKey
		return context.Canceled // whatever went wrong after the command
	})
	var rows int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM files`).Scan(&rows); err != nil {
		t.Fatalf("count the rows: %v", err)
	}
	if rows != 0 {
		t.Errorf("the rolled-back upload left %d rows", rows)
	}
	if got := keysUnder(t, dir); len(got) != 1 || got[0] != orphan {
		t.Errorf("the rolled-back upload left %v on disk, want the one orphan it costs", got)
	}

	// A delete in a transaction that then fails.
	var kept string
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		f, err := svc.Upload(ctx, tx, contracts.Upload{
			Name: "keep.txt", ContentType: "text/plain", Declared: -1,
			Body: strings.NewReader("keep me"),
		})
		kept = f.StorageKey
		return err
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	_ = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var f contracts.File
		if err := tx.DB().Where("storage_key = ?", kept).Take(&f).Error; err != nil {
			return err
		}
		if _, err := svc.Delete(ctx, tx, f.ID); err != nil {
			return err
		}
		return context.Canceled
	})
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM files`).Scan(&rows); err != nil {
		t.Fatalf("count the rows: %v", err)
	}
	if rows != 1 {
		t.Errorf("the rolled-back delete left %d rows, want the file back", rows)
	}
	if body, err := internal.NewLocal(dir).Get(t.Context(), kept); err != nil {
		t.Errorf("the rolled-back delete took the bytes with it: %v", err)
	} else {
		body.Close()
	}
}

// TestTheSubscriptionRemovesTheBlobAndConverges drives the handler the worker
// runs. It is idempotent twice over: kit/events claims each delivery, and a key
// with nothing at it is not an error — so a redelivery after a half-finished
// attempt finishes it instead of failing forever.
func TestTheSubscriptionRemovesTheBlobAndConverges(t *testing.T) {
	_, conn := dbtest.Schema(t)
	dir := t.TempDir()
	store := internal.NewLocal(dir)
	svc := internal.NewService(store, filetest.Limit)
	sub := internal.RemoveBlob(store)

	if sub.Module != "file" || sub.Name != contracts.EventDeleted {
		t.Fatalf("the subscription is %s to %s", sub.Module, sub.Name)
	}

	var payload []byte
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		f, err := svc.Upload(ctx, tx, contracts.Upload{
			Name: "notes.txt", ContentType: "text/plain", Declared: -1,
			Body: strings.NewReader("hello"),
		})
		if err != nil {
			return err
		}
		gone, err := svc.Delete(ctx, tx, f.ID)
		if err != nil {
			return err
		}
		payload, err = json.Marshal(contracts.Deleted{FileID: gone.ID, StorageKey: gone.StorageKey, Size: gone.Size})
		return err
	})
	if err != nil {
		t.Fatalf("upload and delete: %v", err)
	}
	if len(keysUnder(t, dir)) != 1 {
		t.Fatal("the delete removed the bytes in its own transaction")
	}

	// Twice, because at-least-once delivery is what a transport promises.
	for pass := range 2 {
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return sub.Handler(ctx, tx, events.Event{
				Name: contracts.EventDeleted, TenantID: acme.ID, Payload: payload,
			})
		})
		if err != nil {
			t.Fatalf("delivery %d: %v", pass, err)
		}
	}
	if got := keysUnder(t, dir); len(got) != 0 {
		t.Errorf("the bytes are still at %v after the event was handled", got)
	}
}

// TestAStorageKeyIsAUUIDAndNothingElse is the path-traversal argument, checked
// rather than asserted: the disk store refuses a key it did not mint, so a
// caller who found a way to choose one still cannot leave the directory.
func TestAStorageKeyIsAUUIDAndNothingElse(t *testing.T) {
	dir := t.TempDir()
	store := internal.NewLocal(dir)
	for _, key := range []string{
		"../escape", "..", "/etc/passwd", "", "not-a-uuid",
		"../../" + uuid.NewString(), strings.ToUpper(uuid.NewString()),
	} {
		if err := store.Put(t.Context(), key, strings.NewReader("x"), -1); err == nil {
			t.Errorf("Put(%q) was allowed", key)
		}
		if _, err := store.Get(t.Context(), key); err == nil {
			t.Errorf("Get(%q) was allowed", key)
		}
		if err := store.Delete(t.Context(), key); err == nil {
			t.Errorf("Delete(%q) was allowed", key)
		}
	}
	if got := keysUnder(t, dir); len(got) != 0 {
		t.Errorf("something reached the disk: %v", got)
	}

	// And the one it does mint works, once.
	key := uuid.NewString()
	if err := store.Put(t.Context(), key, strings.NewReader("x"), -1); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Put(t.Context(), key, strings.NewReader("y"), -1); err == nil {
		t.Error("a key that already exists was overwritten; a key is minted per upload")
	}
	body, err := store.Get(t.Context(), key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer body.Close()
	if out, _ := io.ReadAll(body); string(out) != "x" {
		t.Errorf("the bytes read back as %q", out)
	}
	if _, err := store.Get(t.Context(), uuid.NewString()); !errors.Is(err, contracts.ErrNoBlob) {
		t.Errorf("a key with nothing at it = %v, want ErrNoBlob", err)
	}
	if err := store.Delete(t.Context(), uuid.NewString()); err != nil {
		t.Errorf("deleting nothing = %v, want it to be no error at all", err)
	}
}

// TestTheUploaderIsTheCaller: Validate stamps it from the actor on the context,
// which is where kit/events reads the actor of an event from too.
func TestTheUploaderIsTheCaller(t *testing.T) {
	_, conn := dbtest.Schema(t)
	svc := internal.NewService(internal.NewLocal(t.TempDir()), filetest.Limit)
	me := uuid.New()

	err := db.Run(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), me), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			f, err := svc.Upload(ctx, tx, contracts.Upload{
				Name: "notes.txt", ContentType: "text/plain", Declared: -1,
				Body: strings.NewReader("hello"),
			})
			if err != nil {
				return err
			}
			if f.UploaderID != me {
				t.Errorf("the uploader is %s, want the caller %s", f.UploaderID, me)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
}

// TestARowThatPointsAtNothingIsAnOutage. It is the one inconsistency the split
// between a row and a blob can produce, and it is neither a 404 nor something
// the caller did: Open returns the error, which reaches huma as a 500 with the
// key in the log.
func TestARowThatPointsAtNothingIsAnOutage(t *testing.T) {
	_, conn := dbtest.Schema(t)
	dir := t.TempDir()
	store := internal.NewLocal(dir)
	svc := internal.NewService(store, filetest.Limit)

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		f, err := svc.Upload(ctx, tx, contracts.Upload{
			Name: "notes.txt", ContentType: "text/plain", Declared: -1,
			Body: strings.NewReader("hello"),
		})
		if err != nil {
			return err
		}
		if err := store.Delete(ctx, f.StorageKey); err != nil {
			return err
		}
		_, _, err = svc.Open(ctx, tx, f.ID, false)
		switch {
		case err == nil:
			t.Error("a row with no bytes behind it opened")
		case errors.Is(err, crud.ErrNotFound):
			t.Error("a row with no bytes behind it reads as not found; it is an outage, not an answer")
		case !errors.Is(err, contracts.ErrNoBlob):
			t.Errorf("Open = %v, want it to name what is missing", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
}
