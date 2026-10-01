package main

import (
	"context"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/ui/page"
)

// accountLocale is the shell's answer to "what language is this page written in",
// which is the durable choice the person in front of it made, or the sign-in
// link's explicit `?lang=` for everybody that choice does not reach.
//
// The order is the whole content of this type. A query string is something anybody
// can type into an address bar and something a link can carry without the reader
// noticing; a choice written through the account command is a person having said
// what they want, and it is the only thing here durable enough to be worth
// keeping. Signing in again therefore answers in the same language as the page
// before signing out — which is the entire point of storing it at all.
//
// A choice this tenant no longer serves is not special-cased here: the shell
// filters every preference through the tenant's own set
// (page.TenantPreferences), so an operator narrowing the set leaves a person
// negotiated for rather than refused. The command that writes the choice refuses a
// tag outside the set at the moment of the write, which is where a refusal belongs.
type accountLocale struct{ users usercontracts.Service }

func (a accountLocale) Locale(ctx context.Context, r page.Request) string {
	if choice := a.chosen(ctx); choice != "" {
		return choice
	}
	return loginLocale(ctx, r)
}

// chosen is the signed-in person's own language, or "" when there is nobody, when
// they never chose, or when the read failed. The last of those three is a
// deliberate fall-through rather than a refusal: a page that could not read a
// preference still has to be served, and the language it falls back to is the one
// it answered in before this field existed.
func (a accountLocale) chosen(ctx context.Context) string {
	principal, signedIn := tenancy.PrincipalFrom(ctx)
	if !signedIn || principal.UserID == uuid.Nil {
		return ""
	}
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return ""
	}
	u, err := a.users.Get(ctx, tx, principal.UserID)
	if err != nil {
		return ""
	}
	return u.PreferredLocale
}

// loginLocale is the half of the answer that is not the person's own: an explicit
// sign-in URL wins over browser negotiation, for the page in front of somebody who
// has not signed in yet and so has no choice stored to be read. Ambiguous query
// values do not select a language, and no preference is persisted implicitly.
func loginLocale(ctx context.Context, _ page.Request) string {
	if request, ok := httpx.RequestFrom(ctx); ok {
		if values := request.URL.Query()["lang"]; len(values) == 1 {
			return values[0]
		}
	}
	return ""
}
