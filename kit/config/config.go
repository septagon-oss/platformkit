// Package config is the one configuration surface: a YAML file, six environment
// overrides, and a check that nothing needed is missing. There is no defaulting
// layer and no reflection; a key that nothing reads does not belong here.
package config

import (
	"bytes"
	"fmt"
	"net"
	"net/url"
	"os"
	"slices"
	"strings"

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
	Log      Log      `yaml:"log"`
	Auth     Auth     `yaml:"auth"`
	Mail     Mail     `yaml:"mail"`
	Audit    Audit    `yaml:"audit"`
	Files    Files    `yaml:"files"`
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
}

// Database holds the two roles: the app connects as one, migrations as the other.
type Database struct {
	URL        string `yaml:"url"`
	MigrateURL string `yaml:"migrate_url"`
}

// NATS is the JetStream endpoint the outbox relay publishes to.
type NATS struct {
	URL string `yaml:"url"`
}

// Log is the logging surface: one level.
type Log struct {
	Level string `yaml:"level"`
}

// Auth is what the auth module cannot decide for itself. Passwords and sessions
// need no configuration — the parameters are constants in the module, because a
// deployment that lowers them is a deployment that has weakened itself — so this
// is one optional identity provider and nothing else.
type Auth struct {
	OIDC OIDC `yaml:"oidc"`
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
// and then main wires the in-memory mailbox and says so at boot — a deployment
// without mail still records every notification and simply sends none.
//
// Username and Password are optional, because a relay on a private network
// authenticates by being unreachable from anywhere else. From is not: a message
// with no sender is refused by the far end, hours later, in somebody else's log.
type Mail struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
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
}

// The defaults. Twenty-five megabytes is what a mail attachment limit taught
// everybody to expect; the directory is relative, so a laptop needs no absolute
// path and a container mounts a volume at it.
const (
	DefaultFilesDir      = "data/files"
	DefaultFilesMaxBytes = 25 << 20
)

// Load reads path, applies the environment overrides, and validates the result.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // an unread key is a mistake, not a comment
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}

	for _, o := range []struct {
		env   string
		field *string
	}{
		{"PLATFORMKIT_SERVER_ADDR", &c.Server.Addr},
		{"PLATFORMKIT_SERVER_PUBLIC_HOST", &c.Server.PublicHost},
		{"PLATFORMKIT_DATABASE_URL", &c.Database.URL},
		{"PLATFORMKIT_DATABASE_MIGRATE_URL", &c.Database.MigrateURL},
		{"PLATFORMKIT_NATS_URL", &c.NATS.URL},
		{"PLATFORMKIT_LOG_LEVEL", &c.Log.Level},
		// The one secret in the surface, and the one key that has an override
		// for a reason rather than for symmetry: rule 7 says never commit a
		// secret, and config.yaml is a file somebody will commit.
		{"PLATFORMKIT_AUTH_OIDC_CLIENT_SECRET", &c.Auth.OIDC.ClientSecret},
		// The second secret, for the same reason as the first.
		{"PLATFORMKIT_MAIL_PASSWORD", &c.Mail.Password},
	} {
		if v, ok := os.LookupEnv(o.env); ok {
			*o.field = v
		}
	}

	for _, f := range []struct {
		key   string
		value string
	}{
		{"server.addr", c.Server.Addr},
		{"server.public_host", c.Server.PublicHost},
		{"database.url", c.Database.URL},
		{"database.migrate_url", c.Database.MigrateURL},
		{"nats.url", c.NATS.URL},
		{"log.level", c.Log.Level},
	} {
		if f.value == "" {
			return Config{}, fmt.Errorf("config %s: %s is empty", path, f.key)
		}
	}

	if !slices.Contains(levels, c.Log.Level) {
		return Config{}, fmt.Errorf("config %s: log.level is %q, one of %v", path, c.Log.Level, levels)
	}
	// Both URLs are parsed here rather than by the driver, so a typo is a
	// message naming the key instead of a dial error four steps later.
	for _, u := range []struct {
		key   string
		value string
	}{
		{"database.url", c.Database.URL},
		{"database.migrate_url", c.Database.MigrateURL},
	} {
		parsed, err := url.Parse(u.value)
		if err != nil {
			return Config{}, fmt.Errorf("config %s: %s is not a URL: %w", path, u.key, err)
		}
		if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
			return Config{}, fmt.Errorf("config %s: %s has scheme %q; PlatformKit speaks postgres and nothing else", path, u.key, parsed.Scheme)
		}
	}
	if err := c.Auth.OIDC.validate(path); err != nil {
		return Config{}, err
	}
	if err := c.Mail.validate(path); err != nil {
		return Config{}, err
	}
	if c.Audit.RetentionDays == 0 {
		c.Audit.RetentionDays = DefaultRetentionDays
	}
	if c.Audit.RetentionDays < 1 {
		return Config{}, fmt.Errorf("config %s: audit.retention_days is %d; a retention period is a number of days", path, c.Audit.RetentionDays)
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
	return c, nil
}

// defaultSMTPPort is submission with STARTTLS, what a modern relay listens on.
const defaultSMTPPort = 587

// validate refuses a half-written mail server, the all-or-none rule the
// identity provider follows: a host with no sender is a mailer that exists and
// cannot send, which is worse than no mailer.
func (m *Mail) validate(path string) error {
	if !m.Enabled() {
		if m.Username != "" || m.Password != "" || m.From != "" {
			return fmt.Errorf("config %s: mail has credentials or a sender and no host", path)
		}
		return nil
	}
	if m.From == "" {
		return fmt.Errorf("config %s: mail.from is empty; a message with no sender is refused by the far end", path)
	}
	if m.Port == 0 {
		m.Port = defaultSMTPPort
	}
	if m.Port < 1 || m.Port > 65535 {
		return fmt.Errorf("config %s: mail.port is %d, which is not a port", path, m.Port)
	}
	if m.Password != "" && m.Username == "" {
		return fmt.Errorf("config %s: mail.password is set and mail.username is empty", path)
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
