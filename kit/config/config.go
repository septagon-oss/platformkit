// Package config is the one configuration surface: a YAML file, the environment
// overrides, the overrides a composition passes to Load, and a check that
// nothing needed is missing or malformed. There is no defaulting layer and no
// reflection; a key that nothing reads does not belong here.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/septagon-oss/platformkit/kit/appname"
	"io/fs"
	"maps"
	"net"
	"net/mail"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// levels is the closed set log.level may name. slog has four; a fifth spelling
// would be silently ignored at boot and noticed during an incident.
var levels = []string{"debug", "info", "warn", "error"}

// Config is the whole configuration of the reference app. See config.example.yaml.
type Config struct {
	Server   Server   `yaml:"server"`
	Database Database `yaml:"database"`
	NATS     NATS     `yaml:"nats"`
	Cache    Cache    `yaml:"cache"`
	Log      Log      `yaml:"log"`
	Auth     Auth     `yaml:"auth"`
	Mail     Mail     `yaml:"mail"`
	// Secrets names where kit/secrets reads from. Its zero value is the process's
	// own environment, which is the one store every deployment has, so a
	// composition is never refused for a secret store it did not name.
	Secrets Secrets `yaml:"secrets"`
	Audit   Audit   `yaml:"audit"`
	Files   Files   `yaml:"files"`
	// Cloud is the one setting that says which seams this deployment runs on —
	// which object store, which shared cache, which event transport, where the
	// spans go. It names the adapters and no address: see Cloud.
	Cloud Cloud `yaml:"cloud"`
	// Webhook is the deployment's share of the outbound webhook sender: the key
	// that wraps each endpoint's signing secret at rest, which addresses this
	// deployment is willing to dial, and how long a signed attempt is kept.
	Webhook Webhook `yaml:"webhook"`
	// Telemetry is where spans and numbers go. Its zero value exports nothing,
	// which is the default the runtime ships with: see Telemetry.
	Telemetry Telemetry `yaml:"telemetry"`
	// Bootstrap is read by `platformkit bootstrap` alone; the server never
	// looks at it. It is in the configuration surface so the one secret the
	// command takes arrives the way every other secret does, through kit/config
	// and its environment override, rather than through a variable the command
	// read for itself.
	Bootstrap Bootstrap `yaml:"bootstrap"`
	// Seed is what the seed command cannot decide for itself: the operator who
	// ran it. `--as` names the person a write belongs to, which is attribution
	// and not permission — a target tenant's admin address is somebody else's
	// answer. The credential below is the permission side: it is checked against
	// the installation tenant's own people, and the operator must hold
	// tenant:manage there, before the command enters the tenant it was pointed
	// at. Both arrive through the environment like every other secret, because
	// argv is in the process table and in shell history.
	//
	// Empty means nobody proved who they are, and the command refuses rather
	// than seeding as the database account it happens to be pointed at.
	Seed Seed `yaml:"seed"`
	// Demo is read by the seed alone. A demonstration needs people a person can
	// sign in as, and the password they sign in with is a deployment's answer,
	// not a fact in a file: nothing under seed/ holds a secret, and the loader
	// refuses a field that looks like one.
	Demo Demo `yaml:"demo"`
	// App is what this composition declares about itself to the things that run
	// before a request does. The slug lives in nats.app, where kit/appname's
	// grammar guards it and every name is already formed from it; what is here is
	// the two facts a migration needs and the database cannot supply.
	App App `yaml:"app"`
	// Flags are this installation's feature flags, by key. They are configuration
	// rather than a flag service because that is the honest size of what the
	// reference application needs: one boolean somebody can throw without a
	// deployment, read the way every other value here is read.
	//
	// A flag never grants a permission, narrows a tenant's scope or replaces a
	// subscription's entitlement (kit/flags says so of the contract this satisfies);
	// it decides only whether an optional product behaviour runs. Which key means
	// what is the consumer's fact, written beside the consumer —
	// apps/platformkit/change.go names the one this application reads — and a key
	// spelled wrong is refused at load rather than ignored (dec.KnownFields).
	//
	// A pointer, so that Config stays comparable: a map field would make the whole
	// configuration incomparable, and the exported API gate refuses that as the
	// break it is (`old is comparable, new is not`). An installation that says
	// nothing about flags has none, which is a nil pointer and not an empty map.
	Flags *Flags `yaml:"flags"`
}

// readTenantApps parses the operator's placement file: `slug=app` lines, with
// blank lines and `#` comments allowed. Every refusal names the file and the line,
// because the file is not in the configuration the person reading the message
// already has open. Values go through the same grammar as nats.app, so a slug a
// durable cannot be formed from is refused here and not at the boot that reads it.
func readTenantApps(path string) (map[string]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("app.tenant_apps_file: %w", err)
	}
	out := make(map[string]string)
	for i, line := range strings.Split(string(body), "\n") {
		where := fmt.Sprintf("app.tenant_apps_file %s:%d", path, i+1)
		text := strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if text == "" {
			continue
		}
		slug, app, found := strings.Cut(text, "=")
		if !found {
			return nil, fmt.Errorf("%s is %q; a line is <tenant-slug>=<app-slug>", where, text)
		}
		slug, app = strings.TrimSpace(slug), strings.TrimSpace(app)
		if slug == "" || app == "" {
			return nil, fmt.Errorf("%s is %q; both a tenant and an app have to be named", where, text)
		}
		if _, err := appname.Parse(app); err != nil {
			return nil, fmt.Errorf("%s names app %q: %w", where, app, err)
		}
		if before, again := out[slug]; again && before != app {
			return nil, fmt.Errorf("%s maps tenant %s to %q, and the file already maps it to %q", where, slug, app, before)
		}
		out[slug] = app
	}
	return out, nil
}

// Seed is the seed command's operator credential. See the field on Config.
type Seed struct {
	OperatorEmail    string `yaml:"operator_email"`
	OperatorPassword string `yaml:"operator_password"`
}

// Demo is what the demo seed cannot decide for itself: the one password every
// demo person signs in with. Empty means the seed generates a password per
// person and prints each once, the way bootstrap does with the administrator's.
// Supply PLATFORMKIT_DEMO_PASSWORD.
type Demo struct {
	Password string `yaml:"password"`
}

// App is the composition's own declaration, read at the migrating boot and put on
// that session by kit/db (db.MigrateDeclaring) for migrations/000043_tenant_app
// to place tenants with. It is a declaration and not a request: nothing at run
// time consults it, and a deployment that names no slug and no hosts is the
// single-app deployment migrating a database that has no tenants to place.
type App struct {
	// Hosts are the hosts this app serves its tenants at. An existing tenant
	// joins this app only when every host it holds is one of these; a tenant with
	// a host outside the list belongs to another composition sharing the database.
	Hosts []string `yaml:"hosts"`
	// TenantApps is the operator's explicit placement, tenant slug to app slug,
	// for the tenants no host can place. Its values go through the same grammar as
	// nats.app, and a mapping that does not cover every tenant is refused by the
	// migration rather than completed by a guess.
	TenantApps map[string]string `yaml:"tenant_apps"`
	// TenantAppsFile names a file of `slug=app` lines holding the same placement,
	// for an operator whose mapping is too long to read in a YAML blob. It is read
	// at load and merged into TenantApps above, the inline map winning a
	// disagreement; a line that is not a pair, a tenant mapped twice to two apps or
	// an app that is not a slug refuses the load. Nothing else reads the key.
	TenantAppsFile string `yaml:"tenant_apps_file"`
}

// Flags is the flags block: one boolean per key. See Config.Flags for why the
// block is a pointer and what a flag may and may not decide.
type Flags struct {
	// Values is the map itself, one level down so a deployment that names no flag
	// writes no `flags:` block at all rather than an empty nested one.
	Values map[string]bool `yaml:"values"`
}

// Telemetry is the measurement surface: one OTLP/gRPC collector, what this
// process is called, and how much of what it starts is kept.
//
// Everything here is optional and the empty configuration exports nothing, which
// is a decision rather than a default: a deployment that has not chosen a backend
// pays nothing for spans it will never read, and still propagates a trace it was
// handed and still leaves a trace context on every outbox row it writes, so the
// trace continues into a process that does export. An unreachable collector, by
// contrast, is never a failed boot: the exporter buffers, warns through
// OpenTelemetry's own error handler and keeps serving.
type Telemetry struct {
	// OTLPEndpoint is the collector's URL — https://collector.example:4318, or a
	// host:port for a collector on the same network with no TLS. Empty exports
	// nothing.
	OTLPEndpoint string `yaml:"otlp_endpoint"`
	// ServiceName is what a trace backend shows this process under. Every replica
	// of one deployment answers the same name; two deployments differ.
	ServiceName string `yaml:"service_name"`
	// Client names the client this installation serves, and is written on the
	// resource of every span and metric. It is empty by default, and empty is
	// right for a shared installation: one process serving many tenants cannot
	// name one client, and a resource attribute is the process's own fact. See
	// kit/telemetry for why the tenant is never a resource attribute.
	Client string `yaml:"client"`
	// SampleRatio is the fraction of the traces this process *starts* that are
	// kept, 0 to 1. A trace that arrives with a sampled parent stays kept whatever
	// this says, so an event handled in a worker remains part of the request that
	// caused it. It is a pointer because 0 is an answer and an omitted key is not:
	// omitting it keeps every new trace.
	SampleRatio *float64 `yaml:"sample_ratio"`
}

// Ratio is the sampling fraction, 1 when the key was omitted.
func (t Telemetry) Ratio() float64 {
	if t.SampleRatio == nil {
		return 1
	}
	return *t.SampleRatio
}

// Bootstrap is what the first-run command cannot decide for itself: the first
// administrator's password. Empty means the command generates one and prints
// it once. Command-line arguments are in the process table and in shell
// history, so a password is not a flag; supply PLATFORMKIT_BOOTSTRAP_PASSWORD.
type Bootstrap struct {
	Password string `yaml:"password"`
}

// Server is where the app listens, what host it believes it is reached at, and
// whether it publishes its own documentation.
type Server struct {
	Addr       string `yaml:"addr"`
	PublicHost string `yaml:"public_host"`
	// Docs serves /openapi.json, /openapi.yaml and /docs. They are public by
	// construction and they publish every route and every permission the
	// application has: a map worth having before an attack and worth
	// withholding during one. It defaults to false, so a deployment that says
	// nothing says no.
	Docs bool `yaml:"docs"`
	// InstallationHost is the host the installation itself is reached at, and
	// the only address that serves the control plane (the /ops surface). Empty
	// means the installation has no host of its own: the routes mount and every
	// request to them is a 404, which app logs once at boot.
	//
	// It is a host and not a tenant, and it is not public_host: that one names
	// the address a customer's site is reached at and only decorates the OpenAPI
	// document and the HSTS header. Two hosts per installation is the point.
	InstallationHost string `yaml:"installation_host"`
	// StorybookDir opts the operator tenant into a locally built Storybook.js.
	// Product applications select private builds through admin.Deps.Storybook.
	StorybookDir string `yaml:"storybook_dir"`

	// ReadTimeout is how long a client has to send a whole request. It is a
	// key rather than a constant because the one number it has to accommodate
	// is a deployment's: an upload of files.max_bytes over a slow connection
	// takes as long as it takes, and thirty seconds is right for a laptop and
	// wrong for a deployment that accepts a gigabyte.
	//
	// There was no read timeout at all, and a review sent a body at a byte a
	// second and held the request — and, before the file module was made to
	// stream outside one, a database transaction — for as long as it liked.
	ReadTimeout time.Duration `yaml:"read_timeout"`

	// Streams bounds the live connections this process serves. There is one number
	// because there is one thing a deployment knows and the kernel does not: how many
	// replicas stand behind the load balancer, and therefore how many browsers one
	// tenant may have open at once.
	Streams Streams `yaml:"streams"`
}

// Streams is the configuration of the live connections this process holds.
type Streams struct {
	// MaxPerTenant is how many live connections one tenant may hold in this one
	// process. A cluster of N replicas admits N times this number, which is the
	// honest meaning of the word "process" here: a cluster-wide cap is not what this
	// counts, and kit/stream/README.md says so beside the follow-up that would.
	//
	// The per-person ceiling is not configurable: it is the number of tabs one
	// person plausibly has open, and a deployment that wanted more of them would be
	// asking for a different feature.
	MaxPerTenant int `yaml:"max_per_tenant"`
}

// Database holds the three roles: the app appends as one, migrations run as
// another, and expiring the audit trail happens as a third. The third is optional
// and empty means the trail never expires — modules/audit's retention job refuses to
// run rather than reaching for a role it was not handed, because the application role
// cannot delete a trail row (migrations/000041_audit_history_append_only.up.sql fences it)
// and the door that admits an expiry is a role that may delete and may not append.
//
// Which one does what: migrate_url migrates (and drains), url appends and reads,
// retain_url expires. retain_url must not name a superuser or a BYPASSRLS role: the
// job opens it with db.Open, which refuses such a role precisely because a trim run by
// one would see every tenant's trail inside the first tenant's transaction.
type Database struct {
	URL        string `yaml:"url"`
	MigrateURL string `yaml:"migrate_url"`
	RetainURL  string `yaml:"retain_url"`
	// Omitted values retain kit/db defaults; explicit zero idle/lifetime
	// disables reuse/retirement. kit/app validates the resolved pool before IO.
	MaxOpenConns    *int           `yaml:"max_open_conns"`
	MaxIdleConns    *int           `yaml:"max_idle_conns"`
	ConnMaxLifetime *time.Duration `yaml:"conn_max_lifetime"`
	// LockTimeout is how long a migration may wait for a lock before the runner stops
	// it and says it is contended; StatementTimeout bounds one statement. Omitted values
	// retain the defaults kit/db owns and explains in db.MigrationBudget.
	LockTimeout      *time.Duration `yaml:"lock_timeout"`
	StatementTimeout *time.Duration `yaml:"statement_timeout"`
}

// NATS configures the event transport and its broker connection. Empty Transport
// preserves the role default: memory for all, JetStream for a separate worker.
// Each application/database/environment needs its own broker account because
// stream and durable consumer names are shared within an account.
type NATS struct {
	Transport string `yaml:"transport"`
	// App is this deployment's own app slug — the name every shared name it forms
	// carries, and the reason two apps on one broker and one database cannot read
	// one another's work. Empty is the deployment of one app: it keeps the names
	// this kernel formed before the app segment existed. kit/appname owns the
	// grammar; Validate is what refuses a slug that could not be a subject token.
	App      string `yaml:"app"`
	URL      string `yaml:"url"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	CACert   string `yaml:"ca_cert"`
}

// AppName is the slug this process serves, as the type every name two apps could
// share is formed from. The empty setting is the deployment of one app and answers
// the zero Name, which is what every constructor of kit/appname reads as "keep the
// name this kernel formed before the app segment existed"; a setting that is
// present and broken is refused. One door, so that a reader of `nats.app` and the
// transport that builds itself from it cannot disagree about what empty means.
func (n NATS) AppName() (appname.Name, error) {
	if n.App == "" {
		return "", nil
	}
	app, err := appname.Parse(n.App)
	if err != nil {
		return "", fmt.Errorf("nats.app: %w", err)
	}
	return app, nil
}

// Validate checks settings without opening files or connecting to the broker.
// Diagnostics name keys without echoing endpoints or credentials.
func (n NATS) Validate() error {
	if _, err := n.AppName(); err != nil {
		return err
	}
	if n.Transport != "" && n.Transport != "memory" && n.Transport != "jetstream" {
		return errors.New("nats.transport must be memory, jetstream or empty for the role default")
	}
	if (n.Username == "") != (n.Password == "") {
		return errors.New("nats.username and nats.password must both be set or both be empty")
	}
	for endpoint := range strings.SplitSeq(n.URL, ",") {
		u, err := url.Parse(strings.TrimSpace(endpoint))
		if err != nil || u.Hostname() == "" || (u.Scheme != "nats" && u.Scheme != "tls") ||
			u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return errors.New("nats.url must contain nats:// or tls:// endpoints without paths, queries or fragments")
		}
		if u.User != nil {
			return errors.New("nats.url cannot contain credentials; use nats.username and PLATFORMKIT_NATS_PASSWORD instead")
		}
		if n.CACert != "" && u.Scheme != "tls" {
			return errors.New("nats.ca_cert requires tls:// for every nats.url endpoint")
		}
		if n.Username != "" && u.Scheme != "tls" && !Local(u.Host) {
			return errors.New("nats.username and nats.password require tls:// outside localhost")
		}
	}
	return nil
}

// Secrets names the store kit/secrets reads: the environment, a mounted directory,
// or an OpenBao. See kit/secrets/README.md for what each one answers and what it
// cannot.
type Secrets struct {
	// Adapter is env, file or openbao. Empty is env: the process's own environment
	// is the one secret store every deployment has, and it opens nothing.
	Adapter string `yaml:"adapter"`
	// Dir is the mounted directory secrets.adapter file reads — a Kubernetes
	// Secret volume, a Docker secret, a Vault-agent template's output.
	Dir string `yaml:"dir"`
	// URL is the secret service secrets.adapter openbao speaks to, and Token is
	// its credential. The token is the one secret that cannot arrive through the
	// port, because the port needs it to read the port: that bootstrap fact is why
	// it is a plain environment override and not a name in the store.
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
	// KVMount and TransitMount are the two mounts openbao reads: KV v2 for the
	// bytes and transit for the envelope. They default to secret and transit.
	KVMount      string `yaml:"kv_mount"`
	TransitMount string `yaml:"transit_mount"`
}

// Validate checks the secret store without opening it, in Cache.Validate's shape:
// one sentence per refusal, and an adapter's fields belong to that adapter alone.
//
// An address is refused rather than probed, for the reason the file adapter gives:
// the store is not dialed at boot, so a name that cannot be a URL is the only
// mistake this line can catch, and a deployment whose secret service is down finds
// out from the first command that needed a credential.
func (s Secrets) Validate() error {
	switch s.Adapter {
	case "", "env", "file", "openbao":
	default:
		return errors.New("secrets.adapter must be env, file, openbao or empty for the process's own environment")
	}
	if s.Adapter != "file" && s.Dir != "" {
		return errors.New("secrets.dir belongs to secrets.adapter file")
	}
	if s.Adapter != "openbao" && (s.URL != "" || s.Token != "" || s.KVMount != "" || s.TransitMount != "") {
		return errors.New("secrets.url, secrets.token, secrets.kv_mount and secrets.transit_mount belong to secrets.adapter openbao")
	}
	if s.Adapter != "openbao" {
		return nil
	}
	bad := errors.New("secrets.url must be an http or https address without credentials, paths or queries")
	u, err := url.Parse(s.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return bad
	}
	if u.User != nil {
		return errors.New("secrets.url cannot contain credentials; use secrets.token and PLATFORMKIT_SECRETS_TOKEN instead")
	}
	if s.Token == "" {
		return errors.New("secrets.token is required with secrets.adapter openbao: the port cannot read the token that opens the port")
	}
	return nil
}

// Cache names where a value every replica must read is kept. Empty Adapter is the
// in-process store: complete for one process, and it says so in one boot log line,
// because forgetting a value there reaches only that process.
type Cache struct {
	Adapter  string `yaml:"adapter"`
	App      string `yaml:"app"`
	URL      string `yaml:"url"`
	Password string `yaml:"password"`
}

// Validate checks the cache settings without opening a connection.
//
// app is required the moment a shared store is named: every key starts with the
// application that wrote it, and two clients sharing one keyspace with no first
// segment are two clients reading each other's entries. For the in-process store
// the segment carries nothing, so it may stay empty there.
func (c Cache) Validate() error {
	switch c.Adapter {
	case "", "memory", "valkey":
	default:
		return errors.New("cache.adapter must be memory, valkey or empty for the in-process store")
	}
	if c.Adapter != "valkey" && (c.URL != "" || c.Password != "") {
		return errors.New("cache.url and cache.password belong to cache.adapter valkey")
	}
	if c.Adapter != "valkey" {
		return nil
	}
	if c.App == "" {
		return errors.New("cache.app is required with cache.adapter valkey: every shared key begins with the application that wrote it")
	}
	if c.URL == "" {
		return errors.New("cache.url is required with cache.adapter valkey")
	}
	// One sentence for every address that is not one: the operator's fix is the
	// same edit to the same line whichever way it was wrong.
	bad := errors.New("cache.url must be redis://, valkey://, rediss:// or unix:// without credentials, paths or queries")
	u, err := url.Parse(c.URL)
	if err != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return bad
	}
	// A unix socket's address is its path, so the two shapes are checked apart.
	// redis.ParseURL accepts exactly these schemes; valkey:// is rewritten to
	// redis:// by the provider, which is what a Valkey answers to.
	switch u.Scheme {
	case "redis", "rediss", "valkey":
		if u.Host == "" || u.Path != "" {
			return bad
		}
	case "unix":
		if u.Host != "" || u.Path == "" {
			return bad
		}
	default:
		return bad
	}
	if u.User != nil {
		return errors.New("cache.url cannot contain credentials; use cache.password and PLATFORMKIT_CACHE_PASSWORD instead")
	}
	return nil
}

// Log is the logging surface: one level.
type Log struct {
	Level string `yaml:"level"`
}

// Auth is what the auth module cannot decide for itself. Passwords and sessions
// need no configuration — the parameters are constants in the module, because a
// deployment that lowers them is a deployment that has weakened itself — so this
// is one optional identity provider, the key that seals a second factor, and the
// one figure that is traffic rather than strength: how many account requests an
// address may make in a window.
type Auth struct {
	OIDC OIDC `yaml:"oidc"`
	// FactorKey seals a second factor's shared secret at rest. Empty means no
	// second factor is offered at all — the enrolment routes are not mounted, so
	// there is no door that can only answer "unavailable". Like the two other
	// secrets in this file it belongs in the environment and not in a committed
	// file, and it is not required: a deployment that has not decided to offer a
	// second factor is not broken, it is one that has not decided.
	FactorKey string `yaml:"factor_key"`
	// AccountRequests is the budget of the one counter every account request
	// spends: a sign-up, a verification resend, a forgotten-password link. A
	// development instance that signs people up all day names a large one; a
	// deployment that names nothing keeps the module's own figure.
	AccountRequests AccountRequests `yaml:"account_requests"`
}

// AccountRequests is how many account requests one address may make inside a
// window. Each field is a pointer for the reason Telemetry.SampleRatio is: an
// omitted key keeps the auth module's default and an explicit zero is refused,
// never read as unlimited. The defaults are the module's, stated once there
// (modules/auth/contracts: ResetRequests, AttemptWindow), so Load writes none
// here.
type AccountRequests struct {
	Quota  *int           `yaml:"quota"`
	Window *time.Duration `yaml:"window"`
}

// validate refuses a budget that counts nothing: a present quota under one or a
// present window of no length. An omitted field is the module's default and is
// not looked at.
func (a AccountRequests) validate(path string) error {
	if a.Quota != nil && *a.Quota < 1 {
		return fmt.Errorf("config %s: auth.account_requests.quota is %d; it is how many account requests one address may make in a window, so at least 1 — omit it to keep the module's default",
			path, *a.Quota)
	}
	if a.Window != nil && *a.Window <= 0 {
		return fmt.Errorf("config %s: auth.account_requests.window is %s; it is a positive duration written Go's way, like 15m — omit it to keep the module's default",
			path, *a.Window)
	}
	return nil
}

// OIDC is one OpenID Connect provider. An empty issuer means there is none, and
// then the two OIDC routes are not registered at all: a route that would answer
// "this application has no identity provider" is a route with nothing to say.
type OIDC struct {
	Issuer       string `yaml:"issuer"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	// RedirectPath is the path the provider sends the browser back to. The host
	// is the request's own, because every tenant is reached at its own host and
	// one registered redirect per host is what the provider expects.
	RedirectPath string `yaml:"redirect_path"`
}

// Enabled reports whether a provider is configured.
func (o OIDC) Enabled() bool { return o.Issuer != "" }

// Mail is the one outgoing mail server, and there is one sender behind it: SMTP
// is what every service worth naming speaks. An empty host means there is none,
// and there is no default stand-in for one: a command whose promise is a message
// (an emailed verification link) refuses with a reasoned 503 and writes nothing,
// and a notice that asks for mail is recorded as suppressed. What a development
// deployment gets instead is the notification module's simulated mailbox, which
// the resolver refuses to pick outside development — see mail.host below and
// modules/notification's three implementations.
//
// Username is optional, because a relay on a private network authenticates by
// being unreachable from anywhere else. From is not: a message with no sender is
// refused by the far end, hours later, in somebody else's log.
//
// There is no password field, and there was one until this line was written. A
// credential is a secret, and a secret has a door of its own now (kit/secrets), so
// a second field on this struct would be a second door on the same environment
// variable with one of them dead: the carrier reads PLATFORMKIT_MAIL_PASSWORD
// through the port at the moment it dials, which is also when it learns the
// password changed.
type Mail struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	From     string `yaml:"from"`
}

// Enabled reports whether a mail server is configured.
func (m Mail) Enabled() bool { return m.Host != "" }

// Audit is how long the audit trail is kept: the one thing modules/audit cannot
// decide for itself, because a retention period is a compliance obligation and
// a module that chose one would be choosing somebody else's. Zero means the
// default and not "forever" — a table nothing ever deletes from is an outage
// with a date on it, and "forever" is spelled with a large number.
type Audit struct {
	RetentionDays int `yaml:"retention_days"`
}

// Webhook is what modules/webhook cannot decide for itself: the key that wraps
// every endpoint's signing secret, whether this deployment dials addresses on the
// private grid, and how long a finished attempt keeps the payload it signed.
type Webhook struct {
	// SecretKey wraps the signing secret of every registered endpoint at rest.
	// It is 32 bytes — AES-256-GCM's own width — and the module refuses to be
	// composed without one, because an installation whose key nobody wrote down
	// cannot move its endpoints to another machine. Put it in
	// PLATFORMKIT_WEBHOOK_SECRET_KEY, as every other secret here is put; the
	// development example carries a laptop key so `make run` boots.
	SecretKey string `yaml:"secret_key"`
	// AllowLocalEndpoints permits loopback and private ranges to be dialled. It is
	// deployment-level: no column, field or route carries it, and it never opens
	// the instance-metadata address. It exists for a developer's laptop running a
	// receiver beside the application, and for this module's own tests.
	AllowLocalEndpoints bool `yaml:"allow_local_endpoints"`
	// RetentionDays is how long a finished delivery row — which holds the whole
	// event it signed — is kept. Zero means the module's own default.
	RetentionDays int `yaml:"retention_days"`
}

// DefaultRetentionDays is a year, the shortest period the obligations that ask
// for an audit trail at all tend to accept.
const DefaultRetentionDays = 365

// Files is where uploaded bytes go and how large one upload may be: the two
// things modules/file cannot decide for itself, because a directory is a
// deployment's disk and a limit is how much of it a deployment is willing to
// lose to one mistake.
type Files struct {
	Dir      string `yaml:"dir"`
	MaxBytes int64  `yaml:"max_bytes"`
	// QuotaBytes is the disk one tenant may hold, enforced at upload against
	// what that tenant already has. Zero means the module's default of a
	// gigabyte; a negative number means no quota, which is what a
	// single-tenant installation wants and a public sign-up must not have.
	QuotaBytes int64 `yaml:"quota_bytes"`
	// MaxImagePixels is the largest frame an uploaded image is decoded into: a
	// file claiming more pixels is refused with the reason rather than
	// allocated, which is the decompression bomb. Zero means the module's own
	// default of forty megapixels; it bounds a decode and not an upload, so it
	// says nothing about how large a file may be (max_bytes does that).
	MaxImagePixels int `yaml:"max_image_pixels"`
	// Retention is how long each class of file lives, keyed by the `kind` an
	// upload carried. It is a table and not a column because a class is the
	// product's word and a duration is the deployment's: this package parses the
	// durations and modules/file matches one token against the other, and a kind
	// the table does not name is never deleted, only logged.
	//
	// A duration is Go's own spelling — "720h", "45m" — the same one
	// server.read_timeout is written in. There is no environment variable per
	// class and no row in the kernel's key table: one class per line is a
	// config-file thing, which is what modules/file.Deps.Retention says too.
	Retention map[string]time.Duration `yaml:"retention"`
	// Store names the object store the bytes go to: "" or local-disk for the
	// directory above, s3 for the one below. Which adapter a composition wires
	// stays that composition's decision; this is the deployment saying which store
	// it has actually provided the money and the address for.
	Store string `yaml:"store"`
	// S3 is where an object store is. It says nothing about whether one is used —
	// store does that — and is the address of whichever store store names.
	S3 S3 `yaml:"s3"`
}

// S3 is where an S3-compatible object store is and how it is reached: AWS S3,
// Garage, SeaweedFS, Ceph RGW, MinIO on a laptop. The settings are the ones the
// client this kernel links cannot do without, plus the two shape keys beside them,
// which exist because a host and a scheme are the same mistake waiting to be made
// twice.
//
// A bucket is addressed by name, so an endpoint is only ever a host, and the
// credentials are static: the client speaks no credential chain, which is why a
// profile that names a cloud still requires the pair instead of assuming the pod
// knows who it is.
type S3 struct {
	// Endpoint is host:port, with no scheme, path or credentials in it.
	Endpoint string `yaml:"endpoint"`
	Bucket   string `yaml:"bucket"`
	Region   string `yaml:"region"`
	// Scheme is http or https, and empty is https: a deployment that talks plain
	// http to a store writes that down on purpose.
	Scheme string `yaml:"scheme"`
	// BucketLookup is how the bucket is addressed — auto, dns or path. Auto is
	// right for the clouds and a coin flip for a store on a LAN, which is why the
	// key exists and why the default is the one that asks the store.
	BucketLookup string `yaml:"bucket_lookup"`
	AccessKey    string `yaml:"access_key"`
	SecretKey    string `yaml:"secret_key"`
}

// The defaults. Twenty-five megabytes is what a mail attachment limit taught
// everybody to expect; the directory is relative, so a laptop needs no absolute
// path and a container mounts a volume at it.
const (
	DefaultFilesDir      = "data/files"
	DefaultFilesMaxBytes = 25 << 20
)

// DefaultServiceName is what a trace backend shows a process under when the
// deployment says nothing. A name is the one attribute that tells one deployment's
// spans from another's, so it is never left empty: the SDK's own default is
// "unknown_service:" plus the executable, which is a name no operator reads as
// this application.
const DefaultServiceName = "platformkit"

// DefaultReadTimeout is how long a client has to send a whole request when a
// deployment says nothing. Thirty seconds is generous for every route this
// application has except an upload on a bad connection, which is the one a
// deployment overrides it for.
const DefaultReadTimeout = 30 * time.Second

// DefaultStreamMaxPerTenant is how many live connections one tenant may hold in
// one process when the deployment names no number. It is a browser per tab per
// person, times the people of a tenant who happen to be looking at the same
// screen — and it is per process, so a deployment that runs ten replicas admits ten
// times this many and should say so to itself before it is surprised.
const DefaultStreamMaxPerTenant = 500

// MaxStreamMaxPerTenant is the largest number a deployment may write there. It
// exists to refuse a mistyped one — a zero meant as "unlimited", or a six-figure
// count that would be a file-descriptor budget pretending to be a policy — rather
// than to price anyone's plan.
const MaxStreamMaxPerTenant = 100_000

// keys is every string-valued key a deployment or a composition may set from
// outside the file: its name, the environment variable that overrides it, where
// it lives in a Config, and whether the application refuses to start without it.
//
// One table, because the three lists it replaced were the same list written
// three times and drifted: a key the environment could override, a key Load
// required, and a key an error message named. A composition's Set now reaches
// exactly the keys the environment reaches, and an unknown name is refused
// against this list rather than ignored.
type key struct {
	name string
	env  string
	// Exactly one of field and intField is set, and it is the one place the key
	// reaches the struct. Port is a number in the file (`port: 587`) and a string
	// in the environment, so the second accessor is not a flourish: a key whose
	// value type the table cannot name is a key the environment cannot reach, and
	// PLATFORMKIT_MAIL_PORT is in the brief by name.
	field    func(*Config) *string
	intField func(*Config) *int
	required bool
}

// assign writes one value into the field this key names, parsing it first when
// the field holds a number. A value that will not parse is refused with the key
// named, because an environment variable that silently leaves a port at zero is
// a deployment that cannot send mail and says nothing about why.
func (k key) assign(c *Config, value string) error {
	if k.intField == nil {
		*k.field(c) = value
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("%s must be a whole number, and %q is not", k.name, value)
	}
	*k.intField(c) = n
	return nil
}

var keys = []key{
	{"server.addr", "PLATFORMKIT_SERVER_ADDR", func(c *Config) *string { return &c.Server.Addr }, nil, true},
	{"server.public_host", "PLATFORMKIT_SERVER_PUBLIC_HOST", func(c *Config) *string { return &c.Server.PublicHost }, nil, true},
	{"server.installation_host", "PLATFORMKIT_SERVER_INSTALLATION_HOST", func(c *Config) *string { return &c.Server.InstallationHost }, nil, false},
	{"database.url", "PLATFORMKIT_DATABASE_URL", func(c *Config) *string { return &c.Database.URL }, nil, true},
	{"database.migrate_url", "PLATFORMKIT_DATABASE_MIGRATE_URL", func(c *Config) *string { return &c.Database.MigrateURL }, nil, true},
	{"database.retain_url", "PLATFORMKIT_DATABASE_RETAIN_URL", func(c *Config) *string { return &c.Database.RetainURL }, nil, false},
	{"nats.url", "PLATFORMKIT_NATS_URL", func(c *Config) *string { return &c.NATS.URL }, nil, true},
	{"nats.transport", "PLATFORMKIT_NATS_TRANSPORT", func(c *Config) *string { return &c.NATS.Transport }, nil, false},
	{"nats.username", "PLATFORMKIT_NATS_USERNAME", func(c *Config) *string { return &c.NATS.Username }, nil, false},
	{"nats.password", "PLATFORMKIT_NATS_PASSWORD", func(c *Config) *string { return &c.NATS.Password }, nil, false},
	{"nats.ca_cert", "PLATFORMKIT_NATS_CA_CERT", func(c *Config) *string { return &c.NATS.CACert }, nil, false},
	{"cache.adapter", "PLATFORMKIT_CACHE_ADAPTER", func(c *Config) *string { return &c.Cache.Adapter }, nil, false},
	{"cache.app", "PLATFORMKIT_CACHE_APP", func(c *Config) *string { return &c.Cache.App }, nil, false},
	{"cache.url", "PLATFORMKIT_CACHE_URL", func(c *Config) *string { return &c.Cache.URL }, nil, false},
	// A secret, so it earns an override for the reason rule 7 gives and earns no
	// place in config.example.yaml with a value in it.
	{"cache.password", "PLATFORMKIT_CACHE_PASSWORD", func(c *Config) *string { return &c.Cache.Password }, nil, false},
	// The secret store's own bootstrap credential, and the one secret in this table
	// that cannot arrive through kit/secrets: the port needs the token to read the
	// token. The adapter and the two mount names are not secrets and are settable
	// here for the same reason any deployment fact is — a fleet sets them per
	// deployment — while the directory is a path and belongs in the file.
	{"secrets.adapter", "PLATFORMKIT_SECRETS_ADAPTER", func(c *Config) *string { return &c.Secrets.Adapter }, nil, false},
	{"secrets.dir", "PLATFORMKIT_SECRETS_DIR", func(c *Config) *string { return &c.Secrets.Dir }, nil, false},
	{"secrets.url", "PLATFORMKIT_SECRETS_URL", func(c *Config) *string { return &c.Secrets.URL }, nil, false},
	{"secrets.token", "PLATFORMKIT_SECRETS_TOKEN", func(c *Config) *string { return &c.Secrets.Token }, nil, false},
	{"secrets.kv_mount", "PLATFORMKIT_SECRETS_KV_MOUNT", func(c *Config) *string { return &c.Secrets.KVMount }, nil, false},
	{"secrets.transit_mount", "PLATFORMKIT_SECRETS_TRANSIT_MOUNT", func(c *Config) *string { return &c.Secrets.TransitMount }, nil, false},
	{"log.level", "PLATFORMKIT_LOG_LEVEL", func(c *Config) *string { return &c.Log.Level }, nil, true},
	// The one secret in the surface with an override for a reason rather than
	// for symmetry: rule 7 says never commit a secret, and config.yaml is a
	// file somebody will commit.
	{"auth.oidc.client_secret", "PLATFORMKIT_AUTH_OIDC_CLIENT_SECRET", func(c *Config) *string { return &c.Auth.OIDC.ClientSecret }, nil, false},
	// The second secret, for the same reason as the first.
	// The factor key, for the same reason as the other three: it seals a
	// credential, and config.yaml is a file somebody will commit.
	{"auth.factor_key", "PLATFORMKIT_AUTH_FACTOR_KEY", func(c *Config) *string { return &c.Auth.FactorKey }, nil, false},
	// Where the mail server is, and on which port. Both are here for the same
	// reason the database URL is: they are facts about the machine the process is
	// running on, not about the software. A development machine has Mailpit on a
	// port this worktree was given; a deployment has a relay. Neither belongs in
	// the one example file every installation copies (see config.example.yaml's
	// mail block), which is why the example keeps host empty and these two lines
	// are how a mail host arrives at all. The password is not here: kit/secrets
	// reads it as mail.password, so the port that knows about vaults holds it.
	{"mail.host", "PLATFORMKIT_MAIL_HOST", func(c *Config) *string { return &c.Mail.Host }, nil, false},
	{"mail.port", "PLATFORMKIT_MAIL_PORT", nil, func(c *Config) *int { return &c.Mail.Port }, false},
	// The fifth: the key that wraps every webhook signing secret. A deployment
	// that loses it cannot sign again with a secret it never read back, so this one
	// is written down by whoever can also restore it.
	{"webhook.secret_key", "PLATFORMKIT_WEBHOOK_SECRET_KEY", func(c *Config) *string { return &c.Webhook.SecretKey }, nil, false},
	// The third: the first administrator's password, read once by the
	// bootstrap command and stored nowhere but as an argon2id hash.
	{"bootstrap.password", "PLATFORMKIT_BOOTSTRAP_PASSWORD", func(c *Config) *string { return &c.Bootstrap.Password }, nil, false},
	{"demo.password", "PLATFORMKIT_DEMO_PASSWORD", func(c *Config) *string { return &c.Demo.Password }, nil, false},
	// The seed command's operator, for the same reason as the other three: a
	// credential is not a flag. Neither is required by the loader, because the
	// server reads neither and the command refuses an empty pair itself.
	{"seed.operator_email", "PLATFORMKIT_SEED_OPERATOR_EMAIL", func(c *Config) *string { return &c.Seed.OperatorEmail }, nil, false},
	{"seed.operator_password", "PLATFORMKIT_SEED_OPERATOR_PASSWORD", func(c *Config) *string { return &c.Seed.OperatorPassword }, nil, false},
	// The sender, overridable like the host above it. It used to have no
	// environment name, on the argument that one client has one from address and
	// writes it once — true of a deployment, and the reason the example file's
	// value stays empty. It is not true of the machine that sets mail.host from
	// the environment: the all-or-none rule below refuses a mail server with no
	// sender, so a development instance that names a host without a sender does
	// not boot, and the only way it could was a second file.
	{"mail.from", "PLATFORMKIT_MAIL_FROM", func(c *Config) *string { return &c.Mail.From }, nil, false},
	// The collector is an endpoint, and an endpoint is a deployment's fact about
	// its own network, which is what an environment variable is for.
	{"telemetry.otlp_endpoint", "PLATFORMKIT_TELEMETRY_OTLP_ENDPOINT", func(c *Config) *string { return &c.Telemetry.OTLPEndpoint }, nil, false},
	{"telemetry.service_name", "PLATFORMKIT_TELEMETRY_SERVICE_NAME", func(c *Config) *string { return &c.Telemetry.ServiceName }, nil, false},
	// The fourth secret-shaped key, and not a secret: which customer a shared
	// installation is serving is not sensitive, but a fleet sets it per deployment.
	{"telemetry.client", "PLATFORMKIT_TELEMETRY_CLIENT", func(c *Config) *string { return &c.Telemetry.Client }, nil, false},
	// The object store: one profile name, one adapter name, an address, and the two
	// static credentials the client speaks. Every one is required: false, because
	// presence is the cloud profile's question and not the loader's — an empty one
	// is what a deployment that never chose a store looks like, and refusing that
	// here would refuse every laptop in this repository's own test suite.
	{"cloud.profile", "PLATFORMKIT_CLOUD_PROFILE", func(c *Config) *string { return &c.Cloud.Profile }, nil, false},
	{"files.store", "PLATFORMKIT_FILES_STORE", func(c *Config) *string { return &c.Files.Store }, nil, false},
	{"files.s3.endpoint", "PLATFORMKIT_FILES_S3_ENDPOINT", func(c *Config) *string { return &c.Files.S3.Endpoint }, nil, false},
	{"files.s3.bucket", "PLATFORMKIT_FILES_S3_BUCKET", func(c *Config) *string { return &c.Files.S3.Bucket }, nil, false},
	{"files.s3.region", "PLATFORMKIT_FILES_S3_REGION", func(c *Config) *string { return &c.Files.S3.Region }, nil, false},
	{"files.s3.scheme", "PLATFORMKIT_FILES_S3_SCHEME", func(c *Config) *string { return &c.Files.S3.Scheme }, nil, false},
	{"files.s3.bucket_lookup", "PLATFORMKIT_FILES_S3_BUCKET_LOOKUP", func(c *Config) *string { return &c.Files.S3.BucketLookup }, nil, false},
	// Two more secrets, for the reason every other secret in this table has: a
	// credential belongs in the environment because config.yaml is a file somebody
	// will commit.
	{"files.s3.access_key", "PLATFORMKIT_FILES_S3_ACCESS_KEY", func(c *Config) *string { return &c.Files.S3.AccessKey }, nil, false},
	{"files.s3.secret_key", "PLATFORMKIT_FILES_S3_SECRET_KEY", func(c *Config) *string { return &c.Files.S3.SecretKey }, nil, false},
}

// Override is one key a composition sets before the configuration is validated.
type Override struct{ key, value string }

// Set names a key — the same name the YAML file uses and the same name an error
// message names — and the value a composition gives it.
//
// It exists because a client overlay that sets a host after Load has returned
// sets a value nothing checked: validation has already run, so a host with a
// scheme and a path in it becomes every link the application builds, and the
// key the overlay was going to fill in has already been refused as empty. An
// override belongs before validation or it is not an override, it is a
// correction nobody read. See Load.
func Set(name, value string) Override { return Override{key: name, value: value} }

// Load reads path, applies the composition's overrides, then the environment's,
// and validates the result.
//
// The order is the whole point of the signature. The file is what a deployment
// wrote down; a composition's override is the value a client's overlay knows
// and the file cannot ("this client is served at collect.example.com"); the
// environment is last because it belongs to whoever is running the process, and
// an override that code could silently outrank is an override that does
// nothing — which is what PLATFORMKIT_SERVER_PUBLIC_HOST was in the flagship
// binary. Validation runs after all three, on the values that will be used.
func Load(path string, overrides ...Override) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		// The most common first-run mistake, said in terms of what to do next
		// rather than of what the file system saw.
		return Config{}, fmt.Errorf("config: %s does not exist; copy config.example.yaml to it (make run does), or use the start command, which needs none", path)
	}
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // an unread key is a mistake, not a comment
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}

	for _, o := range overrides {
		i := slices.IndexFunc(keys, func(k key) bool { return k.name == o.key })
		if i < 0 {
			return Config{}, fmt.Errorf("config %s: %q is not an overridable key; they are %s",
				path, o.key, strings.Join(Keys(), ", "))
		}
		if err := keys[i].assign(&c, o.value); err != nil {
			return Config{}, fmt.Errorf("config %s: the override %s: %w", path, o.key, err)
		}
	}
	for _, k := range keys {
		if k.env == "" {
			continue
		}
		if v, ok := os.LookupEnv(k.env); ok {
			if err := k.assign(&c, v); err != nil {
				return Config{}, fmt.Errorf("config %s: the environment's %s: %w", path, k.env, err)
			}
		}
	}

	// The cloud profile is applied here: under all three sources above, and before
	// anything is checked. It writes only keys that are still empty, so a file, an
	// override or a variable that already answered a seam outranks it, and it runs
	// before the validators so no guard below is shown a half-written seam the
	// profile was about to finish. The name is checked first: a profile the table
	// does not hold writes nothing, and naming that is better than booting on the
	// wrong adapters quietly.
	if err := c.Cloud.validate(); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	c = ApplyCloudProfile(c)

	for _, k := range keys {
		// Only a key that holds a string can be missing: a port that arrived as 0
		// is a port that was not written down at all, and Mail.validate says so in
		// the all-or-none terms the mail block is checked in.
		if k.required && k.field != nil && *k.field(&c) == "" {
			return Config{}, fmt.Errorf("config %s: %s is empty", path, k.name)
		}
	}

	if !slices.Contains(levels, c.Log.Level) {
		return Config{}, fmt.Errorf("config %s: log.level is %q, one of %v", path, c.Log.Level, levels)
	}
	for _, h := range []struct{ key, value string }{
		{"server.public_host", c.Server.PublicHost},
		{"server.installation_host", c.Server.InstallationHost},
	} {
		// A host, and not a URL. It is the key most likely to be written by a
		// template or by an overlay rather than by a person, and the mistake is
		// always the same one: "https://ops.example.com/" is what somebody writes
		// when a key is called a host. Refusing it here names the key; accepting
		// it is a control plane that answers at no address at all.
		if h.value != "" && !validHost(h.value) {
			return Config{}, fmt.Errorf("config %s: %s is %q; it is a host, optionally with a port, and not a URL",
				path, h.key, h.value)
		}
	}
	// The operator's placement, read from the file the configuration points at.
	// The inline map above and this file hold the same fact; the file exists
	// because a mapping of five thousand tenants does not belong in a YAML blob,
	// and the two merge with the inline map winning on a disagreement — it is the
	// one an operator edits beside the setting it describes.
	//
	// A line the loader cannot make sense of is a refusal, correctable at the file:
	// the mapping is the write that puts a tenant under an app, and there is no safe
	// half of one. What it does *not* refuse is a slug naming no tenant that exists:
	// the mapping is written by somebody who may be describing the tenant they are
	// about to create, and the migration that finds a tenant nobody mapped is the
	// one that refuses (migrations/000059_tenant_app_refusal.up.sql), and only of a
	// boot that named itself: see the guard on migrations/000060_tenant_app_place.
	if c.App.TenantAppsFile != "" {
		fromFile, err := readTenantApps(c.App.TenantAppsFile)
		if err != nil {
			return Config{}, fmt.Errorf("config %s: %w", path, err)
		}
		if c.App.TenantApps == nil {
			c.App.TenantApps = map[string]string{}
		}
		for slug, app := range fromFile {
			if _, written := c.App.TenantApps[slug]; !written {
				c.App.TenantApps[slug] = app
			}
		}
	}

	// All three DSNs are parsed here rather than by the driver, so a typo is a
	// message naming the key instead of a dial error four steps later.
	for _, u := range []struct {
		key   string
		value string
	}{
		{"database.url", c.Database.URL},
		{"database.migrate_url", c.Database.MigrateURL},
		// Optional, so an installation that never expires the trail can leave it
		// empty; a value that is not a postgres URL is still a typo worth naming.
		{"database.retain_url", c.Database.RetainURL},
	} {
		if u.value == "" {
			// An optional key that is absent is absent, not malformed.
			continue
		}
		parsed, err := url.Parse(u.value)
		if err != nil {
			return Config{}, fmt.Errorf("config %s: %s is not a URL: %s", path, u.key, safeReason(u.key, u.value, err))
		}
		if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
			return Config{}, fmt.Errorf("config %s: %s has scheme %q; PlatformKit speaks postgres and nothing else", path, u.key, parsed.Scheme)
		}
	}
	if err := c.NATS.Validate(); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	if err := c.Secrets.Validate(); err != nil {
		return Config{}, err
	}
	if err := c.Cache.Validate(); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	if err := c.Auth.OIDC.validate(path); err != nil {
		return Config{}, err
	}
	if err := c.Auth.AccountRequests.validate(path); err != nil {
		return Config{}, err
	}
	if err := c.Mail.validate(path); err != nil {
		return Config{}, err
	}
	if r := c.Telemetry.SampleRatio; r != nil && (*r < 0 || *r > 1) {
		return Config{}, fmt.Errorf("config %s: telemetry.sample_ratio is %v; it is a fraction of the traces this process starts, so between 0 and 1", path, *r)
	}
	if c.Telemetry.ServiceName == "" {
		c.Telemetry.ServiceName = DefaultServiceName
	}
	if c.Audit.RetentionDays == 0 {
		c.Audit.RetentionDays = DefaultRetentionDays
	}
	// The floor is the trigger's, and the trigger cannot read this file: a shorter
	// period configured is a job that would ask for deletes the database refuses, at
	// three in the morning, forever. Saying so at boot is the correctable version of
	// the same mistake — raise the period, or own a lower floor in a deployment
	// migration that re-creates audit_events_expire_only_after and carries that
	// decision in its own review.
	if c.Audit.RetentionDays < 1 {
		return Config{}, fmt.Errorf("config %s: audit.retention_days is %d; a retention period is a number of days", path, c.Audit.RetentionDays)
	}
	if c.Audit.RetentionDays < DefaultRetentionDays {
		return Config{}, fmt.Errorf("config %s: audit.retention_days is %d; the kernel's floor for forgetting audit history is %d days — modules/audit/migrations/000041_audit_history_append_only.up.sql refuses an earlier expiry and no configuration moves it",
			path, c.Audit.RetentionDays, DefaultRetentionDays)
	}
	if c.Server.ReadTimeout == 0 {
		c.Server.ReadTimeout = DefaultReadTimeout
	}
	if c.Server.ReadTimeout < 0 {
		return Config{}, fmt.Errorf("config %s: server.read_timeout is %s; a client either has a deadline or the server has none", path, c.Server.ReadTimeout)
	}
	if c.Server.Streams.MaxPerTenant == 0 {
		c.Server.Streams.MaxPerTenant = DefaultStreamMaxPerTenant
	}
	if c.Server.Streams.MaxPerTenant < 1 || c.Server.Streams.MaxPerTenant > MaxStreamMaxPerTenant {
		return Config{}, fmt.Errorf("config %s: server.streams.max_per_tenant is %d; a tenant's live connections per process are between 1 and %d",
			path, c.Server.Streams.MaxPerTenant, MaxStreamMaxPerTenant)
	}
	if c.Files.Dir == "" {
		c.Files.Dir = DefaultFilesDir
	}
	if c.Files.MaxBytes == 0 {
		c.Files.MaxBytes = DefaultFilesMaxBytes
	}
	if c.Files.MaxBytes < 1 {
		return Config{}, fmt.Errorf("config %s: files.max_bytes is %d; a limit is a number of bytes", path, c.Files.MaxBytes)
	}
	if err := c.Files.validateObjects(); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	// A retention table is a promise that bytes get removed, so the two ways to
	// write one by mistake are refused here rather than discovered by the sweep.
	// An empty key is the worst of them: every upload that named no class has
	// kind '', and the widest policy in this application is one keyed on nothing
	// — which is also why the module keeps a kind it was never given a policy
	// for instead of guessing.
	for _, kind := range slices.Sorted(maps.Keys(c.Files.Retention)) {
		switch keep := c.Files.Retention[kind]; {
		case kind == "":
			return Config{}, fmt.Errorf("config %s: files.retention has an entry with no class name; an empty key is the upload that named no class, which is not a class", path)
		case keep <= 0:
			return Config{}, fmt.Errorf("config %s: files.retention.%s is %s; how long a class lives is a positive duration written Go's way, like 720h", path, kind, keep)
		}
	}
	return c, nil
}

// safeReason is url.Parse's own diagnosis with the DSN taken out of it. The parse error
// quotes the whole value it failed on, and all three of these values are DSNs whose
// userinfo is a password (decision 0010: an error names the key and the mistake, never
// the credential). The typo is worth reporting — "invalid URL escape" is what tells a
// reader to look at the escaping — so the value is replaced rather than the reason
// dropped. A reason that still carries the userinfo after the replacement says the one
// true thing instead of leaking: which key, and that it could not be read.
func safeReason(key, value string, err error) string {
	reason := strings.ReplaceAll(err.Error(), value, "<"+key+">")
	rest := value
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+len("://"):]
	}
	if creds, _, hasAt := strings.Cut(rest, "@"); hasAt && strings.Contains(reason, creds) {
		return "it cannot be read as a URL, and this refusal does not repeat what is in it"
	}
	return reason
}

// validHost reports whether h is a host — a name or an address, optionally with
// a port — rather than a URL, a path or something with a space in it.
//
// It is spelled with url.Parse because that is the parser every consumer of the
// value will use: a string this accepts is one that survives being put after
// "https://" and before "/path".
func validHost(h string) bool {
	u, err := url.Parse("//" + h)
	if err != nil || u.Host != h || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if host, port, err := net.SplitHostPort(h); err == nil {
		return host != "" && port != "" && strings.IndexFunc(port, func(r rune) bool { return r < '0' || r > '9' }) < 0
	}
	return h != ""
}

// defaultSMTPPort is submission with STARTTLS, what a modern relay listens on.
const defaultSMTPPort = 587

// validate refuses a half-written mail server, the all-or-none rule the
// identity provider follows: a host with no sender is a mailer that exists and
// cannot send, which is worse than no mailer.
func (m *Mail) validate(path string) error {
	if !m.Enabled() {
		if m.Username != "" || m.From != "" {
			return fmt.Errorf("config %s: mail has credentials or a sender and no host", path)
		}
		return nil
	}
	if m.From == "" {
		return fmt.Errorf("config %s: mail.from is empty; a message with no sender is refused by the far end", path)
	}
	// One mailbox, with or without a display name: "noreply@acme.example.com" or
	// "Acme <noreply@acme.example.com>". The mailer sends the bare address as the
	// envelope sender (MAIL FROM) and the whole mailbox as the From header, so a
	// name is what a person sees and never what a relay refuses. A list or a
	// malformed address is refused here, at boot, rather than by the relay hours
	// later in somebody else's log.
	if _, err := mail.ParseAddress(m.From); err != nil {
		return fmt.Errorf("config %s: mail.from is %q; it is one address, optionally with a display name", path, m.From)
	}
	if m.Port == 0 {
		m.Port = defaultSMTPPort
	}
	if m.Port < 1 || m.Port > 65535 {
		return fmt.Errorf("config %s: mail.port is %d, which is not a port", path, m.Port)
	}
	return nil
}

// authPath is where the auth module's routes live, and defaultRedirectPath is
// where the provider sends a browser back to. A deployment only sets the second
// when a provider was registered against a different one.
//
// The prefix is spelled here rather than imported because a configuration
// package that named a module would be the configuration surface depending on
// the catalogue. It is checked, though: the callback is mounted by trimming
// this prefix off, so a path outside it is a route on a prefix the module does
// not own — or, for a path shorter than the prefix, a slice out of range at
// boot. Saying so here names the key.
const (
	authPath            = "/api/v1/auth"
	defaultRedirectPath = authPath + "/oidc/callback"
)

// validate refuses a half-written provider. All of it or none of it: a block
// with an issuer and no client id is a login route that exists and cannot work,
// which is worse than no login route.
func (o *OIDC) validate(path string) error {
	if !o.Enabled() {
		switch {
		case o.ClientID != "" || o.ClientSecret != "":
			return fmt.Errorf("config %s: auth.oidc has credentials and no issuer", path)
		}
		return nil
	}
	u, err := url.Parse(o.Issuer)
	switch {
	case err != nil || u.Scheme != "https" && !Local(u.Host):
		return fmt.Errorf("config %s: auth.oidc.issuer is %q; an issuer is an https URL", path, o.Issuer)
	case o.ClientID == "":
		return fmt.Errorf("config %s: auth.oidc.client_id is empty", path)
	case o.ClientSecret == "":
		return fmt.Errorf("config %s: auth.oidc.client_secret is empty; set %s rather than committing it", path, "PLATFORMKIT_AUTH_OIDC_CLIENT_SECRET")
	}
	if o.RedirectPath == "" {
		o.RedirectPath = defaultRedirectPath
	}
	if !strings.HasPrefix(o.RedirectPath, authPath+"/") {
		return fmt.Errorf("config %s: auth.oidc.redirect_path is %q; it is mounted under %s, so it starts with %q",
			path, o.RedirectPath, authPath, authPath+"/")
	}
	return nil
}

// Local reports whether host is a name that only reaches this machine. The set
// is closed and short on purpose: anything cleverer is a rule somebody will find
// a way past. It is exported because the auth module asks the same question
// about the same key: a session cookie is marked Secure unless the application
// is being reached at a local name, and http://localhost is the one place a
// browser would refuse one.
func Local(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		host == "127.0.0.1" || host == "::1" || host == "[::1]"
}
