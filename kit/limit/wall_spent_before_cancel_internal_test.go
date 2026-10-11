package limit

// wall_spent_before_cancel_internal_test.go pins the join the other two wall
// cases leave between them. attempt_wall_internal_test.go goes through run but
// waits on <-ctx.Done() before answering, so the cancellation has always been
// delivered by the time classified reads the attempt;
// wall_deadline_internal_test.go reads the deadline with nothing else answering,
// but hands classified a context of its own making. This case goes through run
// against a real store and never touches Done(): it sleeps to the attempt's own
// deadline and answers the pool's bare sentinel, so it fails if run stops
// handing classified a context whose deadline can be read — the fact wallPassed
// rests on — however classified itself would then decide.

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestABadConnectionTheInstantTheWallPassesIsRefusedThroughRun: the attempt
// reads its own wall, outlives it by nothing, and answers driver.ErrBadConn.
// Whether or not the cancellation has been delivered yet, the deadline is
// behind the attempt, so the answer is ErrBusy — a refusal, not the error a
// fail-open composer admits.
func TestABadConnectionTheInstantTheWallPassesIsRefusedThroughRun(t *testing.T) {
	_, conn := dbtest.Schema(t)
	p := postgres{conns: func(context.Context) (*db.Conn, bool) { return conn, true }}
	badConn := fmt.Errorf("db: begin: %w", driver.ErrBadConn)

	err := p.run(t.Context(), func(ctx context.Context, _ db.Tx[db.System]) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			return fmt.Errorf("the attempt carries no wall of its own to read")
		}
		time.Sleep(time.Until(deadline))
		return badConn
	})
	if !errors.Is(err, ErrBusy) {
		t.Errorf("a bad connection the instant the attempt's %s wall passed = %v; "+
			"want ErrBusy, read from the deadline run handed classified", budget, err)
	}
}
