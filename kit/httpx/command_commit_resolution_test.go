package httpx_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// unacknowledgedCommit delegates every operation to the real transaction and replaces
// one thing: the answer a *successful* COMMIT gives back becomes a transport fault. This
// is the moment the idempotency record exists for — the COMMIT reached the server, which
// may have written it, and this process could not read that either way. The two faults it
// can wear are a severed connection and a deadline that expires on the way back, so the
// error belongs to the case rather than to this type: a caller that read either as a
// rollback would release the key of a write that may have happened.
type unacknowledgedCommit struct {
	gorm.ConnPool
	gorm.TxCommitter
	// ackError answers the COMMIT, after the real COMMIT has already succeeded. It is
	// never an error carrying a SQLState: that is the shape of a refusal the database
	// actually answered, and the middleware is entitled to read that as a rollback.
	ackError  error
	committed atomic.Bool
}

func (c *unacknowledgedCommit) Commit() error {
	if err := c.TxCommitter.Commit(); err != nil {
		return err
	}
	c.committed.Store(true)
	return c.ackError
}

// refusedCommit also delegates everything, and ends the real transaction the way the
// database does when it answers a COMMIT with an error: the work is rolled back, and the
// answer names a SQLState. That is the case the silence above is not — here the outcome
// of the write *is* known, so the identical retry may run.
type refusedCommit struct {
	gorm.ConnPool
	gorm.TxCommitter
	rolledBack atomic.Bool
}

func (c *refusedCommit) Commit() error {
	if err := c.TxCommitter.Rollback(); err != nil {
		return err
	}
	c.rolledBack.Store(true)
	return refusedByDatabase("40001")
}

// refusedByDatabase is a COMMIT error the database answered, which is what makes it a
// statement about the write rather than a silence: the rollback took the marker with the
// work, so the next press finds none.
type refusedByDatabase string

func (e refusedByDatabase) Error() string    { return "db: the COMMIT was refused: serialization failure" }
func (e refusedByDatabase) SQLState() string { return string(e) }

// commandEffect is the keyed command every case mounts: one POST that writes one row of
// its own inside the request's transaction, under the store the case names, and under the
// fault the case installs on the way out of that transaction. The row is the assertion —
// a status code is what the guard claims and a committed row is what the command cost —
// and the handler count beside it says whether the command was run again.
type commandEffect struct {
	ran atomic.Int32
	// path is the route tag this case mounted, which is also the marker its rows
	// carry, so two cases on one database cannot read each other's work.
	path string
	conn *db.Conn
	who  tenancy.Tenant
	// fault, when it returns non-nil, is put on the first attempt's own transaction by
	// the handler, which is the only place that transaction can be reached from. The
	// attempts after it are the point of these cases, so they travel clean.
	fault func(tx db.Tx[db.Tenant]) gorm.ConnPool
}

// mountCommandEffect registers the route and returns the handler that serves it. The tag
// is both the path and the marker the case counts its own rows by, so two cases on one
// database cannot read each other's work.
func mountCommandEffect(t *testing.T, store idempotency.Store, tag string, fault func(db.Tx[db.Tenant]) gorm.ConnPool) (http.Handler, *commandEffect) {
	t.Helper()
	api, router, _ := keyedSetup(t, store, nil)
	c := &commandEffect{path: tag, fault: fault}
	httpx.Register(api.Surfaces(probe).Public, huma.Operation{
		OperationID: "keyed-" + tag, Method: http.MethodPost, Path: "/" + tag,
		DefaultStatus: http.StatusCreated,
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*keyedOut, error) {
		c.ran.Add(1)
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, errors.New("a keyed command reached no request transaction")
		}
		c.conn, _ = httpx.ConnFrom(ctx)
		c.who, _ = tenancy.FromContext(ctx)
		if err := tx.DB().Exec("INSERT INTO platformkit_handled (event_id, durable, tenant_id) VALUES (?, ?, ?)",
			uuid.New(), tag, c.who.ID).Error; err != nil {
			return nil, err
		}
		if c.fault != nil && c.ran.Load() == 1 {
			if f := c.fault(tx); f != nil {
				tx.DB().Statement.ConnPool = f
			}
		}
		return &keyedOut{ID: tag}, nil
	})
	return router, c
}

// committedRows counts this case's own rows, from a tenant transaction of its own on the
// connection the request used.
func (c *commandEffect) committedRows(t *testing.T) int64 {
	t.Helper()
	var n int64
	err := db.Run(tenancy.WithTenant(context.WithoutCancel(t.Context()), c.who), c.conn,
		func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Table("platformkit_handled").Where("durable = ?", c.path).Count(&n).Error
		})
	if err != nil {
		t.Fatalf("count the committed rows: %v", err)
	}
	return n
}

// agedClaim advances the clock the store decides under, which is how these cases reach a
// lapsed five-minute lease without sleeping through it. The caller sets [elapsed] between
// two presses, so the claim that opens the record stays at the write's own moment and
// only the later press is old: a claim advanced from the start would be older than the
// row it reads, which is a different question than the one being asked.
type agedClaim struct {
	idempotency.Store
	elapsed time.Duration
}

func (s *agedClaim) Claim(ctx context.Context, scope idempotency.Scope, at time.Time) (idempotency.Verdict, error) {
	return s.Store.Claim(ctx, scope, at.Add(s.elapsed))
}

const ambiguousBody = `{}`

func ambiguousPath(tag string) string { return "/api/v1/public/probe/" + tag }

// lostAcknowledgement installs the severed-answer fault on the request's transaction and
// returns what it installed, so the case can check the COMMIT it is arguing about.
func lostAcknowledgement(ackError error, seen *atomic.Pointer[unacknowledgedCommit]) func(db.Tx[db.Tenant]) gorm.ConnPool {
	return func(tx db.Tx[db.Tenant]) gorm.ConnPool {
		pool := tx.DB().Statement.ConnPool
		c := &unacknowledgedCommit{ConnPool: pool, TxCommitter: pool.(gorm.TxCommitter), ackError: ackError}
		seen.Store(c)
		return c
	}
}

// committedAnyway fails the case unless its fault reached a COMMIT the database accepted,
// which is the only moment an acknowledgement of it means anything.
func committedAnyway(t *testing.T, seen *atomic.Pointer[unacknowledgedCommit]) {
	t.Helper()
	c := seen.Load()
	if c == nil || !c.committed.Load() {
		t.Fatal("the fault did not reach a successful database commit")
	}
}

// TestALostCommitAcknowledgementDoesNotReleaseTheCommandKey is the brief's own promise:
// done once even when the network is not. The COMMIT went out and its answer never came
// back, and the write is in the database. The key may not be released for that, because
// releasing it is permission for the same submission to commit twice.
func TestALostCommitAcknowledgementDoesNotReleaseTheCommandKey(t *testing.T) {
	var seen atomic.Pointer[unacknowledgedCommit]
	store := &agedClaim{Store: idempotency.Postgres(httpx.ConnFrom)}
	router, c := mountCommandEffect(t, store, "lost-ack", lostAcknowledgement(io.ErrUnexpectedEOF, &seen))
	path := ambiguousPath("lost-ack")

	first := keyedPost(t, router, http.MethodPost, path, "lost-ack", ambiguousBody)
	committedAnyway(t, &seen)
	if first.Code != http.StatusInternalServerError {
		t.Errorf("an attempt whose commit could not be acknowledged = %d %s, want 500: the kernel cannot promise an answer it could not write down",
			first.Code, first.Body.String())
	}
	if n := c.ran.Load(); n != 1 {
		t.Fatalf("the command ran %d times on the attempt that lost its acknowledgement, want once", n)
	}
	if n := c.committedRows(t); n != 1 {
		t.Fatalf("the commit the caller could not read left %d rows, want the one it wrote", n)
	}

	// While the claim is still held the answer is the true one: as far as the record can
	// say this write is still landing, and a second run is what the key exists to refuse.
	repeat := keyedPost(t, router, http.MethodPost, path, "lost-ack", ambiguousBody)
	if repeat.Code != http.StatusConflict || !strings.Contains(repeat.Body.String(), httpx.CodeIdempotencyInFlight) {
		t.Errorf("a repeat of an unacknowledged commit = %d %s, want a reasoned 409", repeat.Code, repeat.Body.String())
	}
	if n := c.committedRows(t); n != 1 {
		t.Errorf("the repeat committed %d rows, want the one the first attempt wrote", n)
	}
}

// TestAnAmbiguousCommandCommitResolvesOnTheNextRequest is the half a claim row cannot
// answer. Once the lease lapses on a begun claim the record holds two possible verdicts
// and no clock that can pick between them; the effect marker written inside the
// transaction picks it. Here the marker says the write committed, so the press that
// arrives is refused and the command is not run again — and it is refused as an answer
// that is gone, not as a write that is still coming.
func TestAnAmbiguousCommandCommitResolvesOnTheNextRequest(t *testing.T) {
	var seen atomic.Pointer[unacknowledgedCommit]
	store := &agedClaim{Store: idempotency.Postgres(httpx.ConnFrom)}
	router, c := mountCommandEffect(t, store, "resolve-marker", lostAcknowledgement(io.ErrUnexpectedEOF, &seen))
	path := ambiguousPath("resolve-marker")

	keyedPost(t, router, http.MethodPost, path, "resolve-marker", ambiguousBody)
	committedAnyway(t, &seen)
	// Past the lease, with nobody left to answer for the claim. This is the press that
	// main's guard answered as spent without reading whether the write landed.
	store.elapsed = idempotency.Lease + time.Second
	resolved := keyedPost(t, router, http.MethodPost, path, "resolve-marker", ambiguousBody)
	if resolved.Code != http.StatusConflict {
		t.Fatalf("the press that resolves an unacknowledged commit = %d %s, want 409", resolved.Code, resolved.Body.String())
	}
	if !strings.Contains(resolved.Body.String(), httpx.CodeIdempotencySpent) {
		t.Errorf("the press read the write as still coming rather than as committed with its answer gone: %d %s",
			resolved.Code, resolved.Body.String())
	}
	if n := c.ran.Load(); n != 1 {
		t.Errorf("the command ran %d times, want once: a resolved commit that runs again is the double apply", n)
	}
	if n := c.committedRows(t); n != 1 {
		t.Errorf("one submission committed %d rows, want one", n)
	}
	// The resolution is stable: the key stays refused rather than being handed back, and
	// the refusal repeats itself rather than deciding again from a different reading.
	if again := keyedPost(t, router, http.MethodPost, path, "resolve-marker", ambiguousBody); again.Code != resolved.Code {
		t.Errorf("a later press = %d %s, want the same %d the first resolution gave", again.Code, again.Body.String(), resolved.Code)
	}
}

// TestACommitCancelledOnTheWayBackDoesNotReleaseTheCommandKey keeps the fence honest for
// the cancellation shape of a lost acknowledgement. A deadline that expires while the
// driver waits for the reply to the COMMIT it already sent is the same silence wearing
// the caller's shape, and it is the one this middleware is most likely to excuse as a
// rollback — which is exactly how a key gets freed for a write that happened.
func TestACommitCancelledOnTheWayBackDoesNotReleaseTheCommandKey(t *testing.T) {
	var seen atomic.Pointer[unacknowledgedCommit]
	store := &agedClaim{Store: idempotency.Postgres(httpx.ConnFrom)}
	router, c := mountCommandEffect(t, store, "cancelled-commit", lostAcknowledgement(context.Canceled, &seen))
	path := ambiguousPath("cancelled-commit")

	first := keyedPost(t, router, http.MethodPost, path, "cancelled-commit", ambiguousBody)
	committedAnyway(t, &seen)
	t.Logf("a commit cancelled on the way back: %d %s", first.Code, first.Body.String())
	store.elapsed = idempotency.Lease + time.Second
	retry := keyedPost(t, router, http.MethodPost, path, "cancelled-commit", ambiguousBody)
	if retry.Code != http.StatusConflict || !strings.Contains(retry.Body.String(), httpx.CodeIdempotencySpent) {
		t.Errorf("the press after a cancelled acknowledgement = %d %s, want the refusal that says this write committed and its answer is gone",
			retry.Code, retry.Body.String())
	}
	if n := c.ran.Load(); n != 1 {
		t.Errorf("the command ran %d times, once after a commit the caller could not read; want once", n)
	}
	if n := c.committedRows(t); n != 1 {
		t.Errorf("one submission committed %d rows after a commit cancelled on the way back, want one", n)
	}
}

// TestACommandWhoseCommitTheDatabaseRefusedRunsAgainOnTheNextPress is the other verdict,
// and the reason the first one is worth anything: a COMMIT the database answered with an
// error is a rollback, its effect marker rolled back inside it, and the identical retry of
// that key must run. A kernel that resolved every unanswered claim to "spent" would spend
// a key on a write that never happened, which is the fault on the other side.
func TestACommandWhoseCommitTheDatabaseRefusedRunsAgainOnTheNextPress(t *testing.T) {
	var refused atomic.Pointer[refusedCommit]
	store := &agedClaim{Store: idempotency.Postgres(httpx.ConnFrom)}
	router, c := mountCommandEffect(t, store, "refused-commit", func(tx db.Tx[db.Tenant]) gorm.ConnPool {
		pool := tx.DB().Statement.ConnPool
		r := &refusedCommit{ConnPool: pool, TxCommitter: pool.(gorm.TxCommitter)}
		refused.Store(r)
		return r
	})
	path := ambiguousPath("refused-commit")

	first := keyedPost(t, router, http.MethodPost, path, "refused-commit", ambiguousBody)
	r := refused.Load()
	if r == nil || !r.rolledBack.Load() {
		t.Fatal("the case never reached a commit that ended in a real rollback")
	}
	if first.Code != http.StatusInternalServerError {
		t.Errorf("a command whose commit was refused = %d %s, want 500", first.Code, first.Body.String())
	}
	if n := c.committedRows(t); n != 0 {
		t.Fatalf("a refused commit left %d rows behind, want none", n)
	}

	store.elapsed = idempotency.Lease + time.Second
	retry := keyedPost(t, router, http.MethodPost, path, "refused-commit", ambiguousBody)
	if retry.Code != http.StatusCreated {
		t.Errorf("the retry of a rolled-back command = %d %s, want it run", retry.Code, retry.Body.String())
	}
	if n := c.ran.Load(); n != 2 {
		t.Errorf("the command ran %d times across a refused commit and its retry, want twice", n)
	}
	if n := c.committedRows(t); n != 1 {
		t.Errorf("the retried command committed %d rows, want the one it wrote", n)
	}
}
