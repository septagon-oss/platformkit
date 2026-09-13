package flags

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/open-feature/go-sdk-contrib/providers/ofrep"
	"github.com/open-feature/go-sdk/openfeature"
)

// OFREPConfig selects an existing OFREP service. TLS uses system roots or the
// supplied CA file. Insecure permits HTTP on an explicit loopback endpoint only.
type OFREPConfig struct {
	Scope           Scope
	URL             string
	CertificatePath string
	BearerToken     string
	Insecure        bool
	Timeout         time.Duration
}

// NewOFREP validates local configuration without contacting the service.
// Construction is not readiness: the first evaluation establishes reachability.
// Redirects and ambient proxy/provider configuration are not used.
func NewOFREP(ctx context.Context, config OFREPConfig) (*OpenFeature, error) {
	endpoint, err := url.Parse(config.URL)
	if err != nil || !validScope(config.Scope) || config.Timeout <= 0 || endpoint.Hostname() == "" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || strings.Contains(config.URL, "#") || endpoint.Opaque != "" {
		return nil, ErrInvalid
	}
	if config.Insecure {
		if endpoint.Scheme != "http" || config.CertificatePath != "" || (endpoint.Hostname() != "localhost" && !net.ParseIP(endpoint.Hostname()).IsLoopback()) {
			return nil, ErrInvalid
		}
	} else if endpoint.Scheme != "https" {
		return nil, ErrInvalid
	}
	if strings.ContainsFunc(config.BearerToken, func(c rune) bool { return c < 33 || c > 126 }) {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var roots *x509.CertPool
	if config.CertificatePath != "" {
		certificate, err := os.ReadFile(config.CertificatePath)
		if err != nil {
			return nil, ErrInvalid
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(certificate) {
			return nil, ErrInvalid
		}
	}
	transport := &http.Transport{
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
		TLSHandshakeTimeout: config.Timeout, ResponseHeaderTimeout: config.Timeout,
		IdleConnTimeout: 30 * time.Second, ForceAttemptHTTP2: true,
	}
	client := &http.Client{Transport: transport, Timeout: config.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	options := []ofrep.Option{ofrep.WithClient(client)}
	if config.BearerToken != "" {
		options = append(options, ofrep.WithBearerToken(config.BearerToken))
	}
	provider := &ofrepProvider{Provider: ofrep.NewProvider(endpoint.String(), options...), transport: transport}
	evaluator, err := NewOpenFeature(ctx, config.Scope, provider, config.Timeout)
	if err != nil {
		transport.CloseIdleConnections()
	}
	return evaluator, err
}

type ofrepProvider struct {
	*ofrep.Provider
	transport *http.Transport
}

func (p *ofrepProvider) Init(openfeature.EvaluationContext) error { return nil }
func (p *ofrepProvider) Shutdown()                                { p.transport.CloseIdleConnections() }

func (p *ofrepProvider) BooleanEvaluation(ctx context.Context, key string, fallback bool, subject openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	// The upstream provider joins the key into a URL path without escaping it.
	// Restrict this adapter's keys to a single, unambiguous path segment.
	if key == "" || key == "." || key == ".." || strings.ContainsFunc(key, func(c rune) bool {
		return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.')
	}) {
		return openfeature.BoolResolutionDetail{Value: fallback, ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
			ResolutionError: openfeature.NewInvalidContextResolutionError("invalid flag key"),
		}}
	}
	return p.Provider.BooleanEvaluation(ctx, key, fallback, subject)
}
