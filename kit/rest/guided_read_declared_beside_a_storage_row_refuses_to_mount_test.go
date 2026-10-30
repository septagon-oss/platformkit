package rest_test

// A guided session has no table: the module's read is the whole of the row, and the two
// catalogue keys a shell dispatches on — `readContract` and `resolved.session` — describe
// a row no storage entity holds. `Session` mounts only through SessionSpec, the entity
// type a guided read is handed, so a storage entity that declares one beside its own
// columns refuses to mount, naming the type that is wrong rather than the mount that
// would have taken it.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// TestAGuidedReadDeclaredBesideAStorageRowRefusesToMount names the third refusal of
// that pair, beside the two that mount the halves together: neither half arrives
// beside a storage row, which speaks neither key. The declaration is otherwise one a
// session mount admits — a read grant, Operations [read], no storage steps — so the
// fault it is refused for is the type it names its row by.
func TestAGuidedReadDeclaredBesideAStorageRowRefusesToMount(t *testing.T) {
	load := func(context.Context, db.Tx[db.Tenant], uuid.UUID, uuid.UUID) (entity.GuidedSession, error) {
		return entity.GuidedSession{}, nil
	}
	hintRefused(t, func() {
		rest.Spec[*hintedOK]{Module: "practice", Entity: "session", Path: "/sessions",
			Read: "practice:read", Operations: []httpx.CRUD{rest.Read},
			Present: entity.EntryHints{Session: true},
			Session: &rest.SessionRead{Load: load}}.Mount(httpx.Surfaces{})
	}, "use SessionSpec")
}
