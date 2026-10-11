package main

// A demo tenant created with no deployment demo password gets a credential
// minted per person. The tenant is created by tenants.Create — the call the
// operator's create route makes inside the running server — and the server's
// structured log is written to the process's stderr (main.go's logger), while a
// container's collector takes its stdout too. A minted password must therefore
// reach neither stream: the seed's own comment promises "never to a log", and a
// server's output streams are its log. The test
// reaches the minting branch through the fixed behaviour too (the person can
// sign in), then checks every token the stream received against the person's
// hash, so it does not depend on how the stream words anything.

import (
	"context"
	"io"
	"os"
	"regexp"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestMintedDemoCredentialStaysOutOfTheServerLogStream(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	// No deployment demo password, whatever the ambient environment says, so the
	// run has to mint one.
	cfg.Demo.Password = ""
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	capture := func(stream **os.File) (restore func() string) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		original := *stream
		*stream = writer
		captured := make(chan string, 1)
		go func() {
			b, _ := io.ReadAll(reader)
			captured <- string(b)
		}()
		return func() string {
			*stream = original
			_ = writer.Close()
			return <-captured
		}
	}
	restoreErr, restoreOut := capture(&os.Stderr), capture(&os.Stdout)

	var person *usercontracts.User
	createErr := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		created, err := c.tenants.Create(ctx, system, tenantcontracts.NewTenant{
			Slug: "demo-log-stream", Name: "Demo", Host: "demo-log-stream.localhost", Demo: true,
		})
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, created.Tenancy(), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			person, err = c.users.ByEmail(ctx, tx, "marta@example.test")
			return err
		})
	})
	stream := restoreErr() + "\n" + restoreOut()
	if createErr != nil {
		t.Fatal(createErr)
	}
	if !person.CanSignIn() {
		t.Fatal("the demo person holds no credential, so the minting branch was not reached")
	}
	// Every run of the password alphabet, so a credential inside a JSON value
	// or a key=value pair is checked as well as one between spaces.
	for _, token := range credentialShaped.FindAllString(stream, -1) {
		if person.CheckPassword(token) {
			t.Fatalf("the process's stdout or stderr — the server's log stream — carries %s's minted password", person.Email)
		}
	}
}

var credentialShaped = regexp.MustCompile(`[A-Za-z0-9_-]{12,}`)
