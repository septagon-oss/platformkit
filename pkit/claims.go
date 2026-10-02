package pkit

// claims.go is the process's record of which application runs on which database,
// and the refusal that keeps it that way round.
//
// `Server.Host` already refuses a process that hosts two applications, and
// `Server.Explain` refuses to write a composition file for one. Both answers are
// read off one `Server` value, which is the whole of their scope: a second
// `Server` value, or a bare `App.Build`, is a second boot the first one never
// heard about, and the two databases they name can be the same database. Two
// compositions over one database is the arrangement kit/app's Runtime.Work says it
// does not support — `events.Relay` claims any unpublished outbox row and stamps
// what it publishes, so one composition can move another's event onto a transport
// nobody subscribes to, and two schedulers over one database silence each other's
// same-named job through the advisory lock — and the review that found it reached
// the same database through a second `pkit.Server` value.
//
// So the claim is recorded per process and keyed by database, because one process
// composes one application per database, and the claim carries that application's
// composition: a second boot with the held name and a different list of modules is
// a different application wearing it, and the events the first one declared are the
// first one's contract rather than the name's. The event catalog answers the other
// half, beside the database rather than over it: it is the union of what the live
// compositions declared (kit/events DeclareMore), a boot neither replaces a shape
// another application is answering under nor takes one away on its way out, and the
// Close of the Runtime that took a declaration gives it back. Refusing the one
// composition per database is what keeps that union from being asked to paper over
// two compositions writing one outbox.
//
// The claim is taken before the first effect, beside the other answers about the
// composition, and released by the release of the lifecycle that took it — a boot
// refused above the connection, a Runtime Closed, a Run that returned. A process
// that holds nothing on a database claims it again freely, which is what lets a
// deployment restart into a different composition once the live one is down.
//
// What this check is not: a lock across processes, and not a reading of what the
// database already holds. Two OS processes pointed at one database each carry
// their own record, and one database spelled two ways is one database to Postgres
// and two keys here. The cross-process and cross-spelling answer is T-0231's — the
// task that puts the app's name in every name two apps share and the check at
// every boundary — and the refusal says so, because an operator who reads "one
// application per database" in one process and has two databases is being told
// about the wrong thing.

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/septagon-oss/platformkit/kit/module"
)

// databaseClaims is the record: which application holds which database in this
// process, and how many of that application's lifecycles are in the hold. A
// database is addressed rather than invented, so the map is keyed by the
// configuration's own database address rather than by anything this package
// generates. The count is what lets one application hold one database from two
// Server values — the arrangement a rolling restart is — without either release
// taking the hold away from the other.
var databaseClaims = struct {
	mu    sync.Mutex
	byKey map[string]*holding
}{byKey: map[string]*holding{}}

// holding is one application's grip on one database: which application, the
// composition it holds the database with, and how many of that application's
// lifecycles are in the grip.
type holding struct {
	app         string
	composition string
	lifecycles  int
}

// compositionSignature is the module list one grip holds, spelled the way the
// refusal below prints it. One application's own second lifecycle repeats it
// exactly; a restart that means to change the composition repeats the name only,
// which is the difference this record makes observable.
func compositionSignature(built []module.Module) string {
	names := make([]string, len(built))
	for i, m := range built {
		names[i] = m.Name
	}
	return strings.Join(names, ", ")
}

// held is one lifecycle's receipt for one hold: whose hold it took, at which
// address, and whether it has been given back. release is safe to call twice and
// from two goroutines: a boot that fails after taking the hold and the Close of
// the Runtime that never happened must not both drop a second lifecycle's grip.
type held struct {
	grip *holding
	at   string
	once sync.Once
}

// release drops this lifecycle's grip, and the hold when no lifecycle is in it.
func (h *held) release() {
	if h == nil {
		return
	}
	h.once.Do(func() {
		databaseClaims.mu.Lock()
		defer databaseClaims.mu.Unlock()
		if h.grip.lifecycles--; h.grip.lifecycles <= 0 {
			// Only drop the entry while it is still this application's: a release
			// that arrives late must not delete a claim a later boot took.
			if databaseClaims.byKey[h.at] == h.grip {
				delete(databaseClaims.byKey, h.at)
			}
		}
	})
}

// claimDatabase records that this application is starting, with this composition,
// on the database this deployment names, and refuses the start when another
// application holds it or when the held name comes back with a different
// composition. Two lifecycles of the application already holding it, composing the
// same modules, is the one arrangement recorded without a word: one image under two
// roles is what decision 0005 says a deployment runs. Each refusal names the
// application it refuses and the one already standing, and the second names the two
// module lists, which is the whole answer, so it comes back alone.
func claimDatabase(d Deployment, app string, built []module.Module) (*held, error) {
	key := databaseKey(d.Config.Database.URL)
	composition := compositionSignature(built)
	databaseClaims.mu.Lock()
	defer databaseClaims.mu.Unlock()
	prior, taken := databaseClaims.byKey[key]
	switch {
	case taken && prior.app != app:
		return nil, fmt.Errorf("pkit: %s: Build: this process already runs %s on this configuration's database, so a second application on it is refused before it opens anything; one process composes one application per database, and many applications over one database wait for T-0231, which puts the app's name in every name two apps share and the check at every boundary (0074 rule 6)",
			app, prior.app)
	case taken && prior.composition != composition:
		return nil, fmt.Errorf("pkit: %s: Build: this process already runs %s on this configuration's database with another composition — that one composes %s, this one composes %s — so the repeated name is a second application wearing it and is refused before anything opens; one process composes one composition per database, and a deployment that means to change the composition closes the lifecycle holding it first",
			app, prior.app, prior.composition, composition)
	case taken:
		prior.lifecycles++
	default:
		prior = &holding{app: app, composition: composition, lifecycles: 1}
		databaseClaims.byKey[key] = prior
	}
	return &held{grip: prior, at: key}, nil
}

// databaseKey is the address the configuration names this database at, spelled
// the one way the record holds it. For a URL the credentials come off — a
// password says nothing about which database this is and has no business in a key
// a debugging dump could print — the host is lowered and the parameters sorted,
// so one database spelled in either order callers write is one key. A DSN in the
// other PostgreSQL spelling (`host=… dbname=…`) is kept as it came: this package
// does not parse that dialect, and inventing a reader for it would be a second
// way to be wrong about an address. What no key is: a resolution. Two routes to
// one database — a schema in `search_path` rather than in its name — are two keys.
func databaseKey(u string) string {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" {
		return u
	}
	parsed.User = nil
	query := parsed.Query()
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	spelled := strings.ToLower(parsed.Host) + parsed.Path
	for _, k := range keys {
		values := append([]string(nil), query[k]...)
		sort.Strings(values)
		for _, v := range values {
			if strings.Contains(spelled, "?") {
				spelled += "&"
			} else {
				spelled += "?"
			}
			spelled += k + "=" + v
		}
	}
	return spelled
}
