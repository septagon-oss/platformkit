package contracts

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// PermissionReader resolves the union of current role permissions in the supplied
// tenant transaction. Unknown roles contribute nothing; an empty input reads nothing.
type PermissionReader interface {
	Permissions(context.Context, db.Tx[db.Tenant], []string) ([]string, error)
}

// GrantAuthorizer evaluates the context's principal in the supplied live tenant
// transaction. A conflicting context tenant or a failed permission read is an
// error, never an allow.
type GrantAuthorizer interface {
	Allowed(context.Context, db.Tx[db.Tenant], tenancy.Grant) (bool, error)
}

// Match reports whether held permissions satisfy a grant: an ordinary permission
// by the wildcard or by naming it, an operator permission only by naming it. It
// validates neither the grant's name nor its tenant.
func Match(held []string, want tenancy.Grant) bool {
	return slices.Contains(held, want.Permission) || !want.Operator && slices.Contains(held, Wildcard)
}

// Decide answers a grant for a principal without retaining or changing its
// authority. A non-nil permission ceiling bypasses the role read; a blank grant
// and a required nil reader are errors, an absent principal is a plain deny.
func Decide(principal tenancy.Principal, want tenancy.Grant, permissions func([]string) ([]string, error)) (bool, error) {
	if strings.TrimSpace(want.Permission) == "" {
		return false, errors.New("auth: a permission is required")
	}
	if principal.UserID == uuid.Nil {
		return false, nil
	}
	if principal.Permissions != nil {
		return Match(principal.Permissions, want), nil
	}
	if len(principal.Roles) == 0 {
		return false, nil
	}
	if permissions == nil {
		return false, errors.New("auth: a role permission reader is required")
	}
	held, err := permissions(principal.Roles)
	if err != nil {
		return false, err
	}
	return Match(held, want), nil
}
