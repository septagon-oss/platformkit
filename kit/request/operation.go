package request

import (
	"context"
	"github.com/septagon-oss/platformkit/kit/operation"
	"sync/atomic"
	"time"
)

// Operation is the portable intent value, independent of request.Context.
type Operation = operation.Identity

// Outcome is an authoritative result; empty means unknown.
type Outcome = operation.Outcome

const (
	Succeeded       = operation.Succeeded
	Failed          = operation.Failed
	PendingApproval = operation.PendingApproval
)

// ValidToken applies the shared operation ID grammar without HTTP quoting.
func ValidToken(id string) bool { return operation.ValidToken(id) }

type operationKey struct{}
type operationContext struct {
	value    Operation
	deferred atomic.Bool
	clock    func() time.Time
}

// WithOperation installs immutable causality for this execution. A zero value
// masks inherited causality, as required for historical event deliveries.
func WithOperation(ctx context.Context, op Operation) context.Context {
	var clock func() time.Time
	if prior, ok := ctx.Value(operationKey{}).(*operationContext); ok {
		clock = prior.clock
	}
	return context.WithValue(ctx, operationKey{}, &operationContext{value: op, clock: clock})
}

// OperationFrom returns the execution's causality, without creating request facts.
func OperationFrom(ctx context.Context) (Operation, bool) {
	s, ok := ctx.Value(operationKey{}).(*operationContext)
	if !ok || s.value == (Operation{}) {
		return Operation{}, false
	}
	return s.value, true
}

// DeferOperation leaves completion to the owner's durable continuation. It must
// be called before returning the accepted synchronous request.
func DeferOperation(ctx context.Context) {
	if s, ok := ctx.Value(operationKey{}).(*operationContext); ok {
		s.deferred.Store(true)
	}
}

// OperationDeferred reports whether the execution explicitly deferred completion.
func OperationDeferred(ctx context.Context) bool {
	s, ok := ctx.Value(operationKey{}).(*operationContext)
	return ok && s.deferred.Load()
}

// WithOperationClock installs the root's explicit clock for nested completion.
// Nil means the owner must supply its own instant rather than reading this slot.
func WithOperationClock(ctx context.Context, op Operation, clock func() time.Time) context.Context {
	return context.WithValue(ctx, operationKey{}, &operationContext{value: op, clock: clock})
}

// OperationTime returns the root's instant when it supplied a clock, otherwise
// the caller's explicit fallback. Storage normalizes it to UTC microseconds.
func OperationTime(ctx context.Context, fallback time.Time) time.Time {
	if s, ok := ctx.Value(operationKey{}).(*operationContext); ok && s.clock != nil {
		return s.clock()
	}
	return fallback
}
