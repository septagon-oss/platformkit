package contenttest_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/modules/content/contracts/contenttest"
)

// Review 1, finding 1. The case named "publishing twice does not move the
// publication time" was a hand-written case that read the answer the second
// Publish gave (`again.PublishedAt.Equal(*first.PublishedAt)`, conformance.go:112
// at 9f08dbd). It is now kit/porttest's generated retry, which reads the store
// either side of the call and never looks at the answer. A service that stores
// the right row and answers a publication time an hour later is a stale row
// returned to the caller — house rule 9's third clause — and the suite that
// carries the sentence no longer refuses it.
//
// The floor this pins: a retry is idempotent in what it *answers* as well as in
// what it writes. It passes as soon as that assertion is made again, whether the
// module writes it by hand in Suite.Own or the description grows a way to say it.
func TestTheRetryReadsTheAnswerAndNotOnlyTheStore(t *testing.T) {
	// Reachability, through the fixed behaviour and not the broken one: the
	// unmutated fake passes the suite, so a failure below is this service's and
	// not the harness being unable to run at all.
	if ok := runSuiteQuietly(t, func(contracts.Service) contracts.Service { return nil }); !ok {
		t.Fatalf("the conformance suite does not pass the fake it ships with; nothing below means anything")
	}

	ok := runSuiteQuietly(t, func(s contracts.Service) contracts.Service {
		return &movesTheAnswer{Service: s, seen: map[uuid.UUID]bool{}}
	})
	if ok {
		t.Errorf("the conformance suite passed a service whose second Publish answers a publication time an hour " +
			"after the one it stored; \"publishing twice does not move the publication time\" is the case that " +
			"refused it before this conversion, and a caller that reads the answer is told the page was " +
			"republished when it was not")
	}
}

// runSuiteQuietly runs contenttest.RunService against the fake, optionally
// wrapped, and reports whether it passed. It runs in a testing.T of its own so a
// suite that fails does not fail the test that is watching it — the same thing
// kit/porttest's own reporter does for the harness's internal cases.
func runSuiteQuietly(t *testing.T, wrap func(contracts.Service) contracts.Service) bool {
	t.Helper()
	return testing.RunTests(
		func(pat, str string) (bool, error) { return true, nil },
		[]testing.InternalTest{{
			Name: "contenttest_RunService",
			F: func(inner *testing.T) {
				contenttest.RunService(inner, func(inner *testing.T, run func(contenttest.Fixture)) {
					fake := contenttest.NewFake()
					var svc contracts.Service = fake
					if wrapped := wrap(fake); wrapped != nil {
						svc = wrapped
					}
					run(contenttest.Fixture{Ctx: inner.Context(), Service: svc,
						Seed: fake.Put, Content: fake.Content, Published: fake.Published})
				})
			},
		}})
}

// movesTheAnswer stores what the fake stores and answers a lie the second time a
// page is published: the store is right, the answer is an hour out.
type movesTheAnswer struct {
	contracts.Service
	seen map[uuid.UUID]bool
}

func (m *movesTheAnswer) Publish(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Content, error) {
	got, err := m.Service.Publish(ctx, tx, id)
	if err != nil || got == nil {
		return got, err
	}
	if m.seen[id] && got.PublishedAt != nil {
		moved := got.PublishedAt.Add(time.Hour)
		answer := *got
		answer.PublishedAt = &moved
		return &answer, nil
	}
	m.seen[id] = true
	return got, nil
}
