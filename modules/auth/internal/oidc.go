package internal

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/danielgtaylor/huma/v2"
	"golang.org/x/oauth2"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// OIDC is one OpenID Connect provider, as this module needs it.
//
// Registration and Roles come from the tenant's row and decide what a verified
// address this tenant has no account for means; the rest are the four strings
// that build an authorization request. The secret is here because the exchange
// needs a string and not a reference — and it got here from the environment in
// this request, never from a row.
type OIDC struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectPath string
	Registration string
	Roles        []string
}

// mode is this provider's registration mode, with the default spelled out: a
// tenant that named nothing, and an installation configured before the choice
// existed, both get today's behaviour.
func (o OIDC) mode() string {
	if o.Registration == "" {
		return contracts.RegistrationExisting
	}
	return o.Registration
}

// stateCookie carries what the callback has to know and the authorization
// server must not choose: the state it will echo, the nonce the id token must
// contain, and the PKCE verifier whose challenge went out in the redirect.
//
// It is a cookie rather than a row because it is worthless five minutes later
// and belongs to one browser. It is short-lived, HttpOnly and Secure like the
// session cookie, and SameSite is Lax because the browser arrives back at the
// callback from the provider — a cross-site navigation, which Strict would eat.
// It carries the same __Host- prefix as the session cookie when it is Secure,
// for the same reason and at the same price: __Host- requires Path=/, so this
// is sent on every request for ten minutes rather than only under this module's
// prefix. A sibling tenant's host being unable to forge it is worth more than
// the narrower path.
const (
	stateCookie = "platformkit_oidc"
	stateTTL    = 10 * time.Minute
)

// Provider is the lazily connected identity provider — for the tenant the
// request resolved to, which is the whole of what changed when the issuer moved
// from the process to the tenant.
//
// Lazily, because discovery is a network call: doing it in Module would make an
// unreachable provider a process that will not start, and an identity provider
// having a bad morning must not stop an application serving the people who are
// already signed in.
//
// Per request, and that is not an implementation detail: `of` reads the
// resolved tenant's row through contracts.OIDCProviders every time, so the
// client id, the mode and the secret reference a sign-in uses are the ones that
// tenant's, and 0028's shared-instance mode has nothing per process to unpick.
// What is kept between requests is the discovery document, keyed by issuer —
// the one fact that is genuinely the same for everybody who names that issuer.
type Provider struct {
	// cfg is the installation's own default, and the fallback for a tenant with
	// no row: the composition that configures one issuer for the whole
	// application still works, unchanged, and answers exactly as it did before
	// this struct grew a port.
	cfg       OIDC
	cookies   Cookies
	secure    bool
	providers contracts.OIDCProviders
	secrets   contracts.Secrets

	mu    sync.Mutex
	found map[string]*oidc.Provider
}

// NewProvider prepares the providers. Nothing is dialled here, and one of these
// serves every tenant: the per-tenant part is the configuration it looks up,
// not the object.
func NewProvider(cfg OIDC, cookies Cookies, secure bool, providers contracts.OIDCProviders, secrets contracts.Secrets) *Provider {
	return &Provider{cfg: cfg, cookies: cookies, secure: secure,
		providers: providers, secrets: secrets, found: map[string]*oidc.Provider{}}
}

// of is this request's provider: the tenant's own when it has one, the
// installation's default when the tenant named nothing, and nothing when there
// is genuinely none — which answers 404 rather than 500, because "this company
// has no single sign-on" is a fact about one tenant and not an outage.
//
// The secret is resolved here and nowhere else, per request, from the reference
// the row holds: an installation that rotates a client secret rotates it in the
// environment and needs no write to any table. A reference that resolves to
// nothing is a 503 — the tenant *does* have single sign-on, and the deployment
// has failed to give it the means — and it writes nothing.
func (p *Provider) of(ctx context.Context, tx db.Tx[db.Tenant]) (OIDC, bool, error) {
	if p.providers != nil {
		settings, ok, err := p.providers.ProviderOf(ctx, tx)
		if err != nil {
			return OIDC{}, false, err
		}
		if ok && settings.Issuer != "" {
			secret := ""
			if p.secrets != nil {
				secret, _ = p.secrets.Lookup(ctx, settings.SecretRef)
			}
			if secret == "" {
				return OIDC{}, false, problem.New(http.StatusServiceUnavailable,
					"this tenant's identity provider secret is not available to this installation")
			}
			return OIDC{Issuer: settings.Issuer, ClientID: settings.ClientID, ClientSecret: secret,
				RedirectPath: p.redirect(settings.RedirectPath)}, true, nil
		}
	}
	if p.cfg.Issuer == "" {
		return OIDC{}, false, nil
	}
	return p.cfg, true, nil
}

// redirect is the path the provider sends the browser back to: the tenant's own
// if it declared one, the installation's otherwise. A path rather than a URL,
// because the URL is built from the request's host and a redirect URI a caller
// could choose is an open redirect with a token attached.
func (p *Provider) redirect(path string) string {
	if path != "" {
		return path
	}
	return p.cfg.RedirectPath
}

// discover returns the provider this issuer documents, from the cache if some
// earlier request — at this tenant or another — already fetched it.
//
// The key is the issuer and nothing else. That is the assertion the two-issuer
// test attacks: a cache keyed by anything less, or a single field, lets the
// first tenant's discovery document answer for every tenant after it, which is
// one process sending two companies' people to one directory.
func (p *Provider) discover(ctx context.Context, issuer string) (*oidc.Provider, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if found, ok := p.found[issuer]; ok {
		return found, nil
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("auth: discover %s: %w", issuer, err)
	}
	p.found[issuer] = provider
	return provider, nil
}

// oauth builds the exchange configuration for the host this request arrived at.
//
// The redirect URI is the request's own host and the configured path, because
// every tenant is reached at its own host and a provider is registered against
// each one. Nothing here is taken from a query parameter: a redirect URI a
// caller could choose is an open redirect with a token attached.
func (p *Provider) oauth(cfg OIDC, provider *oidc.Provider, host string) *oauth2.Config {
	scheme := "https"
	if !p.secure {
		scheme = "http"
	}
	return &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  scheme + "://" + host + cfg.RedirectPath,
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
}

// RegisterOIDCRoutes mounts the two legs of the authorization code flow. They
// are registered only when a provider is configured: a route that would answer
// "this application has no identity provider" is a route with nothing to say,
// and the boot gate counts what is mounted rather than what might have been.
func RegisterOIDCRoutes(surfaces httpx.Surfaces, svc contracts.Service, users contracts.Users, provisioner contracts.Provisioner, p *Provider) {
	app := surfaces.App
	httpx.Register(app, huma.Operation{
		OperationID: "auth-oidc-start",
		Method:      http.MethodGet,
		Path:        "/oidc/start",
		Summary:     "Begin single sign-on",
		Description: "Redirects to the identity provider with PKCE and a state cookie.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusServiceUnavailable},
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*redirectOutput, error) {
		r, ok := httpx.RequestFrom(ctx)
		if !ok {
			return nil, problem.New(http.StatusInternalServerError, "")
		}
		// The tenant's provider, read inside the transaction this request's Host
		// resolved: the reason two companies on one process sign in at two
		// issuers. A tenant with none is a 404, and a tenant that turned single
		// sign-off off is the same 404 answered before the provider is dialled —
		// so "disabled" is not a page that discovers an issuer it will not use.
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		cfg, ok, err := p.of(ctx, tx)
		switch {
		case err != nil:
			return nil, rest.Fault(err)
		case !ok:
			return nil, problem.New(http.StatusNotFound, "this tenant has no single sign-on")
		case cfg.mode() == contracts.RegistrationDisabled:
			return nil, problem.New(http.StatusNotFound, "this tenant does not sign in with an identity provider")
		}
		provider, err := p.discover(ctx, cfg.Issuer)
		if err != nil {
			return nil, problem.New(http.StatusServiceUnavailable, "the identity provider cannot be reached right now")
		}
		state, nonce, verifier := random(), random(), oauth2.GenerateVerifier()
		return &redirectOutput{
			Status:    http.StatusSeeOther,
			Location:  p.oauth(cfg, provider, r.Host).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)),
			SetCookie: []http.Cookie{p.stash(state, nonce, verifier)},
		}, nil
	})

	httpx.Register(app, huma.Operation{
		OperationID: "auth-oidc-callback",
		Method:      http.MethodGet,
		// The leg the identity provider sends the browser back to. Its address
		// is composed here, from this module and the workspace surface; the
		// configured redirect_path is the same address spelled absolutely,
		// because what a provider is registered with is a URL and not a
		// relative path, and kit/config refuses the two when they disagree.
		Path:        "/oidc/callback",
		Summary:     "Finish single sign-on",
		Description: "Exchanges the code, verifies the id token, and opens a session for the user whose verified address it names. An address this tenant does not have is refused: nobody is created here.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusForbidden, http.StatusServiceUnavailable},
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventLoggedIn}},
	}, httpx.Public(), func(ctx context.Context, in *callbackInput) (*redirectOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		r, _ := httpx.RequestFrom(ctx)
		nonce, verifier, err := unstash(in.State, p.stashed(r))
		if err != nil {
			return nil, problem.New(http.StatusForbidden, "this sign-in did not start here, or it took too long")
		}
		cfg, ok, err := p.of(ctx, tx)
		switch {
		case err != nil:
			return nil, rest.Fault(err)
		case !ok:
			return nil, problem.New(http.StatusNotFound, "this tenant has no single sign-on")
		case cfg.mode() == contracts.RegistrationDisabled:
			return nil, problem.New(http.StatusForbidden, "this tenant does not sign in with an identity provider")
		}
		provider, err := p.discover(ctx, cfg.Issuer)
		if err != nil {
			return nil, problem.New(http.StatusServiceUnavailable, "the identity provider cannot be reached right now")
		}
		email, err := p.claim(ctx, cfg, provider, r.Host, in.Code, verifier, nonce)
		if err != nil {
			return nil, problem.New(http.StatusForbidden, err.Error())
		}
		user, err := users.ByEmail(ctx, tx, email)
		if errors.Is(err, crud.ErrNotFound) {
			// This is the tenant's registration mode, read a moment ago from its
			// own row. `existing` — the default, and every tenant that predates
			// the column — refuses: being able to sign in at an identity provider
			// says who somebody is, not that this customer has an account for
			// them. `provision` is the operator having said that here, the first
			// does imply the second, and the person is made with the named roles
			// and nothing else.
			if cfg.mode() != contracts.RegistrationProvision || provisioner == nil {
				return nil, problem.New(http.StatusForbidden, "there is no account here for that address")
			}
			id, err := provisioner.Provision(ctx, tx, email, email, cfg.Roles)
			if err != nil {
				return nil, rest.Fault(err)
			}
			// Read back rather than assumed: the person the provisioner made is
			// the person this tenant's policy can see, and a session opened for a
			// row nobody can read would be a session that identifies as nobody.
			user, err = users.Get(ctx, tx, id)
			if err != nil {
				return nil, rest.Fault(err)
			}
		}
		if err != nil {
			return nil, rest.Fault(err)
		}
		session, _, err := svc.Open(ctx, tx, user.ID, ClientOf(r))
		if err != nil {
			return nil, refusal(err)
		}
		return &redirectOutput{
			Status: http.StatusSeeOther, Location: "/",
			// The session, and the state cookie thrown away: it is worthless
			// now and leaving it is a verifier somebody could replay.
			SetCookie: []http.Cookie{
				p.cookies.Session(session.ID, session.ExpiresAt),
				p.cookies.Forget(stateCookie),
			},
		}, nil
	})
}

// claim exchanges the code and returns the verified address the id token names.
func (p *Provider) claim(ctx context.Context, cfg OIDC, provider *oidc.Provider, host, code, verifier, nonce string) (string, error) {
	token, err := p.oauth(cfg, provider, host).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", errors.New("the identity provider refused that code")
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return "", errors.New("the identity provider returned no id token")
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}).Verify(ctx, raw)
	if err != nil {
		return "", errors.New("that id token does not verify")
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		return "", errors.New("that id token belongs to another sign-in")
	}
	var claims struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return "", errors.New("that id token carries no address")
	}
	if claims.Email == "" || !claims.Verified {
		// An unverified address is an address somebody else may own. Accepting
		// one lets anybody who can register at the provider claim any account.
		return "", errors.New("the identity provider did not confirm that address")
	}
	return claims.Email, nil
}

// stash writes the state cookie; stashed reads it back under whichever name
// this deployment sets it under, and unstash checks the state the provider
// echoed against the one that went out.
//
// The name is not a struct tag on the input, because it depends on whether the
// cookie is Secure and a tag is one string decided at compile time.
func (p *Provider) stash(state, nonce, verifier string) http.Cookie {
	return p.cookies.State(strings.Join([]string{state, nonce, verifier}, "."), int(stateTTL.Seconds()))
}

func (p *Provider) stashed(r *http.Request) string {
	if r == nil {
		return ""
	}
	c, err := r.Cookie(p.cookies.Name(stateCookie))
	if err != nil {
		return ""
	}
	return c.Value
}

func unstash(echoed, cookie string) (nonce, verifier string, err error) {
	parts := strings.Split(cookie, ".")
	if len(parts) != 3 || parts[0] == "" {
		return "", "", errors.New("auth: no sign-in is in progress")
	}
	if subtle.ConstantTimeCompare([]byte(parts[0]), []byte(echoed)) != 1 {
		return "", "", errors.New("auth: that is not the state we sent")
	}
	return parts[1], parts[2], nil
}

// random is 32 bytes of crypto/rand, base64url. It is the state and the nonce:
// both only have to be unguessable and unique.
func random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail on any platform this runs on, and a
		// predictable state would be a sign-in anybody could complete.
		panic("auth: no randomness: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

type callbackInput struct {
	Code  string `query:"code" doc:"The authorization code"`
	State string `query:"state" doc:"The state that went out with the redirect"`
}

// redirectOutput is a browser redirect with the cookies that go with it.
//
// The cookies are a slice and not two fields, because huma writes a scalar
// header field with Set and a slice with Append: two fields would be one
// Set-Cookie, and the second would silently replace the first.
type redirectOutput struct {
	Status    int
	Location  string        `header:"Location"`
	SetCookie []http.Cookie `header:"Set-Cookie"`
}
