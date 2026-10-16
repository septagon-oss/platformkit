package contracts

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The four permissions files have. Reading is reading a private file's bytes,
// minting a grant for them, and listing what there is; managing is uploading and
// deleting. A public file's bytes need neither, which is what "public" means.
//
// Erasing is a different promise from deleting: "and nothing is left" is a
// stronger sentence than "the row is gone", so it has a permission of its own
// rather than borrowing file:manage. Retaining is the same in the other
// direction — placing a hold stops the clock for everybody else's deletion — so
// it is not something a reader does by default. Both are declared in the
// manifest, which is what lets kit/app refuse a route that guards itself with a
// permission nobody can be granted.
//
// The list the manifest declares is in ../module.go, which keeps kit/module out
// of this package's build graph.
const (
	PermissionFileRead   = "file:read"
	PermissionFileManage = "file:manage"
	PermissionFileErase  = "file:erase"
	PermissionFileRetain = "file:retain"
)

// ErrNotPermitted refuses a command to a caller whose own credential does not carry the
// permission it needs. It is not ErrInvalid — nothing about the request is wrong, the
// person simply cannot make it — and not a not-found, because the row is there and this
// caller's ceiling, not the tenant's policy, is what answers.
var ErrNotPermitted = errors.New("file: this caller's credential does not carry the permission")

// Carries is the one authority question a command can ask without the composition's
// authorizer, and every command in this module asks it before it touches a row: does the
// credential making this call carry the permission the command needs?
//
// Only a credential that carries its own ceiling is answered. tenancy.Principal says what
// a non-nil list means — a ceiling and not an addition, the intersection of a key's scopes
// and its holder's grants, recomputed when the request resolved the key — and a command
// that resolved roles of its own would be a second authorizer beside the one the kernel
// already asked, reading the same rows a different way. So:
//
//   - a caller with a ceiling is held to it, by exact name, whatever their roles say, here
//     as in the request middleware that checks the same field before it asks anybody; a
//     key narrowed to one read may not caption a picture because the door it came through
//     happened not to ask;
//   - a caller with no ceiling and a caller with no principal at all are passed: their
//     permission is decided by the guard on the route, and by nothing this module can read.
//     A job, a subscriber and a neighbouring module's own command arrive as that second
//     case, and refusing them would be this module inventing a policy nobody composed.
func Carries(ctx context.Context, permission string) error {
	p, ok := tenancy.PrincipalFrom(ctx)
	if !ok || p.Permissions == nil {
		return nil
	}
	if !slices.Contains(p.Permissions, permission) {
		return fmt.Errorf("%w: %s", ErrNotPermitted, permission)
	}
	return nil
}
