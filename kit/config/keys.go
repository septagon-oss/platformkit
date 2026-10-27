package config

import (
	"fmt"
	"strconv"
	"strings"
)

// keys is every key a deployment or a composition may set from
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
