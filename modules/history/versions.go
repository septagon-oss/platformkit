package history

import (
	"context"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/history/contracts"
)

// KeepVersions adapts this module's store to the port kit/rest records through, so
// that a resource mounted with History keeps its versions here. It is the same
// relationship as modules/file's RecordUses: the kit declares what it needs, this
// module satisfies it, and neither imports the other's vocabulary — which is why
// the two input types are copied field by field here and nowhere else.
type KeepVersions struct{ Service contracts.Service }

var _ httpx.Versions = KeepVersions{}

// RecordVersion answers the version row's id; the store files the references beside
// it, because the store is what decides how long a version lives and therefore what
// a version is entitled to keep alive.
func (k KeepVersions) RecordVersion(ctx context.Context, tx db.Tx[db.Tenant], in httpx.VersionInput) (uuid.UUID, error) {
	if k.Service == nil {
		return uuid.Nil, errNoService
	}
	return k.Service.Record(ctx, tx, contracts.Input{
		Module: in.Module, Entity: in.Entity, Locale: in.Locale, Record: in.Record,
		Version: in.Version, Fields: in.Fields, Changed: in.Changed, Reason: in.Reason,
		Refs: refs(in.Refs), At: in.At, Actor: in.Actor,
	})
}

func (k KeepVersions) ListVersions(ctx context.Context, tx db.Tx[db.Tenant], q httpx.VersionQuery) ([]httpx.Version, error) {
	if k.Service == nil {
		return nil, errNoService
	}
	items, err := k.Service.Page(ctx, tx, contracts.Query{
		Module: q.Module, Entity: q.Entity, Locale: q.Locale, Record: q.Record,
		Limit: q.Limit, Before: q.Before,
	})
	if err != nil {
		return nil, err
	}
	out := make([]httpx.Version, 0, len(items))
	for _, v := range items {
		out = append(out, version(v))
	}
	return out, nil
}

func (k KeepVersions) Version(ctx context.Context, tx db.Tx[db.Tenant], key httpx.VersionKey) (httpx.Version, error) {
	if k.Service == nil {
		return httpx.Version{}, errNoService
	}
	v, err := k.Service.Version(ctx, tx, contracts.Key{
		Module: key.Module, Entity: key.Entity, Locale: key.Locale, Record: key.Record, Version: key.Version,
	})
	if err != nil {
		return httpx.Version{}, err
	}
	return version(v), nil
}

func refs(in []httpx.VersionRefs) []contracts.Ref {
	if len(in) == 0 {
		return nil
	}
	out := make([]contracts.Ref, 0, len(in))
	for _, r := range in {
		out = append(out, contracts.Ref{Field: r.Field, Files: r.Files})
	}
	return out
}

func version(v contracts.Version) httpx.Version {
	return httpx.Version{
		ID: v.ID, Module: v.Module, Entity: v.Entity, Record: v.Record, Locale: v.Locale,
		Version: v.Version, Fields: v.Fields, Changed: v.Changed, Reason: v.Reason,
		Actor: v.Actor, At: v.At,
	}
}
