package httpx

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
)

// RequestTx is the request's opened transaction with its shared handle, the one Close
// commits through, for cases that fault that COMMIT. TxFrom hands a handler only an
// operation of it, a clone whose changes never reach the shared handle.
func RequestTx(ctx context.Context) (db.Tx[db.Tenant], bool) {
	p, ok := ctx.Value(txKey{}).(*db.Pending)
	if !ok {
		return db.Tx[db.Tenant]{}, false
	}
	return p.Opened()
}
