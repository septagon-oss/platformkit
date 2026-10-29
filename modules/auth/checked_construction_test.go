package auth_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// The three doubles below exist only to make one Deps field non-nil: the
// constructor refuses the wiring before it calls any of them.
type registrar struct{}

func (registrar) ByEmail(context.Context, db.Tx[db.Tenant], string) (*user.User, error) {
	return nil, nil
}
func (registrar) Invite(context.Context, db.Tx[db.Tenant], string, string) (*user.User, error) {
	return nil, nil
}
func (registrar) SetRoles(context.Context, db.Tx[db.Tenant], uuid.UUID, []string) (*user.User, error) {
	return nil, nil
}

type authorizer struct{}

func (authorizer) Allowed(context.Context, db.Tx[db.Tenant], tenancy.Grant) (bool, error) {
	return false, nil
}

type permissions struct{}

func (permissions) Permissions(context.Context, db.Tx[db.Tenant], []string) ([]string, error) {
	return nil, nil
}

// TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence covers the seven refusals in the order
// the constructor reaches them, so a Deps wrong in two ways is refused for the
// first one and one mistake stays one sentence.
func TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence(t *testing.T) {
	rows := []struct {
		name string
		deps auth.Deps
		want string
	}{
		{"two registration lifecycles", auth.Deps{
			EmailRegistration:    &contracts.EmailRegistration{},
			ApprovalRegistration: &contracts.ApprovalRegistration{},
		}, "auth: choose only one registration lifecycle"},
		{"an emailed setup with nobody to register and no roles", auth.Deps{EmailRegistration: &contracts.EmailRegistration{}}, "email registration requires a registrar and initial roles"},
		{"an approval door beside open signup", auth.Deps{
			ApprovalRegistration: &contracts.ApprovalRegistration{},
			Registration:         registrar{},
		}, "auth: choose emailed password setup or approval-required registration"},
		{"a mailed link that is not a page on this site", auth.Deps{LinkPages: auth.LinkPages{SetPassword: "account/password"}}, "name an absolute path on the tenant's host"},
		{"a mailed link under the API root", auth.Deps{LinkPages: auth.LinkPages{ConfirmEmail: "/api/v1/verify"}}, "it is under the API root"},
		{"an account budget the counter cannot keep", auth.Deps{AccountRequests: contracts.AccountRequestBudget{Quota: -1}}, "auth: Deps.AccountRequests"},
		{"an authorizer with no reader beside it", auth.Deps{Authorization: authorizer{}}, "auth: supply both Authorization and Permissions, or leave both nil"},
		{"a reader with no authorizer beside it", auth.Deps{Permissions: permissions{}}, "auth: supply both Authorization and Permissions, or leave both nil"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			_, _, err := auth.NewChecked(r.deps)
			if err == nil {
				t.Fatal("NewChecked built a door out of the wiring it should refuse")
			}
			if !strings.Contains(err.Error(), r.want) {
				t.Errorf("refusal = %q, want it to contain %q", err, r.want)
			}
			var got any
			func() {
				defer func() { got = recover() }()
				auth.New(r.deps)
			}()
			if s, ok := got.(string); !ok || s != err.Error() {
				t.Errorf("panicked with %#v, want the string the checked form answered %q", got, err)
			}
		})
	}
}

// TestNewCheckedBuildsFromItsRequiredDependenciesAlone: the ports the plan answers
// are not the constructor's to refuse, and every optional half of auth — no
// registration lifecycle, no second factor, no outside provider — is an installation
// that simply does not have that door.
func TestNewCheckedBuildsFromItsRequiredDependenciesAlone(t *testing.T) {
	svc, m, err := auth.NewChecked(auth.Deps{})
	if err != nil {
		t.Fatalf("NewChecked with an empty Deps: %v", err)
	}
	if svc == nil {
		t.Fatal("the auth service is nil")
	}
	if m.Name != "auth" {
		t.Errorf("manifest name = %q, want auth", m.Name)
	}
	if err := (contracts.AccountRequestBudget{Quota: 10, Window: 15 * time.Minute}).Check(); err != nil {
		t.Errorf("the budget the empty Deps defaults to does not check: %v", err)
	}
}
