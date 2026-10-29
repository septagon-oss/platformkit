// Package syscap mints the one capability that opens cross-tenant work.
//
// It lives under kit/internal so that only packages inside kit/ can import it.
// That is the whole point: a business module cannot call NewSystemToken, so it
// cannot ask kit/db for a transaction that sees every tenant. The compiler, not
// a review comment, keeps tenancy in one place.
//
// tenancy.SystemToken is an alias for the type declared here, so callers name
// the capability through kit/tenancy and only the minting site needs this
// package.
package syscap

// SystemToken is proof that a kernel package asked for cross-tenant access and
// said why.
//
// It is an interface with an unexported method and an unexported implementation,
// so NewSystemToken is the only expression in the program that produces one: a
// package outside kit/ can name tenancy.SystemToken but can neither construct
// nor implement it. The zero value is nil, which kit/db refuses.
type SystemToken interface {
	// Reason is why the holder needs to cross tenants. It is logged when the
	// transaction opens.
	Reason() string

	// Module is whose work opened the transaction: the module the composition
	// minted this token for. Empty says the kernel opened it — host resolution,
	// the relay, a probe — which is a different fact and reads as one.
	Module() string

	// isSystemToken is unexported, so this interface is closed to this package.
	isSystemToken()
}

type token struct{ module, reason string }

func (t token) Reason() string { return t.reason }
func (t token) Module() string { return t.module }
func (token) isSystemToken()   {}

// NewSystemToken mints a token. An empty reason is a programming mistake at the
// minting site — a capability nobody can explain — so it panics there rather
// than producing a token that logs nothing.
func NewSystemToken(reason string) SystemToken {
	if reason == "" {
		panic("syscap.NewSystemToken: a cross-tenant capability must say why it exists")
	}
	return token{reason: reason}
}

// NewSystemTokenFor mints the token of one module, so the audit trail of every
// cross-tenant transaction says both who opened it and what for. An empty
// module or reason is a capability nobody can explain, so it panics at the
// minting site like NewSystemToken does; a module never reaches this function
// by itself — kit/httpx mints on behalf of a manifest that declared the need
// (module.Module.SystemAccess), which is what keeps the list of holders short
// and visible. See docs/adr/0006.
func NewSystemTokenFor(module, reason string) SystemToken {
	if module == "" || reason == "" {
		panic("syscap.NewSystemTokenFor: a cross-tenant capability must name the module it belongs to and why it exists")
	}
	return token{module: module, reason: reason}
}
