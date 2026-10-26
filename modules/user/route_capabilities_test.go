package user_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/internal"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

type onlyInvitations struct {
	next  contracts.InvitationWriter
	calls map[string]int
}

func (n onlyInvitations) Invite(ctx context.Context, tx db.Tx[db.Tenant], email, displayName string) (*contracts.User, error) {
	n.calls["Invite"]++
	return n.next.Invite(ctx, tx, email, displayName)
}
func (n onlyInvitations) SetRoles(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, roles []string) (*contracts.User, error) {
	n.calls["SetRoles"]++
	return n.next.SetRoles(ctx, tx, id, roles)
}

type onlyApprovals struct {
	next  contracts.RegistrationApprover
	calls map[string]int
}

func (n onlyApprovals) ApproveRegistration(arg0 context.Context, arg1 db.Tx[db.Tenant], arg2 uuid.UUID, arg3 uuid.UUID) (*contracts.User, error) {
	n.calls["ApproveRegistration"]++
	return n.next.ApproveRegistration(arg0, arg1, arg2, arg3)
}

type onlyPasswords struct {
	next  contracts.PasswordWriter
	calls map[string]int
}

func (n onlyPasswords) SetPassword(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, password string) error {
	n.calls["SetPassword"]++
	return n.next.SetPassword(ctx, tx, id, password)
}
func (n onlyPasswords) Get(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.User, error) {
	n.calls["Get"]++
	return n.next.Get(ctx, tx, id)
}

type onlyRoles struct {
	next  contracts.RoleSetter
	calls map[string]int
}

func (n onlyRoles) SetRoles(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, roles []string) (*contracts.User, error) {
	n.calls["SetRoles"]++
	return n.next.SetRoles(ctx, tx, id, roles)
}

type onlyHandles struct {
	next  contracts.HandleSetter
	calls map[string]int
}

func (n onlyHandles) SetHandle(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, handle string) (*contracts.User, error) {
	n.calls["SetHandle"]++
	return n.next.SetHandle(ctx, tx, id, handle)
}

type onlyDeactivation struct {
	next  contracts.Deactivator
	calls map[string]int
}

func (n onlyDeactivation) Deactivate(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.User, error) {
	n.calls["Deactivate"]++
	return n.next.Deactivate(ctx, tx, id)
}

func TestUserRoutesUseDistinctNarrowCapabilities(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations)
	svc, _ := user.New(user.Deps{Administration: &contracts.AdministrationFunc{Ask: administering},
		Granting: &contracts.GrantingFunc{Ask: func(context.Context, db.Tx[db.Tenant]) (bool, error) { return true, nil }}})
	actor := uuid.New()
	api, router := httpx.New(httpx.Options{Cache: cache.Memory("pkit"), PublicHost: host, Tenants: everything{}, Conn: conn, Authorize: everything{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: actor}, true, nil
		}, Log: slog.New(slog.DiscardHandler)})
	calls := map[string]int{}
	internal.RegisterRoutes(surfacesOf(api), rest.Spec[*contracts.User]{Module: "user", Entity: "user", Path: "/users", Write: contracts.PermissionUserManage}, internal.RouteServices{
		Invitations:  onlyInvitations{next: svc, calls: calls},
		Approvals:    onlyApprovals{next: svc, calls: calls},
		Passwords:    onlyPasswords{next: svc, calls: calls},
		Roles:        onlyRoles{next: svc, calls: calls},
		Handles:      onlyHandles{next: svc, calls: calls},
		Deactivation: onlyDeactivation{next: svc, calls: calls},
	})
	code, body := call(t, router, "POST", "/api/v1/user/invitations", `{"email":"ada@example.test","roles":["member"]}`)
	if code != 201 {
		t.Fatalf("invitation = %d %s", code, body)
	}
	var invited, pending *contracts.User
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		invited, err = svc.ByEmail(ctx, tx, "ada@example.test")
		if err != nil {
			return err
		}
		pending, err = svc.RegisterPending(ctx, tx, contracts.PendingRegistration{Email: "grace@example.test", Password: "a sufficiently long password", Roles: []string{"member"}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, path, body, method string }{
		{"password", invited.ID.String() + "/set-password", `{"password":"a sufficiently long password"}`, "SetPassword"},
		{"roles", invited.ID.String() + "/roles", `{"roles":["observer"]}`, "SetRoles"},
		{"handle", invited.ID.String() + "/handle", `{"handle":"ada"}`, "SetHandle"},
		{"deactivation", invited.ID.String() + "/deactivate", "{}", "Deactivate"},
		{"approval", pending.ID.String() + "/approve-registration", "{}", "ApproveRegistration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, body := call(t, router, "POST", "/api/v1/user/users/"+tc.path, tc.body)
			if code != 200 || calls[tc.method] == 0 {
				t.Fatalf("%s = %d %s; calls=%v", tc.name, code, body, calls)
			}
		})
	}
	if calls["Get"] == 0 || calls["Invite"] != 1 || !strings.Contains(body, "ada@example.test") {
		t.Fatalf("invitation/password reads missing: %v", calls)
	}
}
