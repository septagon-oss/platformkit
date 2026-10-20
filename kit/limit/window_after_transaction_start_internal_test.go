package limit

import (
	"context"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestARetryWindowCreatedAfterTheTransactionStartedIsBounded(t *testing.T) {
	for _, operation := range []string{"Allow", "Count"} {
		t.Run(operation, func(t *testing.T) {
			admin, conn := dbtest.Schema(t)
			p := postgres{conns: func(context.Context) (*db.Conn, bool) { return conn, true }}
			p.attempt = func(ctx context.Context, conn *db.Conn, fn func(context.Context, db.Tx[db.System]) error) error {
				return queueRun(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
					// Another transaction opens the window after this transaction's clock was fixed.
					if _, err := admin.ExecContext(ctx, "INSERT INTO platformkit_limits (key, window_start, count) VALUES ($1, clock_timestamp(), 1)", p.scoped(ctx, "later-window")); err != nil {
						return err
					}
					return fn(ctx, tx)
				})
			}
			const window = time.Hour
			var retry time.Duration
			var err error
			if operation == "Allow" {
				var ok bool
				ok, retry, err = p.Allow(t.Context(), "later-window", 1, window)
				if ok {
					t.Error("an exhausted window admitted another call")
				}
			} else {
				var count int
				count, retry, err = p.Count(t.Context(), "later-window", window)
				if count != 1 {
					t.Errorf("Count = %d; want the committed call", count)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if retry <= 0 || retry >= window {
				t.Errorf("counted window has %s left; want strictly between zero and %s", retry, window)
			}
		})
	}
}
