package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/appname"

	"gopkg.in/yaml.v3"
)

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
	c, err := readConfig(path)
	if err != nil {
		return Config{}, err
	}
	if err := c.applyOverrides(path, overrides); err != nil {
		return Config{}, err
	}
	// Apply profile defaults only to unanswered settings, after explicit sources
	// and before validation. An unknown profile must fail before applying defaults.
	if err := c.Cloud.validate(); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	c = ApplyCloudProfile(c)

	if err := c.validate(path); err != nil {
		return Config{}, err
	}
	return c, nil
}

func readConfig(path string) (Config, error) {
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

	return c, nil
}

func (c *Config) applyOverrides(path string, overrides []Override) error {
	for _, o := range overrides {
		i := slices.IndexFunc(keys, func(k key) bool { return k.name == o.key })
		if i < 0 {
			return fmt.Errorf("config %s: %q is not an overridable key; they are %s",
				path, o.key, strings.Join(Keys(), ", "))
		}
		if err := keys[i].assign(c, o.value); err != nil {
			return fmt.Errorf("config %s: the override %s: %w", path, o.key, err)
		}
	}
	for _, k := range keys {
		if k.env == "" {
			continue
		}
		if v, ok := os.LookupEnv(k.env); ok {
			if err := k.assign(c, v); err != nil {
				return fmt.Errorf("config %s: the environment's %s: %w", path, k.env, err)
			}
		}
	}

	return nil
}
