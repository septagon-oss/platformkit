package cache

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Pool bounds composed values across tenant scopes in one namespace. It shares
// Group's retention engine and owns no connection, closer, or background worker.
type Pool[V any] struct {
	group     *Group[V]
	namespace string
}

// PoolOf creates an empty pool. Invalid configuration refuses before any store
// operation; ttl and max must be positive and the namespace must be one segment.
func PoolOf[V any](c Cache, namespace string, ttl time.Duration, max int) (*Pool[V], error) {
	if c == nil || badNamespace(namespace) || ttl <= 0 || max <= 0 {
		return nil, errors.New("cache: pool requires a cache, namespace, positive TTL and bound")
	}
	g := GroupOf(c, Shared(namespace), ttl, max, func(context.Context, Key) (V, error) {
		var zero V
		return zero, errors.New("cache: pool loader is required")
	})
	return &Pool[V]{group: g, namespace: namespace}, nil
}
func (p *Pool[V]) valid(key Key) error {
	if err := key.valid(); err != nil {
		return err
	}
	if key.scope.tenant == uuid.Nil || key.scope.ns != p.namespace {
		return ErrScope
	}
	return nil
}

// Load composes a miss once per full tenant key. The loader must capture only
// detached immutable inputs; grants and request transactions must stay outside it.
func (p *Pool[V]) Load(ctx context.Context, key Key, compose func(context.Context) (V, error)) (V, error) {
	var zero V
	if err := p.valid(key); err != nil {
		return zero, err
	}
	if compose == nil {
		return zero, errors.New("cache: pool loader is required")
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	v, err := p.group.load(ctx, key, compose)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return zero, err
	}
	return v, nil
}

// Forget invalidates one admitted tenant key locally and in the shared store.
func (p *Pool[V]) Forget(ctx context.Context, key Key) error {
	if err := p.valid(key); err != nil {
		return err
	}
	return p.group.Forget(ctx, key)
}
