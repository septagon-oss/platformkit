package jobs

// Every replica schedules every job, and the one thing that decides which of them runs the
// work is a Postgres advisory lock. Two answers come back from that lock and they are not
// the same fact: a held key says another replica is running the job and nothing here failed,
// while a lock that could not be taken says nobody ran it and the tick is lost. The scheduler
// answers them at different log levels and with different values of its one span attribute,
// so a case that only reads "the job did not run" cannot tell the two apart.

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

// TestSchedulerRunsNoJobWhileAnotherReplicaHoldsItsLock: with the job's key held by a peer,
// the job body runs on no replica and the run's span answers "another replica" — the value a
// lock that actually failed must never share.
func TestSchedulerRunsNoJobWhileAnotherReplicaHoldsItsLock(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))

	_, conn := dbtest.Schema(t)
	ran := false
	job := Job{Name: "held_by_a_replica", Every: time.Minute,
		Run: func(context.Context, *db.Conn) error { ran = true; return nil }}
	s := NewScheduler(conn, slog.New(slog.NewTextHandler(discard{}, nil)), appname.Name(""), job)

	unlock, err := db.TryLockErr(t.Context(), conn, appname.JobLock("", job.Name))
	if err != nil {
		t.Fatalf("take the job's key as the replica that holds it: %v", err)
	}
	defer unlock()

	s.run(t.Context(), job)

	if ran {
		t.Error("the job ran while another replica held its key")
	}
	outcome := ""
	for _, sp := range recorder.Ended() {
		if sp.Name() == job.Name+" run" {
			outcome, _ = attr(sp.Attributes(), "pkit.job.outcome")
		}
	}
	if outcome != "another replica" {
		t.Errorf("a run refused by a peer says %q, want \"another replica\"; a lock that failed is the other answer", outcome)
	}
}
