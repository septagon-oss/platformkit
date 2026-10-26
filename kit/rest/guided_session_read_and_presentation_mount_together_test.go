package rest_test

// A catalogue row says it is a guided session with two keys of one entry:
// `readContract`, which it writes because the resource carries a guided read, and
// `resolved.session`, which it writes because the resource carries the session
// presentation hint. The mount is what keeps them one declaration: it refuses a
// session read with no hint and a hint over a storage row. The pair is pinned
// there, because a document built from a hand-written resource cannot show a
// composition that stopped requiring both.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// TestAGuidedSessionReadAndItsPresentationRefuseToMountApart mounts each half on
// its own. Left to mount, either would publish one key and not the other: the
// hint over a note hands a phone session copy beside no contract to walk, and a
// guided read with no hint hands a contract whose words nobody wrote.
func TestAGuidedSessionReadAndItsPresentationRefuseToMountApart(t *testing.T) {
	t.Run("the hint over a storage row", func(t *testing.T) {
		hintRefused(t, mountSpec[*hintedOK](entity.EntryHints{Session: true}, "/sessions"),
			"session presentation requires Session")
	})
	t.Run("the read without its hint", func(t *testing.T) {
		load := func(context.Context, db.Tx[db.Tenant], uuid.UUID, uuid.UUID) (entity.GuidedSession, error) {
			return entity.GuidedSession{}, nil
		}
		hintRefused(t, func() {
			rest.SessionSpec{Module: "practice", Entity: "session", Path: "/sessions",
				Read: "practice:read", Operations: []httpx.CRUD{rest.Read},
				Session: &rest.SessionRead{Load: load}}.Mount(httpx.Surfaces{})
		}, "Load and Present.Session are required")
	})
}
