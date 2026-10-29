package pkit_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/auth"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
	"github.com/septagon-oss/platformkit/pkit"
)

type passwordAccounts struct{ fake *usertest.Fake }

func (p passwordAccounts) ByEmail(ctx context.Context, tx db.Tx[db.Tenant], email string) (*usercontracts.User, error) {
	return p.fake.ByEmail(ctx, tx, email)
}
func (p passwordAccounts) Invite(ctx context.Context, tx db.Tx[db.Tenant], email, name string) (*usercontracts.User, error) {
	return p.fake.Invite(ctx, tx, email, name)
}
func (p passwordAccounts) SetRoles(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, roles []string) (*usercontracts.User, error) {
	return p.fake.SetRoles(ctx, tx, id, roles)
}

type emailAccounts struct{ fake *usertest.Fake }

func (p emailAccounts) RegisterUnverified(ctx context.Context, tx db.Tx[db.Tenant], in usercontracts.PasswordRegistration) (*usercontracts.User, error) {
	return p.fake.RegisterUnverified(ctx, tx, in)
}
func (p emailAccounts) VerifyEmail(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, email string) (*usercontracts.User, error) {
	return p.fake.VerifyEmail(ctx, tx, id, email)
}

type pendingAccounts struct{ fake *usertest.Fake }

func (p pendingAccounts) RegisterPending(ctx context.Context, tx db.Tx[db.Tenant], in usercontracts.PendingRegistration) (*usercontracts.User, error) {
	return p.fake.RegisterPending(ctx, tx, in)
}

func identityValue[T any](name string, value T) *pkit.Module {
	return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[T](w, value)
		return module.Module{Name: name}, nil
	}, pkit.Provides[T]())
}

func TestRegistrationProvidersBuildFromTheirOwnUserCapability(t *testing.T) {
	for _, name := range []string{"password", "email", "approval"} {
		t.Run(name, func(t *testing.T) {
			fake := usertest.NewFake()
			var provider, registration *pkit.Module
			switch name {
			case "password":
				provider, registration = identityValue[usercontracts.RegistrationUsers]("accounts", passwordAccounts{fake}), auth.Registration
			case "email":
				provider, registration = identityValue[usercontracts.EmailRegistrar]("accounts", emailAccounts{fake}), auth.EmailRegistration
			case "approval":
				provider, registration = identityValue[usercontracts.PendingRegistrar]("accounts", pendingAccounts{fake}), auth.ApprovalRegistration
			}
			plan, err := pkit.NewApp("signup").Use(registration, provider, doors, desk).Plan(pkit.Deployment{Environment: pkit.Development})
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := pkit.Value[usercontracts.Service](plan); ok {
				t.Fatal("registration acquired an aggregate")
			}
			mode, ok := pkit.Value[authcontracts.RegistrationMode](plan)
			if !ok {
				t.Fatal("registration mode missing")
			}
			checkRegistrationAccount(t, mode)
			if len(fake.Published()) == 0 {
				t.Fatal("the supplied account capability was not called")
			}
		})
	}
}

func checkRegistrationAccount(t *testing.T, mode authcontracts.RegistrationMode) {
	t.Helper()
	ctx, tx := t.Context(), db.Tx[db.Tenant]{}
	in := usercontracts.PasswordRegistration{Email: "ada@example.test", Password: "a sufficiently long password", Roles: []string{"member"}}
	var person *usercontracts.User
	var err error
	switch m := mode.(type) {
	case authcontracts.PasswordRegistration:
		person, err = m.Users.Invite(ctx, tx, in.Email, "Ada")
		if err == nil {
			person, err = m.Users.SetRoles(ctx, tx, person.ID, m.Roles)
		}
		if err == nil {
			person, err = m.Users.ByEmail(ctx, tx, in.Email)
		}
	case authcontracts.EmailRegistrationMode:
		person, err = m.Policy.Users.RegisterUnverified(ctx, tx, in)
		if err == nil {
			person, err = m.Policy.Users.VerifyEmail(ctx, tx, person.ID, in.Email)
		}
	case authcontracts.ApprovalRegistrationMode:
		person, err = m.Policy.Users.RegisterPending(ctx, tx, in)
	}
	if err != nil || person == nil {
		t.Fatalf("registration = %v, %v", person, err)
	}
}

func TestRegistrationSelectionRemainsOptionalAndUnambiguous(t *testing.T) {
	fake := usertest.NewFake()
	app := pkit.NewApp("signup").Use(doors, desk)
	p, err := app.Plan(pkit.Deployment{Environment: pkit.Development})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pkit.Value[authcontracts.RegistrationMode](p); ok {
		t.Fatal("absent registration became a form")
	}
	observer := pkit.NewModule("registrationconsumer", func(w *pkit.Wiring) (module.Module, error) {
		_ = pkit.Get[authcontracts.RegistrationMode](w)
		return module.Module{Name: "registrationconsumer"}, nil
	}, pkit.Optional[authcontracts.RegistrationMode]())
	app.Use(observer, auth.Registration, auth.ApprovalRegistration,
		identityValue[usercontracts.RegistrationUsers]("passwordaccounts", passwordAccounts{fake}),
		identityValue[usercontracts.PendingRegistrar]("pendingaccounts", pendingAccounts{fake}))
	if _, err := app.Plan(pkit.Deployment{Environment: pkit.Development}); err == nil {
		t.Fatal("two registration modes were accepted")
	}
}
