package seed

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Clock makes a run's relative dates deterministic.
type Clock interface{ Now() time.Time }

// Resource is the explicit seed surface an owner offers. Alias is the name in
// embedded files; Module and Entity are the provenance identity in seed_keys.
//
// CanonicalKey is how this owner stores a record's key. A file may write
// `About The Team`; the module that owns the page may store `about-the-team`;
// those are one record, and the seed must address it one way. Provenance, the
// prune keep-set and reference lookup all go through it, because a run that
// stores provenance under the file's spelling and finds the row by the owner's
// reports one record twice: it maps what it cannot see, and a pruning file that
// respells a key then deletes the row it still declares. Nil means the owner
// stores the key exactly as the file spells it.
type Resource struct {
	Alias, Module, Entity, NaturalKey, WriteGrant string
	Prunable                                      bool
	References                                    []Reference
	Commands                                      []string
	CanonicalKey                                  func(value string) string
}

// canonicalKey is the identity a record has where it is stored. Writers declare
// it; nothing else in this package spells a key by hand.
func canonicalKey(r Resource, value string) string {
	if r.CanonicalKey == nil {
		return value
	}
	return r.CanonicalKey(value)
}

// Key pairs a seed name with the owner's existing row ID, when provenance has
// one. A natural-key writer can resolve Value without an ID. Kind is the file
// kind that mapping was written by, empty when no mapping names the record: a
// run needs it because provenance is what prune reads, and a record declared by
// the demo file but mapped by the starter's earlier run is a demo record.
type Key struct {
	Value    string
	RecordID uuid.UUID
	Kind     string
}

// Writer is implemented by each owning resource at composition. Create,
// Update and Delete must use the owner's normal validated, event-producing
// write path. Read locks the owner row when forUpdate is true. Target accounts
// for every field the record declares, by naming it in Fields or in CreateOnly:
// a field in neither is a declaration the run would read and then drop, so it
// refuses the record at its own line before anything is written.
type Writer interface {
	Resource() Resource
	Target(context.Context, Record, map[string]uuid.UUID, time.Time) (Target, error)
	Read(context.Context, db.Tx[db.Tenant], Key, bool) (Snapshot, error)
	Create(context.Context, db.Tx[db.Tenant], Target) (Snapshot, error)
	Update(context.Context, db.Tx[db.Tenant], Snapshot, Target) (Snapshot, error)
	Delete(context.Context, db.Tx[db.Tenant], Snapshot) error
}

// Authorizer rechecks the actor and owner grant in the authoritative tenant
// transaction. A missing authorizer refuses both planning and applying.
type Authorizer interface {
	Check(context.Context, db.Tx[db.Tenant], Resource, Action) error
}

type Deps struct {
	Files     fs.FS
	Root      string
	Clock     Clock
	Writers   []Writer
	Authorize Authorizer
}

type Service struct {
	files     fs.FS
	root      string
	clock     Clock
	writers   map[string]Writer
	authorize Authorizer
}

// New copies the literal writer list and refuses ambiguous aliases or owners.
func New(deps Deps) (*Service, error) {
	if deps.Files == nil || deps.Clock == nil || deps.Authorize == nil || !fs.ValidPath(deps.Root) {
		return nil, errors.New("seed: files, root, clock and authorizer are required")
	}
	s := &Service{files: deps.Files, root: deps.Root, clock: deps.Clock, writers: make(map[string]Writer), authorize: deps.Authorize}
	owners := make(map[string]bool)
	for _, writer := range deps.Writers {
		if writer == nil {
			return nil, errors.New("seed: nil writer")
		}
		r := writer.Resource()
		owner := r.Module + "/" + r.Entity
		if !validName(r.Alias) || !validName(r.Module) || !validName(r.Entity) || r.WriteGrant == "" || s.writers[r.Alias] != nil || owners[owner] {
			return nil, fmt.Errorf("seed: ambiguous or incomplete writer %q (%s)", r.Alias, owner)
		}
		for _, ref := range r.References {
			if ref.Resource != r.Alias {
				return nil, fmt.Errorf("seed: reference %s belongs to %s, not %s", ref.Path, ref.Resource, r.Alias)
			}
		}
		s.writers[r.Alias], owners[owner] = writer, true
	}
	return s, nil
}

type Selection struct{ Demo bool }

type Item struct {
	Action        Action
	Resource, Key string
	Source        Source
	Changed       []string
	RecordID      uuid.UUID
}

type Plan struct {
	TenantID uuid.UUID
	Kinds    []string
	At       time.Time
	Items    []Item
	Warnings []string
}

// String contains no field values, file bytes or credentials.
func (p Plan) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "tenant=%s kind=%s at=%s\n", p.TenantID, strings.Join(p.Kinds, ","), p.At.Format(time.RFC3339))
	counts := map[Action]int{}
	for _, item := range p.Items {
		fmt.Fprintf(&b, "%s %s/%s", strings.ToUpper(string(item.Action)), item.Resource, item.Key)
		if len(item.Changed) > 0 {
			fmt.Fprintf(&b, " %s", strings.Join(item.Changed, ","))
		}
		fmt.Fprintf(&b, " %s\n", item.Source)
		counts[item.Action]++
	}
	for _, warning := range p.Warnings {
		fmt.Fprintf(&b, "WARNING %s\n", warning)
	}
	fmt.Fprintf(&b, "summary: %d created, %d updated, %d unchanged, %d pruned\n", counts[Create], counts[Update], counts[Unchanged], counts[Prune])
	return b.String()
}

const Prune Action = "prune"

// Plan reads owner rows and provenance under RLS but never writes. Its answer
// is informational; Apply reloads and re-decides inside its own transaction.
func (s *Service) Plan(ctx context.Context, tx db.Tx[db.Tenant], selection Selection) (Plan, error) {
	return s.run(ctx, tx, selection, false, false)
}

// Apply takes a transaction advisory lock, rechecks each owner grant and row,
// then writes through owners. The caller must roll back the transaction on any
// error; Run or InTenant does that when the error is propagated.
func (s *Service) Apply(ctx context.Context, tx db.Tx[db.Tenant], selection Selection) (Plan, error) {
	if err := lockRun(tx); err != nil {
		return Plan{}, err
	}
	return s.run(ctx, tx, selection, false, true)
}

// ApplyProvisioned seeds a new tenant without requiring an existing principal.
// It takes the seed lock and refuses nonempty provenance or any existing declared
// record. The composition's Authorizer must verify that a personless caller may
// provision this tenant; the marker alone does not prove a create transaction.
func (s *Service) ApplyProvisioned(ctx context.Context, tx db.Tx[db.Tenant], selection Selection) (Plan, error) {
	if err := lockRun(tx); err != nil {
		return Plan{}, err
	}
	var keys int
	if err := tx.DB().Raw(`SELECT count(*) FROM seed_keys WHERE tenant_id = ?`, db.TenantOf(tx).ID).Row().Scan(&keys); err != nil {
		return Plan{}, fmt.Errorf("seed: provision: read provenance: %w", err)
	}
	if keys != 0 {
		return Plan{}, errors.New("seed: this tenant already holds seeded records, so it is not being provisioned")
	}
	return s.run(withProvisioning(ctx), tx, selection, true, true)
}

// lockRun is the one advisory lock every write run takes, so a concurrent run on
// the same tenant reconciles against the same rows rather than against each
// other's half-written provenance.
func lockRun(tx db.Tx[db.Tenant]) error {
	if err := tx.DB().Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "platformkit:seed:"+db.TenantOf(tx).ID.String()).Error; err != nil {
		return fmt.Errorf("seed: lock run: %w", err)
	}
	return nil
}

// provisioningKey is what ApplyProvisioned puts on the context and what
// Provisioning reads back. It is unexported because the only way to obtain it is
// the run that checked the tenant's provenance was empty.
type provisioningKey struct{}

func withProvisioning(ctx context.Context) context.Context {
	return context.WithValue(ctx, provisioningKey{}, true)
}

// Provisioning reports whether this context belongs to a provisioning run. A
// composition's Authorizer is what decides whether such a run may write, and it
// should ask something about the tenant rather than take the marker at face
// value — see the doc on ApplyProvisioned.
func Provisioning(ctx context.Context) bool {
	ok, _ := ctx.Value(provisioningKey{}).(bool)
	return ok
}

// seedAttribution is the provenance every write of this run leaves on the events
// its owners publish: the kind of cause, the file and line that asked for the
// record, and the person the run named. kit/events owns the type and the outbox
// columns; see migrations/000052_outbox_attribution.up.sql.
func seedAttribution(ctx context.Context, source Source) events.Attribution {
	a := events.Attribution{
		ActorKind:  events.ActorSeed,
		SourceFile: source.File,
		SourceLine: source.Line,
	}
	if p, ok := tenancy.PrincipalFrom(ctx); ok {
		a.InitiatorID = p.UserID
	}
	return a
}
