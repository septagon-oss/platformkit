package admin_test

import (
	"context"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// noVersions is the version store a mounted content resource needs in these
// cases. Every request they make is refused before it commits, so nothing is ever
// written here; the mount is what requires a store to be named at all.
type noVersions struct{}

func (noVersions) RecordVersion(context.Context, db.Tx[db.Tenant], httpx.VersionInput) (uuid.UUID, error) {
	return uuid.Nil, nil
}

func (noVersions) ListVersions(context.Context, db.Tx[db.Tenant], httpx.VersionQuery) ([]httpx.Version, error) {
	return nil, nil
}

func (noVersions) Version(context.Context, db.Tx[db.Tenant], httpx.VersionKey) (httpx.Version, error) {
	return httpx.Version{}, crud.ErrNotFound
}
