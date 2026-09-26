package httpx

// context_keys.go declares the three keys the request path shares — the request
// id, the response buffer and the request transaction — together, because a key
// whose type is unexported is a slot nothing outside this package can fill. The
// two keys with an exported accessor of their own, the request in request.go and
// the connection in authenticate.go, stay beside the middleware that puts them
// there. TxFrom is here because a handler reading the transaction is reading a
// key, not a middleware.

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
)

type (
	requestIDKey struct{}
	bufferKey    struct{}
	txKey        struct{}
)

// TxFrom returns the request's tenant transaction, opening it if this is the
// first query of the request. It reports false for a request that resolved to
// no tenant, and for one whose transaction could not be opened — the middleware
// logs that failure with its cause, and the handler's own error becomes the
// response.
//
// Opening on demand rather than on arrival is what lets a liveness probe reach
// a tenant host while the database is down: a request that never queries never
// needs a database.
//
// This is how a handler reaches the database: there is no other door, and a
// repository that takes db.Tx[db.Tenant] cannot be called without going through
// one.
func TxFrom(ctx context.Context) (db.Tx[db.Tenant], bool) {
	p, ok := ctx.Value(txKey{}).(*db.Pending)
	if !ok {
		return db.Tx[db.Tenant]{}, false
	}
	tx, err := p.Tx(ctx)
	if err != nil {
		return db.Tx[db.Tenant]{}, false
	}
	return tx, true
}
