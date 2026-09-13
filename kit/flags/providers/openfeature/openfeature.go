// Package openfeature adapts a selected OpenFeature SDK provider to flags.
package openfeature

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
	sdk "github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/isolated"
	"github.com/septagon-oss/platformkit/kit/flags"
)

// Evaluator owns an isolated SDK instance. Supply a fresh provider at the
// composition boundary; SDK types do not enter the application's flag contract.
type Evaluator struct {
	scope     flags.Scope
	timeout   time.Duration
	api       *sdk.EvaluationAPI
	client    *sdk.Client
	closed    atomic.Bool
	closeOnce sync.Once
	closeErr  error
}

var _ flags.Evaluator = (*Evaluator)(nil)

// New waits for provider initialization. Providers must honor
// evaluation contexts; arbitrary blocking provider code cannot be preempted.
// Ownership transfers when initialization begins, including cleanup on failure.
func New(ctx context.Context, scope flags.Scope, provider sdk.FeatureProvider, timeout time.Duration) (*Evaluator, error) {
	if !scope.Valid() || provider == nil || timeout <= 0 {
		return nil, flags.ErrInvalid
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
		return nil, errors.Join(flags.ErrUnavailable, initCtx.Err())
	}
	return &Evaluator{scope: scope, timeout: timeout, api: api, client: api.NewClient()}, nil
}

func validPart(value string) bool {
	return strings.TrimSpace(value) != "" && utf8.ValidString(value)
}

func (p *Evaluator) Boolean(ctx context.Context, key string, subject flags.Subject, fallback bool) (flags.Decision, error) {
	failed := flags.Decision{Value: fallback, Defaulted: true}
	if p == nil || p.closed.Load() {
		return failed, flags.ErrClosed
	}
	if !validPart(key) || subject.TenantID == uuid.Nil || !validPart(subject.TargetingKey) {
		return failed, flags.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return failed, err
	}
	// Encoding the tuple prevents delimiter collisions and cross-tenant bucketing.
	target, _ := json.Marshal([5]string{p.scope.Application, p.scope.Installation,
		p.scope.Environment, subject.TenantID.String(), subject.TargetingKey})
	evaluation := sdk.NewEvaluationContext(string(target), map[string]any{
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
		case sdk.FlagNotFoundCode:
			return failed, flags.ErrNotFound
		case sdk.TypeMismatchCode:
			return failed, flags.ErrTypeMismatch
		case sdk.InvalidContextCode, sdk.TargetingKeyMissingCode:
			return failed, flags.ErrInvalid
		default:
			return failed, flags.ErrUnavailable
		}
	}
	return flags.Decision{Value: result.Value, Variant: result.Variant}, nil
}

// Close releases the provider once. Drain application requests first; provider
// shutdown runs with a bounded context, which custom providers must honor.
func (p *Evaluator) Close(ctx context.Context) error {
	p.closeOnce.Do(func() {
		p.closed.Store(true)
		ctx, cancel := context.WithTimeout(ctx, p.timeout)
		defer cancel()
		if err := p.api.Shutdown(ctx); err != nil {
			p.closeErr = errors.Join(flags.ErrUnavailable, ctx.Err())
		}
	})
	return p.closeErr
}
