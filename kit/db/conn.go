package db

import "context"

// conn.go carries the application's connection pool on the context that work runs
// under, so that code a transaction was handed to can reach the database again
// after that transaction has ended.
//
// One place names it, and its owner is this package: a request gets it from
// kit/httpx's transaction middleware, an event delivery from kit/events' consumer,
// and the context an effect deferred with AfterCommit is handed gets it from the
// transaction that just committed — the pool it ran on, which that effect has no
// other way to name. A service that has to write outside the transaction it was
// handed — to record the outcome of something that can only be known once that
// transaction committed — reads it here and opens its own with Run on Detached(ctx);
// a service that never leaves its transaction never asks.

type connKey struct{}

// ConnFrom is the application connection this work is served on, and reports false
// for a context nothing opened a pool on.
func ConnFrom(ctx context.Context) (*Conn, bool) {
	c, ok := ctx.Value(connKey{}).(*Conn)
	return c, ok
}

// WithConn puts a connection on ctx, so that a layer underneath the one that was
// handed the transaction can reach the pool. It is the position kit/httpx fills for
// every request it serves and kit/events fills for every delivery it runs.
func WithConn(ctx context.Context, c *Conn) context.Context {
	return context.WithValue(ctx, connKey{}, c)
}
