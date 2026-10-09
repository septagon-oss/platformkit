package internal_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// TestRevokeAllPublishesOnlyTheSessionsItRemoved is the second half of the
// concurrency claim the sessions file makes for itself.
//
// internal/sessions.go says, of RevokeAllSessions: "Two of these racing each
// other is idempotent: both delete the same rows and the loser publishes
// nothing, because the DELETE that found nothing has nothing to report." The
// one-session command earns its version of that claim with a row lock, and
// TestTwoTabsRevokingOneSessionSettleOnce proves it. This case puts the same
// two transactions against the all-of-them command — which takes no lock, by
// the same comment — and reads the outbox afterwards.
//
// What the trail must say is one revocation per session that left. A person who
// pressed "sign out everywhere" twice — a page double-submitting, a mobile
// client retrying — ended one set of machines, and modules/audit copies every
// payload the outbox carries, so a session that was already gone is announced
// again: a trail saying that machine left twice. The count is the whole
// assertion. For three sessions the trail holds three events, whether the
// command ran once or twice.
//
// The loser's read is the shape of the race. It happens at read committed while
// the winner's DELETE still holds the rows: the list it builds is the state as
// it was a moment ago, and every later step of the command is about rows it no
// longer holds. So what the command reports and publishes has to come from what
// its DELETE removed — what RowsAffected says — and not from what its earlier
// read listed. That is the assertion, and it holds whichever way the fix goes:
// a RowsAffected check, a re-read under the lock, or a DELETE ... RETURNING.
func TestRevokeAllPublishesOnlyTheSessionsItRemoved(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users := realUsers()
	svc := internal.NewService(users, nil, internal.Delivery{})
	seed(t, conn, acme)

	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	var person uuid.UUID
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, person = sessionOfPerson(t, ctx, tx, svc, users, "ada@acme.example.com")
		// Two more machines, so the loser has more than one row to misreport.
		for _, agent := range []string{"Firefox on Linux", "Safari on iPad"} {
			_, _, err := svc.Open(ctx, tx, person, contracts.Client{UserAgent: agent, IP: "203.0.113.2"}, contracts.ViaOIDC)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("open the sessions: %v", err)
	}
	if got := liveSessions(t, ctx, conn, svc, person); got != 3 {
		t.Fatalf("%d live sessions before the two commands, want the three this case counts", got)
	}

	// The winner: it removes the rows, publishes the events, and then holds its
	// transaction open with the row locks still held, so the loser's DELETE is
	// made to wait behind a write that has not settled.
	held, release := make(chan struct{}), make(chan struct{})
	winner := make(chan int, 1)
	go func() { winner <- revokeEverything(ctx, conn, svc, person, held, release) }()
	<-held

	// The loser starts while the winner is still uncommitted. Its read sees the
	// rows as committed — that is what read committed means — and its DELETE is
	// the statement that has to wait.
	loser := make(chan int, 1)
	go func() { loser <- revokeEverything(ctx, conn, svc, person, nil, nil) }()

	// Both are inside the command; let the loser reach its DELETE, then let the
	// winner commit. Whichever order the two writes settle in, this person's
	// sessions are gone exactly once.
	time.Sleep(750 * time.Millisecond)
	close(release)

	first, second := <-winner, <-loser
	if first+second != 3 {
		t.Errorf("the two commands together report %d sessions revoked, want 3: a command returned the rows its read listed rather than the ones its DELETE removed", first+second)
	}
	if got := liveSessions(t, ctx, conn, svc, person); got != 0 {
		t.Fatalf("%d sessions still live after both commands, want none", got)
	}

	// And the trail, read after both transactions have settled: one event per
	// session that left, and not one per session somebody's snapshot listed.
	var events int64
	err = db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventSessionRevoked).
			Count(&events).Error
	})
	if err != nil {
		t.Fatalf("count the revocations: %v", err)
	}
	if events != 3 {
		t.Errorf("the outbox holds %d revocations for 3 sessions, want one per session that left; a revocation the trail reports for a session nobody removed is a claim no command acted on", events)
	}
}

// revokeEverything signs this person out of everything and reports how many
// sessions the command says it removed. The channels, when given, hold the
// transaction open after the work, which is how this case keeps a second
// command from reading a state that has already settled.
func revokeEverything(ctx context.Context, conn *db.Conn, svc *internal.Service, person uuid.UUID, held, release chan struct{}) int {
	var n int
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		removed, err := svc.RevokeAllSessions(ctx, tx, person)
		n = removed
		if held != nil {
			close(held)
			<-release
		}
		return err
	})
	if err != nil {
		panic("revoke everything: " + err.Error())
	}
	return n
}

// liveSessions counts this person's session rows — the state, which the case
// pins before and after so the argument about the trail cannot be won by a
// command that quietly revoked nothing.
func liveSessions(t *testing.T, ctx context.Context, conn *db.Conn, svc *internal.Service, person uuid.UUID) int {
	t.Helper()
	var rows int64
	err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("sessions").Where("user_id = ?", person).Count(&rows).Error
	})
	if err != nil {
		t.Fatalf("count the sessions: %v", err)
	}
	return int(rows)
}
