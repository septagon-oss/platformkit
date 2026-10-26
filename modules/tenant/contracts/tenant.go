// Package contracts is everything another module, an app or a test may know
// about tenants: the entity, the events, the permission and the Service
// interface. The implementation is in ../internal.
//
// A tenant is the unit of isolation: one customer, one or more hosts, and every
// row in every other table belongs to exactly one. That makes this module the
// control plane rather than a business module — the kernel resolves a request's
// host through it before the request is about anything, and the periodic jobs
// walk its list.
package contracts

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The two states, which are both about one question: is this tenant served?
//
// A suspended tenant keeps its rows and stops being served: its hosts resolve
// to nothing, which reads from outside as "no site here". Whether a tenant
// still *is* one is a different fact and it has its own column —
// `deleted_at`, on the row — which `Delete` writes and nothing here clears:
// only a restore does that, and a restore is the inverse half of an export, not
// a state. There is no third `Status`, because "archived" and "deleted" are one
// fact with different retention, and retention is a job, not a lifecycle.
//
// The two axes are read together and never merged: `Get`, `List`, `ByHost` and
// the partial index in migrations/000006 all filter on `deleted_at IS NULL`, so
// a deleted tenant is invisible everywhere a suspended one is merely refused.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

// Tenant is one customer of the platform.
//
// It does not embed crud.Base, and that is the whole shape of this module. Base
// contributes a tenant_id column and row-level security matches on it; a tenant
// has no tenant to belong to. So the entity is a plain struct, there is no
// rest.Spec, and the thirteen routes are written by hand in internal/ — which
// is what an exception to a generic mechanism should cost.
type Tenant struct {
	ID     uuid.UUID `json:"id"`
	Slug   string    `json:"slug"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
	// Operator says this is the installation's own tenant: the one whose
	// administrators may reach the control plane at all. Exactly one row has it,
	// the one `platformkit bootstrap` created, and no route writes it — see
	// NewTenant.
	Operator bool `json:"operator"`
	// App is the composition this tenant belongs to, in the slug kit/appname forms
	// every shared name from. It is a column and it is written once, at the create,
	// from the app the composition boots as — never from a request, and never
	// rewritten, because a tenant that moved between apps would leave its rows under
	// an app that never issued them.
	//
	// It is not in the API document. The app is a fact about the deployment, and
	// every read that returns this row is already scoped to one app, so the label
	// would say the same thing on every row of a response and name the deployment to
	// whoever asked.
	App string `json:"-"`
	// Demo says this tenant was created as a walkthrough tenant, so the
	// application's demo seed records are its own. `platformkit bootstrap` never
	// sets it and no command writes the column afterwards: the seed refuses a
	// demo request for a tenant whose row says false, so a request cannot make
	// itself one — see docs/seed.md.
	Demo bool `json:"demo"`
	// Hosts are the names this tenant is served at, the primary one first. They
	// live in their own table and are loaded with the tenant, because a tenant
	// without its hosts is a row nobody can reach and an admin screen that
	// cannot say so.
	//
	// The order is the whole of what "primary" is for. A tenant may answer at
	// several names and every absolute URL — a mailed set-password link, a
	// notice somebody clicks from outside — has to be built on one of them.
	// Before migrations/000020 the one chosen was whichever sorted first, so
	// adding admin.acme.example.com silently moved every future link off
	// acme.example.com and nobody chose that.
	Hosts []string `json:"hosts" gorm:"-"`
	// DefaultLocale is the language this tenant's pages answer in when the request
	// brought nothing its tenant serves — the browser's own list intersected with
	// Locales, and this when that intersection is empty. It is a column and not a
	// position in Locales because the loader reads it on every request and a
	// position is a fact that moves when somebody reorders a list.
	DefaultLocale string `json:"defaultLocale"`
	// Locales are the languages this tenant is served in, besides the default.
	// They are a set and not a ranking — the one position in this pair that means
	// anything is DefaultLocale — and they live in their own table for the same
	// reason Hosts do, loaded with the tenant for the same reason: a tenant that
	// cannot say what it speaks is a page that guesses.
	Locales []string `json:"locales" gorm:"-"`
	// LocaleRevision counts the declarations made about this tenant's languages,
	// from either door. json:"-": it is not copy and not a field a caller writes
	// through a body — it is what SetOwnLocale compares an expected revision
	// against, and a number a client could post at would be a number it could pick.
	LocaleRevision int64      `json:"-"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	DeletedAt      *time.Time `json:"-"`
}

// TableName pins the table, so the entity and migrations/000006 agree.
func (Tenant) TableName() string { return "tenants" }

// Tenancy is this tenant as the kernel knows it: the identity kit/tenancy carries
// on a context, and the languages a request that resolved it may be answered in.
// The conversion is here so that no other package decides which of these fields the
// kernel gets, and so that a tenant which declared nothing reaches the negotiation
// with no languages rather than with a list holding an empty string — the
// distinction a page reads as "the deployment's own default answers here".
func (t *Tenant) Tenancy() tenancy.Tenant {
	kernel := tenancy.Tenant{ID: t.ID, Slug: t.Slug, Name: t.Name, Operator: t.Operator, Demo: t.Demo}
	if t.DefaultLocale != "" || len(t.Locales) > 0 {
		kernel.Languages = &tenancy.Languages{Default: t.DefaultLocale, Others: t.Locales}
	}
	return kernel
}

// NewTenant is what creating one takes: a slug, a name, and the first host it
// answers at. The host is not optional, because a tenant nothing routes to
// cannot be signed into, and a create that leaves an installation unreachable
// is a create that failed quietly.
type NewTenant struct {
	Slug string `json:"slug" minLength:"2" maxLength:"63" doc:"URL-safe short name, unique across the installation" example:"acme"`
	Name string `json:"name" minLength:"1" maxLength:"200" doc:"Display name" example:"Acme Corporation"`
	Host string `json:"host" minLength:"1" maxLength:"253" doc:"The first host this tenant is served at, which is its primary one" example:"acme.example.com"`

	// Operator marks the installation's own tenant, and it is json:"-": it is
	// in no request body, in no OpenAPI schema and in no generated form, so the
	// create route cannot be asked for one however the body is written. The one
	// writer is Bootstrap, which is the one write in the application with no
	// caller to authorize and can only ever happen once.
	Operator bool `json:"-"`
	// Demo says the tenant being created is a walkthrough tenant. Unlike
	// Operator it is in the body, because the create route is already guarded by
	// the operator permission and creating a demo tenant is the ordinary way to
	// get one; the flag names which seed records the tenant may be given, never
	// who may reach the control plane. It is written here or not at all — see
	// migrations/000050_tenant_demo.up.sql.
	Demo bool `json:"demo" required:"false" default:"false"`
}

// slugPattern is a DNS label: what a slug has to be if it is ever going to be a
// subdomain, which is what a slug is for.
var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// hostPattern is a hostname: labels of letters, digits and hyphens, separated
// by dots. It is deliberately narrower than the DNS allows — no underscores, no
// trailing dot, no port — because kit/httpx has already normalised the incoming
// Host header to exactly this shape before it asks the loader. A port is not the
// key a tenant is resolved by, so a published installation declares its public
// port in server.public_host, the key a mailed link reads a port from.
var hostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

// ValidSlug normalises and checks a slug.
func ValidSlug(slug string) (string, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !slugPattern.MatchString(slug) {
		return "", fmt.Errorf("slug %q is not a DNS label: lower-case letters, digits and hyphens, 1 to 63 of them", slug)
	}
	return slug, nil
}

// ValidHost normalises and checks a host. Normalisation is kit/httpx's, because
// the string stored here is compared against the one the middleware derives
// from a Host header, and two normalisations that disagree is a domain that
// resolves for nobody.
func ValidHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" || len(host) > 253 || !hostPattern.MatchString(strings.ToLower(host)) {
		return "", fmt.Errorf("host %q is not a hostname", host)
	}
	return host, nil
}

// localePattern is a BCP 47 language tag in the shape a language list is written
// in: a primary subtag, then script, region and variant subtags of two to eight
// characters each. It deliberately accepts more than the deployment has catalogues
// for — which languages exist is the catalogues' business — and rejects the shapes
// that are not a tag at all, because a locale stored badly is a preference that
// never matches and a page that answers in the wrong language forever.
var localePattern = regexp.MustCompile(`^[a-zA-Z]{2,3}(-[a-zA-Z0-9]{2,8})*$`)

// ValidLocale normalises one language tag to the form a negotiation compares it
// in: lower-case language, Title-Case script, UPPER-CASE region, hyphens between.
//
// The case is not cosmetics. What a tenant stores is matched against what
// `golang.org/x/text` spells a selected language as — `pt-PT`, not `pt-pt` — and
// two spellings of one tag is a preference that never matches, which is the
// quietest possible failure: the page answers, just not in the language anybody
// chose.
//
// It is not a list of the languages the installation speaks, and it must not become
// one: a kernel module that knew which languages exist could not serve a deployment
// that ships a catalogue it never imagined.
func ValidLocale(tag string) (string, error) {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if !localePattern.MatchString(tag) {
		return "", fmt.Errorf("locale %q is not a language tag: a two or three letter language, then optional subtags of two to eight characters, joined by hyphens", tag)
	}
	parts := strings.Split(tag, "-")
	for i, part := range parts {
		switch {
		case i == 0: // the language itself, which is lower-case in every spelling
		case len(part) == 4: // a script: Latn, Cyrl
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		case len(part) == 2, len(part) == 3 && part[0] >= '0' && part[0] <= '9': // a region: PT, US, 419
			parts[i] = strings.ToUpper(part)
		}
	}
	return strings.Join(parts, "-"), nil
}

// CodeLanguageUnusable names the refusal of a language this installation has no
// copy for, and CodeLanguageTag the refusal of a string that is not a language tag
// at all. They ride in front of the prose a refusal is made of, in the shape
// kit/httpx's own refusals take (`AUTH_DENIED: …`), because that prose is a sentence
// in the language this module was written in and a page that has to show the refusal
// in another one has nothing to look its own sentence up with but a name.
const (
	CodeLanguageUnusable = "LANGUAGE_UNUSABLE"
	CodeLanguageTag      = "LANGUAGE_TAG"
)

// LocaleRevision counts the language declarations one tenant has had, which is the
// number a declaration brings back to say what it was read at.
//
// It is a type with its own JSON codec rather than a plain 64-bit member for one
// reason: kit/httpx documents a request body that holds a 64-bit integer member as
// a derived component of its own, and the body this member belongs to — SetLocale —
// is a component the released document already names and a client already parses.
// The number of one tenant's language saves is nowhere near the range where a
// caller can lose a digit, so the published member stays an ordinary JSON integer.
type LocaleRevision int64

// UnmarshalJSON binds one revision counter from a JSON integer token. A quoted
// number, a fraction, a word or a magnitude outside int64 is refused.
func (r *LocaleRevision) UnmarshalJSON(data []byte) error {
	value, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return errors.New("expected an exact revision counter")
	}
	*r = LocaleRevision(value)
	return nil
}

// SetLocale is what saying which languages one tenant is served in takes: the
// default, the list the negotiation may choose from, and — for a declaration read
// from a page — the revision it was read at.
//
// The default has to be in the list, which the module refuses rather than repairs:
// a set written without its own default is a tenant whose fallback language is one
// it does not serve, and the page would answer in a language nobody chose.
type SetLocale struct {
	Default string `json:"default" minLength:"2" maxLength:"35" doc:"The language a request with no usable preference is answered in" example:"pt-PT"`
	// Supported is a set: duplicates are dropped and the order does not matter,
	// because the only position that means anything is Default.
	Supported []string `json:"supported" doc:"Every language this tenant is served in, besides the default"`
	// ExpectedRevision is the revision this declaration was read at, as the command
	// that counts the pair reports it. It is a pointer because absence is a real
	// answer and not a zero: a caller that names a number is saying "this is what I
	// was looking at", and a changed pair read at a superseded number is refused
	// with a conflict that writes nothing and publishes nothing. A caller that names
	// none states an intent — "say this tenant is served in these languages" — which
	// is what bootstrap does from inside the transaction that created the tenant,
	// and which no screen is: a page always has a revision under its feet, drawn
	// with the set it offers to change.
	//
	// The revision is not stored, so it is not on Tenant and not in the row: it is
	// one command's argument, and `Tenant.LocaleRevision` carries `json:"-"` for the
	// same reason — the number a caller compares is read from the command that owns
	// the count, not written back into a row.
	ExpectedRevision *LocaleRevision `json:"expectedRevision,omitempty" doc:"The revision this declaration was read at; a changed pair brought from a superseded one conflicts"`
}

// Locale is the tenant's own declaration of the languages it is served in, with the
// revision that declaration carries. It is not tenancy.Tenant's Languages field:
// that one is what the loader hands a request, and a number in it would be a number
// the kernel was asked to carry without ever reading.
type Locale struct {
	Default   string
	Supported []string
	Revision  int64
}

// LocaleManager answers one question inside the authoritative transaction: may the
// caller of this request set this tenant's own languages?
//
// It is declared here because the answer lives in another module's table — the
// grants are the auth module's — and this one reads no table it does not own.
// Composition satisfies it over the same authorizer the route guard consulted,
// which is the point: a guard at the door is an early refusal, not the command's
// authority, and a command that writes a tenant's languages has to ask the question
// again where its own transaction can be answered. An empty answer is a refusal, and
// a refusal writes nothing and publishes nothing.
type LocaleManager interface {
	MaySetOwnLocale(ctx context.Context, tx db.Tx[db.Tenant]) (bool, error)
}

// Rename is what a tenant is called. There is no Slug field, and that absence
// is the decision: a slug is a DNS label and the base of every URL the platform
// builds for the tenant, so changing it is another tenant's create, not this
// tenant's rename. The same refusal as NewTenant.Operator's json:"-" — a thing
// that cannot be asked for has no field to ask with.
type Rename struct {
	Name string `json:"name" minLength:"1" maxLength:"200" doc:"Display name" example:"Acme Corporation"`
}

// Delete is the confirmation. Slug is what the body has to repeat, because a
// destructive control-plane verb has to be mounted somewhere, and the guard that
// makes it safe to mount at all is that the operator writes down which customer
// they are removing. It is not a password and it is not secret: it is the
// one-keystroke refusal that stops a mis-clicked POST.
type Delete struct {
	Confirm string `json:"confirm" maxLength:"63" doc:"The tenant's slug, repeated" example:"acme"`
}

// The three registration modes migrations/000030's CHECK admits, named here for
// the write and in modules/auth/contracts for the read. They are one vocabulary
// spoken by two modules, and that is deliberate: the column's domain is the
// schema's, and the module that stores a fact and the module that acts on it
// each name the values in their own terms rather than one importing the other's
// — which is what keeps modules/tenant from importing the module that signs
// people in.
const (
	RegistrationDisabled  = "disabled"
	RegistrationExisting  = "existing"
	RegistrationProvision = "provision"
)

// OIDCSettings is which identity provider one tenant's people sign in against,
// and what an address that provider vouches for means here. It is the same
// record read and written: a command that took a narrower input than the read
// returns would be two shapes to keep in step for no rule.
//
// SecretRef is a name and not a secret, for the reason migrations/000030 gives:
// a row is copied into an outbox payload and into the audit trail, and a secret
// in a payload is a secret in the trail.
type OIDCSettings struct {
	Issuer       string   `json:"issuer" maxLength:"512" doc:"The provider's own URL, https unless it is local" example:"https://id.acme.example/realms/people"`
	ClientID     string   `json:"clientId" maxLength:"256" doc:"The client this application registers as at that provider"`
	SecretRef    string   `json:"secretRef" maxLength:"64" doc:"Where the client secret is kept — an environment variable's name, never the secret" example:"PLATFORMKIT_OIDC_ACME_SECRET"`
	RedirectPath string   `json:"redirectPath,omitempty" maxLength:"256" doc:"The path the provider sends the browser back to; empty is this installation's own"`
	Registration string   `json:"registration,omitempty" doc:"disabled, existing or provision: what an address the provider vouches for and this tenant has no account for does"`
	Roles        []string `json:"roles,omitempty" doc:"What a provisioned person is given, and only under provision"`
}

// Hook is something that has to happen inside the transaction that creates a
// tenant. main hands this module a list of them.
//
// It is how a module that sits above this one is notified without this one
// importing it: the auth module seeds a new tenant's roles, and if it did so by
// subscribing to tenant.created the roles would appear whenever the worker got
// around to it — which is after the administrator that the same transaction
// created has already tried to sign in.
type Hook func(ctx context.Context, tx db.Tx[db.System], t *Tenant) error

// Inviter is how the control plane gives a tenant its first administrator
// without this module naming the one that owns people.
//
// It is declared here and satisfied in apps/platformkit over the user module,
// the way notification's RecipientLookup is: the consumer declares the
// capability it needs and the application decides who has it, so this module
// still imports nothing.
//
// The capability is narrow on purpose and it is a real one. It takes a system
// transaction, because the tenant being invited into is not the tenant the
// request resolved to — an operator is at their own host and the row belongs
// somewhere else — and there is no other way to write into a tenant from
// outside it. What it must not become is "create a user anywhere": the address
// and the tenant are the arguments, the roles are this module's decision, and
// no password crosses it, so the person invited is somebody who has to read
// their own mailbox before they are anybody at all.
type Inviter interface {
	Invite(ctx context.Context, tx db.Tx[db.System], tenantID uuid.UUID, email, displayName string) error
}

// ErrNoOperatorTenant says this installation has no operator tenant, so a
// lifecycle command cannot write the audit row the installation keeps beside the
// customer's own. The command refuses rather than half-auditing: a control-plane
// verb whose trail is one-sided is a verb nobody can account for afterwards.
//
// It is not a caller's fault and it is not state the caller can change, so it is
// not one of kit/crud's three; the routes answer 503 and write nothing. Every
// request that could reach one of these routes came through the operator
// tenant's own credentials, so in a bootstrapped installation this is
// unreachable — which is the point: the failure it names is a broken
// installation, not a bad request.
var ErrNoOperatorTenant = errors.New("tenant: this installation has no operator tenant, so a lifecycle command cannot audit both sides")

// LanguageDeclaration is one tenant's own statement of the languages it speaks,
// read and written from inside that tenant's transaction: the set, the revision it
// is held at, and the change that has to agree with it.
//
// It is a second interface beside Service, not methods on it, for the reason
// modules/task's contracts.LockedReader gives, and for one more that is measured:
// Service is on the frozen surface of the v2.0.0 line (docs/COMPATIBILITY.md), so a
// language method added to it is a break the release line refuses. Neither half
// names a tenant — a tenant cannot be shown a door that reaches a neighbour, and a
// form cannot expect a revision it was never shown.
//
// The control plane's door onto the same fact (Service.SetLocale) stays a different
// capability: it decides over a row a tenant transaction may only read.
type LanguageDeclaration interface {
	// CurrentLanguages locks this tenant's row and reads its current language
	// set. A preference command calls it before locking its user row.
	//
	// It is the plain read, and it is the one every reader of it stands on:
	// modules/user's contracts.LocalePolicy is the same question, answered by this
	// door for exactly this one method. OwnLocale is the read beside it that also
	// carries the revision a form is about to send back.
	CurrentLanguages(ctx context.Context, tx db.Tx[db.Tenant]) (tenancy.Languages, error)

	// OwnLocale is this tenant's declaration as the tenant's own transaction may
	// read it: the set, and the revision a command has to agree with.
	//
	// It is CurrentLanguages plus the revision, and it is a separate method because
	// the two questions have different askers. A page resolving a person's language
	// needs the set and must not care about the count; the settings page that offers
	// to change the set needs the count, because it is about to submit an expected
	// revision and a form cannot expect a number it was never shown.
	OwnLocale(ctx context.Context, tx db.Tx[db.Tenant]) (Locale, error)

	// SetOwnLocale is the tenant's own declaration of the same pair, through the
	// transaction that already resolved to that tenant: no id names the tenant, because
	// a tenant cannot be shown a door it could point at a neighbour, and the row it
	// writes is this transaction's own under row-level security.
	//
	// It normalises, decides and publishes exactly what SetLocale does — one fact, one
	// event, one refusal to leave a tenant with a default it does not serve — and it
	// rechecks inside this transaction what the route guard only checked at the door:
	// the actor's grant (LocaleManager), the row under a lock, the set's validity, and
	// the expected revision. An administrator's page left open across somebody else's
	// change is refused rather than overwritten.
	//
	// The last-language rule is checked against the proposed union: the default is the
	// floor, so removing the final *other* language is allowed and a blank default is
	// not. Refusing removes nothing: a refusal returns no row and publishes nothing.
	SetOwnLocale(ctx context.Context, tx db.Tx[db.Tenant], expectedRevision int64, in SetLocale) (*Tenant, error)
}

// Service is the tenant lifecycle, and the kernel's host resolution.
//
// Every command takes a db.Tx[db.System], because these rows belong to no
// tenant: a create happens before there is a tenant to scope a transaction to,
// and a list is the one query in the application that is meant to see every
// tenant there is. A module cannot mint that capability, so every caller of
// these methods was handed one — which is what makes them greppable. See
// docs/adr/0006.
//
// The errors are kit/crud's, so one mapping answers for these routes and the
// generated ones alike: crud.ErrNotFound, crud.ErrInvalid, crud.ErrConflict.
// A tenant's own languages — the set, the counted read, the declaration — are
// LanguageDeclaration, a door beside this one, which this interface's frozen
// surface does not carry.
type Service interface {
	// Create writes the tenant, its first host and whatever the hooks add, in
	// one transaction. A slug or a host already taken is a conflict.
	Create(ctx context.Context, tx db.Tx[db.System], in NewTenant) (*Tenant, error)

	// AddHost gives an existing tenant another name to answer at, and says
	// whether it becomes the primary one — the host every absolute URL this
	// application builds for that tenant is built on. Adding a host the tenant
	// already answers at changes nothing unless it promotes it.
	AddHost(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, host string, primary bool) (*Tenant, error)

	// Suspend stops the tenant being served. Suspending it again changes
	// nothing and publishes nothing.
	Suspend(ctx context.Context, tx db.Tx[db.System], id uuid.UUID) (*Tenant, error)

	// Rename changes what a tenant is called. Renaming it to what it is already
	// called changes nothing and publishes nothing, as every other command here
	// does. The slug never moves: see Rename.
	Rename(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, in Rename) (*Tenant, error)

	// Reactivate resumes serving a suspended tenant. An active tenant changes
	// nothing and publishes nothing. A deleted tenant is not found: only a
	// restore brings a deleted tenant back, and there is none here.
	Reactivate(ctx context.Context, tx db.Tx[db.System], id uuid.UUID) (*Tenant, error)

	// RemoveHost stops serving one name. The primary host, and a tenant's last
	// host, are refused: the first is what every absolute URL for this tenant is
	// built on, and the second is the floor — a tenant nobody routes to cannot be
	// signed into, which is the sentence Create's own comment uses about a create
	// with no host.
	//
	// A host the tenant does not answer at changes nothing and publishes nothing.
	// Once the row is gone there is nothing left to distinguish "never yours"
	// from "yours and gone", and the answer that does not leak who serves it now
	// is the one that is also retry-safe.
	RemoveHost(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, host string) (*Tenant, error)

	// Delete retires a tenant: `deleted_at` is written, the row and every row it
	// owns stay where they are, and the two names the platform routes on are
	// released — the slug by the partial index in migrations/000006, and the
	// hosts by their rows, because `tenant_hosts.host` is a global key and a
	// tenant that is not served cannot reserve a hostname from the next one. A
	// retired customer's own rows, its languages and the trail are what a delete
	// keeps, and `Deleted` names the hosts it released. What a restore — which
	// this module does not have — would have to re-attach is a host.
	// The installation's own tenant is refused: it is the door every one of these
	// routes is walked through, and a delete that closed it could not be undone
	// through it. A retired tenant is not found, by this verb as by every read:
	// a retry that answered "already done" would be a reader that admits it can
	// still see a customer nobody else can.
	Delete(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, in Delete) (*Tenant, error)

	// SetLocale says which languages one tenant is served in, and which of them a
	// request that brought nothing usable is answered in. Setting the same pair
	// again changes nothing and publishes nothing, as every other command here
	// does: an operator's retry must not appear twice in an audit.
	//
	// It is a control-plane command and not a tenant's own, because the row it
	// writes is on the control plane: migrations/000006 lets a tenant transaction
	// read its own row and write nothing there. What a tenant may write for itself
	// is copy, in a tenant-scoped table of its own, and that is a different
	// capability with a different policy — not this one pretending to be it.
	//
	// A declaration that names `SetLocale.ExpectedRevision` is compared with the row
	// under the lock this command takes: a changed pair read at a superseded revision
	// is crud.ErrConflict, and a refusal returns no row, writes nothing and publishes
	// nothing. One counter answers both doors, so an operator who read the set before a
	// tenant administrator saved their own is refused rather than allowed to overwrite
	// it. A declaration that names no revision is an intent, and the lock is what makes
	// an intent leave the set whole; only a caller that never saw the row has a reason
	// to bring one.
	SetLocale(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, in SetLocale) (*Tenant, error)

	// SetOIDC says which identity provider one tenant signs in against, and what
	// an unknown-but-verified address does there. Setting the same values again
	// changes nothing and publishes nothing, as every other command here does.
	//
	// It refuses half a provider — an issuer with no client, a provision mode
	// with no roles, a secret reference that is not a name — because each is a
	// configuration that cannot be exercised and reads, to whoever wrote it, as
	// one that can. The pair of CHECKs in migrations/000030 is the same rule
	// where SQL can see it; this is the part it cannot, and the reason the
	// refusal names the field: an operator reading a 422 should not have to
	// guess which of six columns they left empty.
	//
	// The event carries the reference and not a secret, for the reason the
	// column gives: an outbox payload is copied into the audit trail.
	SetOIDC(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, in OIDCSettings) (*Tenant, error)

	// ClearOIDC removes a tenant's provider, so single sign-on answers 404 there
	// again. People already signed in stay signed in: a session is a row, and
	// taking a company's SSO away is not a revocation of anybody's — the two are
	// different decisions and the second one is the sessions page's or an
	// administrator's, never a side effect of the first.
	ClearOIDC(ctx context.Context, tx db.Tx[db.System], id uuid.UUID) (*Tenant, error)

	// OIDCOf reads the provider of the tenant the transaction belongs to. It is
	// the read the sign-in makes, per request, and it is the same tenant-scoped
	// read HostsOf already is: the policy over tenants lets a tenant transaction
	// read its own row and write nothing, so which provider a company signs in
	// against is readable there and writable only by the control plane.
	//
	// It answers false, not an error, when this tenant has no provider.
	OIDCOf(ctx context.Context, tx db.Tx[db.Tenant]) (*OIDCSettings, bool, error)

	// SetSAML says which SAML 2.0 identity provider one tenant's people sign in
	// against. It refuses the same half-providers SetOIDC refuses — an entity ID
	// with no metadata, metadata with no entity ID, `provision` with no roles —
	// naming the field an operator has to fix, and it refuses a metadata document
	// that carries no signing certificate, because an assertion presented against
	// that document could never be verified and the moment worth saying so is the
	// write rather than the first person turned away at the door.
	//
	// Writing the same values again changes nothing and publishes nothing.
	SetSAML(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, in SAMLSettings) (*Tenant, error)

	// ClearSAML takes a tenant's SAML provider away, leaving its OIDC provider —
	// if it has one — exactly where it was: the two column families are
	// independent and neither command touches the other's columns. People already
	// signed in stay signed in, for the reason ClearOIDC gives.
	ClearSAML(ctx context.Context, tx db.Tx[db.System], id uuid.UUID) (*Tenant, error)

	// SAMLSettingsOf is OIDCOf for the other protocol: the read the SAML legs
	// make, per request, in the transaction the Host header resolved, answering
	// false and not an error when this tenant signs in no other way than it
	// always has. A tenant may answer both reads at once.
	SAMLSettingsOf(ctx context.Context, tx db.Tx[db.Tenant]) (*SAMLSettings, bool, error)

	// Get is one tenant with its hosts.
	Get(ctx context.Context, tx db.Tx[db.System], id uuid.UUID) (*Tenant, error)

	// List is every tenant that is not deleted, suspended ones included: this
	// is the control plane's own list, and a suspended tenant is precisely what
	// somebody reading it is looking for.
	List(ctx context.Context, tx db.Tx[db.System]) ([]*Tenant, error)

	// ByHost is httpx.TenantLoader. It answers tenancy.ErrNoSuchHost for a host
	// nobody serves and for a host whose tenant is suspended, because from
	// outside those are the same fact and neither is an outage.
	ByHost(ctx context.Context, tx db.Tx[db.System], host string) (tenancy.Tenant, error)

	// Hosts are the names the transaction's own tenant is served at, the
	// primary one first.
	//
	// It is the one read here that takes a tenant transaction rather than a
	// system one, and that is what it is for: a worker handling one tenant's
	// event needs the host that tenant's people reach the application at — to
	// turn a notice's path into a URL a mail client can follow — and it has no
	// business seeing anybody else's. The policy on tenant_hosts already says
	// so, so this needs no capability and grants none: the rows a tenant
	// transaction can see are that tenant's.
	//
	// It answers no error and no hosts for a tenant that has none, which cannot
	// happen through Create and can happen to a row somebody edited.
	Hosts(ctx context.Context, tx db.Tx[db.Tenant]) ([]string, error)
}

// Active adapts a Service to jobs.TenantLister: the tenants the periodic jobs
// walk, which are the ones actually being served.
//
// It is a type rather than a method because jobs.TenantLister's method is called
// List and Service.List answers a different question — every tenant, suspended
// ones included. Two questions, two names, and the adapter says which is which.
type Active struct{ Service }

// List is every tenant a job should visit.
func (a Active) List(ctx context.Context, tx db.Tx[db.System]) ([]tenancy.Tenant, error) {
	all, err := a.Service.List(ctx, tx)
	if err != nil {
		return nil, err
	}
	out := make([]tenancy.Tenant, 0, len(all))
	for _, t := range all {
		if t.Status == StatusActive {
			out = append(out, t.Tenancy())
		}
	}
	return out, nil
}

// Languages is the set a tenant's locale may be set to: what this installation's
// copy is written in, read off the catalogues the composition installed.
//
// It is a contract and not a Deps field because the composition is the thing
// that read the files, and the module that refuses a locale has to be told what
// exists — this module names no tag of its own. A composition that installs no
// catalogues composes no provider, and then nothing is checked against anything.
//
// It is a struct around the tags to provide one vocabulary value. An unnamed
// slice need takes contributions; a named contract is one provided value. See
// docs/adr/0021-named-slices-are-contract-values.md for the resolver rule.
type Languages struct {
	Tags []string
}
