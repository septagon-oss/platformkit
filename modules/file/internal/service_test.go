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
	"time"

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
		svc := internal.NewService(store, filetest.Limit, 0)
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
	svc := internal.NewService(internal.NewLocal(dir), filetest.Limit, 0)

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
	svc := internal.NewService(store, filetest.Limit, 0)
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
	svc := internal.NewService(internal.NewLocal(t.TempDir()), filetest.Limit, 0)
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
	svc := internal.NewService(store, filetest.Limit, 0)

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

// TestATenantCannotFillTheDisk. The per-upload limit bounds one mistake; the
// quota bounds a campaign of them, which is what an anonymous sign-up is.
//
// The usage is a sum over the tenant's own rows, so a delete gives the room
// back with no bookkeeping to get wrong, and the check is under row-level
// security, so one tenant's uploads are not counted against another's.
func TestATenantCannotFillTheDisk(t *testing.T) {
	_, conn := dbtest.Schema(t)
	// Room for ten bytes, and a per-upload limit far past it, so the refusal
	// below can only be the quota.
	svc := internal.NewService(internal.NewLocal(t.TempDir()), filetest.Limit, 10)
	other := tenancy.Tenant{ID: uuid.New(), Slug: "other", Name: "Other"}

	put := func(t *testing.T, who tenancy.Tenant, body string) error {
		t.Helper()
		return db.Run(tenancy.WithTenant(t.Context(), who), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.Upload(ctx, tx, contracts.Upload{
				Name: "x.bin", ContentType: "application/octet-stream",
				Declared: -1, Body: strings.NewReader(body),
			})
			return err
		})
	}

	if err := put(t, acme, "12345678"); err != nil {
		t.Fatalf("eight bytes of ten: %v", err)
	}
	// Two bytes left, so five is past it. The answer names the quota rather
	// than the deployment's limit, because "delete something" and "send
	// something smaller" are different remedies.
	if err := put(t, acme, "12345"); !errors.Is(err, contracts.ErrQuota) {
		t.Errorf("an upload past the quota = %v, want ErrQuota", err)
	}
	// Exactly the room left still fits, which is what makes it a boundary.
	if err := put(t, acme, "12"); err != nil {
		t.Errorf("the last two bytes of the quota: %v", err)
	}
	if err := put(t, acme, "1"); !errors.Is(err, contracts.ErrQuota) {
		t.Errorf("an upload with no room at all = %v, want ErrQuota", err)
	}
	// Another tenant is not charged for this one's disk.
	if err := put(t, other, "12345678"); err != nil {
		t.Errorf("another tenant's first upload: %v", err)
	}
}

// TestTheOrphansAreSweptUp is the other half of "the bytes go before the row":
// an upload whose transaction fails leaves a blob nobody references, and
// nothing in the database records that it exists. So the sweep starts at the
// store and asks the database which keys it knows, under system access, because
// the question crosses every tenant by construction.
func TestTheOrphansAreSweptUp(t *testing.T) {
	_, conn := dbtest.Schema(t)
	dir := t.TempDir()
	store := internal.NewLocal(dir)
	svc := internal.NewService(store, filetest.Limit, 0)

	// One file that committed, and one whose transaction did not.
	var kept string
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		f, err := svc.Upload(ctx, tx, contracts.Upload{Name: "keep.txt", ContentType: "text/plain", Declared: -1, Body: strings.NewReader("keep")})
		kept = f.StorageKey
		return err
	}); err != nil {
		t.Fatalf("the upload that commits: %v", err)
	}
	_ = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := svc.Upload(ctx, tx, contracts.Upload{Name: "lost.txt", ContentType: "text/plain", Declared: -1, Body: strings.NewReader("lost")}); err != nil {
			return err
		}
		return context.Canceled
	})
	if got := keysUnder(t, dir); len(got) != 2 {
		t.Fatalf("the store holds %d blobs, want the kept one and the orphan", len(got))
	}

	sweep := internal.NewReconcile(store, time.Second)
	jobs := sweep.Jobs()
	if len(jobs) != 1 || jobs[0].Name != "file-reconcile" {
		t.Fatalf("the module schedules %v", jobs)
	}
	// Without the capability it deletes nothing and says why: a job that swept
	// under a tenant transaction would see one tenant's rows and delete every
	// other tenant's blobs.
	if err := jobs[0].Run(t.Context(), conn); err == nil {
		t.Error("the sweep ran with no system capability")
	}
	sweep.Use(dbtest.SystemToken())

	// Nothing is old enough yet, and that is the safety property: an upload in
	// flight has bytes and no row.
	if err := jobs[0].Run(t.Context(), conn); err != nil {
		t.Fatalf("the sweep: %v", err)
	}
	if got := keysUnder(t, dir); len(got) != 2 {
		t.Fatalf("the sweep removed a blob written a moment ago: %d left", len(got))
	}

	// Age both past the hour and run it again.
	old := time.Now().Add(-2 * time.Hour)
	for _, key := range keysUnder(t, dir) {
		at := filepath.Join(dir, key[:2], key)
		if err := os.Chtimes(at, old, old); err != nil {
			t.Fatalf("age %s: %v", at, err)
		}
	}
	if err := jobs[0].Run(t.Context(), conn); err != nil {
		t.Fatalf("the sweep: %v", err)
	}
	left := keysUnder(t, dir)
	if len(left) != 1 || left[0] != kept {
		t.Errorf("the sweep left %v, want only the blob a row names", left)
	}
}
