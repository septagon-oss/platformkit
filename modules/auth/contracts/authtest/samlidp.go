package authtest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/xml"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
)

// SAMLIdP is an in-process SAML 2.0 identity provider a test can sign in against:
// metadata at one address, and an SSO endpoint that answers a redirect-binding request
// with the base64 `SAMLResponse` and the ACS URL it was addressed to.
//
// It is built from the same library the service provider is built from —
// `saml.IdentityProvider`'s own `Validate`/`MakeAssertion`/`PostBinding` path — because
// the claim a stand-in here has to carry is the *signature*: an assertion the service
// provider accepts must verify against the certificate in the metadata the tenant's row
// holds, and a hand-written document could not make that claim.
//
// The refusals are produced by construction rather than by a bug: sign the Response and
// not the assertion, address the audience at another tenant, name another host as the
// recipient, move the validity window. The issuer is never a knob — it is the
// document's own entity ID, which is the only way the check that reads it means
// anything.
//
// The package may name an SDK for the reason `Issuer` gives beside it: a double that
// signs things cannot be written without a signing library, and nothing that ships
// imports this one.
type SAMLIdP struct {
	*httptest.Server
	idp *saml.IdentityProvider

	mu           sync.Mutex
	trusted      map[string]*saml.EntityDescriptor
	address      string
	attribute    string
	audience     string
	recipient    string
	notBefore    time.Time
	notOnOrAfter time.Time
	unsigned     bool
	metadataHits int
}

// NewSAMLIdP starts one, generating its own key and self-signed certificate, and closes
// it when the test ends. That certificate is what lands in the metadata document, which
// is what the tenant's row stores, which is what an assertion's signature is checked
// against — the whole chain, from a key nobody but this test holds.
func NewSAMLIdP(t interface {
	Helper()
	Fatalf(string, ...any)
	Cleanup(func())
}) *SAMLIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("authtest: generate the identity provider's key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "platformkit-test-identity-provider"},
		NotBefore:    time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("authtest: self-sign the identity provider's certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("authtest: read the identity provider's certificate back: %v", err)
	}

	provider := &SAMLIdP{idp: &saml.IdentityProvider{
		Key: key, Signer: key, Certificate: cert, Logger: quiet{},
	}}
	provider.idp.ServiceProviderProvider = provider
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serve))
	// The metadata URL doubles as the entity ID — the library derives one from the
	// other when none is set, and an issuer that is a URL is the ordinary shape for a
	// hosted IdP. Both are only nameable once the listener exists.
	host := provider.Server.Listener.Addr().String()
	provider.idp.MetadataURL = url.URL{Scheme: "http", Host: host, Path: "/metadata"}
	provider.idp.SSOURL = url.URL{Scheme: "http", Host: host, Path: "/sso"}
	t.Cleanup(provider.Server.Close)
	return provider
}

// MetadataURL is what an operator would paste at the tenant's control-plane route.
func (i *SAMLIdP) MetadataURL() string { return i.idp.MetadataURL.String() }

// MetadataXML fetches that document over HTTP rather than writing one by hand, so what
// a tenant's row holds is byte-for-byte what an IdP serves and what the certificate
// check consumes.
func (i *SAMLIdP) MetadataXML(t interface {
	Helper()
	Fatalf(string, ...any)
}) string {
	t.Helper()
	res, err := http.Get(i.MetadataURL())
	if err != nil {
		t.Fatalf("authtest: fetch the identity provider's metadata: %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("authtest: read the identity provider's metadata: %v", err)
	}
	return string(body)
}

// MetadataHits counts how many times the metadata endpoint was dialled, which is how a
// case says "this refusal happened before anything was dialled".
func (i *SAMLIdP) MetadataHits() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.metadataHits
}

// Trust is the registration step: an administrator downloaded this tenant's service
// provider metadata and gave it to the IdP, so the IdP knows where to send an
// assertion and for whose entity ID.
func (i *SAMLIdP) Trust(entityID string, descriptor *saml.EntityDescriptor) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.trusted == nil {
		i.trusted = map[string]*saml.EntityDescriptor{}
	}
	i.trusted[entityID] = descriptor
}

// GetServiceProvider is saml.ServiceProviderProvider. An entity ID nobody registered
// answers os.ErrNotExist, which is what makes an unregistered service provider a
// refusal at the IdP rather than an assertion addressed nowhere.
func (i *SAMLIdP) GetServiceProvider(_ *http.Request, serviceProviderID string) (*saml.EntityDescriptor, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if found, ok := i.trusted[serviceProviderID]; ok {
		return found, nil
	}
	return nil, fmt.Errorf("authtest: no service provider registered as %q: %w", serviceProviderID, os.ErrNotExist)
}

// SignInAs says who the person is and which attribute carries the address. The
// attribute name is the tenant's configuration, so a case that names a different one is
// testing the refusal that says so.
func (i *SAMLIdP) SignInAs(email, attribute string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.address, i.attribute = email, attribute
}

// AudienceIs addresses the assertion at somebody else's entity ID: the forwarded
// assertion, signed by a valid IdP for a valid audience that is not this one.
func (i *SAMLIdP) AudienceIs(entityID string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.audience = entityID
}

// RecipientIs names another assertion consumer service in the subject confirmation —
// the same audience, a different door. The two knobs are separate because the two
// refusals are separate rules, and a case that moved both could not tell them apart.
func (i *SAMLIdP) RecipientIs(acsURL string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.recipient = acsURL
}

// Window moves the validity period. A `NotOnOrAfter` inside the library's 180-second
// skew must still be accepted and one outside it refused, which is a pair only the
// caller can choose.
func (i *SAMLIdP) Window(notBefore, notOnOrAfter time.Time) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.notBefore, i.notOnOrAfter = notBefore, notOnOrAfter
}

// UnsignedAssertion is the mis-configuration the brief's first refusal names: the IdP
// signs the Response envelope and not the assertion inside it — which is exactly the
// document `ParseXMLArtifactResponse` would accept, and the reason the service provider
// calls `ParseXMLResponse` instead.
func (i *SAMLIdP) UnsignedAssertion() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.unsigned = true
}

func (i *SAMLIdP) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/metadata":
		i.mu.Lock()
		i.metadataHits++
		i.mu.Unlock()
		body, err := xml.MarshalIndent(i.idp.Metadata(), "", "  ")
		if err != nil {
			http.Error(w, fmt.Sprintf("authtest: marshal the metadata: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/samlmetadata+xml")
		w.Write(append([]byte(xml.Header), body...))
	case "/sso":
		i.serveSSO(w, r)
	default:
		http.NotFound(w, r)
	}
}

// serveSSO answers one AuthnRequest with two plain-text lines: the ACS URL it built the
// response for and the base64 `SAMLResponse`. Not the auto-submitting HTML form the
// library's own `WriteResponse` sends, because posting that response is the thing under
// test and a test has to hold the bytes it posts.
func (i *SAMLIdP) serveSSO(w http.ResponseWriter, r *http.Request) {
	request, err := saml.NewIdpAuthnRequest(i.idp, r)
	if err != nil {
		http.Error(w, fmt.Sprintf("authtest: parse the authn request: %v", err), http.StatusBadRequest)
		return
	}
	if err := request.Validate(); err != nil {
		http.Error(w, fmt.Sprintf("authtest: validate the authn request: %v", err), http.StatusBadRequest)
		return
	}
	i.mu.Lock()
	address, attribute := i.address, i.attribute
	audience, recipient := i.audience, i.recipient
	notBefore, notOnOrAfter, unsigned := i.notBefore, i.notOnOrAfter, i.unsigned
	i.mu.Unlock()

	session := &saml.Session{
		ID: "session", CreateTime: time.Now(), ExpireTime: time.Now().Add(time.Hour),
		Index: "index", NameID: address, UserEmail: address,
	}
	maker := saml.DefaultAssertionMaker{}
	if err := maker.MakeAssertion(request, session); err != nil {
		http.Error(w, fmt.Sprintf("authtest: make the assertion: %v", err), http.StatusInternalServerError)
		return
	}
	if attribute != "" {
		request.Assertion.AttributeStatements[0].Attributes = append(
			request.Assertion.AttributeStatements[0].Attributes, saml.Attribute{
				Name:   attribute,
				Values: []saml.AttributeValue{{Type: "xs:string", Value: address}},
			})
	}
	if audience != "" && len(request.Assertion.Conditions.AudienceRestrictions) > 0 {
		request.Assertion.Conditions.AudienceRestrictions[0].Audience.Value = audience
	}
	if recipient != "" && len(request.Assertion.Subject.SubjectConfirmations) > 0 {
		request.Assertion.Subject.SubjectConfirmations[0].SubjectConfirmationData.Recipient = recipient
	}
	if !notOnOrAfter.IsZero() {
		request.Assertion.Conditions.NotOnOrAfter = notOnOrAfter
		for _, confirmation := range request.Assertion.Subject.SubjectConfirmations {
			confirmation.SubjectConfirmationData.NotOnOrAfter = notOnOrAfter
		}
	}
	if !notBefore.IsZero() {
		request.Assertion.Conditions.NotBefore = notBefore
	}

	if unsigned {
		// The assertion goes into the document unsigned and the Response is still
		// signed over it: not a signing that failed, but the document an IdP with
		// assertion signing switched off really sends.
		request.AssertionEl = request.Assertion.Element()
	} else if err := request.MakeAssertionEl(); err != nil {
		http.Error(w, fmt.Sprintf("authtest: sign the assertion: %v", err), http.StatusInternalServerError)
		return
	}
	form, err := request.PostBinding()
	if err != nil {
		http.Error(w, fmt.Sprintf("authtest: build the response: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "%s\n%s", form.URL, form.SAMLResponse)
}

// IdPMetadata is the tenant module's write-time rule, exposed so a case that configures
// a tenant from this document proves the document is storable before it proves anything
// about a sign-in.
func IdPMetadata(document string) (*saml.EntityDescriptor, error) {
	return samlsp.ParseMetadata([]byte(document))
}

// quiet is the stand-in's logger: its noise is not what a case asserts, and every
// failure it could report is answered as an HTTP status the test reads instead. The
// interface is the library's own, nine methods wide, and this is all of it.
type quiet struct{}

func (quiet) Printf(string, ...any) {}
func (quiet) Print(...any)          {}
func (quiet) Println(...any)        {}
func (quiet) Fatal(...any)          {}
func (quiet) Fatalf(string, ...any) {}
func (quiet) Fatalln(...any)        {}
func (quiet) Panic(...any)          {}
func (quiet) Panicf(string, ...any) {}
func (quiet) Panicln(...any)        {}
