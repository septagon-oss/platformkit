package db_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// A Pending's owner is the context it was created with; every Tx caller is an
// operation whose context bounds its own statements and nothing else.

func TestAnUnusedLazyTransactionTouchesNoConnection(t *testing.T) {
	_, appURL := dbtest.URLs(t)
	app, err := db.OpenWithPool(t.Context(), appURL, db.Pool{MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	owner, cancelOwner := context.WithCancel(tenancy.WithTenant(t.Context(), newTenant("acme")))
	_, p, err := db.Lazy(owner, app, dbtest.SystemToken())
	if err != nil {
		t.Fatal(err)
	}
	for i, keep := range []bool{true, false} {
		if err := p.Close(keep); err != nil {
			t.Errorf("close %d of an unused transaction = %v, want nil", i, err)
		}
	}
	cancelOwner()
	if err := p.Close(true); err != nil {
		t.Errorf("close after the owner ended = %v, want nil", err)
	}
	if err := p.Err(); err != nil {
		t.Errorf("Err = %v, want nil", err)
	}
	if s := app.Stats(); s.OpenConnections != 0 || s.WaitCount != 0 {
		t.Errorf("an unused transaction touched the pool: %+v", s)
	}
}

func TestACancelledCallerLeavesTheRequestsTransactionToTheNextCaller(t *testing.T) {
	recorder := recordSpans(t)
	admin, app, acme := acmeThings(t)
	owner, p, _ := lazy(t, app, acme)

	first, cancelFirst := context.WithCancel(owner)
	insertThrough(t, first, p, acme, "a")
	cancelFirst()

	second, cancelSecond := context.WithCancel(owner)
	defer cancelSecond()
	tx, err := p.Tx(second)
	if err != nil {
		t.Fatalf("a live caller after a cancelled one was refused: %v", err)
	}
	if err := insert(tx.DB(), acme.ID, "b"); err != nil {
		t.Fatalf("a live caller's statement failed after another caller was cancelled: %v", err)
	}
	if err := p.Close(true); err != nil {
		t.Fatalf("commit = %v", err)
	}
	if err := p.Close(true); err != nil {
		t.Errorf("a second Close = %v, want nil", err)
	}
	if n := committed(t, admin); n != 2 {
		t.Errorf("committed rows = %d, want 2", n)
	}
	spans := transactionSpans(recorder)
	if len(spans) != 1 || spans[0].Status().Code == codes.Error {
		t.Errorf("transaction spans = %d (want one, not failed)", len(spans))
	}
}

func TestAStatementPastItsDeadlineTakesTheTransactionWithIt(t *testing.T) {
	admin, app, acme := acmeThings(t)
	owner, p, _ := lazy(t, app, acme)

	insertThrough(t, owner, p, acme, "a")
	short, cancel := context.WithTimeout(owner, 300*time.Millisecond)
	defer cancel()
	slow, err := p.Tx(short)
	if err != nil {
		t.Fatal(err)
	}
	if err := slow.DB().Exec("SELECT pg_sleep(30)").Error; err == nil {
		t.Fatal("a statement past its deadline succeeded")
	}
	if tx, err := p.Tx(owner); err == nil {
		_ = insert(tx.DB(), acme.ID, "b")
	}
	if err := p.Close(true); err == nil {
		t.Error("a transaction whose statement was cancelled committed")
	}
	if n := committed(t, admin); n != 0 {
		t.Errorf("committed rows = %d, want 0", n)
	}
}

func TestAnOwnerCancelledMidHandlerReleasesItsTransaction(t *testing.T) {
	admin, app, acme := acmeThings(t)
	base := app.Stats().InUse
	owner, p, cancelOwner := lazy(t, app, acme)

	wrote, release, done := make(chan error), make(chan struct{}), make(chan error)
	go func() {
		// A caller whose own context never ends: the transaction must still be
		// the owner's.
		op := context.WithoutCancel(owner)
		tx, err := p.Tx(op)
		if err == nil {
			err = insert(tx.DB(), acme.ID, "a")
		}
		wrote <- err
		<-release
		done <- insert(tx.DB(), acme.ID, "b")
	}()
	if err := <-wrote; err != nil {
		t.Fatal(err)
	}
	cancelOwner()
	waitUntil(t, "the connection to return to the pool", func() bool { return app.Stats().InUse == base })
	if n := committed(t, admin); n != 0 {
		t.Errorf("rows visible after the owner ended = %d, want 0", n)
	}
	close(release)
	if err := <-done; err == nil {
		t.Error("a statement after the owner ended succeeded")
	}
	if err := p.Close(true); !errors.Is(err, context.Canceled) {
		t.Errorf("Close after the owner ended = %v, want context.Canceled", err)
	}
	if n := committed(t, admin); n != 0 {
		t.Errorf("committed rows = %d, want 0", n)
	}
}

func TestACancelBeforeOrDuringBeginStrandsNoConnection(t *testing.T) {
	t.Run("operation ended before begin", func(t *testing.T) {
		admin, app, acme := acmeThings(t)
		owner, p, _ := lazy(t, app, acme)
		gone, cancel := context.WithCancel(owner)
		cancel()
		if _, err := p.Tx(gone); !errors.Is(err, context.Canceled) {
			t.Fatalf("Tx for a cancelled caller = %v, want context.Canceled", err)
		}
		if err := p.Err(); err != nil {
			t.Fatalf("a caller's cancel was remembered as the transaction's failure: %v", err)
		}
		tx, err := p.Tx(owner)
		if err != nil {
			t.Fatalf("a live caller after a cancelled one was refused: %v", err)
		}
		if err := insert(tx.DB(), acme.ID, "a"); err != nil {
			t.Fatal(err)
		}
		if err := p.Close(true); err != nil {
			t.Fatal(err)
		}
		if n := committed(t, admin); n != 1 {
			t.Errorf("committed rows = %d, want 1", n)
		}
	})
	t.Run("owner ended before begin", func(t *testing.T) {
		_, app := dbtest.Schema(t)
		owner, p, cancelOwner := lazy(t, app, newTenant("acme"))
		cancelOwner()
		if _, err := p.Tx(context.WithoutCancel(owner)); !errors.Is(err, context.Canceled) {
			t.Fatalf("Tx after the owner ended = %v, want context.Canceled", err)
		}
		if err := p.Err(); err != nil {
			t.Errorf("the owner's end was remembered as a database failure: %v", err)
		}
		waitUntil(t, "no connection in use", func() bool { return app.Stats().InUse == 0 })
	})
	t.Run("owner ended while waiting for a connection", func(t *testing.T) {
		_, appURL := dbtest.URLs(t)
		app, err := db.OpenWithPool(t.Context(), appURL, db.Pool{MaxOpenConns: 1})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = app.Close() })
		acme := newTenant("acme")

		held, release, holderDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		go func() {
			holderDone <- db.Run(tenancy.WithTenant(context.WithoutCancel(t.Context()), acme), app,
				func(context.Context, db.Tx[db.Tenant]) error {
					close(held)
					<-release
					return nil
				})
		}()
		<-held
		before := app.Stats().WaitCount
		owner, p, cancelOwner := lazy(t, app, acme)
		opened := make(chan error, 1)
		go func() {
			_, err := p.Tx(context.WithoutCancel(owner))
			opened <- err
		}()
		waitUntil(t, "the open to wait for the pool", func() bool { return app.Stats().WaitCount > before })
		cancelOwner()
		if err := <-opened; !errors.Is(err, context.Canceled) {
			t.Errorf("an open whose owner ended while it waited = %v, want context.Canceled", err)
		}
		if err := p.Err(); err != nil {
			t.Errorf("the owner's end was remembered as a database failure: %v", err)
		}
		waits := app.Stats().WaitCount
		if _, err := p.Tx(context.WithoutCancel(owner)); err == nil {
			t.Error("Tx after the owner ended opened a transaction")
		}
		if got := app.Stats().WaitCount; got != waits {
			t.Errorf("a refused open waited for the pool: WaitCount %d → %d", waits, got)
		}
		close(release)
		if err := <-holderDone; err != nil {
			t.Fatal(err)
		}
		_ = p.Close(true)
		waitUntil(t, "no connection in use", func() bool { return app.Stats().InUse == 0 })
	})
}

func TestConcurrentCallersOpenOneTransaction(t *testing.T) {
	recorder := recordSpans(t)
	_, app := dbtest.Schema(t)
	owner, p, _ := lazy(t, app, newTenant("acme"))

	// The opens race; the statements then run one at a time, because one
	// connection answers one statement at a time.
	start := make(chan struct{})
	txs := make([]db.Tx[db.Tenant], 8)
	errs := make([]error, 8)
	var wg sync.WaitGroup
	for i := range txs {
		wg.Go(func() {
			<-start
			txs[i], errs[i] = p.Tx(owner)
		})
	}
	close(start)
	wg.Wait()
	ids := make([]int64, len(txs))
	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: %v", i, err)
		}
		if err := txs[i].DB().Raw("SELECT txid_current()").Row().Scan(&ids[i]); err != nil {
			t.Fatalf("caller %d: %v", i, err)
		}
		if ids[i] != ids[0] {
			t.Errorf("caller %d ran in transaction %d, caller 0 in %d", i, ids[i], ids[0])
		}
	}
	if err := p.Close(true); err != nil {
		t.Fatal(err)
	}
	if n := len(transactionSpans(recorder)); n != 1 {
		t.Errorf("transaction spans = %d, want 1", n)
	}
}

func TestACancelRacingTheCommitFinalisesOnceAndNeverReopens(t *testing.T) {
	admin, app, acme := acmeThings(t)
	for i := range 50 {
		owner, p, cancelOwner := lazy(t, app, acme)
		insertThrough(t, owner, p, acme, "x")
		before := committed(t, admin)
		start := make(chan struct{})
		closed := make(chan error, 1)
		var wg sync.WaitGroup
		wg.Go(func() { <-start; cancelOwner() })
		wg.Go(func() { <-start; closed <- p.Close(true) })
		close(start)
		wg.Wait()
		err := <-closed
		added := committed(t, admin) - before
		if err == nil && added != 1 {
			t.Fatalf("iteration %d: Close reported a commit and %d rows landed", i, added)
		}
		if added == 0 && err == nil {
			t.Fatalf("iteration %d: nothing landed and Close said it committed", i)
		}
		if err := p.Close(true); err != nil {
			t.Fatalf("iteration %d: a second Close = %v, want nil", i, err)
		}
		if _, err := p.Tx(context.WithoutCancel(owner)); err == nil {
			t.Fatalf("iteration %d: Tx after the owner ended opened a transaction", i)
		}
	}
	waitUntil(t, "no connection in use", func() bool { return app.Stats().InUse == 0 })
}

type valueKey struct{}

func TestAStatementCarriesItsCallersValuesAndTheTransactionSpan(t *testing.T) {
	recorder := recordSpans(t)
	_, app := dbtest.Schema(t)
	parent, ownerSpan := otel.Tracer("test").Start(tenancy.WithTenant(t.Context(), newTenant("acme")), "request")
	defer ownerSpan.End()
	owner, p, _ := lazyFrom(t, app, parent)

	op, cancel := context.WithTimeout(context.WithValue(owner, valueKey{}, "first"), time.Minute)
	defer cancel()
	tx, err := p.Tx(op)
	if err != nil {
		t.Fatal(err)
	}
	h := tx.DB()
	if got := h.Statement.Context.Value(valueKey{}); got != "first" {
		t.Errorf("the statement's context value = %v, want the caller's", got)
	}
	want, _ := op.Deadline()
	if got, ok := h.Statement.Context.Deadline(); !ok || !got.Equal(want) {
		t.Errorf("the statement's deadline = %v, want the caller's %v", got, want)
	}
	other, err := p.Tx(context.WithValue(owner, valueKey{}, "second"))
	if err != nil {
		t.Fatal(err)
	}
	if got := other.DB().Statement.Context.Value(valueKey{}); got != "second" {
		t.Errorf("a second caller's statement carries %v, want its own value", got)
	}
	if got := h.Statement.Context.Value(valueKey{}); got != "first" {
		t.Errorf("a second caller changed the first caller's handle: %v", got)
	}
	current := trace.SpanFromContext(h.Statement.Context).SpanContext()
	if err := p.Close(true); err != nil {
		t.Fatal(err)
	}
	spans := transactionSpans(recorder)
	if len(spans) != 1 {
		t.Fatalf("transaction spans = %d, want 1", len(spans))
	}
	if !current.Equal(spans[0].SpanContext()) {
		t.Error("the statement does not run under the transaction span")
	}
	if spans[0].Parent().SpanID() != ownerSpan.SpanContext().SpanID() {
		t.Error("the transaction span is not a child of the owner's span")
	}
}

func TestATransactionFoundOnTheContextIsTheFindersOperation(t *testing.T) {
	admin, app, acme := acmeThings(t)
	owner, p, _ := lazy(t, app, acme)
	if _, err := p.Tx(owner); err != nil {
		t.Fatal(err)
	}

	op, cancel := context.WithTimeout(context.WithValue(owner, valueKey{}, "finder"), time.Minute)
	defer cancel()
	found, ok := db.TxFrom(op)
	if !ok {
		t.Fatal("TxFrom found no transaction on a request that opened one")
	}
	h := found.DB()
	if got := h.Statement.Context.Value(valueKey{}); got != "finder" {
		t.Errorf("the found handle's context value = %v, want the finder's", got)
	}
	want, _ := op.Deadline()
	if got, ok := h.Statement.Context.Deadline(); !ok || !got.Equal(want) {
		t.Errorf("the found handle's deadline = %v, want the finder's %v", got, want)
	}
	cancel()
	if err := insert(h, acme.ID, "cancelled"); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled finder's write = %v, want context.Canceled", err)
	}
	live, ok := db.TxFrom(owner)
	if !ok {
		t.Fatal("TxFrom found no transaction after a finder was cancelled")
	}
	if err := insert(live.DB(), acme.ID, "kept"); err != nil {
		t.Fatalf("a live finder's write after a cancelled one = %v", err)
	}
	if err := p.Close(true); err != nil {
		t.Fatalf("commit = %v", err)
	}
	if n := committed(t, admin); n != 1 {
		t.Errorf("committed rows = %d, want only the live finder's", n)
	}
}

func TestACancelAfterCommitIsReportedAsCommitted(t *testing.T) {
	admin, app, acme := acmeThings(t)
	owner, p, cancelOwner := lazy(t, app, acme)
	insertThrough(t, owner, p, acme, "a")
	if err := p.Close(true); err != nil {
		t.Fatal(err)
	}
	cancelOwner()
	if err := p.Close(true); err != nil {
		t.Errorf("Close after a commit and a cancel = %v, want nil", err)
	}
	if err := p.Err(); err != nil {
		t.Errorf("Err = %v, want nil", err)
	}
	if n := committed(t, admin); n != 1 {
		t.Errorf("committed rows = %d, want 1", n)
	}
}

// acmeThings opens a schema holding the things table, for the tenant acme.
func acmeThings(t *testing.T) (*sql.DB, *db.Conn, tenancy.Tenant) {
	t.Helper()
	admin, app := dbtest.Schema(t)
	createThings(t, t.Context(), admin)
	return admin, app, newTenant("acme")
}

func lazy(t *testing.T, app *db.Conn, tenant tenancy.Tenant) (context.Context, *db.Pending, context.CancelFunc) {
	t.Helper()
	return lazyFrom(t, app, tenancy.WithTenant(t.Context(), tenant))
}

// lazyFrom creates a Pending whose owner is a cancellable child of parent, and
// returns the owner, the Pending and the cancel that ends the owner.
func lazyFrom(t *testing.T, app *db.Conn, parent context.Context) (context.Context, *db.Pending, context.CancelFunc) {
	t.Helper()
	owner, cancel := context.WithCancel(parent)
	t.Cleanup(cancel)
	ctx, p, err := db.Lazy(owner, app, dbtest.SystemToken())
	if err != nil {
		t.Fatal(err)
	}
	return ctx, p, cancel
}

// insertThrough opens ctx's operation of p and inserts one of tenant's things,
// failing the test if either is refused.
func insertThrough(t *testing.T, ctx context.Context, p *db.Pending, tenant tenancy.Tenant, name string) {
	t.Helper()
	tx, err := p.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := insert(tx.DB(), tenant.ID, name); err != nil {
		t.Fatal(err)
	}
}

// waitUntil polls cond until it holds, failing after ten seconds.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	return recorder
}

func transactionSpans(r *tracetest.SpanRecorder) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, s := range r.Ended() {
		if s.Name() == "database transaction" {
			out = append(out, s)
		}
	}
	return out
}

// committed counts the rows of things as the owner role, which no policy binds.
func committed(t *testing.T, admin *sql.DB) int {
	t.Helper()
	return countRows(t, admin, "SELECT count(*) FROM things")
}
