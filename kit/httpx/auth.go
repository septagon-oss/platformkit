package httpx

import (
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
	feature    string
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
		Feature    string   `json:"feature,omitempty"`
	}{a.kind, a.permission, a.feature})
}

// Needing returns this declaration for an operation that is part of a plan
// feature: the caller must hold the permission and the tenant's plan must
// include the named feature, in that order.
//
// It is a method on Auth rather than a field of the operation because the two
// questions belong together — "who may do this, and is it in what they are
// paying for" — and because Auth is already the one typed value the document
// publishes and the middleware enforces. A second extension key would be a
// second thing to forget.
//
// The name is the plan's, not a permission: what a feature entitles somebody to
// is the module's business, so a plan can be re-priced without touching an
// authorization. See modules/billing.Plan.Features.
func (a Auth) Needing(feature string) Auth {
	if feature == "" {
		panic("httpx.Needing: a feature is a name; use the declaration without it")
	}
	a.feature = feature
	return a
}

// Feature is the plan feature this declaration requires, or empty when it
// requires none. kit/app reads it to refuse a startup that declares features
// with nothing to answer them.
func (a Auth) Feature() string { return a.feature }

// Declared reports whether a came from one of the four constructors. The zero
// Auth is not a declaration: Go lets any package write httpx.Auth{}, and an
// empty struct must never read as "public".
//
// It is exported because kit/rest asks it about an optional field — a command
// that names no Auth takes its Spec's — and that question is the same one this
// package asks before it mounts anything.
func (a Auth) Declared() bool {
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
	if !ok || !a.Declared() {
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

// String is the declaration as prose: what a refusal message, a Mounted record
// and the boot log print. It is the same four names the OpenAPI document
// carries, spelled the way a person quotes them — "permission task:read" rather
// than a JSON object — because the place it is read most is an error message
// somebody has to act on.
func (a Auth) String() string {
	switch a.kind {
	case kindPermission:
		return "permission " + a.permission
	case kindOperator:
		return "operator_permission " + a.permission
	case kindPublic:
		return "public"
	case kindSignedIn:
		return "signed_in"
	}
	return "undeclared"
}

// Operator reports whether this declaration is the installation's rather than a
// customer's. The question is asked in two places — kit/rest, to decide which
// router a command belongs on, and this package's own authorizer, before it
// asks anybody's roles table — and it is one question, so it has one answer.
func (a Auth) Operator() bool { return a.kind == kindOperator }
