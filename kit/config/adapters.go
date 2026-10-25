package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/septagon-oss/platformkit/kit/appname"
)

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
