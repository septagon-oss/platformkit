package events

// The tests that need to reach inside the package: the delivery ladder, which
// is a package variable so a test can run it in milliseconds rather than the
// fifty seconds the shipped one takes.

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// fast shortens the redelivery ladder for one test. The shape is what is under
// test — five attempts, then dead — not the wall-clock waits.
func fast(t *testing.T) {
	t.Helper()
	was := backoff
	backoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { backoff = was })
}

// TestAPoisonEventIsDeadLetteredAndStopsComingBack. Before the cap, a handler
// that could never succeed was retried forever, which spent the subscription
// and — through the memory transport's bounded queue — the relay behind it.
func TestAPoisonEventIsDeadLetteredAndStopsComingBack(t *testing.T) {
	fast(t)
	admin, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	var mu sync.Mutex
	attempts := 0
	poison := errors.New("this will never work")
	transport := Memory()
	err := Consume(ctx, conn, transport, []Subscription{{
		Module: "ledger", Name: "billing.invoice_issued",
		Handler: func(context.Context, db.Tx[db.Tenant], Event) error {
			mu.Lock()
			attempts++
			mu.Unlock()
			return poison
		},
	}})
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return Publish(tx, "billing.invoice_issued", nil)
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("Relay: %v", err)
	}

	// The dead letter is the signal that the transport gave up.
	var (
		got  string
		name string
	)
	deadline := time.Now().Add(20 * time.Second)
	for {
		row := admin.QueryRowContext(t.Context(), `SELECT error, name FROM platformkit_dead_letters`)
		if err := row.Scan(&got, &name); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the poison event was never dead-lettered")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got != poison.Error() || name != "billing.invoice_issued" {
		t.Errorf("the dead letter says %q for %q", got, name)
	}

	// And it stays given up on: no further attempts after the cap.
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if attempts != maxDeliveries {
		t.Errorf("the handler ran %d times, want %d and then a dead letter", attempts, maxDeliveries)
	}
}

// TestJetStreamStopsRedeliveringAPoisonEvent is the same policy on the other
// transport, against the NATS `make up` starts. Without nats.MaxDeliver a bare
// Nak is handed straight back by the server, so one poison event became a
// redelivery storm measured in thousands per second.
func TestJetStreamStopsRedeliveringAPoisonEvent(t *testing.T) {
	url := os.Getenv("PLATFORMKIT_TEST_NATS_URL")
	if url == "" {
		t.Fatal("PLATFORMKIT_TEST_NATS_URL is unset; start the stack with `make up`")
	}
	fast(t)
	admin, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	// This run's own subject and consumer: the stream is shared with every
	// other run.
	name := "test_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "") + ".happened"
	transport, err := JetStream(url)
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	t.Cleanup(func() { _ = transport.(interface{ Close() error }).Close() })

	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	var mu sync.Mutex
	attempts := 0
	err = Consume(ctx, conn, transport, []Subscription{{
		Module: "ledger", Name: name,
		Handler: func(context.Context, db.Tx[db.Tenant], Event) error {
			mu.Lock()
			attempts++
			mu.Unlock()
			return errors.New("this will never work")
		},
	}})
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return Publish(tx, name, nil)
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := Relay(t.Context(), conn, transport); err != nil {
		t.Fatalf("Relay: %v", err)
	}

	var got int
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_dead_letters`).Scan(&got); err != nil {
			t.Fatalf("count the dead letters: %v", err)
		}
		if got == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("JetStream never gave up: %d dead letters after %d attempts", got, attempts)
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts != maxDeliveries {
		t.Errorf("the handler ran %d times, want %d; the cap is nats.MaxDeliver", attempts, maxDeliveries)
	}
}
