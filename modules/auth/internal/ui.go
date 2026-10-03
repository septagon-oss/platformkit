package internal

// ui.go is where this module's page meets the command it runs.
//
// The page — the markup, the form, what a person sees when they open the link in
// the mail — lives in internal/ui, which is the only layer here allowed to write
// HTML. The credential, the account state and the one command that spends a
// credential live in this package. The page reaches them through ui.Confirmation,
// and this file is the whole of that reach: three methods, each one line, no
// second implementation of anything (docs/adr/0007).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/internal/ui"
)

// Pages is the chrome of the page the verification link opens. It is the
// composition's own type to fill (see auth.Deps.Pages); the page that reads it
// lives in internal/ui.
type Pages = ui.Pages

// MountVerifyEmailPage serves the page the verification mail's address opens, on
// the module's public face. policy names the account lifecycle whose credential
// the page spends, and its Pages chrome is the installation's.
func MountVerifyEmailPage(s httpx.Surfaces, svc *Service, policy contracts.EmailRegistration, p Pages) {
	ui.Mount(s, pageCommands{svc: svc, users: policy.Users}, p)
}

// pageCommands is ui.Confirmation over the service. Every method is a call the
// JSON door makes too, from the request the page's own route carries.
type pageCommands struct {
	svc   *Service
	users contracts.EmailRegistrar
}

// Addressee reads the address a live credential points at, to say it back to the
// person being asked to confirm it. It spends nothing: the DELETE belongs to
// Spend, the one command that changes state, and a mail scanner that fetches
// every link in every message must not be able to spend one by looking.
func (c pageCommands) Addressee(ctx context.Context, token string) (string, error) {
	tx, err := transaction(ctx)
	if err != nil {
		return "", err
	}
	var email string
	err = tx.DB().Raw("SELECT email FROM verification_tokens WHERE token_hash = ? AND expires_at > clock_timestamp()",
		contracts.Hash(token)).Row().Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", contracts.ErrCredentials
	}
	if err != nil {
		return "", fmt.Errorf("auth: read email verification: %w", err)
	}
	return email, nil
}

// Spend consumes the credential and moves the account it names, in the page
// request's own transaction: the same Service.verifyEmail the POST door calls.
func (c pageCommands) Spend(ctx context.Context, token string) error {
	tx, err := transaction(ctx)
	if err != nil {
		return err
	}
	return c.svc.verifyEmail(ctx, tx, c.users, token)
}

// MayRedeem counts one attempt from the machine that asked.
func (c pageCommands) MayRedeem(ctx context.Context, r *http.Request) bool {
	return c.svc.MayRedeem(ctx, ClientOf(r).IP)
}
