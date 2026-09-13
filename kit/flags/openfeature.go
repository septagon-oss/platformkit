package flags

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/isolated"
)

// OpenFeature owns an isolated SDK instance. Supply a fresh provider at the
// composition boundary; SDK types do not enter the application's flag contract.
type OpenFeature struct {
	scope     Scope
	timeout   time.Duration
	api       *openfeature.EvaluationAPI
	client    *openfeature.Client
	closed    atomic.Bool
	closeOnce sync.Once
	closeErr  error
}

var _ Evaluator = (*OpenFeature)(nil)

// NewOpenFeature waits for provider initialization. Providers must honor
// evaluation contexts; arbitrary blocking provider code cannot be preempted.
// Ownership transfers when initialization begins, including cleanup on failure.
func NewOpenFeature(ctx context.Context, scope Scope, provider openfeature.FeatureProvider, timeout time.Duration) (*OpenFeature, error) {
	if !validScope(scope) || provider == nil || timeout <= 0 {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	api := isolated.NewAPI()
	initCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := api.SetProviderAndWait(initCtx, provider); err != nil {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer stop()
		_ = api.Shutdown(cleanup)
		return nil, errors.Join(ErrUnavailable, initCtx.Err())
	}
	return &OpenFeature{scope: scope, timeout: timeout, api: api, client: api.NewClient()}, nil
}

func validScope(scope Scope) bool {
	return validPart(scope.Application) && validPart(scope.Installation) && validPart(scope.Environment)
}

func validPart(value string) bool {
	return strings.TrimSpace(value) != "" && utf8.ValidString(value)
}

func (p *OpenFeature) Boolean(ctx context.Context, key string, subject Subject, fallback bool) (Decision, error) {
	failed := Decision{Value: fallback, Defaulted: true}
	if p == nil || p.closed.Load() {
		return failed, ErrClosed
	}
	if !validPart(key) || subject.TenantID == uuid.Nil || !validPart(subject.TargetingKey) {
		return failed, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return failed, err
	}
	// Encoding the tuple prevents delimiter collisions and cross-tenant bucketing.
	target, _ := json.Marshal([5]string{p.scope.Application, p.scope.Installation,
		p.scope.Environment, subject.TenantID.String(), subject.TargetingKey})
	evaluation := openfeature.NewEvaluationContext(string(target), map[string]any{
		"application": p.scope.Application, "installation": p.scope.Installation,
		"environment": p.scope.Environment, "tenant_id": subject.TenantID.String(),
		"subject": subject.TargetingKey,
	})
	result, err := p.client.BooleanValueDetails(ctx, key, fallback, evaluation)
	if contextErr := ctx.Err(); contextErr != nil {
		return failed, contextErr
	}
	if err != nil {
		switch result.ErrorCode {
		case openfeature.FlagNotFoundCode:
			return failed, ErrNotFound
		case openfeature.TypeMismatchCode:
			return failed, ErrTypeMismatch
		case openfeature.InvalidContextCode, openfeature.TargetingKeyMissingCode:
			return failed, ErrInvalid
		default:
			return failed, ErrUnavailable
		}
	}
	return Decision{Value: result.Value, Variant: result.Variant}, nil
}

// Close releases the provider once. Drain application requests first; provider
// shutdown runs with a bounded context, which custom providers must honor.
func (p *OpenFeature) Close(ctx context.Context) error {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		ctx, cancel := context.WithTimeout(ctx, p.timeout)
		defer cancel()
		if err := p.api.Shutdown(ctx); err != nil {
			p.closeErr = errors.Join(ErrUnavailable, ctx.Err())
		}
	})
	return p.closeErr
}
