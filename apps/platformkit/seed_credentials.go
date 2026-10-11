package main

// A seed run can mint a credential: a demo record asks for a sign-in, the
// deployment names no PLATFORMKIT_DEMO_PASSWORD, and the run generates a password
// for that one person (see userSeeder.commands). Where that secret goes is the
// whole of the difference between a credential and a leak, and the answer cannot
// be "the process's output" for both of the runs that mint one.
//
// One of those runs is this command line, where a person is reading and the
// password has to reach them or the demonstration has nobody to sign in as. The
// other is a new tenant's own creation, which happens inside the running server,
// on the far side of a socket from the operator who asked for the tenant — and a
// server's stdout and stderr are its log stream (apps/platformkit/main.go writes
// its structured log to stderr, and a container collector takes both). So the
// minted value is collected for the caller, and only the caller that collected
// one prints it.

import (
	"context"
	"fmt"
	"os"
)

// mintedCredential is one person and the password this run gave them. Nothing
// here holds a hash, and nothing here reads one back.
type mintedCredential struct{ Email, Password string }

// mintedKey is what a collector hangs off the context. kit/events carries
// provenance the same way for the same reason: the code that mints the value sits
// several calls away from the code that asked for the run.
type mintedKey struct{}

// withMinted returns a context that collects the credentials this run mints into
// sink. A run that collects nothing mints exactly the same hashes and learns
// nothing about the values it minted — which is the correct answer for a tenant's
// create transaction: nothing reaches the caller of OnTenantCreate but an error,
// so a credential minted there exists only as a hash nobody can recover.
// docs/seed.md names the consequence for a deployment that creates demo tenants
// through the tenant route.
func withMinted(ctx context.Context, sink *[]mintedCredential) context.Context {
	return context.WithValue(ctx, mintedKey{}, sink)
}

// mintInto hands a minted password to whoever asked for this run and says whether
// anybody did. The minting never depends on that answer: an invited demo person
// with no credential is not a walkthrough, so the hash is written either way and
// what the collector changes is only who gets told.
func mintInto(ctx context.Context, email, password string) bool {
	sink, ok := ctx.Value(mintedKey{}).(*[]mintedCredential)
	if !ok {
		return false
	}
	*sink = append(*sink, mintedCredential{Email: email, Password: password})
	return true
}

// printMintedCredentials is the command line's half of the bargain, and the only
// place any of this reaches a stream. It runs after the transaction has committed
// — the old print ran inside the create, so a run that rolled back afterwards had
// already published a credential belonging to nobody — and it is written once per
// person, to stderr, beside the plan the run printed.
func printMintedCredentials(minted []mintedCredential) {
	for _, m := range minted {
		fmt.Fprintf(os.Stderr, "\n  password for %s: %s\n  It is not stored and will not be shown again.\n\n", m.Email, m.Password)
	}
}
