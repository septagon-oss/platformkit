package internal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// SAML is the service provider, built per request from the row the Host resolved.
//
// Per request is the shape and not a detail: one installation fronts many tenants,
// each with its own entity ID, its own IdP and its own ACS URL, and `samlsp.Middleware`
// — the library's own convenience layer — is one process-wide SP behind one fixed
// path, which is a single-tenant application. What is kept between requests is only
// the parsed IdP metadata, keyed by the document's own bytes and by the URL it came
// from: the one fact genuinely shared by everyone who names that IdP, and a cache
// keyed by anything looser would let the first tenant's federation answer for every
// tenant after it. A copy fetched from a URL is kept for an hour and no longer; a copy
// the tenant's own row carries is keyed by its bytes and so cannot go stale.
//
// No signing key. `Key` is nil on every SP this builds, so AuthnRequests go
// unsigned and the SP metadata carries no KeyDescriptor; see contracts.SAMLProvider
// for why an SP key is not a column yet. The assertion signature this module does
// check is the IdP's, and it is the one the tenant's row makes verifiable.
type SAML struct {
	cookies   Cookies
	secure    bool
	providers contracts.SAMLProviders

	// acsPath and metadataPath are composed once, at registration, from the surface
	// the routes actually mounted on: the ACS URL is the address in the browser's
	// POST and in the SP metadata, and the recipient check compares an assertion's
	// SubjectConfirmation against the first. Spelling it a second way here would be
	// a check that passes against one address and serves another.
	acsPath      string
	metadataPath string

	mu   sync.Mutex
	idps map[string]cachedIdP

	// ttl is how long a document fetched from a URL stays the answer. It is a field
	// rather than only a constant so a case can age an entry out without waiting for
	// an hour; composition never sets it.
	ttl time.Duration

	// httpClient is the metadata fetch's own: a deadline and a ceiling, on an address
	// an operator typed.
	httpClient *http.Client
}

// NewSAMLProvider prepares the providers. Nothing is dialled here: a metadata URL
// that cannot be reached is a sign-in that answers 503, not a process that will not
// start — `discover`'s rule for an unreachable issuer, for the same reason.
func NewSAMLProvider(cookies Cookies, secure bool, providers contracts.SAMLProviders) *SAML {
	return &SAML{
		cookies: cookies, secure: secure, providers: providers,
		idps: map[string]cachedIdP{}, ttl: metadataTTL,
		httpClient: &http.Client{Timeout: metadataTimeout, Transport: boundedBody{next: http.DefaultTransport}},
	}
}

// How long a metadata document fetched from a URL is answered from memory, how many
// documents are held, and what one may weigh and take to arrive.
//
// The expiry is what makes the cache a cache rather than a decision. An identity provider
// rotates its signing certificate; the document at its metadata URL says so the moment it
// does, and a tenant whose provider is trusted from a copy fetched months ago is then
// refused at 403 for a fact nobody in this installation can see, until somebody restarts
// the process. Keeping the copy for an hour keeps the performance and bounds the lockout
// — and bounds how long a certificate its owner has taken out of service goes on being
// trusted here, which is the same fact read from the other side. `ValidUntil`, when the
// document carries one, cuts the hour short; nothing ever extends it.
//
// The ceiling exists because the keys are operator-supplied strings — a URL, or the hash
// of a stored document — and a map nothing bounds is a memory leak with a tenant-shaped
// knob on it. An evicted entry costs one fetch, which is what a cache is for.
const (
	metadataTTL      = time.Hour
	metadataMax      = 64
	metadataTimeout  = 10 * time.Second
	metadataMaxBytes = 1 << 20
)

// cachedIdP is one entry. An inline document is keyed by a hash of its own bytes, so the
// value is that document forever and `expiresAt` is zero; a document fetched from a URL
// is keyed by the URL, which outlives the document it named, and carries the moment this
// installation stops vouching for the copy.
type cachedIdP struct {
	descriptor *saml.EntityDescriptor
	expiresAt  time.Time
}

// trackedCookie remembers which AuthnRequests *this browser* started, so that an
// assertion presented without one — an IdP-initiated POST, or a forced cross-site
// POST — is refused.
//
// SameSite is None rather than the Lax the OIDC state cookie uses, and that is the
// one cookie attribute the ACS's whole defense turns on: the browser arrives at the
// callback by a POST the IdP's form makes, which is a cross-site write, and a Lax
// cookie is not attached to it. HttpOnly and the __Host- prefix mean an attacker
// cannot read the id or set one for a victim's host, so the ids in here are
// unguessable by anybody but the browser that received them. kit/httpx's CSRF gate
// cannot cover the ACS at all — an anonymous cross-site POST carries no session
// cookie to compare against — which is exactly why this cookie exists and why
// IdP-initiated sign-in is refused rather than allowed with less checking.
const (
	trackedCookie = "platformkit_saml_request"
	trackedMax    = 5
	trackedTTL    = 10 * time.Minute
)

// of is this request's provider: the tenant's row, or nothing. There is no
// installation-level SAML default to fall back to, so the port's answer is the whole
// answer, and "this company has no SAML" is a 404 rather than a 500 for the reason
// OIDC's is.
func (p *SAML) of(ctx context.Context, tx db.Tx[db.Tenant]) (contracts.SAMLProvider, bool, error) {
	if p.providers == nil {
		return contracts.SAMLProvider{}, false, nil
	}
	settings, ok, err := p.providers.ProviderOf(ctx, tx)
	if err != nil || !ok || settings == nil {
		return contracts.SAMLProvider{}, false, err
	}
	return *settings, true, nil
}

// sp builds the service provider for one request: this tenant's entity ID and IdP,
// addressed at the host this request arrived at.
//
// The host comes from the request and never from the payload or a parameter, which
// is what makes an assertion minted for another tenant fail its recipient check here
// rather than being accepted and attributed to the wrong customer.
func (p *SAML) sp(cfg contracts.SAMLProvider, idp *saml.EntityDescriptor, host string) *saml.ServiceProvider {
	scheme := "https"
	if !p.secure {
		scheme = "http"
	}
	acs := url.URL{Scheme: scheme, Host: host, Path: p.acsPath}
	meta := url.URL{Scheme: scheme, Host: host, Path: p.metadataPath}
	return &saml.ServiceProvider{
		EntityID:    cfg.EntityID,
		MetadataURL: meta,
		AcsURL:      acs,
		IDPMetadata: idp,
		// HTTP-Redirect for the request out, POST for the assertion back: the two
		// bindings every real IdP offers, and the pair the tenant's metadata is
		// refused at the write unless it names a location for.
		AuthnNameIDFormat: saml.EmailAddressNameIDFormat,
		// This tenant's entity ID is the only audience, and it has to be named.
		ValidateAudienceRestriction: audienceOf(cfg.EntityID),
	}
}

// audienceOf is the audience rule this installation writes in place of the library's:
// the assertion must name this tenant's entity ID as one of its audiences, and an
// assertion that names no audience at all names nobody.
//
// The rule is spelled here because the default two lines above is weaker than this
// module's: `service_provider.go:1247` opens with
// `audienceRestrictionsValid := len(assertion.Conditions.AudienceRestrictions) == 0`,
// so a document stripped of its `<AudienceRestriction>` — signed, from an IdP this
// tenant trusts, and addressed to nobody — reads as addressed here. That is the
// forwarded assertion at its cheapest: one signed document from a shared IdP, posted at
// whichever tenant's ACS the attacker chose, with nothing in its bytes saying who it is
// for. `ValidateAudienceRestriction` is the library's own door for a stricter rule, and
// what it replaces is the weaker default rather than a check of ours bolted on after.
func audienceOf(entityID string) func(*saml.Assertion) error {
	return func(assertion *saml.Assertion) error {
		if assertion.Conditions == nil {
			return fmt.Errorf("assertion carries no conditions, so it names no audience")
		}
		for _, restriction := range assertion.Conditions.AudienceRestrictions {
			if restriction.Audience.Value == entityID {
				return nil
			}
		}
		return fmt.Errorf("assertion names no audience equal to %q", entityID)
	}
}

// idpMetadata resolves the document that names who may vouch for an address here.
//
// Inline XML wins over the URL when both are stored, so an IdP whose metadata
// endpoint is offline still signs people in — the case the tenant's row keeps the
// document for. A URL is fetched, cached under itself for as long as the entry is
// vouched for (see metadataTTL), and fetched again after that; a fetch failure is a 503
// that writes nothing, which is `discover`'s answer to a wrong issuer.
func (p *SAML) idpMetadata(ctx context.Context, cfg contracts.SAMLProvider) (*saml.EntityDescriptor, error) {
	if cfg.MetadataXML != "" {
		key := "sha256:" + fmt.Sprintf("%x", sha256.Sum256([]byte(cfg.MetadataXML)))
		if found, ok := p.cached(key); ok {
			return found, nil
		}
		parsed, err := samlsp.ParseMetadata([]byte(cfg.MetadataXML))
		if err != nil {
			return nil, problem.New(http.StatusServiceUnavailable, "this tenant's SAML metadata does not parse")
		}
		p.remember(key, parsed, time.Time{})
		return parsed, nil
	}
	if cfg.MetadataURL == "" {
		return nil, problem.New(http.StatusServiceUnavailable, "this tenant's SAML provider names no metadata")
	}
	if found, ok := p.cached(cfg.MetadataURL); ok {
		return found, nil
	}
	u, err := url.Parse(cfg.MetadataURL)
	if err != nil {
		return nil, problem.New(http.StatusServiceUnavailable, "this tenant's SAML metadata URL cannot be parsed")
	}
	fetched, err := samlsp.FetchMetadata(ctx, p.httpClient, *u)
	if err != nil {
		return nil, problem.New(http.StatusServiceUnavailable, "this tenant's SAML identity provider cannot be reached right now")
	}
	p.remember(cfg.MetadataURL, fetched, p.freshFor(fetched))
	return fetched, nil
}

// freshFor is how long a freshly fetched document is the answer: this installation's
// bound, cut short by the document's own `ValidUntil` when it carries one.
func (p *SAML) freshFor(descriptor *saml.EntityDescriptor) time.Time {
	until := db.Now().Add(p.ttl)
	if v := descriptor.ValidUntil; !v.IsZero() && v.Before(until) {
		// A document that expired before it arrived is not cached at all: an entry
		// already stale is a fetch skipped for a provider nobody has checked on.
		return v
	}
	return until
}

// boundedBody is the metadata fetch's ceiling on what one document may weigh. The
// library reads the response body itself, so the only place a caller of it can bound the
// read is under the transport: the body is read here, to the byte, and a document past
// the ceiling is an error rather than a slice of memory nobody asked for. The address is
// operator-supplied, so this is a mistaken URL rather than an attack; a deadline without
// a ceiling would still let one answer the process out.
type boundedBody struct{ next http.RoundTripper }

func (b boundedBody) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := b.next.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, metadataMaxBytes+1))
	if err != nil || int64(len(body)) > metadataMaxBytes {
		if err == nil {
			err = fmt.Errorf("metadata over %d bytes", metadataMaxBytes)
		}
		return nil, err
	}
	res.Body = io.NopCloser(bytes.NewReader(body))
	res.ContentLength = int64(len(body))
	return res, nil
}

func (p *SAML) cached(key string) (*saml.EntityDescriptor, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	found, ok := p.idps[key]
	if !ok {
		return nil, false
	}
	if !found.expiresAt.IsZero() && !found.expiresAt.After(db.Now()) {
		delete(p.idps, key)
		return nil, false
	}
	return found.descriptor, true
}

// remember keeps one document, evicting whatever has already aged out and, if that is not
// enough, the entry whose own lease ends soonest.
func (p *SAML) remember(key string, idp *saml.EntityDescriptor, expiresAt time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !expiresAt.IsZero() && !expiresAt.After(db.Now()) {
		// A document whose lease had already run out when it arrived is not worth a
		// map entry: whoever asks next is going to fetch again anyway.
		delete(p.idps, key)
		return
	}
	if len(p.idps) >= metadataMax {
		now := db.Now()
		oldest, oldestKey := expiresAt, ""
		for k, entry := range p.idps {
			if !entry.expiresAt.IsZero() && entry.expiresAt.Before(now) {
				delete(p.idps, k)
				continue
			}
			if oldestKey == "" || (!entry.expiresAt.IsZero() && entry.expiresAt.Before(oldest)) {
				oldest, oldestKey = entry.expiresAt, k
			}
		}
		if oldestKey != "" {
			delete(p.idps, oldestKey)
		}
	}
	p.idps[key] = cachedIdP{descriptor: idp, expiresAt: expiresAt}
}

// RegisterSAMLRoutes mounts the three SAML legs: the request out, the assertion
// back, and this tenant's own metadata for an IdP administrator to configure
// against. They are registered only when a composition can resolve a tenant's
// provider at all.
//
// The service is this module's own and not the contract, for the reason
// `RegisterRegistrationRoutes` gives: the leg needs the counters behind it, which are
// what the assertion POST in front of a parser and an RSA verification is bounded by.
func RegisterSAMLRoutes(surfaces httpx.Surfaces, svc *Service, users contracts.Users, provisioner contracts.Provisioner, p *SAML) {
	app := surfaces.App
	p.acsPath = app.Path("/saml/callback")
	p.metadataPath = app.Path("/saml/metadata")

	httpx.Register(app, huma.Operation{
		OperationID: "auth-saml-start",
		Method:      http.MethodGet,
		Path:        "/saml/start",
		Summary:     "Begin SAML single sign-on",
		Description: "Redirects to this tenant's identity provider with a deflated SAMLRequest, and remembers the request id in the browser so the answer can be recognised as an answer to it.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusNotFound, http.StatusForbidden, http.StatusServiceUnavailable},
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*redirectOutput, error) {
		r, ok := httpx.RequestFrom(ctx)
		if !ok {
			return nil, problem.New(http.StatusInternalServerError, "")
		}
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		cfg, err := p.tenantProvider(ctx, tx)
		if err != nil {
			return nil, err
		}
		idp, err := p.idpMetadata(ctx, cfg)
		if err != nil {
			return nil, rest.Fault(err)
		}
		// The request is built and then redirected by hand, rather than through
		// MakeRedirectAuthenticationRequest, for one field: `req.ID` is the value the
		// IdP will echo in InResponseTo, and the ACS recognises an answer to it — the
		// door the tracking cookie and the refusal of IdP-initiated sign-in both stand
		// behind. The helper deflates the request into a URL and does not hand the id
		// back, so using it would mean tracking nothing and refusing everything.
		sp := p.sp(cfg, idp, r.Host)
		request, err := sp.MakeAuthenticationRequest(
			sp.GetSSOBindingLocation(saml.HTTPRedirectBinding), saml.HTTPRedirectBinding, saml.HTTPPostBinding)
		if err != nil {
			return nil, problem.New(http.StatusServiceUnavailable, "this tenant's SAML identity provider cannot be reached right now")
		}
		location, err := request.Redirect("", sp)
		if err != nil {
			return nil, problem.New(http.StatusServiceUnavailable, "this tenant's SAML identity provider cannot be reached right now")
		}
		return &redirectOutput{
			Status:    http.StatusSeeOther,
			Location:  location.String(),
			SetCookie: []http.Cookie{p.rememberRequest(r, request.ID)},
		}, nil
	})

	httpx.Register(app, huma.Operation{
		OperationID: "auth-saml-callback",
		Method:      http.MethodPost,
		// The Assertion Consumer Service: the address in this tenant's SP metadata,
		// which the IdP's administrator configured from /saml/metadata, and which
		// the browser posts a base64 SAMLResponse to.
		Path:        "/saml/callback",
		Summary:     "Finish SAML single sign-on",
		Description: "Verifies the assertion the IdP posted — its signature, its audience, its recipient, its window and the request it answers — spends its id so it cannot be presented twice, and opens the same session any other door opens. An unsigned assertion, one addressed to another tenant, and one already spent are each refused, and a refusal writes nothing at all: an assertion stays presentable until a session for it commits.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusNotFound, http.StatusForbidden, http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusServiceUnavailable},
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventLoggedIn}},
	}, httpx.Public(), func(ctx context.Context, in *struct{}) (*redirectOutput, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		r, _ := httpx.RequestFrom(ctx)
		cfg, err := p.tenantProvider(ctx, tx)
		if err != nil {
			return nil, err
		}
		// Ahead of the base64, the XML tree and the signature: what this bound buys
		// is the right not to do that work, and a limit that ran afterwards would
		// count the cost without refusing it. The tenant is resolved first because
		// the counter is the tenant's — one address spent against acme does not draw
		// down globex's — and the address is the peer's, never a header's.
		if !svc.limiter.AssertionPresented(ctx, ClientOf(r).IP) {
			return nil, problem.New(http.StatusTooManyRequests,
				"too many assertions from this address; wait a minute and try again")
		}
		encoded := r.PostFormValue("SAMLResponse")
		if encoded == "" {
			return nil, problem.New(http.StatusForbidden, "that request carried no SAML response")
		}
		response, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, problem.New(http.StatusForbidden, "that SAML response is not base64 XML")
		}
		idp, err := p.idpMetadata(ctx, cfg)
		if err != nil {
			return nil, rest.Fault(err)
		}
		sp := p.sp(cfg, idp, r.Host)
		// ParseXMLResponse and not ParseXMLArtifactResponse: the former requires a
		// signature on the assertion itself, the latter accepts a response-level one
		// and lets an unsigned assertion through underneath it. The brief's rule —
		// signed assertions are required — is this entry point, not a library default.
		// The library's own signature rule is weaker than this module's, and the
		// comment at `service_provider.go:1036` says so out loud: "since the request
		// has a signature, none of the Assertions need one". An IdP that signs the
		// Response envelope and not the assertion therefore verifies — which is the
		// exact document the brief refuses. `SPECIFY.md` read that branch the other
		// way, and the case that caught it is TestAnUnsignedSAMLAssertionIsRefused.
		// So the rule is spelled here: the assertion carries its own signature, or
		// nothing is parsed. Checking presence rather than re-verifying is sound
		// because the Response's enveloped signature covers the whole document — an
		// assertion signature stripped out of somebody else's Response invalidates
		// that Response's own — and when the Response is unsigned the library
		// verifies this signature itself.
		switch shape := assertionsIn(response); {
		case !shape.signed():
			return nil, problem.New(http.StatusForbidden, "that assertion is not signed")
		case !shape.boundToItsBearer():
			return nil, problem.New(http.StatusForbidden,
				"that assertion is not bound to whoever presents it, or names no window in which it is valid")
		}
		assertion, err := sp.ParseXMLResponse(response, p.tracked(r), sp.AcsURL)
		if err != nil {
			return nil, problem.New(http.StatusForbidden, "that assertion is not one this tenant may sign in with")
		}
		if assertion.ID == "" {
			return nil, problem.New(http.StatusForbidden, "that assertion names no id, so it can never be spent")
		}
		// Spent here, in this transaction, and nowhere else: the claim and the
		// session commit together, so a sign-in refused for a closed account leaves
		// the assertion presentable and a sign-in that succeeds spends it exactly
		// once. A second presentation of a spent assertion blocks on the unique index
		// until the first transaction ends, and then refuses.
		spent, err := spendAssertion(ctx, tx, assertion)
		if err != nil {
			return nil, rest.Fault(err)
		}
		if !spent {
			return nil, problem.New(http.StatusForbidden, "that assertion has already been used")
		}
		email := addressIn(assertion, cfg.EmailAttribute)
		if email == "" {
			return nil, problem.New(http.StatusForbidden,
				"that assertion carries no address in the attribute this tenant named")
		}
		user, err := users.ByEmail(ctx, tx, email)
		if errors.Is(err, crud.ErrNotFound) {
			// The tenant's registration mode, read from its own row a moment ago:
			// the same three answers as OIDC, because what an address a provider
			// vouched for means does not depend on which protocol did it.
			if cfg.RegistrationMode() != contracts.RegistrationProvision || provisioner == nil {
				return nil, problem.New(http.StatusForbidden, "there is no account here for that address")
			}
			id, made := provisioner.Provision(ctx, tx, email, email, cfg.Roles)
			if made != nil {
				return nil, rest.Fault(made)
			}
			if user, err = users.Get(ctx, tx, id); err != nil {
				return nil, rest.Fault(err)
			}
		}
		if err != nil {
			return nil, rest.Fault(err)
		}
		if user, err = users.ConfirmAddress(ctx, tx, user.ID, email); err != nil {
			return nil, refusedAtTheDoor(err)
		}
		session, _, err := svc.Open(ctx, tx, user.ID, ClientOf(r), contracts.ViaSAML)
		if err != nil {
			return nil, refusedAtTheDoor(err)
		}
		return &redirectOutput{
			Status: http.StatusSeeOther, Location: "/",
			SetCookie: []http.Cookie{
				p.cookies.Session(session.ID, session.ExpiresAt),
				p.cookies.Forget(trackedCookie),
			},
		}, nil
	})

	httpx.Register(app, huma.Operation{
		OperationID: "auth-saml-metadata",
		Method:      http.MethodGet,
		// Served per tenant host, from the row that host resolved: an IdP
		// administrator configuring two customers on one installation downloads two
		// documents naming two entity IDs and two ACS URLs. A process-global document
		// here would be one customer's metadata sent to another's IdP.
		Path:        "/saml/metadata",
		Summary:     "This tenant's service provider metadata",
		Description: "The EntityDescriptor an identity provider administrator configures against: this tenant's entity ID, and the assertion consumer service on this host. It is public and carries no secret — the SP is unsigned, so the document names no key.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusNotFound},
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*httpx.Page, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		r, _ := httpx.RequestFrom(ctx)
		cfg, err := p.tenantProvider(ctx, tx)
		if err != nil {
			return nil, err
		}
		body, err := xml.MarshalIndent(p.sp(cfg, &saml.EntityDescriptor{}, r.Host).Metadata(), "", "  ")
		if err != nil {
			return nil, rest.Fault(err)
		}
		return &httpx.Page{
			Status:      http.StatusOK,
			ContentType: "application/samlmetadata+xml",
			Body:        append([]byte(xml.Header), body...),
		}, nil
	})
}

// tenantProvider is the read every leg starts with, including the mode: a tenant
// that turned SAML off is refused before its metadata is parsed or its IdP dialled,
// so "disabled" is never a request that reaches a provider it will not use.
func (p *SAML) tenantProvider(ctx context.Context, tx db.Tx[db.Tenant]) (contracts.SAMLProvider, error) {
	cfg, ok, err := p.of(ctx, tx)
	switch {
	case err != nil:
		return contracts.SAMLProvider{}, rest.Fault(err)
	case !ok:
		return contracts.SAMLProvider{}, problem.New(http.StatusNotFound, "this tenant has no SAML single sign-on")
	case cfg.RegistrationMode() == contracts.RegistrationDisabled:
		return contracts.SAMLProvider{}, problem.New(http.StatusForbidden, "this tenant does not sign in with SAML")
	}
	return cfg, nil
}

// rememberRequest adds the id this request went out with to the browser's own list,
// most recent last, bounded so a person who starts a sign-in ten times does not grow
// a cookie without end.
func (p *SAML) rememberRequest(r *http.Request, id string) http.Cookie {
	ids := append(p.tracked(r), id)
	if len(ids) > trackedMax {
		ids = ids[len(ids)-trackedMax:]
	}
	value := base64.RawURLEncoding.EncodeToString([]byte(strings.Join(ids, " ")))
	return http.Cookie{
		Name: p.cookies.Name(trackedCookie), Value: value, Path: "/",
		MaxAge: int(trackedTTL.Seconds()), HttpOnly: true, Secure: p.secure,
		SameSite: http.SameSiteNoneMode,
	}
}

// tracked is the browser's list of requests it started, newest last. A missing or
// unreadable cookie is an empty list, which is what refuses an IdP-initiated
// assertion: nothing in the posted bytes can fill it.
func (p *SAML) tracked(r *http.Request) []string {
	if r == nil {
		return nil
	}
	c, err := r.Cookie(p.cookies.Name(trackedCookie))
	if err != nil {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return nil
	}
	var ids []string
	for _, id := range strings.Fields(string(raw)) {
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// spendAssertion claims (tenant, assertion id) for this transaction. It answers
// false when the pair is already there, which is the replay refusal, and the caller
// writes nothing further.
//
// The insert is the whole claim: the primary key is the pair, so two concurrent
// presentations of one assertion block on the index and exactly one of them gets a
// row. expires_at is the assertion's own NotOnOrAfter plus the library's clock skew,
// which is the last moment the library could still accept the document — protecting
// it past that is protecting nothing, and the hourly sweep takes the row then.
func spendAssertion(ctx context.Context, tx db.Tx[db.Tenant], assertion *saml.Assertion) (bool, error) {
	expires := db.Now().Add(saml.MaxClockSkew)
	if !assertion.Conditions.NotOnOrAfter.IsZero() {
		expires = assertion.Conditions.NotOnOrAfter.UTC().Add(saml.MaxClockSkew)
	}
	result := tx.DB().WithContext(ctx).Exec(
		"INSERT INTO saml_assertion_replays (tenant_id, assertion_id, claimed_at, expires_at) "+
			"VALUES (?, ?, now(), ?) ON CONFLICT DO NOTHING",
		db.TenantOf(tx).ID, assertion.ID, expires)
	if result.Error != nil {
		return false, fmt.Errorf("auth: claim that assertion: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

// assertionShape is what the document carries, read off its XML before the library is
// given it: how many assertions there are, and how many of them satisfy each rule this
// installation cannot let the library decide. Reading them in one pass over the same
// bytes is what lets the two refusals below name their own reason instead of sharing a
// vague one.
type assertionShape struct {
	found         int // <saml:Assertion> elements an ACS could try to read
	signatures    int // of those, the ones carrying their own enveloped signature
	confirmations int // of those, the ones confirmed for whoever presents them
	windows       int // of those, the ones carrying a <Conditions> element
}

// signed reports whether the document carries an assertion and every one of them is
// signed in its own right.
func (s assertionShape) signed() bool {
	return s.found > 0 && s.signatures == s.found
}

// boundToItsBearer reports whether every assertion names a subject confirmation in the
// bearer method and a validity window of its own.
//
// The library checks Recipient, InResponseTo and the confirmation's own expiry for
// every confirmation a document carries — and nothing at all when the list is empty:
// `service_provider.go:1188` guards that whole block with the loop over them. An
// assertion whose subject is a bare `<NameID>`, or bound holder-of-key or
// sender-vouches, therefore verifies against a certificate the tenant trusts while
// naming nobody who could present it. Neither could ever be consumed here: v1 holds no
// service provider key, so there is no certificate to hold a key against and no
// assertion is ever decrypted — the same reason an `EncryptedAssertion` is no assertion
// to the walk below. Refusing them at the structure is the true answer rather than a
// 403 three steps later, and it is what keeps the library's own checks from standing on
// a nil subject.
func (s assertionShape) boundToItsBearer() bool {
	return s.found > 0 && s.confirmations == s.found && s.windows == s.found
}

// assertionsIn reads an assertion document's shape. It is not a validator: it decides
// nothing about signatures, audiences or windows, which the service provider does with
// the tenant's IdP certificate in hand; it decides only what this installation will not
// ask anyone to read.
func assertionsIn(document []byte) assertionShape {
	var shape assertionShape
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(document); err != nil {
		return shape
	}
	var walk func(el *etree.Element)
	walk = func(el *etree.Element) {
		if el.Tag != "Assertion" || el.NamespaceURI() != assertionNS {
			for _, child := range el.ChildElements() {
				walk(child)
			}
			return
		}
		shape.found++
		for _, child := range el.ChildElements() {
			if child.NamespaceURI() != assertionNS && child.NamespaceURI() != signatureNS {
				continue
			}
			switch child.Tag {
			case "Signature":
				shape.signatures++
			case "Subject":
				if confirmedAsBearer(child) {
					shape.confirmations++
				}
			case "Conditions":
				shape.windows++
			}
		}
	}
	if root := doc.Root(); root != nil {
		walk(root)
	}
	return shape
}

// confirmedAsBearer is whether one <saml:Subject> binds the assertion to whoever
// presents it: at least one <SubjectConfirmation> whose Method is absent — SAML core
// 3.2.1 reads an absent Method as bearer — or spelled as bearer. Any other method is a
// binding this installation has no key to honour.
func confirmedAsBearer(subject *etree.Element) bool {
	for _, child := range subject.ChildElements() {
		if child.Tag != "SubjectConfirmation" || child.NamespaceURI() != assertionNS {
			continue
		}
		if method := child.SelectAttrValue("Method", ""); method == "" || method == bearerMethod {
			return true
		}
	}
	return false
}

// The namespaces a SAML document's own assertion and signature carry, and the one
// spelling of "bearer" worth matching. The library marshals them this way and every
// mainstream IdP does; a document that spells the namespaces as the default rather than
// as a prefix is caught here by namespace rather than assumed away, and an `EncryptedAssertion`
// is no assertion at all to this walk because v1 holds no service provider key to open it
// with — the refusal is the true one rather than a parse failure three steps later.
const (
	assertionNS  = "urn:oasis:names:tc:SAML:2.0:assertion"
	signatureNS  = "http://www.w3.org/2000/09/xmldsig#"
	bearerMethod = "urn:oasis:names:tc:SAML:2.0:cm:bearer"
)

// addressIn is the assertion's own words for the address: the first non-empty value
// of the attribute the tenant named, across every attribute statement the document
// carries.
//
// The NameID is never used as one. The attribute is the contract the tenant's
// directory and this installation struck at the write, and a subject format that
// changes at the IdP would otherwise change who may sign in without anything in this
// installation moving. No `email_verified` equivalent is sought either: within SAML,
// the tenant's own configuration of that attribute, backed by the signature the IdP's
// metadata certificate verifies, *is* the confirmation OIDC's claim stands for.
func addressIn(assertion *saml.Assertion, attribute string) string {
	if attribute == "" {
		return ""
	}
	for _, statement := range assertion.AttributeStatements {
		for _, attr := range statement.Attributes {
			if attr.Name != attribute {
				continue
			}
			for _, value := range attr.Values {
				if email := strings.TrimSpace(value.Value); email != "" {
					return email
				}
			}
		}
	}
	return ""
}
