package httpx

// schemas.go is the other half of a resource's registration: the routes go to
// huma, and the resource itself is recorded here, so that something which did
// not compile against the entity can still list, read and write it.
//
// It is what makes ARCHITECTURE.md's eighth idea reachable. kit/rest already
// derived a schema from every Spec, and until now nothing read it — the screens
// that were supposed to be generated from it did not exist yet. This is the
// register they are generated from, and modules/admin is its one reader.
//
// The kernel stays ignorant of kit/rest: a Resource is declared here, kit/rest
// fills one in, and nothing in this package imports it back.

import (
	"context"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
)

// Resource is one entity as a screen sees it: what it is called, where its API
// lives, which permissions guard it, what shape it has, and the five operations
// bound to its type.
//
// The operations are closures because generics do not survive the trip: a
// screen knows a resource by name and cannot name its Go type, so the type is
// closed over at registration instead. Each runs inside the request's own
// transaction, which it takes from the context — a screen holds no capability
// the route beside it does not.
//
// Rows are maps because that is what a schema-driven screen renders: a column
// is a field name from Schema, and a value is whatever the entity's own JSON
// says it is. There is no second serialization; it is encoding/json, once.
type Resource struct {
	Module, Entity, Path string
	// Read and Write are the permissions the Spec declared. A screen carries
	// the same ones, so a person who cannot use the API cannot use the screen.
	Read, Write string
	// Immutable are the fields a command owns, shown read-only in a form.
	Immutable []string
	Schema    crud.Schema

	List   func(ctx context.Context, q crud.Query) ([]map[string]any, int64, error)
	Get    func(ctx context.Context, id uuid.UUID) (map[string]any, error)
	Create func(ctx context.Context, values map[string]any) (map[string]any, error)
	Update func(ctx context.Context, id uuid.UUID, values map[string]any) (map[string]any, error)
	Delete func(ctx context.Context, id uuid.UUID) error
}

// RegisterResource records a resource. kit/rest calls it from Spec.Mount, in
// the same breath as the routes, so a resource and its API cannot disagree
// about a permission or a path.
func (a *API) RegisterResource(r Resource) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.resources = append(a.resources, r)
}

// Resources is every registered resource, in mount order. modules/admin reads
// it in Routes, which is why the shell is composed last: a module that mounts
// after it registers a resource no screen was generated for.
func (a *API) Resources() []Resource {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Resource(nil), a.resources...)
}
