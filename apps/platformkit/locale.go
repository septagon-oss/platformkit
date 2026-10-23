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

// Revision and ChooseAt are the same door counted. The read is the account door's
// own — the choice and the number it moves with, from the one row, with no id to
// lie about — and the write is the versioned command, so a form drawn at revision 2
// that arrives after the person's other tab landed revision 3 is a conflict that
// writes nothing and publishes nothing.
//
// The header's control is the caller that needed this: unlike a JSON client that
// was never given the revision, it drew the choice from a read and shows it, and a
// picture of an old row submitted back is a write from a read whether or not
// anybody calls it one.
func (a accountLanguage) Revision(ctx context.Context, tx db.Tx[db.Tenant]) (int64, error) {
	own, err := a.users.OwnLanguage(ctx, tx)
	if err != nil {
		return 0, err
	}
	return own.Revision, nil
}

func (a accountLanguage) ChooseAt(ctx context.Context, tx db.Tx[db.Tenant], tag string, expectedRevision int64) error {
	_, err := a.users.SetPreferredLocaleAt(ctx, tx, expectedRevision, tag)
	return err
}

// tenantLocales is user.Deps.LocalePolicy: which languages the tenant of the
// running transaction is served in, read where that fact lives.
//
// The tenant module's Service answers the interface as it stands — the port and the
// method are the same question — so this type only carries the value and refuses to
// answer before the control plane is composed, rather than inventing a set. It is
// late-bound because the control plane is built after the module that asks: a new
// tenant's first administrator is a person, and people are the user module's rows.
type tenantLocales struct{ tenants tenantcontracts.Service }

func (l *tenantLocales) CurrentLanguages(ctx context.Context, tx db.Tx[db.Tenant]) (tenancy.Languages, error) {
	if l.tenants == nil {
		return tenancy.Languages{}, errors.New("platformkit: the tenant module is not composed yet, so no language set can be read")
	}
	return l.tenants.CurrentLanguages(ctx, tx)
}

// tenantLocaleManager is the tenant module's own question — may this caller set
// this tenant's languages? — answered over the same authorizer the kernel
// enforces with. The route guard asks first; this is the ask that happens inside
// the transaction that writes, which is the one the command is authoritative about.
//
// auth is filled by the access module's build, the one build in this composition
// that runs after the authentication service exists: the authorizer is built from
// the modules and the tenant module is one of them, so it cannot be handed one at
// its own line. Until it is filled the answer is an error rather
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

// pageLocale is the language the pages this application draws for a person a guard
// refused are worded in.
//
// It is the same answer the module shells get — accountLocale, the person's own
// durable choice first, the sign-in link's `?lang=` after it — held late because the
// refusal page is composed with the sentence, before any service exists, and the
// answer reads the user service. Until the access module fills it, the page falls
// back to the half of the answer that needs no caller, which is what it did before
// the choice existed at all.
type pageLocale struct {
	resolve func(context.Context, page.Request) string
}

func (l *pageLocale) Locale(ctx context.Context, r page.Request) string {
	if l.resolve == nil {
		return loginLocale(ctx, r)
	}
	return l.resolve(ctx, r)
}

// latePorts are the language edges this product answers for its own modules and
// chrome, all of them late-bound because the service that answers is built after
// the reader: which languages a person may choose is the control plane's
// fact and the control plane needs the user service first, who may set a
// tenant's languages is the authorizer's answer and the authorizer is built from
// every module including the tenant one, and the language a refusal page is
// worded in reads the same user service the guard's page is composed before.
//
// They are one value because they are one decision: the product puts every holder
// needing nobody, and the access module — the first build that has met the control
// plane, the authorizer and the user service — fills them. No edge here is a
// declared need in either direction, because the graph they would make is the
// cycle pkit refuses.
type latePorts struct {
	locales     *tenantLocales
	localeGrant *tenantLocaleManager
	chrome      *pageLocale
}

func newLatePorts() *latePorts {
	return &latePorts{locales: &tenantLocales{}, localeGrant: &tenantLocaleManager{}, chrome: &pageLocale{}}
}
