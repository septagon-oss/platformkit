package config

import (
	"fmt"
	"maps"
	"net"
	"net/url"
	"slices"
	"strings"
)

// levels is the closed set log.level may name. slog has four; a fifth spelling
// would be silently ignored at boot and noticed during an incident.
var levels = []string{"debug", "info", "warn", "error"}

// safeReason retains a URL parser's diagnosis while removing the supplied DSN.
// If credentials would remain, it returns a generic refusal naming no secret.
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

func (c *Config) validate(path string) error {
	if err := c.validateRequired(path); err != nil {
		return err
	}
	if err := c.mergeTenantApps(path); err != nil {
		return err
	}
	if err := c.validateDatabase(path); err != nil {
		return err
	}
	if err := c.validateAdapters(path); err != nil {
		return err
	}
	if err := c.validateDefaults(path); err != nil {
		return err
	}
	return c.validateFiles(path)
}

func (c *Config) validateRequired(path string) error {
	for _, k := range keys {
		// Only a key that holds a string can be missing: a port that arrived as 0
		// is a port that was not written down at all, and Mail.validate says so in
		// the all-or-none terms the mail block is checked in.
		if k.required && k.field != nil && *k.field(c) == "" {
			return fmt.Errorf("config %s: %s is empty", path, k.name)
		}
	}

	if !slices.Contains(levels, c.Log.Level) {
		return fmt.Errorf("config %s: log.level is %q, one of %v", path, c.Log.Level, levels)
	}
	for _, h := range []struct{ key, value string }{
		{"server.public_host", c.Server.PublicHost},
		{"server.installation_host", c.Server.InstallationHost},
	} {
		// Reject URLs where routing requires a host, naming the configuration key.
		if h.value != "" && !validHost(h.value) {
			return fmt.Errorf("config %s: %s is %q; it is a host, optionally with a port, and not a URL",
				path, h.key, h.value)
		}
	}
	return nil
}

func (c *Config) mergeTenantApps(path string) error {
	// Merge file-based tenant placements with inline values taking precedence.
	// The loader validates syntax; migrations check placement for existing tenants.
	if c.App.TenantAppsFile != "" {
		fromFile, err := readTenantApps(c.App.TenantAppsFile)
		if err != nil {
			return fmt.Errorf("config %s: %w", path, err)
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

	return nil
}

func (c *Config) validateDatabase(path string) error {
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
			return fmt.Errorf("config %s: %s is not a URL: %s", path, u.key, safeReason(u.key, u.value, err))
		}
		if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
			return fmt.Errorf("config %s: %s has scheme %q; PlatformKit speaks postgres and nothing else", path, u.key, parsed.Scheme)
		}
	}
	return nil
}

func (c *Config) validateAdapters(path string) error {
	if err := c.NATS.Validate(); err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	if err := c.Secrets.Validate(); err != nil {
		return err
	}
	if err := c.Cache.Validate(); err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	if err := c.Auth.OIDC.validate(path); err != nil {
		return err
	}
	if err := c.Auth.AccountRequests.validate(path); err != nil {
		return err
	}
	if err := c.Mail.validate(path); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateDefaults(path string) error {
	if r := c.Telemetry.SampleRatio; r != nil && (*r < 0 || *r > 1) {
		return fmt.Errorf("config %s: telemetry.sample_ratio is %v; it is a fraction of the traces this process starts, so between 0 and 1", path, *r)
	}
	if c.Telemetry.ServiceName == "" {
		c.Telemetry.ServiceName = DefaultServiceName
	}
	if c.Audit.RetentionDays == 0 {
		c.Audit.RetentionDays = DefaultRetentionDays
	}
	// The database's audit-expiry trigger enforces the same minimum; refusing a
	// shorter period here avoids scheduling deletes that can never succeed.
	if c.Audit.RetentionDays < 1 {
		return fmt.Errorf("config %s: audit.retention_days is %d; a retention period is a number of days", path, c.Audit.RetentionDays)
	}
	if c.Audit.RetentionDays < DefaultRetentionDays {
		return fmt.Errorf("config %s: audit.retention_days is %d; the kernel's floor for forgetting audit history is %d days — modules/audit/migrations/000041_audit_history_append_only.up.sql refuses an earlier expiry and no configuration moves it",
			path, c.Audit.RetentionDays, DefaultRetentionDays)
	}
	if c.Server.ReadTimeout == 0 {
		c.Server.ReadTimeout = DefaultReadTimeout
	}
	if c.Server.ReadTimeout < 0 {
		return fmt.Errorf("config %s: server.read_timeout is %s; a client either has a deadline or the server has none", path, c.Server.ReadTimeout)
	}
	if c.Server.Streams.MaxPerTenant == 0 {
		c.Server.Streams.MaxPerTenant = DefaultStreamMaxPerTenant
	}
	if c.Server.Streams.MaxPerTenant < 1 || c.Server.Streams.MaxPerTenant > MaxStreamMaxPerTenant {
		return fmt.Errorf("config %s: server.streams.max_per_tenant is %d; a tenant's live connections per process are between 1 and %d",
			path, c.Server.Streams.MaxPerTenant, MaxStreamMaxPerTenant)
	}
	return nil
}

func (c *Config) validateFiles(path string) error {
	if c.Files.Dir == "" {
		c.Files.Dir = DefaultFilesDir
	}
	if c.Files.MaxBytes == 0 {
		c.Files.MaxBytes = DefaultFilesMaxBytes
	}
	if c.Files.MaxBytes < 1 {
		return fmt.Errorf("config %s: files.max_bytes is %d; a limit is a number of bytes", path, c.Files.MaxBytes)
	}
	if err := c.Files.validateObjects(); err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	// Refuse empty classes and nonpositive durations before a sweep can
	// interpret them as retention policies.
	for _, kind := range slices.Sorted(maps.Keys(c.Files.Retention)) {
		switch keep := c.Files.Retention[kind]; {
		case kind == "":
			return fmt.Errorf("config %s: files.retention has an entry with no class name; an empty key is the upload that named no class, which is not a class", path)
		case keep <= 0:
			return fmt.Errorf("config %s: files.retention.%s is %s; how long a class lives is a positive duration written Go's way, like 720h", path, kind, keep)
		}
	}
	return nil
}
