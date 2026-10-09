package limit

// attempt_wall_internal_test.go pins whose wall classified reads. The table in
// classified_internal_test.go hands classified a context directly; this case goes
// through run against a real store, so it fails if run hands classified the
// caller's context instead of the attempt's own detached wall. The two can
// disagree in both directions: the attempt drops the caller's deadline, and the
// attempt's wall can expire while the caller's request lives on.

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestABadConnectionIsJudgedByTheAttemptsOwnWallNotTheCallers: a bare
// driver.ErrBadConn that arrives once the attempt's wall has expired is a refusal
// even though the caller's context never expires; one that arrives at once is an
// outage even though the caller's own deadline has already passed.
func TestABadConnectionIsJudgedByTheAttemptsOwnWallNotTheCallers(t *testing.T) {
	_, conn := dbtest.Schema(t)
	p := postgres{conns: func(context.Context) (*db.Conn, bool) { return conn, true }}
	badConn := fmt.Errorf("db: begin: %w", driver.ErrBadConn)

	caller := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: [16]byte{1}, Slug: "acme"})
	err := p.run(caller, func(ctx context.Context, _ db.Tx[db.System]) error {
		<-ctx.Done()
		return badConn
	})
	if !errors.Is(err, ErrBusy) {
		t.Errorf("a bad connection after the attempt's %s wall, caller still live = %v; want ErrBusy, "+
			"a refusal rather than an error a fail-open caller admits", budget, err)
	}

	late, cancel := context.WithDeadline(caller, time.Now().Add(-time.Second))
	defer cancel()
	err = p.run(late, func(context.Context, db.Tx[db.System]) error { return badConn })
	if err == nil || errors.Is(err, ErrBusy) {
		t.Errorf("a bad connection at once, attempt's wall still standing, caller's deadline past = %v; "+
			"want the outage kept as an error", err)
	}
}
