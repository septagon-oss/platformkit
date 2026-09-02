package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// AuthExtension is the OpenAPI extension an operation's declaration is written
// to. It is a document key as well as a runtime one: the declaration a reviewer
// reads in /openapi.json is the value the middleware enforces, because Register
// writes it once and both sides read that same map.
const AuthExtension = "x-platformkit-auth"

// authKind is the closed set of things an operation can say about who may call
// it. There are four, and Auth has one constructor for each.
type authKind string

const (
	kindPermission authKind = "permission"
	// kindOperator is a permission only the operator's own tenant may exercise
	// at all. It is a kind of its own rather than a flag inside kindPermission
	// so that the declaration a reviewer reads in /openapi.json says which of
	// the two a route is, and so that a route and a manifest disagreeing about
	// it is a startup failure rather than a silent widening.
	kindOperator authKind = "operator_permission"
	kindPublic   authKind = "public"
	kindSignedIn authKind = "signed_in"
)

// permissionToken is the grammar of a permission: "<resource>:<action>", both
// lower-case identifiers. Stating it as a regexp here means a typo in a route
// declaration is caught where the route is written, not by a policy engine that
// silently answers "no" to a permission nobody grants.
var permissionToken = regexp.MustCompile(`^[a-z][a-z0-9_]*:[a-z][a-z0-9_]*$`)

// ValidPermission reports whether token is a well-formed permission. kit/module
// checks a manifest's permission keys with it, so the grammar exists once.
func ValidPermission(token string) bool { return permissionToken.MatchString(token) }

// Auth is the authorization an operation declares. Its fields are unexported
// and its constructors are the only way to build a usable value, so "some
// operation declares an authorization I did not think of" is not expressible.
type Auth struct {
	kind       authKind
	permission string
}

// Permission requires the caller to hold token, checked against Options.Authorize
// in the tenant the request resolved to.
//
// An ill-formed token is a wiring mistake rather than a request-time condition,
// so it panics at the registration site instead of turning into a permission
// nobody can ever hold.
func Permission(token string) Auth {
	if !ValidPermission(token) {
		panic(fmt.Sprintf("httpx.Permission(%q): a permission is %q, both lower-case identifiers", token, "<resource>:<action>"))
	}
	return Auth{kind: kindPermission, permission: token}
}

// OperatorPermission requires the caller to hold token, and requires the tenant
// the request resolved to be the operator's own.
//
// It exists because the control plane is served at every tenant's host — an
// installation has no host of its own, only its customers' — so a permission
// alone does not guard it: a customer's administrator holds the wildcard in
// their own tenant, and that wildcard used to list, create and suspend the
// tenants beside them. The kernel refuses such a route on an ordinary tenant
// before it asks the Authorizer anything, and no wildcard satisfies one.
func OperatorPermission(token string) Auth {
	if !ValidPermission(token) {
		panic(fmt.Sprintf("httpx.OperatorPermission(%q): a permission is %q, both lower-case identifiers", token, "<resource>:<action>"))
	}
	return Auth{kind: kindOperator, permission: token}
}

// Public admits every caller, signed in or not. It is the declaration that has
// to be justified in review.
func Public() Auth { return Auth{kind: kindPublic} }

// SignedIn admits any caller carrying a principal for the resolved tenant,
// whatever that principal may do. It is for operations about the caller
// themselves, where there is no resource to name a permission on.
func SignedIn() Auth { return Auth{kind: kindSignedIn} }

// MarshalJSON writes the declaration into the OpenAPI document as
// {"kind":"permission|public|signed_in","permission":"..."}. huma renders the
// YAML spec by converting the JSON one, so this is the only encoder needed.
func (a Auth) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind       authKind `json:"kind"`
		Permission string   `json:"permission,omitempty"`
	}{a.kind, a.permission})
}

// declared reports whether a came from one of the three constructors. The zero
// Auth is not a declaration: Go lets any package write httpx.Auth{}, and an
// empty struct must never read as "public".
func (a Auth) declared() bool {
	switch a.kind {
	case kindPublic, kindSignedIn:
		return true
	case kindPermission, kindOperator:
		return ValidPermission(a.permission)
	default:
		return false
	}
}

// grant is the permission question this declaration asks, and whether it asks
// one at all. Public and SignedIn ask none.
func (a Auth) grant() (tenancy.Grant, bool) {
	switch a.kind {
	case kindPermission:
		return tenancy.Grant{Permission: a.permission}, true
	case kindOperator:
		return tenancy.Grant{Permission: a.permission, Operator: true}, true
	}
	return tenancy.Grant{}, false
}

// declarationOf returns the authorization op declares, and whether it declares
// one at all.
//
// Only a value this package minted counts. An operation registered straight
// through huma carries nothing under the key; one carrying a hand-written map
// carries something that is not an Auth. Both are undeclared, which is what
// ValidateDeclarations reports and what the middleware denies.
//
// Because the declaration is one typed value under one key, "declares two
// contradictory authorizations" is not a state a caller can reach; the older
// design this replaces used three independent extension keys and had to check
// for it on every request.
func declarationOf(op *huma.Operation) (Auth, bool) {
	if op == nil || op.Extensions == nil {
		return Auth{}, false
	}
	a, ok := op.Extensions[AuthExtension].(Auth)
	if !ok || !a.declared() {
		return Auth{}, false
	}
	return a, true
}

// declare writes auth into op.Extensions, which is where the recorder, the
// OpenAPI document and the request-time middleware all read it.
func declare(op *huma.Operation, auth Auth) {
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions[AuthExtension] = auth
}

// Register mounts an operation together with the authorization it declares. It
// is the only way a module registers a handler, and the declaration is a
// parameter rather than a field, so an operation cannot be written without one.
func Register[I, O any](api *API, op huma.Operation, auth Auth, handler func(context.Context, *I) (*O, error)) {
	if !auth.declared() {
		panic("httpx.Register: " + describe(&op) + " was passed the zero Auth; use Permission, Public or SignedIn")
	}
	declare(&op, auth)
	huma.Register(api.api, op, handler)
}
