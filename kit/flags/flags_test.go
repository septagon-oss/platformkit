package flags_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/flags"
)

// A product can supply immutable definitions without importing any SDK.
type definitions map[string]bool

func (d definitions) Boolean(ctx context.Context, key string, _ flags.Subject, fallback bool) (flags.Decision, error) {
	failed := flags.Decision{Value: fallback, Defaulted: true}
	if err := ctx.Err(); err != nil {
		return failed, err
	}
	value, ok := d[key]
	if !ok {
		return failed, flags.ErrNotFound
	}
	return flags.Decision{Value: value}, nil
}

func ExampleEvaluator() {
	var evaluator flags.Evaluator = definitions{"preview": false}
	subject := flags.Subject{TenantID: uuid.MustParse("bce3e021-5053-4c36-b4bd-c0a03c6f77b7"), TargetingKey: "member-1"}
	decision, err := evaluator.Boolean(context.Background(), "preview", subject, true)
	fmt.Println(decision.Value, decision.Defaulted, err)
	decision, err = evaluator.Boolean(context.Background(), "missing", subject, true)
	fmt.Println(decision.Value, decision.Defaulted, err)
	// Output:
	// false false <nil>
	// true true flags: flag not found
}

func TestScopeRejectsIncompleteOrUnencodableTargetingIdentities(t *testing.T) {
	for _, scope := range []flags.Scope{
		{},
		{Application: "app", Installation: " ", Environment: "test"},
		{Application: "app", Installation: "one", Environment: "\xff"},
	} {
		if scope.Valid() {
			t.Errorf("invalid targeting scope accepted: %+v", scope)
		}
	}
	scope := flags.Scope{Application: "app/one", Installation: "São Paulo", Environment: "test"}
	before := scope
	if !scope.Valid() || scope != before {
		t.Fatal("validation rejected or changed the installation's identity")
	}
}
