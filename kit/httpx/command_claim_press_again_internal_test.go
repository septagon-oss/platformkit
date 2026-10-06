package httpx

// claimPressAgain is the one decision that keeps the repeated claim honest, and it is
// worth pinning on its own because the failure it refuses to retry is the one this
// package has already been caught making twice: an error is not a rollback. Every
// behavioural case in this directory asks what the request was answered with; this
// asks the classifier directly, because no fault a test can inject at the database
// reproduces a COMMIT whose acknowledgement is lost, and the whole safety of pressing
// again rests on refusing that one.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

// sqlStateError is the shape the driver's error has for the question this asks: the
// interface claimPressAgain reads is the one the driver satisfies, not the driver.
type sqlStateError string

func (e sqlStateError) Error() string    { return "the database answered " + string(e) }
func (e sqlStateError) SQLState() string { return string(e) }

func TestTheCommandClaimIsPressedForOnlyWhatCannotHaveLanded(t *testing.T) {
	for _, c := range []struct {
		what string
		err  error
		want bool
	}{
		{"a wait the budget refused", sqlStateError("55P03"), true},
		{"two writers who chose badly", sqlStateError("40001"), true},
		{"a deadlock", sqlStateError("40P01"), true},
		{"a connection that went away", sqlStateError("08006"), true},
		{"a server that stopped", sqlStateError("57P01"), true},
		{"this request's own budget", fmt.Errorf("db: run: %w", context.DeadlineExceeded), true},
		{"a commit nobody answered", fmt.Errorf("db: run: %w", db.ErrCommitUnacknowledged), false},
		{"a table that is not there", sqlStateError("42P01"), false},
		{"a policy that refuses", sqlStateError("42501"), false},
		{"a class nobody names", sqlStateError("XX001"), false},
		{"an error that says nothing", errors.New("the connector refused"), false},
	} {
		if got := claimPressAgain(c.err); got != c.want {
			t.Errorf("claimPressAgain(%s) said %v, want %v", c.what, got, c.want)
		}
	}
}
