// Package appname is the one place a name two apps could share is formed, and
// the type that says which app a name belongs to.
//
// A server hosts many apps over one database and one broker (decision 0074), so
// every name the kernel derives — where an event is published, which consumer
// owns it, which job lock it takes, which cookie a browser sends back, which
// rate-limit bucket a write spends, where a stored file's bytes sit — is a name
// more than one app wants for itself. Tenant-scoped is not enough any more:
// inside one app the tenant is the boundary, and between two apps it is only a
// label, because tenant ids and module names are the same vocabulary on both
// sides. A name alone is a convention, so each of these constructors comes with
// a boundary that reads the app back (transport.AddressMismatch, delivery's
// tenant check, the relay's claim, tenant lookup by host); the constructors here
// make the boundary expressible.
//
// # The rule
//
// Every shared name is formed by a function in this package, from a validated
// Name. A census test (census_test.go) scans the tree for shared names formed
// any other way and names the site it found, so a shared name cannot be spelled
// inline in a caller — the same argument that puts an event's address in
// transport.Subject and nowhere else.
//
// # The shape of the name
//
// The app segment is the client's slug, the one that names its directory in
// clients/<slug>/ and its subjects on the broker: collect, academy, shelf. Parse
// refuses anything a subject token, a consumer name, a cookie name and a path
// segment could not all hold: lower-case alphanumerics joined with single
// dashes, which is a DNS label's grammar because every one of those carriers is
// a DNS label or maps onto one.
//
// # What stays shared, and why
//
// Four names are deliberately not app-scoped, each for a reason a reader can
// check in one line:
//
//   - the PLATFORMKIT stream (kit/events/providers/nats/jetstream.go), because
//     its subjects are app-scoped, so one stream carries many apps without one
//     app ever filtering another's tokens; a stream per app would trade a shared
//     name for a per-app limit an operator has to raise;
//   - the dead-letter table, because every row carries a durable, and a durable
//     carries the app, so the rows are already separated by the only column that
//     queries them;
//   - the composition migration lock and the migration owners
//     (kit/db/migrate.go, kit/app/migrations.go), because they guard one schema
//     that the apps share; scoping them would let two apps migrate it at once;
//   - CSRF (kit/httpx/csrf.go), because it checks origins and holds no token, so
//     there is no name for two apps to collide over.
//
// The process's telemetry service.name stays one name too: one process hosts
// many apps, and the app belongs on each record as an attribute (see App), not
// in the name of the process.
package appname

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// Prefix is the one namespace every PlatformKit name of this kind sits under:
// the first token of an event subject, and the word every shared address begins
// with. A deployment that wants its own prefix gets it by renaming the stream,
// not by editing this.
const Prefix = "platformkit"

// Name is an app's slug: the one segment every name two apps could share carries
// so that one app can never receive another's work. It is a validated value
// rather than a string because the alternative is a name formed from a
// configuration value nobody parsed, reaching a broker subject or a cookie name
// with a dot, a slash or an empty segment in it.
type Name string

// slug is the grammar: a lower-case letter, then lower-case alphanumerics, with
// single dashes as separators, at most 32 characters. Thirty-two is the longest
// DNS label that leaves room for the rest of a __Host- cookie name and the
// longest JetStream consumer name prefix; the slug is a hostname segment today,
// so the bound is one the deployment already satisfies.
const slug = `^[a-z][a-z0-9]*(-[a-z0-9]+)*$`

var slugPattern = regexp.MustCompile(slug)

// Parse validates a slug read from configuration, a manifest or a wire.
//
// The refusal names what a valid one looks like rather than only the bad value:
// the reader is an operator who has to fix a configuration file, and the grammar
// in the sentence is the fix. No client's own slug is quoted here: this is shared
// code, and a customer's name in a kernel error is a fact about one deployment in
// everybody's binary.
func Parse(value string) (Name, error) {
	if !slugPattern.MatchString(value) || len(value) > 32 {
		return "", fmt.Errorf("appname: %q is not an app name: a lower-case slug of at most 32 characters — letters, digits and single dashes, starting with a letter, like \"your-app-slug\"", value)
	}
	return Name(value), nil
}

// MustParse is Parse for a name written in source, where a typo is a bug and the
// moment to find it is at boot. Composition uses it; nothing reads it from input.
func MustParse(value string) Name {
	n, err := Parse(value)
	if err != nil {
		panic(err)
	}
	return n
}

// String lets a Name reach a format string, a map key and a log attribute as the
// slug it is.
func (n Name) String() string { return string(n) }

// Valid reports whether n holds a usable slug. A Name built by conversion
// bypasses Parse, so every boundary asks rather than trusting the type.
func (n Name) Valid() bool { return slugPattern.MatchString(string(n)) && len(n) <= 32 }

// App is the log, audit, metric and span attribute that names the app a record
// belongs to. One name, spelled once, so a query joining a request's log line to
// the event it caused to the job that retried it joins on one key.
const App = "app"

// Subject is the address one event is published at:
//
//	platformkit.<app>.<tenant>.<module>.<event>
//
// The app token comes before the tenant because the two wildcards a consumer
// needs are both suffix wildcards in a NATS subject: an app's subscribers filter
// the app exactly and the tenant loosely, and no filter could say "this tenant,
// any app" — which is right, because a subscription always belongs to one app.
func Subject(app Name, tenant uuid.UUID, name string) string {
	if !app.Named() {
		return PreviousSubject(tenant, name)
	}
	return Prefix + "." + app.token() + "." + tenant.String() + "." + name
}

// PreviousSubject is the address the build before the app token published at:
// platformkit.<tenant>.<module>.<event>. Nothing here writes it — see Filters
// for who does, and for what closes the window.
func PreviousSubject(tenant uuid.UUID, name string) string {
	return Prefix + "." + tenant.String() + "." + name
}

// OldestSubject is the address before the tenant token, three tokens:
// platformkit.<module>.<event>. It names no tenant, which is why an event
// arriving on it contradicts nothing about which tenant it belongs to.
func OldestSubject(name string) string { return Prefix + "." + name }

// Filter is the wildcard a subscription to one event name uses: that event, in
// every tenant of this app, and no other app's.
func Filter(app Name, name string) string {
	if !app.Named() {
		return PreviousFilter(name)
	}
	return Prefix + "." + app.token() + ".*." + name
}

// PreviousFilter is the wildcard the build before the app token filtered:
// platformkit.*.<module>.<event>, which matches every app's delivery of that
// event. It is in Filters for one rollout and no longer than that, and what makes
// it safe to read at all is transport.AddressMismatch: a message on this address
// names no app, so an app that names itself refuses it and only the deployment of
// one app — which has no app token to agree with — answers it. The filter's reach
// is wider than the app and the boundary is not.
func PreviousFilter(name string) string { return Prefix + ".*." + name }

// Filters is every address a subscription to one event name has to answer, newest
// first: this build's, the previous build's, the build before that one's. A NATS
// `*` matches exactly one token and a stream subject transform cannot be added to
// a stream that already exists, so a rolling window needs several filters and not
// a cleverer one.
//
// The window closes when no process still running an older build publishes: delete
// the two entries after PreviousFilter from this list and remake every consumer
// without them. It is the same window transport's comment describes, moved here
// where the addresses are written once.
func Filters(app Name, name string) []string {
	if !app.Named() {
		return []string{PreviousFilter(name), OldestSubject(name)}
	}
	return []string{Filter(app, name), PreviousFilter(name), OldestSubject(name)}
}

// Space is the address space one app publishes into: the namespace, then the
// app's own token and the separator, ready for a tenant and an event name — or for
// a placeholder in a published document, which is what kit/app's AsyncAPI document
// spells to describe the addresses without inventing an address of its own.
func Space(app Name) string {
	if !app.Named() {
		return Prefix + "."
	}
	return Prefix + "." + app.token() + "."
}

// SubjectSpace is the stream's subject space: every address a PlatformKit event
// travels on, whichever app's it is. One namespace, so one stream carries every
// app's traffic and an operator's `nats stream report` still means one thing.
func SubjectSpace() string { return Prefix + ".>" }

// Durable names one subscription on a transport: <app>+<module>-<event>.
//
// Two rules shape it. A durable consumer name may not hold a dot (JetStream's
// own rule) while the subject it filters is made of dots, so the event half is a
// transliteration of the name and not the name — which is why renaming a durable
// needs the migration that copies the handled ledger and rewrites the dead
// letters with it, or a renamed durable sees every redelivered event as first.
//
// And the app half stays separable, because the durable is the one name that
// says which app owns a consumer: it is the JetStream consumer name on the single
// PLATFORMKIT stream, the deliver group every replica of that app joins, and half
// the primary key of platformkit_handled and platformkit_dead_letters. Joining
// with a dash would not survive the deployment this rule exists for: a dash is
// legal inside an app slug ("acme-billing" parses), so app "acme" with module
// "billing" and event "billing.plan.created" would answer to the same consumer as
// app "acme-billing" with module "billing" and event "plan.created" — one
// consumer, one queue group, and one app load-balancing another app's tenants'
// events into its own handlers. The plus sign is the app's join because it is in
// none of the three grammars an app name, a module name or an event name is written
// in (kit/appname's slug is [a-z0-9-], kit/module's moduleName is [a-z0-9_],
// transport's eventName adds only dots) and JetStream still accepts it in a consumer
// name. One plus sign therefore ends the app, and the first dash after it ends the
// module — a module name holds no dash either, so the two halves behind the app stay
// as separable as they always were.
//
// The join behind the app is a dash rather than a second plus for one reason beyond
// looks: it is the join this name carried before the app segment existed, so the
// scoped durable is the unscoped one with "<app>+" in front of it. That is what
// makes the move of the two ledgers expressible at all, because a row of
// platformkit_handled holds a durable and nothing else — no module, no event name,
// and for an event the purge has taken no outbox row left to ask — so the only
// rename that can be written against the table is a prefix. It is not the reason the
// move is absent: kit/appname/README.md, *Limits*, names that, and
// TestADurableCarriesNoDot is the case that holds the prefix property whatever
// eventually performs the move has to be able to rely on.
func Durable(app Name, module, event string) string {
	return DurablePrefix(app) + module + eventJoin + strings.ReplaceAll(event, ".", eventJoin)
}

// DurablePrefix is the whole of what an app adds to a durable: its token and the
// join, and the empty string for a deployment that names no app. It is the same
// string Durable puts in front of the unscoped name, which is why it is a function
// in this package and not a concatenation in a caller: the one rename of the two
// delivery ledgers that can be written against platformkit_handled — whose rows
// hold a durable and nothing else — is a prefix rename, and it has to be formed from
// the same two characters Durable uses or the move renames rows onto names no
// consumer has. kit/events/ledger.go is that caller.
func DurablePrefix(app Name) string {
	if !app.Named() {
		return ""
	}
	return app.token() + appJoin
}

// appJoin separates the app from the rest of a durable, and eventJoin
// transliterates the dots of an event name. '+' is in none of the three grammars
// above, which is the whole separability argument for the app half; '-' is what no
// module name holds, which keeps the module and the event apart and makes the
// scoped name the unscoped one with a prefix on it — the fact any move of the two
// ledgers has to be built on, and the one TestADurableCarriesNoDot checks.
const (
	appJoin   = "+"
	eventJoin = "-"
)

// JobLock names the advisory lock one periodic job takes. Two apps each running
// a job the module named the same way must both run, so the lock name carries the
// app; without it one app's job silences the other's on every replica.
func JobLock(app Name, job string) string {
	if !app.Named() {
		return "job:" + job
	}
	return app.token() + "/job:" + job
}

// Cookie is the name a first-party cookie is set under.
//
// __Host- is a rule the browser enforces — accepted only with Secure, Path=/ and
// no Domain — and it is dropped when the cookie is not Secure, because a browser
// refuses one over http://localhost. The app segment goes into both branches:
// two apps served on two hosts of one deployment with the same cookie base are
// one __Host-session cookie apart from signing each other out, and over http://
// one localhost port the two apps share the cookie jar outright.
func Cookie(app Name, base string, secure bool) string {
	if !app.Named() {
		// No slug set is the single-app deployment, which is the deployment every
		// cookie jar in the field already belongs to: it gets the name that jar
		// already carries rather than a new one nobody asked for.
		if secure {
			return "__Host-" + base
		}
		return base
	}
	if secure {
		return "__Host-" + app.token() + "-" + base
	}
	return app.token() + "-" + base
}

// PreviousCookies is every spelling the build before the app token set a cookie
// base under, prefixed and plain, oldest first: what a jar from before the upgrade
// still carries, and what a deployment still reads and never writes.
func PreviousCookies(base string) []string {
	return []string{"__Host-" + base, base}
}

// RateLimitKey is a limit key as it is stored: the app, then the tenant, then the
// caller's own key. Both fixed-length identifiers come before the caller's text,
// so no caller's key can forge another app's or tenant's prefix, and a bucket an
// operator reads names both owners rather than one of them.
func RateLimitKey(app Name, tenant uuid.UUID, key string) string {
	if !app.Named() {
		return tenant.String() + "/" + key
	}
	return app.token() + "/" + tenant.String() + "/" + key
}

// CacheKey is a cache entry's key: <app>:<tenant>:<key>. The separator is a colon
// and not a slash because a cache key is often a path elsewhere, and an entry
// whose first segment is a slug cannot be mistaken for one.
func CacheKey(app Name, tenant uuid.UUID, key string) string {
	return app.token() + ":" + tenant.String() + ":" + key
}

// StoragePath is where a stored file's bytes sit inside an adapter's root:
// <app>/<tenant>/<key>. The persisted key stays the caller's UUID and the tenant
// stays the row's — this is one adapter's physical layout, and it names both
// owners so two apps sharing a volume or a bucket never write one path.
// With no slug set the layout is the one a deployment of one app already holds:
// the tenant's own directory, with the key's first two characters as a fan-out
// inside it, because a directory with a million entries is slow in every
// filesystem worth naming and an operator's `mv` of bytes already written is a
// boot's step and not this function's.
func StoragePath(app Name, tenant, key uuid.UUID) string {
	if !app.Named() {
		shard := key.String()[:2]
		return tenant.String() + "/" + shard + "/" + key.String()
	}
	return app.token() + "/" + tenant.String() + "/" + key.String()
}

// PreviousStoragePath is where a release before the port carried a scope wrote
// these bytes: the key's own two-character fan-out under the adapter's root, with
// no tenant and no app above it. An adapter reads it so an installation's existing
// uploads keep working; nothing writes there, which is why an app that names
// itself and an app that does not share the name and share the bytes.
func PreviousStoragePath(key uuid.UUID) string {
	return key.String()[:2] + "/" + key.String()
}

// Source is the CloudEvents `source` of an event: the app and the module that
// emitted it, as a path. A bridge that receives documents from several apps on
// one broker reads this to tell them apart without opening the payload; the
// subject already said it, and the envelope says it again for a consumer that
// sees only the document.
func Source(app Name, module string) string {
	if !app.Named() {
		return "/" + module
	}
	return "/" + app.token() + "/" + module
}

// ConnectionName names one process's broker connection the way an operator reads
// it in connectionz: the process, then every app it hosts. An operator deciding
// which worker to restart reads a name like platformkit-worker/collect+academy
// and knows what will stop; "platformkit" answers nothing.
func ConnectionName(process string, apps ...Name) string {
	if process == "" {
		process = Prefix
	}
	if len(apps) == 0 {
		return process
	}
	tokens := make([]string, 0, len(apps))
	for _, a := range apps {
		if a.Named() {
			tokens = append(tokens, a.token())
		}
	}
	if len(tokens) == 0 {
		return process
	}
	return process + "/" + strings.Join(tokens, "+")
}

// token is the slug as it enters a name. Every constructor runs the slug through
// it, so a Name built by conversion that is not a slug is refused rather than
// published as a broker wildcard: an empty segment would collapse two tokens of
// an address into one, and a dot would let an app name filter another app's
// subjects. Refusing yields the nil slug, which no address of any app collides
// with, and every boundary that reads an app back refuses it.
func (n Name) token() string {
	if !n.Valid() {
		return ""
	}
	return string(n)
}

// Named says this Name names an app. The zero Name is the deployment that hosts
// one app and names no slug: every constructor answers with the name that
// deployment already uses, so the app segment appears exactly when a second app
// could share the name. A non-empty Name that is not a slug is the other case — a
// name that was set and is broken — and token refuses it.
//
// A boundary that reads the app back asks this before it compares, because the
// two answers are different rules and not one rule with an empty side: an app
// that names itself has an address to be compared with, and one that names
// nothing has only the addresses this kernel formed before decision 0074.
func (n Name) Named() bool { return string(n) != "" }
