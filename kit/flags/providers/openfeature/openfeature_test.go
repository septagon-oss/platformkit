package openfeature_test

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	sdk "github.com/open-feature/go-sdk/openfeature"
	"github.com/septagon-oss/platformkit/kit/flags"
	"github.com/septagon-oss/platformkit/kit/flags/providers/openfeature"
)

type testProvider struct {
	sdk.NoopProvider
	evaluate func(context.Context, string, bool, sdk.FlattenedContext) sdk.BoolResolutionDetail
	initErr  error
	stopped  atomic.Int32
}

func (p *testProvider) BooleanEvaluation(ctx context.Context, key string, fallback bool, subject sdk.FlattenedContext) sdk.BoolResolutionDetail {
	return p.evaluate(ctx, key, fallback, subject)
}
func (p *testProvider) Init(sdk.EvaluationContext) error { return p.initErr }
func (p *testProvider) Shutdown()                        { p.stopped.Add(1) }
func (p *testProvider) InitWithContext(ctx context.Context, _ sdk.EvaluationContext) error {
	return errors.Join(ctx.Err(), p.initErr)
}
func (p *testProvider) ShutdownWithContext(context.Context) error {
	p.Shutdown()
	return nil
}

func testEvaluator(t *testing.T, scope flags.Scope, provider *testProvider, timeout time.Duration) *openfeature.Evaluator {
	t.Helper()
	evaluator, err := openfeature.New(t.Context(), scope, provider, timeout)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := evaluator.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return evaluator
}

func TestScopedEvaluationsRemainIndependent(t *testing.T) {
	tenants := []uuid.UUID{uuid.New(), uuid.New()}
	scopes := []flags.Scope{{Application: "app/one", Installation: "two", Environment: "test"}, {Application: "app", Installation: "one/two", Environment: "test"}}
	seen := make(chan sdk.FlattenedContext, 20)
	providers := make([]*openfeature.Evaluator, len(scopes))
	for i, scope := range scopes {
		provider := &testProvider{evaluate: func(_ context.Context, _ string, _ bool, facts sdk.FlattenedContext) sdk.BoolResolutionDetail {
			seen <- maps.Clone(facts)
			facts["application"] = "provider mutation"
			return sdk.BoolResolutionDetail{Value: scope.Application == "app/one"}
		}}
		providers[i] = testEvaluator(t, scope, provider, time.Second)
	}
	var pending sync.WaitGroup
	for i := range 20 {
		pending.Go(func() {
			index := i % 2
			subject := flags.Subject{TenantID: tenants[(i/2)%2], TargetingKey: "account/one"}
			decision, err := providers[index].Boolean(t.Context(), "new-editor", subject, false)
			if err != nil || decision.Value != (index == 0) || decision.Defaulted {
				t.Errorf("composition %d: decision=%+v error=%v", index, decision, err)
			}
		})
	}
	pending.Wait()
	close(seen)
	keys := map[string]bool{}
	for facts := range seen {
		key := facts[sdk.TargetingKey].(string)
		var parts []string
		if err := json.Unmarshal([]byte(key), &parts); err != nil || len(parts) != 5 || parts[4] != "account/one" {
			t.Fatalf("invalid scoped targeting identity: %q", key)
		}
		if facts["application"] != parts[0] || facts["installation"] != parts[1] || facts["environment"] != "test" || facts["tenant_id"] != parts[3] {
			t.Fatalf("scope was mixed or mutated: %v", facts)
		}
		if parts[3] != tenants[0].String() && parts[3] != tenants[1].String() {
			t.Fatalf("another tenant reached the provider: %q", parts[3])
		}
		keys[key] = true
	}
	if len(keys) != 4 {
		t.Fatal("two applications and two tenants must have four distinct targeting identities")
	}
}

func TestEvaluationDefaultsAndSafeErrors(t *testing.T) {
	scope := flags.Scope{Application: "app", Installation: "installation", Environment: "test"}
	subject := flags.Subject{TenantID: uuid.New(), TargetingKey: "actor"}
	cases := []struct {
		name   string
		result sdk.BoolResolutionDetail
		want   error
	}{
		{"evaluated false", sdk.BoolResolutionDetail{ProviderResolutionDetail: sdk.ProviderResolutionDetail{Variant: "control", Reason: sdk.DefaultReason}}, nil},
		{"missing", sdk.BoolResolutionDetail{ProviderResolutionDetail: sdk.ProviderResolutionDetail{ResolutionError: sdk.NewFlagNotFoundResolutionError("secret")}}, flags.ErrNotFound},
		{"wrong type", sdk.BoolResolutionDetail{ProviderResolutionDetail: sdk.ProviderResolutionDetail{ResolutionError: sdk.NewTypeMismatchResolutionError("secret")}}, flags.ErrTypeMismatch},
		{"unavailable", sdk.BoolResolutionDetail{ProviderResolutionDetail: sdk.ProviderResolutionDetail{ResolutionError: sdk.NewGeneralResolutionError("secret")}}, flags.ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := &testProvider{evaluate: func(context.Context, string, bool, sdk.FlattenedContext) sdk.BoolResolutionDetail {
				return tc.result
			}}
			evaluator := testEvaluator(t, scope, provider, time.Second)
			for _, fallback := range []bool{false, true} {
				decision, err := evaluator.Boolean(t.Context(), "editor", subject, fallback)
				if !errors.Is(err, tc.want) || decision.Defaulted != (tc.want != nil) {
					t.Fatalf("decision=%+v error=%v", decision, err)
				}
				if tc.want != nil && (decision.Value != fallback || decision.Variant != "" || strings.Contains(err.Error(), "secret")) {
					t.Fatalf("unsafe failure: decision=%+v error=%v", decision, err)
				}
				if tc.want == nil && (decision.Value || decision.Variant != "control") {
					t.Fatalf("a configured false must differ from failure: %+v", decision)
				}
			}
		})
	}
}

func TestCancellationValidationAndLifecycle(t *testing.T) {
	var calls atomic.Int32
	provider := &testProvider{evaluate: func(ctx context.Context, _ string, _ bool, _ sdk.FlattenedContext) sdk.BoolResolutionDetail {
		calls.Add(1)
		<-ctx.Done()
		return sdk.BoolResolutionDetail{Value: false}
	}}
	evaluator := testEvaluator(t, flags.Scope{Application: "app", Installation: "one", Environment: "test"}, provider, 20*time.Millisecond)
	subject := flags.Subject{TenantID: uuid.New(), TargetingKey: "actor"}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	decision, err := evaluator.Boolean(ctx, "editor", subject, true)
	if !errors.Is(err, context.Canceled) || !decision.Value || !decision.Defaulted || calls.Load() != 0 {
		t.Fatalf("cancelled request reached provider: %+v %v", decision, err)
	}
	for _, invalid := range []flags.Subject{{}, {TenantID: subject.TenantID}, {TargetingKey: "actor"}, {TenantID: subject.TenantID, TargetingKey: "\xff"}} {
		if decision, err := evaluator.Boolean(t.Context(), "editor", invalid, true); !errors.Is(err, flags.ErrInvalid) || !decision.Value || !decision.Defaulted {
			t.Fatalf("invalid subject accepted: %+v %v", decision, err)
		}
	}
	decision, err = evaluator.Boolean(t.Context(), "editor", subject, true)
	if !errors.Is(err, context.DeadlineExceeded) || !decision.Value || !decision.Defaulted || calls.Load() != 1 {
		t.Fatalf("evaluation deadline or fallback lost: %+v %v", decision, err)
	}
	var pending sync.WaitGroup
	for range 10 {
		pending.Go(func() {
			if err := evaluator.Close(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	pending.Wait()
	if provider.stopped.Load() != 1 {
		t.Fatal("provider shutdown was not exactly once")
	}
	if decision, err := evaluator.Boolean(t.Context(), "editor", subject, true); !errors.Is(err, flags.ErrClosed) || !decision.Value || !decision.Defaulted {
		t.Fatalf("closed evaluator accepted work: %+v %v", decision, err)
	}
}

func TestFailedInitializationCleansUp(t *testing.T) {
	provider := &testProvider{initErr: errors.New("secret provider diagnostics")}
	evaluator, err := openfeature.New(t.Context(), flags.Scope{Application: "app", Installation: "one", Environment: "test"}, provider, time.Second)
	if evaluator != nil || !errors.Is(err, flags.ErrUnavailable) || strings.Contains(err.Error(), "secret") || provider.stopped.Load() != 1 {
		t.Fatalf("failed provider was not safely released: evaluator=%v error=%v stopped=%d", evaluator, err, provider.stopped.Load())
	}
}

func TestInvalidScopeDoesNotTransferProviderOwnership(t *testing.T) {
	provider := &testProvider{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, scope := range []flags.Scope{{}, {Application: "app", Installation: "\xff", Environment: "test"}} {
		evaluator, err := openfeature.New(ctx, scope, provider, time.Second)
		if evaluator != nil || !errors.Is(err, flags.ErrInvalid) || provider.stopped.Load() != 0 {
			t.Fatalf("invalid scope reached initialization: %v %v stopped=%d", evaluator, err, provider.stopped.Load())
		}
	}
}
