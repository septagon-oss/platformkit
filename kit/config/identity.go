package config

import (
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

// Auth is what the auth module cannot decide for itself. Passwords and sessions
// need no configuration — the parameters are constants in the module, because a
// deployment that lowers them is a deployment that has weakened itself — so this
// is one optional identity provider, the key that seals a second factor, and the
// one figure that is traffic rather than strength: how many account requests an
// address may make in a window.
type Auth struct {
	OIDC OIDC `yaml:"oidc"`
	// FactorKey seals a second factor's shared secret at rest. Empty means no
	// second factor is offered at all — the enrolment routes are not mounted, so
	// there is no door that can only answer "unavailable". Like the two other
	// secrets in this file it belongs in the environment and not in a committed
	// file, and it is not required: a deployment that has not decided to offer a
	// second factor is not broken, it is one that has not decided.
	FactorKey string `yaml:"factor_key"`
	// AccountRequests is the budget of the one counter every account request
	// spends: a sign-up, a verification resend, a forgotten-password link. A
	// development instance that signs people up all day names a large one; a
	// deployment that names nothing keeps the module's own figure.
	AccountRequests AccountRequests `yaml:"account_requests"`
}

// AccountRequests is how many account requests one address may make inside a
// window. Each field is a pointer for the reason Telemetry.SampleRatio is: an
// omitted key keeps the auth module's default and an explicit zero is refused,
// never read as unlimited. The defaults are the module's, stated once there
// (modules/auth/contracts: ResetRequests, AttemptWindow), so Load writes none
// here.
type AccountRequests struct {
	Quota  *int           `yaml:"quota"`
	Window *time.Duration `yaml:"window"`
}

// validate refuses a budget that counts nothing: a present quota under one or a
// present window of no length. An omitted field is the module's default and is
// not looked at.
func (a AccountRequests) validate(path string) error {
	if a.Quota != nil && *a.Quota < 1 {
		return fmt.Errorf("config %s: auth.account_requests.quota is %d; it is how many account requests one address may make in a window, so at least 1 — omit it to keep the module's default",
			path, *a.Quota)
	}
	if a.Window != nil && *a.Window <= 0 {
		return fmt.Errorf("config %s: auth.account_requests.window is %s; it is a positive duration written Go's way, like 15m — omit it to keep the module's default",
			path, *a.Window)
	}
	return nil
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

// Mail configures the outgoing SMTP server. An empty Host disables SMTP, and
// there is no default stand-in for one: a command whose promise is a message
// refuses with a reasoned 503 and writes nothing, and a notice that asks for mail
// is recorded as suppressed. What a development deployment gets instead is the
// notification module's simulated mailbox, which the resolver refuses to pick
// outside development. Username is optional, while an enabled server requires
// From. The carrier reads its password through kit/secrets when dialing, so this
// holds no credential.
type Mail struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	From     string `yaml:"from"`
}

// Enabled reports whether a mail server is configured.
func (m Mail) Enabled() bool { return m.Host != "" }

// defaultSMTPPort is submission with STARTTLS, what a modern relay listens on.
const defaultSMTPPort = 587

// validate refuses a half-written mail server, the all-or-none rule the
// identity provider follows: a host with no sender is a mailer that exists and
// cannot send, which is worse than no mailer.
func (m *Mail) validate(path string) error {
	if !m.Enabled() {
		if m.Username != "" || m.From != "" {
			return fmt.Errorf("config %s: mail has credentials or a sender and no host", path)
		}
		return nil
	}
	if m.From == "" {
		return fmt.Errorf("config %s: mail.from is empty; a message with no sender is refused by the far end", path)
	}
	// One mailbox, with or without a display name: "noreply@acme.example.com" or
	// "Acme <noreply@acme.example.com>". The mailer sends the bare address as the
	// envelope sender (MAIL FROM) and the whole mailbox as the From header, so a
	// name is what a person sees and never what a relay refuses. A list or a
	// malformed address is refused here, at boot, rather than by the relay hours
	// later in somebody else's log.
	if _, err := mail.ParseAddress(m.From); err != nil {
		return fmt.Errorf("config %s: mail.from is %q; it is one address, optionally with a display name", path, m.From)
	}
	if m.Port == 0 {
		m.Port = defaultSMTPPort
	}
	if m.Port < 1 || m.Port > 65535 {
		return fmt.Errorf("config %s: mail.port is %d, which is not a port", path, m.Port)
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
