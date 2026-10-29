package httpx

// authorize.go rechecks the caller's grants and the tenant's plan inside the
// request, publishes the code each refusal carries, and writes the refusal.

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// ExpectedPrincipalHeader optionally pins a protected, state-changing request
// to the actor who prepared it. A mismatch is refused before authorization or
// the handler runs. It is a request precondition, never an authentication input.
const ExpectedPrincipalHeader = "X-Expected-Principal"

// authorize enforces the operation's declaration.
//
// A caller nobody recognised is answered 401 (see anonymous and challenge),
// because signing in is what would change the answer. Every other refusal is a
// 403: which of the conditions failed says nothing an attacker can act on, but
// it says a great deal to whoever is debugging a deployment, so the code is in
// the response and the request that carried it is in the log.
func (a *API) authorize(ctx huma.Context, next func(huma.Context)) {
	auth, ok := declarationOf(ctx.Operation())
	if !ok {
		// Defense in depth. kit/app runs ValidateDeclarations before it
		// listens, so reaching this branch means an operation was mounted after
		// the gate ran.
		a.deny(ctx, CodeUndeclared, "this operation declares no authorization")
		return
	}
	if auth.kind == kindPublic {
		// A public operation asks nothing about the caller and may still ask
		// about the tenant: a public site that is part of a paid plan is a
		// public route with a feature on it. The tenant is resolved before this
		// middleware runs, except on a host that resolves to none — and a host
		// with no tenant has no plan, so there is nothing that could include
		// the feature.
		if auth.feature != "" && !a.entitled(ctx, auth) {
			return
		}
		// Not a.pass: a public operation has no account to attribute a refusal to.
		next(ctx)
		return
	}

	p, admitted := a.admitPrincipal(ctx, auth)
	if !admitted {
		return
	}
	t, hasTenant := tenancy.FromContext(ctx.Context())
	if !hasTenant {
		a.deny(ctx, CodeNoTenant, "this operation is tenant work and the host resolved to none")
		return
	}
	if !a.admitCredential(ctx, auth, p) {
		return
	}
	if auth.kind == kindSignedIn || auth.kind == kindAnyCredential || auth.kind == kindBearerKey {
		if !a.entitled(ctx, auth) {
			return
		}
		// A declaration that names no grant still reaches the row question: this is
		// the shape a signed-in member resolves an object of their own with.
		a.pass(ctx, next)
		return
	}

	if !a.admitGrant(ctx, auth, p, t) {
		return
	}
	// The plan question last, so a caller who may not do this at all is told
	// that and not told to buy something. The order matters for what a person
	// reads: "ask your administrator" and "upgrade" are different sentences and
	// only one of them is true.
	if !a.entitled(ctx, auth) {
		return
	}
	a.pass(ctx, next)
}

// pass runs the operation a guard admitted. The grant is the first question;
// which row is the second, and a module asks it through tenancy.RequirePolicy
// after this middleware is done. The observer installed here records that refusal
// as a fact of this request and hands it to the audit composition, whatever
// response the module maps it to — and it goes in behind either guard, which is
// the point: a signed-in-only command auditing nothing while a command of a
// named grant audits everything is the drift this method exists to end.
//
// A public operation is passed none. Options.Denied declines a denial with no
// account behind it — a row per probe would make every stranger's request a
// database write — and the page a refusal fact draws claims the caller already
// holds what the row's own rule withheld, which is nobody's sentence to read.
func (a *API) pass(ctx huma.Context, next func(huma.Context)) {
	refused := func(_ context.Context, r tenancy.PolicyRequest, d tenancy.PolicyDecision) {
		noteRefused(ctx.Context(), Refusal{
			Code: CodePolicyDenied, Action: r.Action, ResourceKind: r.Resource.Kind,
			ResourceID: r.Resource.ID, Reason: d.Reason, Revision: d.Revision,
			Method: ctx.Method(), Path: ctx.URL().Path,
		})
		a.denied(ctx, http.StatusForbidden, CodePolicyDenied,
			fmt.Sprintf("%s on %s %s: %s (policy %s)", r.Action, r.Resource.Kind, r.Resource.ID, d.Reason, d.Revision))
	}
	next(huma.WithContext(ctx, tenancy.WithPolicyRefusals(ctx.Context(), refused)))
}

// entitled renders the plan decision as the route's own refusal. The decision
// lives in `planDecision` below, which the in-process closures and the doors in
// kit/httpx/schemas.go ask too, so the answer a caller reads at the address and
// the answer a page's door, a navigation entry and a catalogue document give
// cannot drift apart.
//
// A refusal that names a code is a denial, and 402 rather than 403: a plan that
// does not include something is not a permission a person can be granted, and a
// client that cannot tell the two apart shows the wrong way out of both. A
// refusal that names none is nobody's fault and asks to be come back to.
func (a *API) entitled(ctx huma.Context, auth Auth) bool {
	ok, status, code, detail := a.planDecision(ctx.Context(), auth)
	if ok {
		return true
	}
	if code != "" {
		a.refuse(ctx, status, code+": "+detail)
		a.denied(ctx, status, code, detail)
		return false
	}
	if status == http.StatusServiceUnavailable {
		ctx.SetHeader("Retry-After", "3")
	}
	a.refuse(ctx, status, detail)
	return false
}

// planDecision is the one place the plan question a declaration names is asked.
// Its callers render the answer — the middleware as a response, a closure as the
// error it returns — and neither decides anything: a declaration that names no
// feature asks nothing, a tenant whose plan includes it is admitted, a tenant
// whose plan does not is refused 402 with CodePlanExcludes, and an Entitler that
// cannot decide is an outage and not a denial — 503, exactly as an Authorizer
// that cannot is — because billing being unreachable must not read as "your plan
// does not include this".
//
// A host the tenant loader resolved to nobody has no plan that could include
// anything, which is the answer such a host is given wherever else it asks: no
// site is served here.
func (a *API) planDecision(ctx context.Context, auth Auth) (ok bool, status int, code, detail string) {
	if auth.feature == "" {
		return true, 0, "", ""
	}
	t, hasTenant := tenancy.FromContext(ctx)
	if !hasTenant {
		return false, http.StatusNotFound, "", "no site is served at this host"
	}
	path := ""
	if r, isRequest := RequestFrom(ctx); isRequest {
		path = r.URL.Path
	}
	if a.opts.Entitle == nil {
		// Defense in depth, like the undeclared branch above: ValidateDeclarations
		// refuses this composition, so reaching here means the gate did not run.
		// Closed rather than open, and an outage rather than a denial, because
		// the fault is the application's and not the caller's.
		a.rlog(ctx).ErrorContext(ctx, "httpx: an operation declares a feature and nothing answers it",
			"feature", auth.feature, "path", path)
		return false, http.StatusServiceUnavailable, "", "the plan could not be read right now"
	}
	included, err := a.opts.Entitle.Includes(ctx, t, auth.feature)
	if err != nil {
		a.rlog(ctx).ErrorContext(ctx, "httpx: entitlement decision unavailable",
			"feature", auth.feature, "tenant", t.Slug, "path", path, "error", err)
		return false, http.StatusServiceUnavailable, "", "the plan could not be read right now"
	}
	if !included {
		a.rlog(ctx).InfoContext(ctx, "httpx: plan excludes this operation",
			"feature", auth.feature, "tenant", t.Slug, "path", path)
		return false, http.StatusPaymentRequired, CodePlanExcludes,
			"this tenant's plan does not include " + auth.feature
	}
	return true, 0, "", ""
}

// Refusal codes connect guard decisions to localized page messages and problem
// documents. CodeWriteElsewhere and CodePlanExcludes retain their literal details
// because those details carry an address or feature the caller needs.
const (
	CodeAnonymous         = "AUTH_ANONYMOUS"
	CodeDenied            = "AUTH_DENIED"
	CodeNotOperator       = "AUTH_NOT_OPERATOR"
	CodeUndeclared        = "AUTH_UNDECLARED"
	CodeNoTenant          = "AUTH_NO_TENANT"
	CodePrincipalChanged  = "AUTH_PRINCIPAL_CHANGED"
	CodeCSRFOrigin        = "CSRF_ORIGIN"
	CodePublicSetsACookie = "PUBLIC_SETS_A_COOKIE"
	// CodeLimitExhausted is the public surface's own refusal, and the one code
	// here that a caller without a session, a tenant of their own or an account
	// ever reads: the anonymous visitor whose form submissions ran out. They are
	// refused in the shape they asked in, which is why the code is published
	// rather than written into the call that answers.
	CodeLimitExhausted = "LIMIT_EXHAUSTED"
	// CodeWriteElsewhere is the address a write belongs at, named because the
	// caller who asked here is one line away from the right address: they read
	// the resource's own path out of the catalog and the resource's writes are
	// served on another surface. It is a refusal and not a redirect — nothing is
	// written either way, and a redirect would send a POST through an address the
	// caller's own client may then GET.
	CodeWriteElsewhere = "WRITE_ELSEWHERE"
	// CodePlanExcludes names the feature to ask the tenant's plan for.
	CodePlanExcludes = "PLAN_EXCLUDES"
	// CodePolicyDenied is a module's object-scope refusal (tenancy.RequirePolicy): the
	// grant was held and the policy refused this row. kit/rest answers it with the same
	// code.
	CodePolicyDenied = "POLICY_DENIED"
	// The idempotency key's own four refusals, published rather than written into the
	// calls because a caller that sent a key reads all four of them and the document
	// declares the header: a code only visible in a log line is a code a client
	// cannot branch on. The distinction between the last two is the one a client acts
	// on differently — an in-flight write can be retried and will eventually be
	// answered, a spent key can only be abandoned.
	CodeIdempotencyKey         = "IDEMPOTENCY_KEY_INVALID"
	CodeIdempotencyConflict    = "IDEMPOTENCY_KEY_IN_USE"
	CodeIdempotencyInFlight    = "IDEMPOTENCY_IN_FLIGHT"
	CodeIdempotencySpent       = "IDEMPOTENCY_ANSWER_NOT_RECORDED"
	CodeIdempotencyTooLarge    = "IDEMPOTENCY_BODY_TOO_LARGE"
	CodeIdempotencyUnavailable = "IDEMPOTENCY_UNAVAILABLE"
)

// notHere answers the control plane's own answer: the 404 an address nobody
// mounted gets, at the address the control plane is served at. The detail is
// the same sentence, deliberately: a tenant host, or a host that resolves to a
// tenant that is not the installation's, is told nothing beyond "nothing is
// served here", which is the same thing it was told before the surface existed.
func (a *API) notHere(ctx huma.Context) {
	a.rlog(ctx.Context()).InfoContext(ctx.Context(), "httpx: the control plane is not served at this address",
		"method", ctx.Method(), "path", ctx.URL().Path)
	// The same verdict, the same sentence and the same writer as the address nobody
	// mounted — refuse is the fail of the chain, so the answer is dressed by the
	// renderer the never-mounted address is dressed by and the verdict is still
	// decided for the transaction. This is what makes the answer byte-identical
	// to the one a never-mounted address gets, Fault page included, which is the
	// whole point of the 404.
	a.refuse(ctx, http.StatusNotFound, "nothing is served at this address")
}

// deny logs the machine-readable reason and answers 403 with it. The code is
// the first word of the detail so a log line and a response can be matched
// without adding a field to the one error shape kit/problem defines.
//
// Through refuse, because this is the answer six of the codes below travel in,
// and a person who navigated to a page they hold no grant for is the reader
// ui/page's table of sentences exists for. Answering them with the document is
// the defect "a person who navigated and gets JSON".
// challenge refuses a BearerKey operation to a request that brought no key the
// installation recognises: 401 with a Bearer challenge (RFC 6750 §3.1), because a
// key is the only credential that operation accepts, so there is one scheme to
// name. A revoked, expired or unknown key gets the same answer as no key at all,
// so the refusal never says whether a revocation landed.
func (a *API) challenge(ctx huma.Context) {
	a.rlog(ctx.Context()).InfoContext(ctx.Context(), "httpx: authorization denied",
		"code", CodeAnonymous, "method", ctx.Method(), "path", ctx.URL().Path)
	challenge := "Bearer"
	if strings.HasPrefix(ctx.Header("Authorization"), "Bearer ") {
		challenge = `Bearer error="invalid_token"`
	}
	ctx.SetHeader("WWW-Authenticate", challenge)
	a.refuse(ctx, http.StatusUnauthorized, CodeAnonymous+": this operation requires a bearer key")
}

// anonymous refuses a protected operation to a request that brought no
// credential the installation recognises: 401, where a 403 would tell a caller
// who is known that signing in changes nothing. It names no WWW-Authenticate
// scheme, because a session is a cookie and no challenge scheme describes one;
// the BearerKey door, whose one credential has a scheme, answers through
// challenge instead. Options.Denied is not told: it has nobody to attribute
// the refusal to.
func (a *API) anonymous(ctx huma.Context) {
	a.rlog(ctx.Context()).InfoContext(ctx.Context(), "httpx: authorization denied",
		"code", CodeAnonymous, "method", ctx.Method(), "path", ctx.URL().Path)
	a.refuse(ctx, http.StatusUnauthorized, CodeAnonymous+": this operation requires a signed-in caller")
}

func (a *API) deny(ctx huma.Context, code, detail string) {
	a.rlog(ctx.Context()).InfoContext(ctx.Context(), "httpx: authorization denied",
		"code", code, "method", ctx.Method(), "path", ctx.URL().Path)
	a.refuse(ctx, http.StatusForbidden, code+": "+detail)
	a.denied(ctx, http.StatusForbidden, code, detail)
}

// denied hands one refusal to Options.Denied when it has somebody to attribute it
// to. See Options.Denied for why an anonymous refusal is not passed.
func (a *API) denied(ctx huma.Context, status int, code, detail string) {
	if a.opts.Denied == nil {
		return
	}
	p, hasPrincipal := tenancy.PrincipalFrom(ctx.Context())
	t, hasTenant := tenancy.FromContext(ctx.Context())
	if !hasPrincipal || p.UserID == uuid.Nil || !hasTenant {
		return
	}
	operation := ""
	if op := ctx.Operation(); op != nil {
		operation = op.OperationID
	}
	permission, label := "", ""
	if r, ok := Refused(ctx.Context()); ok {
		permission, label = r.Permission, r.Label
	}
	a.opts.Denied(ctx.Context(), Denial{
		Status: status, Code: code, Detail: detail, Method: ctx.Method(), Path: ctx.URL().Path,
		Permission: permission, Label: label,
		Operation: operation, RequestID: requestIDFrom(ctx.Context()), Tenant: t, Principal: p,
	})
}

func (a *API) admitPrincipal(ctx huma.Context, auth Auth) (tenancy.Principal, bool) {
	p, hasPrincipal := tenancy.PrincipalFrom(ctx.Context())
	if !hasPrincipal || p.UserID == uuid.Nil {
		// A page has somewhere to send somebody who has nobody to be; an API
		// route has not. See SignInExtension.
		if to, page := signInFor(ctx); page {
			ctx.SetHeader("Location", to)
			ctx.SetStatus(http.StatusSeeOther)
			return tenancy.Principal{}, false
		}
		if auth.kind == kindBearerKey {
			a.challenge(ctx)
			return tenancy.Principal{}, false
		}
		a.anonymous(ctx)
		return tenancy.Principal{}, false
	}
	if expected := ctx.Header(ExpectedPrincipalHeader); unsafeMethod(ctx.Method()) && expected != "" && expected != p.UserID.String() {
		a.deny(ctx, CodePrincipalChanged, "the signed-in account differs from the one that prepared this request")
		return tenancy.Principal{}, false
	}
	return p, true
}

func (a *API) admitCredential(ctx huma.Context, auth Auth, p tenancy.Principal) bool {
	// Authentication already read the principal inside this tenant's transaction.
	// Scoped credentials cannot use SignedIn to recover their holder's full authority;
	// AnyCredential may admit them because its result is filtered by their grants.
	if auth.kind == kindBearerKey && p.TokenID == uuid.Nil {
		a.deny(ctx, CodeDenied, "this operation acts on the key that authenticated the request, and a session carries none")
		return false
	}
	if auth.kind == kindSignedIn && p.Permissions != nil {
		a.deny(ctx, CodeDenied, "this credential carries its own scope, and this operation answers only to its holder")
		return false
	}
	return true
}

func (a *API) admitGrant(ctx huma.Context, auth Auth, p tenancy.Principal, t tenancy.Tenant) bool {
	grant, _ := auth.grant()
	// The second half: a credential that carries its own authority is held to it
	// before the Authorizer is asked. The check is an exact name, so a key never
	// inherits a wildcard it was not given, and a key whose holder has since been
	// stood down from the role arrives here with an empty list — the roles are
	// rechecked on every request, in the resolution that built this one.
	if p.Permissions != nil && !slices.Contains(p.Permissions, grant.Permission) {
		a.deny(ctx, CodeDenied, "this credential does not carry "+grant.Permission)
		return false
	}
	// Check operator tenancy before roles: a customer's wildcard grants no operator
	// power. Ops conceals the refusal with its ordinary 404; other surfaces use 403.
	if grant.Operator && !t.Operator {
		if SurfaceOf(ctx.Context()) == SurfaceOps {
			a.notHere(ctx)
			return false
		}
		a.deny(ctx, CodeNotOperator, grant.Permission+" is the operator's, and this is not the operator's tenant")
		return false
	}

	allowed, err := a.opts.Authorize.Allowed(ctx.Context(), t, grant)
	if err != nil {
		// An authorization decision that could not be made is not a denial, and
		// saying so would send a person away from work they are entitled to do.
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: authorization decision unavailable",
			"permission", grant.Permission, "operator", grant.Operator, "tenant", t.Slug, "error", err)
		ctx.SetHeader("Retry-After", "3")
		a.refuse(ctx, http.StatusServiceUnavailable, "authorization is temporarily unavailable")
		return false
	}
	if !allowed {
		// The fact before the sentence. The page and the audit row read what is
		// recorded here; the words below stay for the log line and the problem
		// document. A person refused here is now told which grant they lack in the
		// words of the module that defines it, which is a different page.
		noteRefused(ctx.Context(), Refusal{
			Code: CodeDenied, Permission: grant.Permission, Label: a.grantLabel(grant.Permission),
			Method: ctx.Method(), Path: ctx.URL().Path,
		})
		a.deny(ctx, CodeDenied, "this operation requires "+grant.Permission)
		return false
	}
	return true
}
