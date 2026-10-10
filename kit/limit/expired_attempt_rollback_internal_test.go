package limit

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestAnExpiredAttemptRollsBackBeforeReportingBusy(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	p := postgres{conns: func(context.Context) (*db.Conn, bool) { return conn, true }}
	wrote := false
	err := p.run(t.Context(), func(ctx context.Context, tx db.Tx[db.System]) error {
		if err := tx.DB().Exec("INSERT INTO platformkit_limits (key, count) VALUES (?, ?)", "expired-attempt", 1).Error; err != nil {
			return err
		}
		wrote = true
		<-ctx.Done()
		return driver.ErrBadConn
	})
	if !wrote {
		t.Fatalf("attempt did not reach its write: %v", err)
	}
	if !errors.Is(err, ErrBusy) {
		t.Errorf("expired attempt = %v; want ErrBusy", err)
	}
	var count int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_limits WHERE key = $1", "expired-attempt").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expired attempt left %d rows; want no committed counter", count)
	}
}
