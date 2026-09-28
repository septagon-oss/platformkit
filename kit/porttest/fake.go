package porttest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Fake is what a module's fake embeds: the clock it stamps with, the grants a
// fixture hands out and the events it would have written. A module keeps its
// stores and its decisions, which is the whole of what makes its fake
// trustworthy — the rules are the module's own rule functions, called here.
type Fake struct {
	Clock  *Clock
	Grants *Grants
	Events *Recorder
}

// NewFake builds the plumbing: a clock reading at, an empty grant table and a
// recorder that refuses an event name the module does not declare.
func NewFake(at time.Time, declared []string) *Fake {
	return &Fake{
		Clock:  NewClock(at),
		Grants: &Grants{},
		Events: &Recorder{declared: slices.Clone(declared)},
	}
}

// Store is one kind of row, partitioned by the tenant on the context and handed
// out as a copy that shares no storage with the row it holds (`detached`), which
// is what a read gives a real command. A fake that shared one map across tenants
// would pass a cross-tenant read the database refuses, which is the opposite of
// what a fake is for.
//
// What it still cannot claim, and what every module's fake says in its own
// words: row-level security, the unique indexes, the append-only triggers and
// the row lock that settles two writers. Those are database facts, tested
// against the schema.
type Store[T any] struct {
	mu    sync.Mutex
	id    func(T) uuid.UUID
	rows  map[uuid.UUID]map[uuid.UUID]T
	order map[uuid.UUID][]uuid.UUID
}

// NewStore returns an empty store. id reads a row's own identifier.
func NewStore[T any](id func(T) uuid.UUID) *Store[T] {
	return &Store[T]{
		id:    id,
		rows:  map[uuid.UUID]map[uuid.UUID]T{},
		order: map[uuid.UUID][]uuid.UUID{},
	}
}

// tenantOf is the tenant every door of this store goes through. It panics where
// the context names none: every command of every module runs inside a tenant's
// transaction, and there is no door here that takes one as an argument.
func tenantOf(ctx context.Context) uuid.UUID {
	t, ok := tenancy.FromContext(ctx)
	if !ok {
		panic("porttest: a store was reached outside a tenant; every operation resolves an explicit tenant")
	}
	return t.ID
}

// Put writes a row into the tenant on the context, in insertion order. What the
// store keeps is a copy, so a fixture that goes on mutating the row it seeded
// moves nothing it holds.
func (s *Store[T]) Put(ctx context.Context, row T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.put(ctx, row)
}

func (s *Store[T]) put(ctx context.Context, row T) {
	row = detached(row)
	tenant, id := tenantOf(ctx), s.id(row)
	if s.rows[tenant] == nil {
		s.rows[tenant] = map[uuid.UUID]T{}
	}
	if _, held := s.rows[tenant][id]; !held {
		s.order[tenant] = append(s.order[tenant], id)
	}
	s.rows[tenant][id] = row
}

// Get is the row as the tenant on the context holds it, or crud.ErrNotFound. A
// row of another tenant is not in this tenant's store at all, so nothing here
// discloses that somebody else has one. The row that comes back is the tenant's
// row and nobody else's: writing through a slice, map or pointer field of it
// cannot reach the stored one, which is what keeps a command that was refused
// after it decided what to write from leaving the write behind.
func (s *Store[T]) Get(ctx context.Context, id uuid.UUID) (T, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(ctx, id)
}

func (s *Store[T]) get(ctx context.Context, id uuid.UUID) (T, error) {
	row, held := s.rows[tenantOf(ctx)][id]
	if !held {
		var zero T
		return zero, fmt.Errorf("%w: no such row", crud.ErrNotFound)
	}
	return detached(row), nil
}

// All is every row of this tenant in insertion order, each of them a copy on the
// same terms as Get's, so a fake never iterates a Go map and a list case cannot
// pass by luck.
func (s *Store[T]) All(ctx context.Context) []T {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant := tenantOf(ctx)
	out := make([]T, 0, len(s.order[tenant]))
	for _, id := range s.order[tenant] {
		if row, held := s.rows[tenant][id]; held {
			out = append(out, detached(row))
		}
	}
	return out
}

// Find is the first row this tenant holds that matches, in insertion order.
func (s *Store[T]) Find(ctx context.Context, match func(T) bool) (T, bool) {
	for _, row := range s.All(ctx) {
		if match(row) {
			return row, true
		}
	}
	var zero T
	return zero, false
}

// Delete removes a row from this tenant's store, and tells the witness when a
// case was watching that row: a refused call that took the row away from the
// tenant that owns it is what the tenant case exists to catch.
func (s *Store[T]) Delete(ctx context.Context, id uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant := tenantOf(ctx)
	delete(s.rows[tenant], id)
	s.order[tenant] = slices.DeleteFunc(s.order[tenant], func(held uuid.UUID) bool { return held == id })
	storeWitness.lost(id, tenant)
}

// rowWitness is the store's answer to the one question a tenant case cannot ask
// its own description.
//
// Provoke's documented job is to move the world into the state the refusal
// answers, and for the tenant case that state is another tenant's. A Snapshot
// taken through that world renders the row absent whether the refusal left it
// with its owner or destroyed it, so "a refused call wrote nothing" is written
// from the visitor's side of the refusal and the row's owner is not asked. The
// store is the one party in a fake the description cannot move: it holds the row
// for the tenant that owns it, and it can say whether it still does.
//
// A row is watched by the id the case minted for it, for as long as one refused
// call runs, and a loss is recorded only against a watched row. So the account
// holds nothing between cases, and no case reads another's: the ids are uuids one
// case made for itself. A world whose rows live where this Store cannot watch
// them — a real transaction — is not covered by this; its tenant case reads the
// row through the owner's own context, as this package's notes port does, or
// declines the case with a reason that says so.
type rowWitness struct {
	mu       sync.Mutex
	watching map[uuid.UUID]bool
	gone     map[uuid.UUID][]string
}

// storeWitness is the one account, because it belongs to the stores and the
// harness reads it between them; they never meet otherwise.
var storeWitness rowWitness

// watch starts one case watching rows and returns the reading that ends it: what
// the stores lost of them in the meantime, in the store's own words, or "" when
// it lost nothing of them.
func (w *rowWitness) watch(rows ...uuid.UUID) func() string {
	w.mu.Lock()
	if w.watching == nil {
		w.watching = map[uuid.UUID]bool{}
		w.gone = map[uuid.UUID][]string{}
	}
	for _, row := range rows {
		w.watching[row] = true
	}
	w.mu.Unlock()
	return func() string {
		w.mu.Lock()
		defer w.mu.Unlock()
		var out []string
		for _, row := range rows {
			out = append(out, w.gone[row]...)
			delete(w.gone, row)
			delete(w.watching, row)
		}
		return strings.Join(out, "; ")
	}
}

// lost is a store reporting that one tenant no longer holds a row. A row nobody
// is watching is the store's own business, which is what keeps the account empty.
func (w *rowWitness) lost(row, tenant uuid.UUID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.watching[row] {
		return
	}
	w.gone[row] = append(w.gone[row], fmt.Sprintf("row %s is gone from tenant %s's store", row, tenant))
}

// Seed puts a row in the store the way a create route would: it gives the row an
// id if it has none, stamps it from the fake's clock, runs the entity's own
// Validate and returns the id. It is the door a suite cannot reach through the
// port, because creating a row is kit/rest's five routes.
//
// It panics on a row Validate refuses: a fixture that seeds an invalid row is
// testing the fake's tolerance rather than the port, and the suite would go on
// to assert against a row the database would never hold.
//
// A revision is not stamped here. The kernel's Base carries none, so which field
// holds one is the module's own answer, and Command.Revision is where the module
// gives it.
func Seed[T any, PT interface {
	*T
	crud.Entity
}](ctx context.Context, f *Fake, s *Store[T], row T) uuid.UUID {
	stamped := PT(&row)
	base := entity.BaseOf(stamped)
	if base.ID == uuid.Nil {
		base.ID = uuid.New()
	}
	base.TenantID = tenantOf(ctx)
	at := f.Clock.Now()
	if base.CreatedAt.IsZero() {
		base.CreatedAt = at
	}
	base.UpdatedAt = at
	if v, ok := any(stamped).(crud.Validator); ok {
		if err := v.Validate(ctx); err != nil {
			panic(fmt.Sprintf("porttest: seeded an invalid %s: %v", stamped.TableName(), err))
		}
	}
	s.Put(ctx, row)
	return base.ID
}

// Clock is the fake's time: explicit, settable, UTC. The harness reads no wall
// clock of its own, so "a review goes stale" is a clock move a case makes.
type Clock struct {
	mu sync.Mutex
	at time.Time
}

// NewClock reads at, in UTC.
func NewClock(at time.Time) *Clock { return &Clock{at: at.UTC()} }

func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *Clock) Set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = at.UTC()
}

func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d).UTC()
}

// Grants is the actor-to-permission table a fixture fills in before a case acts.
type Grants struct {
	mu   sync.Mutex
	held map[uuid.UUID]map[string]bool
}

// Grant gives one actor permissions. It names the actor rather than a context,
// because a fixture sets its people up before any of them acts.
func (g *Grants) Grant(actor uuid.UUID, permissions ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.held == nil {
		g.held = map[uuid.UUID]map[string]bool{}
	}
	if g.held[actor] == nil {
		g.held[actor] = map[string]bool{}
	}
	for _, p := range permissions {
		g.held[actor][p] = true
	}
}

// Holds answers whether the actor on the context holds the permission. It is the
// recheck inside the command, not a wrapper the caller may forget. The module
// wraps its own denial sentinel around a false answer, because whose sentinel
// that is belongs to the module: one module denies with a contract of its own
// and another with tenancy.ErrPolicyDenied, and the kernel names neither.
func (g *Grants) Holds(ctx context.Context, permission string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.held[Actor(ctx)][permission]
}

// Actor is the principal on the context, or uuid.Nil for nobody at all. It reads
// the actor kit/httpx establishes, and the principal's own id where a fixture
// set that instead.
func Actor(ctx context.Context) uuid.UUID {
	if actor, ok := tenancy.ActorFrom(ctx); ok {
		return actor
	}
	if p, ok := tenancy.PrincipalFrom(ctx); ok {
		return p.UserID
	}
	return uuid.Nil
}

// Recorder is what a command said, in order.
type Recorder struct {
	mu       sync.Mutex
	declared []string
	names    []string
}

// Emit records an event, and refuses a name the module does not declare: an
// event a module never declared is a subscriber nobody wrote.
func (r *Recorder) Emit(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.declares(name) {
		return fmt.Errorf("porttest: %q is not one of the events this port declares (%v)", name, r.declared)
	}
	r.names = append(r.names, name)
	return nil
}

// declares reports whether the module named this event. The declared list is
// written once, when the fake is built, so it needs no lock of its own and Do
// can ask before it writes.
func (r *Recorder) declares(name string) bool { return slices.Contains(r.declared, name) }

// Names is the events recorded so far, in order, as a copy.
func (r *Recorder) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.names)
}

// Count is how many, which is what a refusal case reads before the call it means
// to be refused: an empty slice is nil, and a helper that returned early on nil
// would stop checking the first refusal in a fresh world.
func (r *Recorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.names)
}

// Command is the shape a single-row command takes in a fake that runs it
// through Do — all six of tasktest's and contenttest's commands. The order
// is Do's, so "a refused mutation writes nothing and emits nothing" is
// structural, not a discipline each fake keeps: Apply runs only after Decide.
//
// A command that gathers more than one row — a publication that reads a
// service, its figures, their reviews and a register — calls Grants.Holds,
// Store.Get, Clock.Now and Recorder.Emit directly, in that same order. None
// does here; sitetest.Save keeps its fake's own doors. A sequencing language
// would be the second language decision 0034 refuses.
type Command[T any] struct {
	// Permission the caller must hold, or "" for an anonymous operation.
	Permission string
	// Denied is the module's own sentinel for a caller who holds nothing.
	Denied error
	// Row is the id this command reads and writes.
	Row uuid.UUID
	// Expected is the revision the caller says it read, and Revision reads the
	// stored row's own. A nil Revision skips the check, for a row that has none
	// or a call that sends none.
	Expected int64
	Revision func(T) int64
	// Stale is the module's own refusal for a revision that has moved.
	Stale func(stored, expected int64) error
	// Decide is the module's rule over the row as stored; a non-nil answer is the
	// refusal, and nothing is written.
	Decide func(T) error
	// Apply writes the decision onto the row and answers with the events the
	// write says. It runs only after Decide accepted, and a call that changed
	// nothing answers no events, which is what makes the retry silent.
	Apply func(*T) []string
}

// Do runs one Command and answers with the row as it now stands: grant, load,
// revision, decide, and only then apply and emit. It holds the store for the
// whole command — the fake's stand-in for the row lock, not a claim about it.
func Do[T any](ctx context.Context, f *Fake, s *Store[T], c Command[T]) (T, error) {
	var zero T
	if c.Permission != "" && !f.Grants.Holds(ctx, c.Permission) {
		if c.Denied == nil {
			panic("porttest: a command that names a permission needs the module's own denial sentinel")
		}
		return zero, c.Denied
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	row, err := s.get(ctx, c.Row)
	if err != nil {
		return zero, err
	}
	if c.Revision != nil {
		if stored := c.Revision(row); stored != c.Expected {
			if c.Stale == nil {
				panic("porttest: a command that checks a revision needs the module's own refusal for one that moved")
			}
			return zero, c.Stale(stored, c.Expected)
		}
	}
	if c.Decide != nil {
		if err := c.Decide(row); err != nil {
			return zero, err
		}
	}
	events := c.Apply(&row)
	// The names are checked before the write, so an undeclared event cannot
	// leave the row written and the event unsaid: domain state and what the
	// command says commit together or neither does. `row` came out of `get`, which
	// hands out a `detached` copy, so what Apply wrote is this command's storage
	// and not the stored row's — the half a `bool` field cannot show, since a
	// slice, map or pointer field is a header over storage the row shares.
	for _, name := range events {
		if !f.Events.declares(name) {
			return zero, fmt.Errorf("porttest: %q is not one of the events this port declares (%v)", name, f.Events.declared)
		}
	}
	s.put(ctx, row)
	for _, name := range events {
		if err := f.Events.Emit(name); err != nil {
			return zero, err
		}
	}
	return row, nil
}
