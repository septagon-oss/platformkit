//go:build never

// This file documents a compile-time guarantee and is excluded from every build:
// `go build ./...`, `go vet ./...` and `go test ./...` all skip it because nothing
// ever sets the `never` tag. Remove the tag and the package stops compiling — that
// failure is the guarantee, and it is the same shape as
// kit/db/scope_compile_test.go one layer up.
//
// What it holds is the half of the tenant rule row-level security cannot: a store
// call is made with the scope of one tenant, so a value that deliberately names no
// tenant — db.Tx[System], the connection the orphan sweep is handed precisely
// because it must list the whole store — has nowhere to come from here. Ask for a
// Scope out of a system transaction and there is no such call; ask a Signer to sign
// on one and there is no such method. The cross-tenant path is
// contracts.Reconciler, which takes the tenant ids it is asking about as arguments
// instead of wearing one in its transaction.
package contracts

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/db"
)

// askedOfTenant is what a request's own transaction answers: the scope of the
// tenant that owns it, and a grant signed under that scope.
func askedOfTenant(ctx context.Context, tx db.Tx[db.Tenant], signer Signer, f *File) (*Grant, error) {
	return signer.Sign(ctx, ScopeOfTx(tx), f, 0)
}

// askedOfSystem is what no system transaction can answer. Both lines below are
// compile errors — ScopeOfTx takes a Tx[Tenant], and no conversion between the two
// transaction types exists. If one were ever added, this file would start compiling
// and the guarantee would be gone with it, which is why the file is kept rather
// than deleted.
func askedOfSystem(ctx context.Context, tx db.Tx[db.System], signer Signer, f *File) (*Grant, error) {
	scope := ScopeOfTx(tx) // cannot use tx (variable of type db.Tx[System]) as db.Tx[Tenant]
	return signer.Sign(ctx, scope, f, 0)
}
