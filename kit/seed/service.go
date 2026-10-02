package seed

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
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
type Resource struct {
	Alias, Module, Entity, NaturalKey, WriteGrant string
	Prunable                                      bool
	References                                    []Reference
	Commands                                      []string
}

// Key pairs a seed name with the owner's existing row ID, when provenance has
// one. A natural-key writer can resolve Value without an ID.
type Key struct {
	Value    string
	RecordID uuid.UUID
}

// Writer is implemented by each owning resource at composition. Create,
// Update and Delete must use the owner's normal validated, event-producing
// write path. Read locks the owner row when forUpdate is true.
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

// ApplyProvisioned is the run a tenant's own creation makes, and the only way a
// seed reaches an owner without a person on the context.
//
// The gap it answers is real: a brand-new tenant has no people, so there is no
// address `--as` could name, and "your app opens empty until somebody runs a
// command" is not an application. Refusing a personless run outright would keep
// the authorizer honest and lose the feature. So the provisioning run carries
// its own proof, and the proof is state rather than a claim: the tenant has no
// seed provenance at all, and every record the files declare is still absent
// (see run). A tenant that has been seeded, or that already holds records the
// files name, is not being provisioned, and this refuses it.
//
// What this cannot prove is that the call came from a create transaction — only
// the composition knows that, and it is the composition's authorizer that decides
// what a personless run may write. apps/platformkit's hook answers the rest with
// the tenant's own people table: a tenant with anybody in it is not new.
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
// columns; see migrations/000037.
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

func (s *Service) run(ctx context.Context, tx db.Tx[db.Tenant], selection Selection, provisioning, apply bool) (Plan, error) {
	var plan Plan
	plan.TenantID = db.TenantOf(tx).ID
	plan.At = s.clock.Now().UTC()
	plan.Kinds = []string{"starter"}
	if selection.Demo {
		demo, err := persistedDemo(tx)
		if err != nil {
			return Plan{}, err
		}
		if !demo {
			return Plan{}, errors.New("seed: demo records are refused for a non-demo tenant")
		}
		plan.Kinds = append(plan.Kinds, "demo")
	}
	documents, err := Load(s.files, s.root, plan.Kinds...)
	if err != nil {
		return Plan{}, err
	}
	var references []Reference
	for _, doc := range documents {
		writer := s.writers[doc.Resource]
		if writer == nil {
			return Plan{}, fmt.Errorf("seed: %s: no writer for %s", doc.Source, doc.Resource)
		}
		r := writer.Resource()
		if doc.Prune && !r.Prunable {
			return Plan{}, fmt.Errorf("seed: %s: %s has no owner delete path", doc.Source, doc.Resource)
		}
		for _, record := range doc.Records {
			for _, command := range record.Commands {
				if !slices.Contains(r.Commands, command.Name) {
					return Plan{}, fmt.Errorf("seed: %s: %s has no command %s", command.Source, doc.Resource, command.Name)
				}
			}
			if len(record.I18n) > 0 {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s/%s i18n skipped: translation writer unavailable", doc.Resource, record.Key))
			}
		}
		references = append(references, r.References...)
	}
	resolved := make(map[string]uuid.UUID)
	ordered, err := Order(documents, references, func(resource, key string) (bool, error) {
		id, present, err := s.existing(ctx, tx, resource, key)
		if err != nil {
			return false, err
		}
		if present {
			resolved[resource+"/"+key] = id
		}
		return present, nil
	})
	if err != nil {
		return Plan{}, err
	}
	for _, entry := range ordered {
		writer := s.writers[entry.Resource]
		r := writer.Resource()
		// Every owner write this record causes carries the record's own address
		// onto the event the owner publishes. It is on the context rather than an
		// argument because the code that publishes is the owner's write path,
		// several calls away from this loop.
		wctx := events.WithAttribution(ctx, seedAttribution(ctx, entry.Record.Source))
		// The resource's own write grant, asked before an update-locking read of
		// the owner's row: a run whose actor holds nothing is refused without
		// taking a lock a person is waiting on.
		if err := s.authorize.Check(ctx, tx, r, Update); err != nil {
			return Plan{}, fmt.Errorf("seed: %s: %w", entry.Record.Source, err)
		}
		key, owned, err := lookupKey(tx, r, entry.Record.Key)
		if err != nil {
			return Plan{}, fmt.Errorf("seed: %s: %w", entry.Record.Source, err)
		}
		current, err := writer.Read(ctx, tx, key, apply)
		if err != nil {
			return Plan{}, fmt.Errorf("seed: %s: %w", entry.Record.Source, err)
		}
		target, err := writer.Target(ctx, entry.Record, maps.Clone(resolved), plan.At)
		if err != nil {
			return Plan{}, fmt.Errorf("seed: %s: %w", entry.Record.Source, err)
		}
		decision := Decide(current, target)
		if provisioning && decision.Action != Create {
			return Plan{}, fmt.Errorf("seed: %s: %s/%s already exists, so this tenant is not being provisioned",
				entry.Record.Source, entry.Resource, entry.Record.Key)
		}
		if current.Present && !owned && decision.Action == Update {
			return Plan{}, fmt.Errorf("seed: %s: %s/%s is an unowned natural-key row", entry.Record.Source, entry.Resource, entry.Record.Key)
		}
		// A create is a different grant from an update, and the check belongs to
		// the decision rather than to the write: a dry run that printed CREATE
		// for a caller who could not have created the row would be a plan that
		// lied, and the run it predicted would refuse. Plan and Apply therefore
		// ask the same question here, before either of them writes.
		if decision.Action == Create {
			if err := s.authorize.Check(ctx, tx, r, Create); err != nil {
				return Plan{}, fmt.Errorf("seed: %s: %w", entry.Record.Source, err)
			}
		}
		item := Item{Action: decision.Action, Resource: entry.Resource, Key: entry.Record.Key, Source: entry.Record.Source, Changed: decision.Changed, RecordID: current.ID}
		if apply {
			switch decision.Action {
			case Create:
				// The create grant was asked above, at the decision — the same
				// question, in the same transaction, that Plan asks.
				current, err = writer.Create(wctx, tx, target)
			case Update:
				current, err = writer.Update(wctx, tx, current, target)
			}
			if err != nil {
				return Plan{}, fmt.Errorf("seed: %s: %w", entry.Record.Source, err)
			}
			if decision.Action != Unchanged {
				if !current.Present {
					return Plan{}, fmt.Errorf("seed: %s: owner returned no row after %s", entry.Record.Source, decision.Action)
				}
				if err := putKey(tx, r, entry.Record.Key, entry.Kind, current.ID); err != nil {
					return Plan{}, fmt.Errorf("seed: %s: %w", entry.Record.Source, err)
				}
				item.RecordID = current.ID
			}
		}
		resolved[entry.Resource+"/"+entry.Record.Key] = current.ID
		plan.Items = append(plan.Items, item)
	}
	if err := s.prune(ctx, tx, documents, apply, &plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// existing answers the question a reference to a record outside these files
// asks: does this tenant already hold the row it names. It is the same two
// reads a record's own pass makes — the provenance mapping, then the owner's
// row by the ID that mapping holds or, for a resource with a natural key, by
// that key — because a target the seed wrote earlier is often known to the seed
// by nothing but its opaque key.
func (s *Service) existing(ctx context.Context, tx db.Tx[db.Tenant], alias, key string) (uuid.UUID, bool, error) {
	writer := s.writers[alias]
	if writer == nil {
		return uuid.Nil, false, nil
	}
	r := writer.Resource()
	name, _, err := lookupKey(tx, r, key)
	if err != nil {
		return uuid.Nil, false, err
	}
	row, err := writer.Read(ctx, tx, name, false)
	if err != nil {
		return uuid.Nil, false, err
	}
	if !row.Present {
		return uuid.Nil, false, nil
	}
	return row.ID, true, nil
}

// prune visits referring resources before their targets. Only mappings from a
// file that explicitly opts into pruning are candidates; an unowned row is
// never inferred from a missing YAML record.
func (s *Service) prune(ctx context.Context, tx db.Tx[db.Tenant], docs []Document, apply bool, plan *Plan) error {
	byAlias := make(map[string][]Document)
	for _, doc := range docs {
		if doc.Prune {
			byAlias[doc.Resource] = append(byAlias[doc.Resource], doc)
		}
	}
	if len(byAlias) == 0 {
		return nil
	}
	visited := make(map[string]bool)
	visiting := make(map[string]bool)
	var resources []string
	var visit func(string) error
	visit = func(alias string) error {
		if visiting[alias] {
			return fmt.Errorf("seed: resource reference cycle involving %s prevents prune", alias)
		}
		if visited[alias] {
			return nil
		}
		visiting[alias] = true
		for _, ref := range s.writers[alias].Resource().References {
			if ref.Target != alias && len(byAlias[ref.Target]) > 0 {
				if err := visit(ref.Target); err != nil {
					return err
				}
			}
		}
		visiting[alias], visited[alias] = false, true
		resources = append(resources, alias)
		return nil
	}
	for _, alias := range slices.Sorted(maps.Keys(byAlias)) {
		if err := visit(alias); err != nil {
			return err
		}
	}
	for i := len(resources) - 1; i >= 0; i-- {
		alias := resources[i]
		writer := s.writers[alias]
		resource := writer.Resource()
		for _, doc := range byAlias[alias] {
			keep := make(map[string]bool, len(doc.Records))
			for _, record := range doc.Records {
				keep[record.Key] = true
			}
			keys, err := ownedKeys(tx, resource, doc.Kind)
			if err != nil {
				return fmt.Errorf("seed: %s: %w", doc.Source, err)
			}
			// A prune is a write the file caused, so its event cites the file too.
			dctx := events.WithAttribution(ctx, seedAttribution(ctx, doc.Source))
			for _, key := range keys {
				if keep[key.Value] {
					continue
				}
				if err := s.authorize.Check(ctx, tx, resource, Prune); err != nil {
					return fmt.Errorf("seed: %s: %w", doc.Source, err)
				}
				row, err := writer.Read(dctx, tx, key, apply)
				if err != nil {
					return fmt.Errorf("seed: %s: %w", doc.Source, err)
				}
				if !row.Present {
					// House rule 8: the write that finds none is not refused.
					// Somebody already deleted the owner's row through the product;
					// what remains to do is forget the mapping, so the run stops
					// refusing this file forever the moment the record is dropped.
					if apply {
						if err := deleteKey(tx, resource, key.Value); err != nil {
							return fmt.Errorf("seed: %s: %w", doc.Source, err)
						}
					}
					plan.Items = append(plan.Items, Item{Action: Prune, Resource: alias, Key: key.Value, Source: doc.Source})
					continue
				}
				if apply {
					if err := writer.Delete(dctx, tx, row); err != nil {
						return fmt.Errorf("seed: %s: %w", doc.Source, err)
					}
					if err := deleteKey(tx, resource, key.Value); err != nil {
						return fmt.Errorf("seed: %s: %w", doc.Source, err)
					}
				}
				plan.Items = append(plan.Items, Item{Action: Prune, Resource: alias, Key: key.Value, Source: doc.Source, RecordID: row.ID})
			}
		}
	}
	return nil
}
