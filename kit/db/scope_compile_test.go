//go:build never

// This file documents a compile-time guarantee and is excluded from every
// build: `go build ./...`, `go vet ./...` and `go test ./...` all skip it
// because nothing ever sets the `never` tag. Remove the tag and the package
// stops compiling — that failure is the guarantee.
//
// Cited by docs/adr/0003-tenancy-by-postgres.md.
package db

func repository(tx Tx[Tenant]) {}

func crossTenantCaller(tx Tx[System]) {
	repository(tx) // cannot use tx (variable of type Tx[System]) as Tx[Tenant]
}
