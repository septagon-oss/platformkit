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

	"github.com/google/uuid"
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
	Dev      Dev      `yaml:"dev"`
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

// Dev stands in for the tenant and auth modules until stage E3 ships them: a
// list of hosts that are tenants, and one principal every request is.
//
// It is deleted in E3 along with apps/platformkit/dev.go, which reads it. Until
// then it is the reason `make run` boots on an empty database with no seeding
// step, and it is dangerous by construction — it hands every caller every
// permission — so Load refuses it unless server.public_host is a local name.
type Dev struct {
	// Enabled turns the whole block on. It defaults to false, so a deployment
	// that says nothing gets no development identity.
	Enabled bool `yaml:"enabled"`
	// Principal is who every request is. Empty is not allowed when Enabled.
	Principal DevPrincipal `yaml:"principal"`
	// Tenants are the hosts that resolve, and what they resolve to.
	Tenants []DevTenant `yaml:"tenants"`
}

// DevPrincipal is the caller the development identity hook returns for every
// request. Its tenant is the one the request host resolved to, so it is not
// named here.
type DevPrincipal struct {
	UserID string   `yaml:"user_id"`
	Roles  []string `yaml:"roles"`
}

// DevTenant is one host and the tenant it is.
type DevTenant struct {
	Host string `yaml:"host"`
	ID   string `yaml:"id"`
	Slug string `yaml:"slug"`
	Name string `yaml:"name"`
}

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
	if err := c.Dev.validate(path, c.Server.PublicHost); err != nil {
		return Config{}, err
	}
	return c, nil
}

// validate refuses a development block that is either off a laptop or missing
// something the hook it feeds cannot invent.
//
// The public-host rule is the important one and it lives here rather than in
// kit/app, which knows nothing about a development identity and should not
// start to: this is the one place both keys are already in hand. A localhost
// name is not a security boundary — it is a deployment that has to be
// deliberate about turning "everyone is an admin" on.
func (d Dev) validate(path, publicHost string) error {
	if !d.Enabled {
		return nil
	}
	if !local(publicHost) {
		return fmt.Errorf("config %s: dev.enabled is true and server.public_host is %q; the development identity makes every caller an administrator of every tenant, so it is refused anywhere but a local name", path, publicHost)
	}
	if _, err := uuid.Parse(d.Principal.UserID); err != nil {
		return fmt.Errorf("config %s: dev.principal.user_id is %q, which is not a uuid", path, d.Principal.UserID)
	}
	if len(d.Tenants) == 0 {
		return fmt.Errorf("config %s: dev.enabled is true and dev.tenants is empty, so no host would resolve", path)
	}
	for i, t := range d.Tenants {
		switch {
		case t.Host == "":
			return fmt.Errorf("config %s: dev.tenants[%d] has no host", path, i)
		case t.Slug == "":
			return fmt.Errorf("config %s: dev.tenants[%d] (%s) has no slug", path, i, t.Host)
		case t.Name == "":
			return fmt.Errorf("config %s: dev.tenants[%d] (%s) has no name", path, i, t.Host)
		}
		if _, err := uuid.Parse(t.ID); err != nil {
			return fmt.Errorf("config %s: dev.tenants[%d] (%s) has id %q, which is not a uuid", path, i, t.Host, t.ID)
		}
	}
	return nil
}

// local reports whether host is a name that only reaches this machine. The set
// is closed and short on purpose: anything cleverer is a rule somebody will
// find a way past, and the answer to "I want the development identity on a real
// host" is no.
func local(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		host == "127.0.0.1" || host == "::1" || host == "[::1]"
}
