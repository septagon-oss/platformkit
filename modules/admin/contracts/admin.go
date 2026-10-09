// Package contracts holds the ports modules/admin declares and consumes: the
// sign-in page's two halves, the language the shell answers in before the
// browser negotiates one, and the composition the gallery shows.
package contracts

import (
	"context"

	"github.com/septagon-oss/platformkit/ui/export"
	"github.com/septagon-oss/platformkit/ui/page"
)

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
	// Address is where the sign-in form posts.
	Address string
	// Registration is the sign-up form the sign-in page links, or nil for the
	// password door alone.
	Registration *Registration
}

// Registration is the sign-up form a composition offers, and which of the two
// forms the door takes.
type Registration struct {
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
