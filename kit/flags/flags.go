// Package flags evaluates optional product behavior. Flags never grant
// permissions, change tenant isolation, or replace subscription entitlements.
package flags

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Evaluator returns the explicit fallback with Defaulted set on every error.
// Evaluate once before an operation, using facts loaded by its trusted owner.
type Evaluator interface {
	Boolean(context.Context, string, Subject, bool) (Decision, error)
}

// Scope is fixed at composition, never selected from request input.
type Scope struct {
	Application  string
	Installation string
	Environment  string
}

// Subject belongs to the resolved tenant. TargetingKey is a stable, preferably
// pseudonymous account or actor identifier supplied by the application.
type Subject struct {
	TenantID     uuid.UUID
	TargetingKey string
}

type Decision struct {
	Value     bool
	Variant   string
	Defaulted bool // the caller's fallback was used because evaluation failed
}

var (
	ErrInvalid      = errors.New("flags: invalid evaluation or configuration")
	ErrUnavailable  = errors.New("flags: provider unavailable")
	ErrNotFound     = errors.New("flags: flag not found")
	ErrTypeMismatch = errors.New("flags: flag is not boolean")
	ErrClosed       = errors.New("flags: evaluator closed")
)
