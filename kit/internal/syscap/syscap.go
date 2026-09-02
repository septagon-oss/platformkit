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

	// isSystemToken is unexported, so this interface is closed to this package.
	isSystemToken()
}

type token struct{ reason string }

func (t token) Reason() string { return t.reason }
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
