package httpx

// context_keys.go declares the three keys the request path shares — the response
// buffer, the request transaction and the note of the tenant a request resolved
// — together, because a key whose type is unexported is a slot nothing outside
// this package can fill. The keys with an exported accessor of their own stay
// beside the middleware that puts them there: the connection in authenticate.go,
// and the call's own three facts in kit/request, which is where the outbox and
// the audit trail read them from. TxFrom is here because a handler reading the
// transaction is reading a key, not a middleware.

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
)

type (
	cacheKey      struct{}
	bufferKey     struct{}
	txKey         struct{}
	answerNoteKey struct{}
)

// TxFrom returns the request's tenant transaction, opening it if this is the first query
// of the request. It reports false for a request that resolved to no tenant, and for one
// whose transaction could not be opened — the middleware logs that failure with its cause,
// and the handler's own error becomes the response.
//
// Opening on demand rather than on arrival is what lets a liveness probe reach a tenant
// host while the database is down: a request that never queries never needs a database.
//
// A context already inside a transaction answers with that one, and not with the request's:
// work running under db.Run is inside a transaction whose commit somebody else decided,
// and opening the request's beside it would hold two, pinning one of them open until the
// request ended — the caller that chose the transaction is the caller that ends it.
//
// This is how a handler reaches the database: there is no other door.
func TxFrom(ctx context.Context) (db.Tx[db.Tenant], bool) {
	if tx, ok := db.TxFrom(ctx); ok {
		return tx, true
	}
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

// CacheFrom returns the runtime store borrowed by this real request. Outside a
// request it returns false; it grants neither tenant scope nor source access.
func CacheFrom(ctx context.Context) (cache.Cache, bool) {
	c, ok := ctx.Value(cacheKey{}).(cache.Cache)
	return c, ok && c != nil
}
