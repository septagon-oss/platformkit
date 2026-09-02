// Package config is the one configuration surface: a YAML file, six environment
// overrides, and a check that nothing needed is missing. There is no defaulting
// layer and no reflection; a key that nothing reads does not belong here.
package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the whole configuration of the reference app. See config.example.yaml.
type Config struct {
	Server   Server   `yaml:"server"`
	Database Database `yaml:"database"`
	NATS     NATS     `yaml:"nats"`
	Log      Log      `yaml:"log"`
}

// Server is where the app listens and what host it believes it is reached at.
type Server struct {
	Addr       string `yaml:"addr"`
	PublicHost string `yaml:"public_host"`
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
	return c, nil
}
