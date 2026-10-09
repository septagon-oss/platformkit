package internal

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// reconcileCron is a quarter to four in the morning, daily. An orphan costs
// disk and nothing else, so this is the least urgent work in the application
// and it runs when nothing else does.
const reconcileCron = "45 3 * * *"

// orphanAge is how old a blob has to be before it is considered abandoned. An
// upload writes the bytes and then the row, and the two are one HTTP request
// apart, so an hour is four orders of magnitude more slack than the window it
// is protecting — and the cost of getting it wrong is deleting the bytes of a
// file somebody has just uploaded.
const orphanAge = time.Hour

// Reconcile removes blobs no row references.
//
// It is the store-facing sweep, and it exists because of the ordering the module
// chose on purpose: an upload writes the bytes first, so a transaction that fails
// afterwards leaves bytes nobody references. That is the right trade — the other
// order leaves a row whose download fails forever — and this is the cost of it,
// swept up once a day.
//
// It is not jobs.PerTenant, and the reason is the shape of the problem rather
// than a preference. PerTenant walks the tenants and hands each one a tenant
// transaction; the ones this job is looking for are exactly the ones no row
// names, so there is no tenant to walk to them from. It starts at the store and
// asks the database which names are known, under system access, because the
// question crosses every tenant by construction. That is why the port half it
// uses — contracts.Reconciler — takes a db.Tx[db.System] and its predecessor did
// not: an installation-wide listing is a cross-tenant read, and a sweep is the
// only thing allowed to ask for one, so the type says so.
//
// The retention sweep (retention.go) is the other direction — it starts at the
// rows and runs inside each tenant's own transaction — and the two are separate
// jobs precisely because they start at opposite ends.
type Reconcile struct {
	storage contracts.Storage
	every   time.Duration
	token   tenancy.SystemToken
}

// NewReconcile prepares the sweep. every replaces the daily schedule for a test.
func NewReconcile(storage contracts.Storage, every time.Duration) *Reconcile {
	return &Reconcile{storage: storage, every: every}
}

// Use hands over the capability that opens a cross-tenant transaction. It is
// called from Module.Routes, which is the one moment the kernel offers a token
// — a job is constructed before the API exists — and the job does nothing
// without one, which is a boot-order mistake rather than a request-time
// condition, so it says so in the log rather than deleting anything.
func (r *Reconcile) Use(token tenancy.SystemToken) { r.token = token }

// Jobs is the daily sweep, or none: a Storage that cannot enumerate what it
// holds cannot be reconciled, and a job that could never do anything is worse
// than no job because it appears in the schedule.
func (r *Reconcile) Jobs() []jobs.Job {
	if _, ok := r.storage.(contracts.Reconciler); !ok {
		return nil
	}
	job := jobs.Job{Name: "file-reconcile", Cron: reconcileCron, Run: r.run}
	if r.every > 0 {
		job.Cron, job.Every = "", r.every
	}
	return []jobs.Job{job}
}

// batch is how many blobs one query asks about. A store with a million blobs is
// a million-element IN list otherwise, which Postgres will accept and nobody
// should send.
const batch = 500

// appSegment is the store's answer to a question its listing cannot answer: which
// app's bytes it holds. A store that keeps one app in its own segment of a volume
// or bucket every app on the server shares knows that (`local.go`, `Local.app`),
// and a sweep has to ask it, because the listing says which directories this
// adapter walked and never which app the tenants inside them belong to. The
// deployment of one app — the store that names no segment — holds the un-prefixed
// position, which is every app's, and it is the answer for a store that does not
// implement this: the empty Name, which is what `NewLocal` builds.
//
// It is a capability the way `contracts.Reconciler`, `Prover` and `Signer` are:
// asserted where it is used, so a store written outside this module satisfies it
// by having the method and never has to name the interface.
type appSegment interface {
	App() appname.Name
}

func (r *Reconcile) run(ctx context.Context, conn *db.Conn) error {
	reconciler, ok := r.storage.(contracts.Reconciler)
	if !ok {
		return nil
	}
	if r.token == nil {
		return fmt.Errorf("file: the reconciliation was never given the system capability; Module.Routes hands it over")
	}
	// Whose bytes the store says these are; whose tenants they sit under is the
	// question this transaction answers, one chunk at a time with the rest.
	app := appOf(r.storage)
	var removed, foreign int
	err := db.RunSystem(ctx, conn, r.token, func(ctx context.Context, tx db.Tx[db.System]) error {
		blobs, err := reconciler.Blobs(ctx, tx, time.Now().Add(-orphanAge))
		if err != nil {
			return fmt.Errorf("file: list what the store holds: %w", err)
		}
		for _, chunk := range chunks(blobs, batch) {
			known, err := knownKeys(tx, chunk)
			if err != nil {
				return fmt.Errorf("file: which of these blobs are known: %w", err)
			}
			others, err := foreignTenants(tx, chunk, app)
			if err != nil {
				return fmt.Errorf("file: which of these tenants this app holds: %w", err)
			}
			for _, b := range chunk {
				if slices.Contains(known, b.Key.String()) {
					continue
				}
				if slices.Contains(others, b.TenantID) {
					// Another app's tenant: the bytes are aged and unclaimed, and
					// they are still not this app's to remove.
					foreign++
					continue
				}
				// The removal is in the same system transaction as the question
				// that justified it, so a sweep that died halfway reports as
				// failed and retries rather than reporting success over work it
				// did not do. Removing what is gone is success, so a retry is
				// cheap.
				if err := reconciler.RemoveBlob(ctx, tx, b); err != nil {
					return fmt.Errorf("file: remove the orphan %s: %w", b.Key, err)
				}
				removed++
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if removed > 0 {
		slog.InfoContext(ctx, "file: removed blobs no row references", "count", removed)
	}
	if foreign > 0 {
		// Left on the volume on purpose, and said out loud: an operator looking at
		// a directory that will not empty has to be able to see that a sweep ran
		// over it and declined these bytes.
		slog.InfoContext(ctx, "file: left blobs whose tenant another app holds",
			"count", foreign, "app", app.String())
	}
	return nil
}

// foreignTenants names which of these blobs' tenants some other app holds, read
// from tenants.app — the same column, compared the same way, as the delivery
// check `kit/events` runs (holdsTenant) and the relay's claim on its own app's
// rows, because the three halves of one rule written differently drift, and drift
// here is one app unlinking another app's tenant's bytes.
//
// It is asked of the database because the filesystem cannot answer it. An app that
// names itself lists only its own segment, so its listing already names one app;
// the deployment of one app lists the un-prefixed position, where the bytes a
// named app wrote before its boot added its segment still sit until the operator's
// `mv` (kit/appname/README.md, *Stored files*), beside bytes of its own. Nothing in
// that directory says which app wrote which file — that is why a named store does
// not list it at all — and the row does: a tenant belongs to exactly one app.
//
// A tenant with no row is nobody's and is not named here, which is `holdsTenant`'
// and `RelayApp`'s answer for the same case: the bytes a store listed under a
// tenant that no longer exists are its own to clean up, whoever that tenant served.
func foreignTenants(tx db.Tx[db.System], chunk []contracts.Blob, app appname.Name) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(chunk))
	for _, b := range chunk {
		// A blob the store cannot locate under any tenant — the flat directory of a
		// release before the port carried a scope — names no tenant, so no row can
		// say another app holds it. What protects those bytes is still the row that
		// names their key, which is knownKeys.
		if b.TenantID != uuid.Nil && !slices.Contains(ids, b.TenantID) {
			ids = append(ids, b.TenantID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var foreign []uuid.UUID
	err := tx.DB().Raw(`SELECT tn.id FROM tenants tn WHERE tn.id IN ? AND coalesce(tn.app, '') <> ?`,
		ids, app.String()).Scan(&foreign).Error
	return foreign, err
}

// appOf is the store's own answer, or the deployment of one app for a store that
// names no segment: the answer `NewLocal` gives, and the conservative one, because
// an app named `collect` sweeping the bytes of a store that never said it held them
// would be the harm this asks about.
func appOf(storage contracts.Storage) appname.Name {
	if s, ok := storage.(appSegment); ok {
		return s.App()
	}
	return appname.Name("")
}

// knownKeys asks, under system access, which of these storage keys any tenant's
// row names. System scope is not a shortcut here: a key belonging to another
// tenant's row is a key that must not be deleted, and a tenant-scoped query
// could not see it to protect it.
func knownKeys(tx db.Tx[db.System], chunk []contracts.Blob) ([]string, error) {
	keys := make([]string, len(chunk))
	for i, b := range chunk {
		keys[i] = b.Key.String()
	}
	var known []string
	err := tx.DB().Model(&contracts.File{}).Where("storage_key IN ?", keys).
		Pluck("storage_key", &known).Error
	return known, err
}

// chunks splits a listing so a million blobs never become a million-element IN
// list.
func chunks(blobs []contracts.Blob, n int) [][]contracts.Blob {
	var out [][]contracts.Blob
	for len(blobs) > n {
		out = append(out, blobs[:n])
		blobs = blobs[n:]
	}
	if len(blobs) > 0 {
		out = append(out, blobs)
	}
	return out
}

// RemoveBlob is contracts.Reconciler's other half on the filesystem: remove one
// object the listing named. A blob the listing found in the flat layout carries
// no tenant, and the only name that could have written it is the key itself, so
// uuid.Nil is where the sweep deletes from — and that name holds the bytes of one
// app as much as of another, because the flat layout names neither.
//
// A blob the listing located under a tenant goes to Local.remove, which takes the
// app segment from the store rather than from the listing: two apps mounted at one
// root each remove from their own directory and never from the other's. Blobs
// names no object outside that directory, so no other app's bytes reach this line
// from the sweep, and the older positions of a tenant this store does name are
// erased with the current one because bytes still lying in an older directory are
// a copy that is still here, which is what Prove then counts.
func (l *Local) RemoveBlob(_ context.Context, _ db.Tx[db.System], b contracts.Blob) error {
	id, ok := minted(b.Key.String())
	if !ok {
		return fmt.Errorf("%w: %q is not a storage key; a key is a lower-case UUID", contracts.ErrInvalidKey, b.Key)
	}
	if b.TenantID == uuid.Nil {
		return l.erase(l.join(appname.PreviousStoragePath(id)))
	}
	return l.remove(b.TenantID, id)
}
