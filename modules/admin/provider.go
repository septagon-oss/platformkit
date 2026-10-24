package admin

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	admincontracts "github.com/septagon-oss/platformkit/modules/admin/contracts"
	"github.com/septagon-oss/platformkit/modules/admin/internal"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module builds the workspace shell after every resource is declared. Its
// navigation uses the kernel authorizer and its header optionally reads the caller.
var Module = pkit.NewModule("admin", wire,
	pkit.Needs[authcontracts.RoleEditor](),
	pkit.Needs[authcontracts.SessionAdministration](),
	pkit.Needs[httpx.Authorizer](),
	pkit.Needs[tenantcontracts.Lister](),
	// Optional, not Needs: a composition with no user module has no names to show,
	// and the header says "Signed in" rather than the application refusing to boot.
	pkit.Optional[usercontracts.Lookup](),
	pkit.Optional[admincontracts.Signin](),
	pkit.Optional[admincontracts.Pictures](),
	pkit.Optional[admincontracts.Locale](),
	pkit.Optional[admincontracts.Storybook](),
	pkit.After(pkit.Everything),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	skin := w.Skin()
	signin := pkit.Get[admincontracts.Signin](w)
	return New(Deps{
		Modules:    pkit.Composition(w),
		Authorize:  pkit.Get[httpx.Authorizer](w),
		TenantList: pkit.Get[tenantcontracts.Lister](w),
		// The three questions the shell asks are the same value seen through the
		// three interfaces it uses: what may this caller do, which roles exist,
		// which sessions are live.
		Roles:    pkit.Get[authcontracts.RoleEditor](w),
		Sessions: pkit.Get[authcontracts.SessionAdministration](w),
		// The fourth question is who this is, and unlike the three above it needs an
		// adapter: the shell asks about the caller by id and gets back the two words
		// a header can say, not a row. Eight lines, and they are the whole of what
		// this module knows about the user module.
		People:    callerOf(pkit.Get[usercontracts.Lookup](w)),
		Theme:     skin.Theme,
		Messages:  skin.Copy,
		Locale:    pkit.Get[admincontracts.Locale](w),
		Storybook: pkit.Get[admincontracts.Storybook](w),
		SignIn:    signin.Address,
		// A generated picture cell fetches through the file module's door, which this
		// shell does not mount and so cannot name: the composition that published that
		// door to the workspace catalogue hands it here as the same string.
		PictureSource:  pkit.Get[admincontracts.Pictures](w).Source,
		ComposedSource: pkit.Get[admincontracts.Pictures](w).ComposedSource,
		// The form on the shell's login page posts to the auth module's door, and
		// the link to it is the same value, so neither half can offer what the
		// other did not mount.
		Registration: registration(signin.Registration),
	}), nil
}

// callerOf is the header's read of the caller's own account row, or nothing at all
// when this application composes no user module — which is what leaves the frame to
// say "Signed in" rather than to fail.
func callerOf(users usercontracts.Lookup) People {
	if users == nil {
		return nil
	}
	return people{users: users}
}

type people struct{ users usercontracts.Lookup }

// Person reads one row: this caller's, in the transaction their request already
// holds. The tenant is not an argument, because the transaction is the tenant and
// row-level security has already said which rows are in it; an account of another
// tenant answers "not here", which the frame says as "Signed in".
func (p people) Person(ctx context.Context, userID uuid.UUID) (internal.Person, error) {
	tx, live := httpx.TxFrom(ctx)
	if !live {
		return internal.Person{}, errors.New("admin: this request carries no transaction to read the caller in")
	}
	user, err := p.users.Get(ctx, tx, userID)
	if err != nil {
		return internal.Person{}, err
	}
	return internal.Person{DisplayName: user.DisplayName, Email: user.Email}, nil
}

// registration is the port's form choice read through the two lifecycles this
// shell renders. Nothing maps a word the shell does not render: an unknown kind
// is no form at all, which is the same answer a nil gave.
func registration(in *admincontracts.Registration) *Registration {
	if in == nil {
		return nil
	}
	out := &Registration{Address: in.Address}
	switch in.Kind {
	case admincontracts.KindEmail:
		out.Kind = RegistrationKindEmail
	case admincontracts.KindPassword:
		out.Kind = RegistrationKindPassword
	}
	return out
}
