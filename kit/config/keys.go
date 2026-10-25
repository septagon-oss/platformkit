package config

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
	name     string
	env      string
	field    func(*Config) *string
	required bool
}

var keys = []key{
	{"server.addr", "PLATFORMKIT_SERVER_ADDR", func(c *Config) *string { return &c.Server.Addr }, true},
	{"server.public_host", "PLATFORMKIT_SERVER_PUBLIC_HOST", func(c *Config) *string { return &c.Server.PublicHost }, true},
	{"server.installation_host", "PLATFORMKIT_SERVER_INSTALLATION_HOST", func(c *Config) *string { return &c.Server.InstallationHost }, false},
	{"database.url", "PLATFORMKIT_DATABASE_URL", func(c *Config) *string { return &c.Database.URL }, true},
	{"database.migrate_url", "PLATFORMKIT_DATABASE_MIGRATE_URL", func(c *Config) *string { return &c.Database.MigrateURL }, true},
	{"database.retain_url", "PLATFORMKIT_DATABASE_RETAIN_URL", func(c *Config) *string { return &c.Database.RetainURL }, false},
	{"nats.url", "PLATFORMKIT_NATS_URL", func(c *Config) *string { return &c.NATS.URL }, true},
	{"nats.transport", "PLATFORMKIT_NATS_TRANSPORT", func(c *Config) *string { return &c.NATS.Transport }, false},
	{"nats.username", "PLATFORMKIT_NATS_USERNAME", func(c *Config) *string { return &c.NATS.Username }, false},
	{"nats.password", "PLATFORMKIT_NATS_PASSWORD", func(c *Config) *string { return &c.NATS.Password }, false},
	{"nats.ca_cert", "PLATFORMKIT_NATS_CA_CERT", func(c *Config) *string { return &c.NATS.CACert }, false},
	{"cache.adapter", "PLATFORMKIT_CACHE_ADAPTER", func(c *Config) *string { return &c.Cache.Adapter }, false},
	{"cache.app", "PLATFORMKIT_CACHE_APP", func(c *Config) *string { return &c.Cache.App }, false},
	{"cache.url", "PLATFORMKIT_CACHE_URL", func(c *Config) *string { return &c.Cache.URL }, false},
	// A secret, so it earns an override for the reason rule 7 gives and earns no
	// place in config.example.yaml with a value in it.
	{"cache.password", "PLATFORMKIT_CACHE_PASSWORD", func(c *Config) *string { return &c.Cache.Password }, false},
	// The secret store's own bootstrap credential, and the one secret in this table
	// that cannot arrive through kit/secrets: the port needs the token to read the
	// token. The adapter and the two mount names are not secrets and are settable
	// here for the same reason any deployment fact is — a fleet sets them per
	// deployment — while the directory is a path and belongs in the file.
	{"secrets.adapter", "PLATFORMKIT_SECRETS_ADAPTER", func(c *Config) *string { return &c.Secrets.Adapter }, false},
	{"secrets.dir", "PLATFORMKIT_SECRETS_DIR", func(c *Config) *string { return &c.Secrets.Dir }, false},
	{"secrets.url", "PLATFORMKIT_SECRETS_URL", func(c *Config) *string { return &c.Secrets.URL }, false},
	{"secrets.token", "PLATFORMKIT_SECRETS_TOKEN", func(c *Config) *string { return &c.Secrets.Token }, false},
	{"secrets.kv_mount", "PLATFORMKIT_SECRETS_KV_MOUNT", func(c *Config) *string { return &c.Secrets.KVMount }, false},
	{"secrets.transit_mount", "PLATFORMKIT_SECRETS_TRANSIT_MOUNT", func(c *Config) *string { return &c.Secrets.TransitMount }, false},
	{"log.level", "PLATFORMKIT_LOG_LEVEL", func(c *Config) *string { return &c.Log.Level }, true},
	// The one secret in the surface with an override for a reason rather than
	// for symmetry: rule 7 says never commit a secret, and config.yaml is a
	// file somebody will commit.
	{"auth.oidc.client_secret", "PLATFORMKIT_AUTH_OIDC_CLIENT_SECRET", func(c *Config) *string { return &c.Auth.OIDC.ClientSecret }, false},
	// The second secret, for the same reason as the first.
	// The factor key, for the same reason as the other three: it seals a
	// credential, and config.yaml is a file somebody will commit.
	{"auth.factor_key", "PLATFORMKIT_AUTH_FACTOR_KEY", func(c *Config) *string { return &c.Auth.FactorKey }, false},
	// The fifth: the key that wraps every webhook signing secret. A deployment
	// that loses it cannot sign again with a secret it never read back, so this one
	// is written down by whoever can also restore it.
	{"webhook.secret_key", "PLATFORMKIT_WEBHOOK_SECRET_KEY", func(c *Config) *string { return &c.Webhook.SecretKey }, false},
	// The third: the first administrator's password, read once by the
	// bootstrap command and stored nowhere but as an argon2id hash.
	{"bootstrap.password", "PLATFORMKIT_BOOTSTRAP_PASSWORD", func(c *Config) *string { return &c.Bootstrap.Password }, false},
	{"demo.password", "PLATFORMKIT_DEMO_PASSWORD", func(c *Config) *string { return &c.Demo.Password }, false},
	// The seed command's operator, for the same reason as the other three: a
	// credential is not a flag. Neither is required by the loader, because the
	// server reads neither and the command refuses an empty pair itself.
	{"seed.operator_email", "PLATFORMKIT_SEED_OPERATOR_EMAIL", func(c *Config) *string { return &c.Seed.OperatorEmail }, false},
	{"seed.operator_password", "PLATFORMKIT_SEED_OPERATOR_PASSWORD", func(c *Config) *string { return &c.Seed.OperatorPassword }, false},
	// No environment override, and still overridable: the sender is a value a
	// composition knows — one client, one from address — and a deployment that
	// wrote it in the file wrote it once.
	// A host is a deployment's fact about its own network, which is what an
	// environment variable is for; the sender beside it is a composition's.
	{"mail.host", "PLATFORMKIT_MAIL_HOST", func(c *Config) *string { return &c.Mail.Host }, false},
	{"mail.from", "", func(c *Config) *string { return &c.Mail.From }, false},
	// The collector is an endpoint, and an endpoint is a deployment's fact about
	// its own network, which is what an environment variable is for.
	{"telemetry.otlp_endpoint", "PLATFORMKIT_TELEMETRY_OTLP_ENDPOINT", func(c *Config) *string { return &c.Telemetry.OTLPEndpoint }, false},
	{"telemetry.service_name", "PLATFORMKIT_TELEMETRY_SERVICE_NAME", func(c *Config) *string { return &c.Telemetry.ServiceName }, false},
	// The fourth secret-shaped key, and not a secret: which customer a shared
	// installation is serving is not sensitive, but a fleet sets it per deployment.
	{"telemetry.client", "PLATFORMKIT_TELEMETRY_CLIENT", func(c *Config) *string { return &c.Telemetry.Client }, false},
	// The object store: one profile name, one adapter name, an address, and the two
	// static credentials the client speaks. Every one is required: false, because
	// presence is the cloud profile's question and not the loader's — an empty one
	// is what a deployment that never chose a store looks like, and refusing that
	// here would refuse every laptop in this repository's own test suite.
	{"cloud.profile", "PLATFORMKIT_CLOUD_PROFILE", func(c *Config) *string { return &c.Cloud.Profile }, false},
	{"files.store", "PLATFORMKIT_FILES_STORE", func(c *Config) *string { return &c.Files.Store }, false},
	{"files.s3.endpoint", "PLATFORMKIT_FILES_S3_ENDPOINT", func(c *Config) *string { return &c.Files.S3.Endpoint }, false},
	{"files.s3.bucket", "PLATFORMKIT_FILES_S3_BUCKET", func(c *Config) *string { return &c.Files.S3.Bucket }, false},
	{"files.s3.region", "PLATFORMKIT_FILES_S3_REGION", func(c *Config) *string { return &c.Files.S3.Region }, false},
	{"files.s3.scheme", "PLATFORMKIT_FILES_S3_SCHEME", func(c *Config) *string { return &c.Files.S3.Scheme }, false},
	{"files.s3.bucket_lookup", "PLATFORMKIT_FILES_S3_BUCKET_LOOKUP", func(c *Config) *string { return &c.Files.S3.BucketLookup }, false},
	// Two more secrets, for the reason every other secret in this table has: a
	// credential belongs in the environment because config.yaml is a file somebody
	// will commit.
	{"files.s3.access_key", "PLATFORMKIT_FILES_S3_ACCESS_KEY", func(c *Config) *string { return &c.Files.S3.AccessKey }, false},
	{"files.s3.secret_key", "PLATFORMKIT_FILES_S3_SECRET_KEY", func(c *Config) *string { return &c.Files.S3.SecretKey }, false},
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
