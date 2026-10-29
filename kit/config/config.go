// Package config is the one configuration surface: a YAML file, the environment
// overrides, the overrides a composition passes to Load, and a check that
// nothing needed is missing or malformed. There is no defaulting layer and no
// reflection; a key that nothing reads does not belong here.
package config

import (
	"time"
)

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
	// Flags selects optional product behavior by key; it never grants permissions,
	// entitlements or tenant access. Nil means none. The pointer keeps Config
	// comparable while consumers own each key's meaning.
	Flags *Flags `yaml:"flags"`
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
// that session by kit/db (db.MigrateWithOptions) for migrations/000043_tenant_app
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

	// ReadTimeout bounds how long a client can send its complete request,
	// including uploads. Zero selects DefaultReadTimeout; negatives refuse Load.
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

// Log is the logging surface: one level.
type Log struct {
	Level string `yaml:"level"`
}

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
