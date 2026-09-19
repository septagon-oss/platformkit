package auth

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
)

// AdministeringRoles is the names of this tenant's roles that grant
// role:manage — the permission that can grant every other one back, and so the
// one a tenant has to keep somebody holding.
//
// It is what goes inside user/contracts.AdministrationFunc, which is why it exists:
// the user module refuses to take the last administrator's roles away, or to
// deactivate or delete them, and it cannot tell who an administrator is without
// asking whoever owns roles. apps/platformkit/modules.go wires this to that field
// through that adapter.
//
// It is a package-level function and not a method on the service for one
// practical reason and one design one. Practically, the user module is
// composed before the authentication service exists — it is what the
// authentication service is built on — so a method would need late binding for
// a question that needs no state to answer. By design, SeedRoles is already
// this shape: the roles table is this module's, and a function that takes the
// caller's transaction is how another part of the application reaches it
// without holding a service.
//
// The answer is read inside the caller's own tenant transaction, under that
// tenant's policy, so it is about one customer whoever asks.
func AdministeringRoles(ctx context.Context, tx db.Tx[db.Tenant]) ([]string, error) {
	return internal.AdministeringRoles(ctx, tx)
}
