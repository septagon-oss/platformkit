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
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// OIDC is one OpenID Connect provider, as this module needs it.
type OIDC struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectPath string
}

// stateCookie carries what the callback has to know and the authorization
// server must not choose: the state it will echo, the nonce the id token must
// contain, and the PKCE verifier whose challenge went out in the redirect.
//
// It is a cookie rather than a row because it is worthless five minutes later
// and belongs to one browser. It is short-lived, HttpOnly and Secure like the
// session cookie, and SameSite is Lax because the browser arrives back at the
// callback from the provider — a cross-site navigation, which Strict would eat.
const (
	stateCookie = "platformkit_oidc"
	stateTTL    = 10 * time.Minute
)

// Provider is the lazily connected identity provider.
//
// Lazily, because discovery is a network call: doing it in Module would make an
// unreachable provider a process that will not start, and an identity provider
// having a bad morning must not stop an application serving the people who are
// already signed in.
type Provider struct {
	cfg     OIDC
	cookies Cookies
	secure  bool

	mu       sync.Mutex
	provider *oidc.Provider
}

// NewProvider prepares the provider. Nothing is dialled here.
func NewProvider(cfg OIDC, cookies Cookies, secure bool) *Provider {
	return &Provider{cfg: cfg, cookies: cookies, secure: secure}
}

func (p *Provider) discover(ctx context.Context) (*oidc.Provider, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.provider != nil {
		return p.provider, nil
	}
	provider, err := oidc.NewProvider(ctx, p.cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("auth: discover %s: %w", p.cfg.Issuer, err)
	}
	p.provider = provider
	return provider, nil
}

// oauth builds the exchange configuration for the host this request arrived at.
//
// The redirect URI is the request's own host and the configured path, because
// every tenant is reached at its own host and a provider is registered against
// each one. Nothing here is taken from a query parameter: a redirect URI a
// caller could choose is an open redirect with a token attached.
func (p *Provider) oauth(provider *oidc.Provider, host string) *oauth2.Config {
	scheme := "https"
	if !p.secure {
		scheme = "http"
	}
	return &oauth2.Config{
		ClientID:     p.cfg.ClientID,
		ClientSecret: p.cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  scheme + "://" + host + p.cfg.RedirectPath,
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
}

// RegisterOIDCRoutes mounts the two legs of the authorization code flow. They
// are registered only when a provider is configured: a route that would answer
// "this application has no identity provider" is a route with nothing to say,
// and the boot gate counts what is mounted rather than what might have been.
func RegisterOIDCRoutes(api *httpx.API, svc contracts.Service, users contracts.Users, p *Provider) {
	httpx.Register(api, huma.Operation{
		OperationID: "auth-oidc-start",
		Method:      http.MethodGet,
		Path:        Path + "/oidc/start",
		Summary:     "Begin single sign-on",
		Description: "Redirects to the identity provider with PKCE and a state cookie.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusServiceUnavailable},
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*redirectOutput, error) {
		r, ok := httpx.RequestFrom(ctx)
		if !ok {
			return nil, problem.New(http.StatusInternalServerError, "")
		}
		provider, err := p.discover(ctx)
		if err != nil {
			return nil, problem.New(http.StatusServiceUnavailable, "the identity provider cannot be reached right now")
		}
		state, nonce, verifier := random(), random(), oauth2.GenerateVerifier()
		return &redirectOutput{
			Status:    http.StatusSeeOther,
			Location:  p.oauth(provider, r.Host).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)),
			SetCookie: []http.Cookie{p.stash(state, nonce, verifier)},
		}, nil
	})

	httpx.Register(api, huma.Operation{
		OperationID: "auth-oidc-callback",
		Method:      http.MethodGet,
		Path:        Path + p.cfg.RedirectPath[len(Path):],
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
		nonce, verifier, err := unstash(in.State, in.Cookie)
		if err != nil {
			return nil, problem.New(http.StatusForbidden, "this sign-in did not start here, or it took too long")
		}
		provider, err := p.discover(ctx)
		if err != nil {
			return nil, problem.New(http.StatusServiceUnavailable, "the identity provider cannot be reached right now")
		}
		email, err := p.claim(ctx, provider, r.Host, in.Code, verifier, nonce)
		if err != nil {
			return nil, problem.New(http.StatusForbidden, err.Error())
		}
		user, err := users.ByEmail(ctx, tx, email)
		if errors.Is(err, crud.ErrNotFound) {
			// No automatic provisioning. Being able to sign in at an identity
			// provider says who somebody is; it does not say that this customer
			// has an account for them, and inventing one would let anybody with
			// an address at the provider's domain into the tenant.
			return nil, problem.New(http.StatusForbidden, "there is no account here for that address")
		}
		if err != nil {
			return nil, crud.Fault(err)
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
				expire(stateCookie, p.secure),
			},
		}, nil
	})
}

// claim exchanges the code and returns the verified address the id token names.
func (p *Provider) claim(ctx context.Context, provider *oidc.Provider, host, code, verifier, nonce string) (string, error) {
	token, err := p.oauth(provider, host).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", errors.New("the identity provider refused that code")
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return "", errors.New("the identity provider returned no id token")
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: p.cfg.ClientID}).Verify(ctx, raw)
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

// stash writes the state cookie; unstash reads it back and checks the state the
// provider echoed against the one that went out.
func (p *Provider) stash(state, nonce, verifier string) http.Cookie {
	return http.Cookie{
		Name: stateCookie, Value: strings.Join([]string{state, nonce, verifier}, "."),
		Path: Path + "/oidc", MaxAge: int(stateTTL.Seconds()),
		HttpOnly: true, Secure: p.secure, SameSite: http.SameSiteLaxMode,
	}
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

func expire(name string, secure bool) http.Cookie {
	return http.Cookie{Name: name, Path: Path + "/oidc", MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode}
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
	Code   string `query:"code" doc:"The authorization code"`
	State  string `query:"state" doc:"The state that went out with the redirect"`
	Cookie string `cookie:"platformkit_oidc" doc:"The state cookie this application set"`
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
