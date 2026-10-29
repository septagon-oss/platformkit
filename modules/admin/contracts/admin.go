// Package contracts holds the ports modules/admin declares and consumes: the
// sign-in page's two halves, where a generated screen fetches a picture, the
// language the shell answers in before the browser negotiates one, the
// composition the gallery shows, and the action the workspace home leads with.
package contracts

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/ui/export"
	"github.com/septagon-oss/platformkit/ui/page"
)

// PrimaryAction is the one thing this application's people come to the workspace
// to do: the home draws it first, as its only primary button, above the counters.
// It is a navigation entry's three facts — the words on the button, the screen
// relative to the workspace ("task/tasks"), and the permission a caller must hold
// to be shown it — and is filtered the way a nav entry is, by the Authorizer the
// route enforces with, so the button never leads to a refusal.
//
// It is Optional: the zero value draws no action and the home is the dashboard it
// always was. A non-empty Screen with an empty Label or Permission, or a Permission
// no composed module defines, refuses the plan; a Screen no route serves is
// reported at boot and draws nothing.
type PrimaryAction module.NavEntry

// Signin is the shell's own front door: where the sign-in form posts, and the
// way in for a person the tenant has no account for.
//
// The two halves are one value on purpose. The form posts to the auth module's
// session route, which only the composition knows the address of — writing it in
// the shell would be this shell naming another module's door — and offering a
// registration form that posts to a route nobody mounted is a door painted on a
// wall. One value means neither half can offer what the other did not mount.
//
// It is Optional because a composition with no auth module has no login page to
// speak of; Registration nil is the password door alone, which is what the
// empty field always meant.
type Signin struct {
	// OperationID must name Address's mounted operation. Empty is the shell with no
	// auth module: Views then declares the sign-in page with no door to post through.
	OperationID string
	// Address is where the sign-in form posts.
	Address string
	// Registration is the sign-up form the sign-in page links, or nil for the
	// password door alone.
	Registration *Registration
}

// Registration is the sign-up form a composition offers, and which of the two
// forms the door takes.
type Registration struct {
	// OperationID must name Address's mounted operation; empty values panic in Views.
	OperationID string
	// Kind is which form the shell renders. KindEmail asks for an address alone
	// and the tenant then invites the person; KindPassword takes a password and
	// its confirmation and the account waits for the mailbox link.
	Kind RegistrationKind
	// Address is the door the form posts to.
	Address string
}

// RegistrationKind is which registration form the shell renders. The two are
// distinct lifecycles, not a toggle: which one an installation offers is its
// composition's decision.
type RegistrationKind string

const (
	// KindEmail asks for an address alone; the person is then invited and
	// chooses a password from the mail.
	KindEmail RegistrationKind = "email"
	// KindPassword takes a password and its confirmation, and the account waits
	// for the mailbox link before it can sign in.
	KindPassword RegistrationKind = "password"
)

// Pictures is the door the generated screens fetch a picture field's bytes from:
// the address template, with the literal `{id}` standing for the file id a field
// holds. The shell draws a `present:image` cell as a picture and not as the id its
// column holds, and the door belongs to the module that stores the bytes, so a
// shell that named the address would be naming a route it does not mount.
//
// It is Optional because a composition with no file store composes no such door:
// the cell then draws the plain value it drew before picture fields existed, which
// is an answer rather than a mistake — the same one an absent `pictures` block
// gives the workspace catalogue.
//
// The same address is published to a shell that is not a browser as that
// document's `pictures.source`, so a phone and a browser row fetch the same bytes
// from the same door; the composition that has met both writes it once.
type Pictures struct {
	// Source is the address template, `{id}` standing for the file id.
	Source string
	// ComposedSource is the composed-picture address with a {reference} placeholder.
	ComposedSource string
}

// Locale selects a language for the sign-in page before the browser negotiates
// one. Return "" to leave the negotiation alone; never persist a preference
// implicitly, and never let an ambiguous query value decide.
//
// It is a port because a preference is this installation's own rule about its own
// query string, and a shell that read `?lang=` itself would be a shell that
// decided what a URL parameter means for every product that composes it.
type Locale func(context.Context, page.Request) string

// Storybook selects and authorizes the composition for the resolved tenant and
// principal in ctx. Return an error to deny access; never select from a query
// parameter or fall back to another tenant. Nil exposes Core only to the
// operator tenant. Empty Examples stays empty. Every gallery endpoint also
// requires the shell's own gallery permission before calling this function.
type Storybook func(context.Context) (export.Storybook, error)
