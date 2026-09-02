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
// said why. Its reason is the only state it carries, and the only way to set it
// is NewSystemToken: a forged token (the zero value, which Go lets any package
// write as tenancy.SystemToken{}) has an empty reason, which kit/db rejects.
type SystemToken struct{ reason string }

// NewSystemToken mints a token. The reason must be non-empty; it is recorded on
// the transaction and logged when the transaction opens.
func NewSystemToken(reason string) SystemToken { return SystemToken{reason: reason} }

// Reason is why the holder needs to cross tenants.
func (t SystemToken) Reason() string { return t.reason }
