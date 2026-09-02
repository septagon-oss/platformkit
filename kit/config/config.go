// Package config is the one configuration surface: a YAML file, six environment
// overrides, and a check that nothing needed is missing. There is no defaulting
// layer and no reflection; a key that nothing reads does not belong here.
package config

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"slices"

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
	return c, nil
}
