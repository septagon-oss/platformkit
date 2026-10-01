package httpx

// authorize.go rechecks the caller's grants and the tenant's plan inside the
// request, publishes the code each refusal carries, and writes the refusal.

import (
	"context"
	"fmt"
	"net/http"

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
// Every refusal is a 403, never a 401: which of the four conditions failed says
// nothing an attacker can act on, but it says a great deal to whoever is
// debugging a deployment, so the code is in the response and the request that
// carried it is in the log.
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
		if auth.feature != "" {
			t, hasTenant := tenancy.FromContext(ctx.Context())
			if !hasTenant {
				a.refuse(ctx, http.StatusNotFound, "no site is served at this host")
				return
			}
			if !a.entitled(ctx, t, auth) {
				return
			}
		}
		next(ctx)
		return
	}

	p, hasPrincipal := tenancy.PrincipalFrom(ctx.Context())
	if !hasPrincipal || p.UserID == uuid.Nil {
		// A page has somewhere to send somebody who has nobody to be; an API
		// route has not. See SignInExtension.
		if to, page := signInFor(ctx); page {
			ctx.SetHeader("Location", to)
			ctx.SetStatus(http.StatusSeeOther)
			return
		}
		a.deny(ctx, CodeAnonymous, "this operation requires a signed-in caller")
		return
	}
	if expected := ctx.Header(ExpectedPrincipalHeader); unsafeMethod(ctx.Method()) && expected != "" && expected != p.UserID.String() {
		a.deny(ctx, CodePrincipalChanged, "the signed-in account differs from the one that prepared this request")
		return
	}
	t, hasTenant := tenancy.FromContext(ctx.Context())
	if !hasTenant {
		a.deny(ctx, CodeNoTenant, "this operation is tenant work and the host resolved to none")
		return
	}
	// There is no principal-belongs-to-this-tenant check, because there is no
	// way for it to fail: the principal was built from a row read inside this
	// tenant's own transaction. See Principal.
	if auth.kind == kindSignedIn {
		if !a.entitled(ctx, t, auth) {
			return
		}
		next(ctx)
		return
	}

	grant, _ := auth.grant()
	// Before the Authorizer, and that order is the point of the declaration: a
	// customer's administrator holds the wildcard in their own tenant, and asking
	// the roles table first would answer a question about everybody's with the
	// answer about theirs. A tenant that is not the operator's cannot exercise an
	// operator permission however its roles are written.
	//
	// The surface decided *which address* serves the control plane — see the
	// installation host gate, which answered a 404 before this middleware ran at
	// all. What is left here is the second, independent guarantee: a row of data
	// may point the installation's host at some tenant, and the answer to that is
	// still no. On the control-plane surface it is the same 404 the host gate
	// gave, so the address does not disclose that it knows the difference;
	// anywhere else — a generated page of the workspace that reads across
	// tenants, an operator write mounted beside a customer's — it is the ordinary
	// refusal of a caller who may not, and no wildcard satisfies it.
	if grant.Operator && !t.Operator {
		if SurfaceOf(ctx.Context()) == SurfaceOps {
			a.notHere(ctx)
			return
		}
		a.deny(ctx, CodeNotOperator, grant.Permission+" is the operator's, and this is not the operator's tenant")
		return
	}

	allowed, err := a.opts.Authorize.Allowed(ctx.Context(), t, grant)
	if err != nil {
		// An authorization decision that could not be made is not a denial, and
		// saying so would send a person away from work they are entitled to do.
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: authorization decision unavailable",
			"permission", grant.Permission, "operator", grant.Operator, "tenant", t.Slug, "error", err)
		ctx.SetHeader("Retry-After", "3")
		a.refuse(ctx, http.StatusServiceUnavailable, "authorization is temporarily unavailable")
		return
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
		return
	}
	// The plan question last, so a caller who may not do this at all is told
	// that and not told to buy something. The order matters for what a person
	// reads: "ask your administrator" and "upgrade" are different sentences and
	// only one of them is true.
	if !a.entitled(ctx, t, auth) {
		return
	}
	// The grant is the first question; which row is the second, and a module asks it
	// through tenancy.RequirePolicy after this middleware is done. Its refusal is
	// audited here, where the request is known, whatever response the module maps it to.
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

// entitled answers the feature a declaration names, and writes the refusal
// itself when the answer is no — 402 rather than 403, because a plan that does
// not include something is not a permission a person can be granted, and a
// client that cannot tell the two apart shows the wrong way out of both.
//
// A declaration that names no feature asks nothing. An Entitler that cannot
// decide is an outage and not a denial, exactly as an Authorizer that cannot:
// billing being unreachable must not read as "your plan does not include this".
func (a *API) entitled(ctx huma.Context, t tenancy.Tenant, auth Auth) bool {
	if auth.feature == "" {
		return true
	}
	if a.opts.Entitle == nil {
		// Defense in depth, like the undeclared branch above: ValidateDeclarations
		// refuses this composition, so reaching here means the gate did not run.
		// Closed rather than open, and an outage rather than a denial, because
		// the fault is the application's and not the caller's.
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: an operation declares a feature and nothing answers it",
			"feature", auth.feature, "path", ctx.URL().Path)
		a.refuse(ctx, http.StatusServiceUnavailable, "the plan could not be read right now")
		return false
	}
	included, err := a.opts.Entitle.Includes(ctx.Context(), t, auth.feature)
	if err != nil {
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: entitlement decision unavailable",
			"feature", auth.feature, "tenant", t.Slug, "error", err)
		ctx.SetHeader("Retry-After", "3")
		a.refuse(ctx, http.StatusServiceUnavailable, "the plan could not be read right now")
		return false
	}
	if !included {
		a.rlog(ctx.Context()).InfoContext(ctx.Context(), "httpx: plan excludes this operation",
			"feature", auth.feature, "tenant", t.Slug, "path", ctx.URL().Path)
		a.refuse(ctx, http.StatusPaymentRequired, CodePlanExcludes+": this tenant's plan does not include "+auth.feature)
		a.denied(ctx, http.StatusPaymentRequired, CodePlanExcludes, "this tenant's plan does not include "+auth.feature)
		return false
	}
	return true
}

// The refusal codes, exported because the presentation layer translates them.
//
// A refusal has one value and two shapes (docs/adr/0015): the same verdict as a
// problem document for a program and as a sentence for a person. The second
// shape was English-only, because the sentence was a string literal in the
// guard that made it. These constants are the carrier the code travels in,
// ui/page/fault.go holds the one table from a code to a catalog key, and the
// refusal page is then negotiated from the request's own Accept-Language — so a
// shell that ships the sentence for a code answers a refusal in the language it
// ships, and a shell that ships none is answered in the English below and
// declared English, which is what it is. A guard that refuses in a shape a
// person can be shown, and not only a machine, writes one of these and answers
// through API.refuse; a code that travelled as a string literal could not be
// looked up by anybody, which is how LIMIT_EXHAUSTED arrived untranslated twice
// over — no constant, so no table row, and huma's writer, so no page.
//
// Two codes are named here and left out of that table on purpose, for one
// reason: their sentence carries something the caller has to have rather than
// something they have to understand. CodeWriteElsewhere names the address the
// write belongs at and CodePlanExcludes the feature to ask for; a translated
// sentence would drop it, because the mechanism replaces a sentence and does not
// interpolate an argument. Both are shown as they are written.
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
// the defect apps/platformkit/app_fault_test.go names in its own opening line:
// "a person who navigated and gets JSON".
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
