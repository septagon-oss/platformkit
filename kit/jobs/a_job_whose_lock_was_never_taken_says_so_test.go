package jobs

// An advisory lock answers two different questions and the scheduler must not answer them with one
// value: "another replica" says the work is happening elsewhere, "lock refused" says nobody ran it.
// A held key is the first answer and a connection that cannot be taken is the second; an operator
// who reads the first when the second happened waits for a peer that is gone, and a job whose lock
// was never taken goes unrun while the dashboard says the cluster is busy.

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestSchedulerNamesALostLockDifferentlyFromAPeerHoldingTheKey: with no pool left to take the job's
// key on, the body runs nowhere and the run's span answers "lock refused" — the answer a tick that
// lost the race to a peer must never share, and the pair is only pinned when both halves are read.
func TestSchedulerNamesALostLockDifferentlyFromAPeerHoldingTheKey(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))

	_, conn := dbtest.Schema(t)
	ran := false
	job := Job{Name: "lock_that_cannot_be_taken", Every: time.Minute,
		Run: func(context.Context, *db.Conn) error { ran = true; return nil }}
	s := NewScheduler(conn, slog.New(slog.NewTextHandler(discard{}, nil)), appname.Name(""), job)
	if err := conn.Close(); err != nil {
		t.Fatalf("end the pool the job would take its key on: %v", err)
	}

	s.run(t.Context(), job)

	if ran {
		t.Error("the job ran although its lock was never taken")
	}
	outcome := ""
	for _, sp := range recorder.Ended() {
		if sp.Name() == job.Name+" run" {
			outcome, _ = attr(sp.Attributes(), "pkit.job.outcome")
		}
	}
	if outcome != "lock refused" {
		t.Errorf("a run whose lock could not be taken says %q, want \"lock refused\"; another replica is the other answer", outcome)
	}
}
