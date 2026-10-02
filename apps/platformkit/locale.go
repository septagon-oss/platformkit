package main

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
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

// accountLanguage is the same fact as accountLocale's, reached through the door a
// person uses to change it rather than the one every page reads it through: the
// shell's account control reads the choice to draw itself and hands a submitted one
// to the user module's command.
//
// Two types over one field is deliberate. The locale resolver only ever reads, and a
// resolver that could write would be a page that changes what it was asked about;
// the account surface writes, and so it is a second, narrower thing composition
// wires — both to the same service, which is the one place the decision lives.
// Choose takes no user id for the reason the command gives no user id: the
// principal of the request transaction is the subject, and an id written by a
// browser is an id that can name a colleague.
type accountLanguage struct{ users usercontracts.Service }

func (a accountLanguage) Preferred(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) (string, error) {
	u, err := a.users.Get(ctx, tx, userID)
	if err != nil {
		return "", err
	}
	return u.PreferredLocale, nil
}

func (a accountLanguage) Choose(ctx context.Context, tx db.Tx[db.Tenant], tag string) error {
	_, err := a.users.SetPreferredLocale(ctx, tx, tag)
	return err
}

// tenantLocaleManager is the tenant module's own question — may this caller set
// this tenant's languages? — answered over the same authorizer the kernel
// enforces with. The route guard asks first; this is the ask that happens inside
// the transaction that writes, which is the one the command is authoritative about.
//
// auth is filled after the tenant module is constructed, beside the line that
// fills roles.auth, because the authorizer is built from the modules and the
// tenant module is one of them. Until it is filled the answer is an error rather
// than false: "nobody may" and "we could not ask" are different facts, and the
// second one is an outage somebody has to be able to see.
type tenantLocaleManager struct{ auth httpx.Authorizer }

func (m *tenantLocaleManager) MaySetOwnLocale(ctx context.Context, _ db.Tx[db.Tenant]) (bool, error) {
	if m.auth == nil {
		return false, errors.New("platformkit: the tenant language command ran before its authorizer was wired")
	}
	principal, signedIn := tenancy.PrincipalFrom(ctx)
	if !signedIn || principal.UserID == uuid.Nil {
		return false, nil
	}
	who, ok := tenancy.FromContext(ctx)
	if !ok {
		return false, nil
	}
	return m.auth.Allowed(ctx, who, tenancy.Grant{Permission: tenantcontracts.PermissionLocaleManage})
}
