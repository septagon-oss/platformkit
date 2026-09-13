// Package ofrep connects flags to an explicitly configured OFREP endpoint.
package ofrep

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

	ofrepsdk "github.com/open-feature/go-sdk-contrib/providers/ofrep"
	sdk "github.com/open-feature/go-sdk/openfeature"
	"github.com/septagon-oss/platformkit/kit/flags"
	"github.com/septagon-oss/platformkit/kit/flags/providers/openfeature"
)

// Config selects an existing OFREP service. TLS uses system roots or the
// supplied CA file. Insecure permits HTTP on an explicit loopback endpoint only.
type Config struct {
	Scope           flags.Scope
	URL             string
	CertificatePath string
	BearerToken     string
	Insecure        bool
	Timeout         time.Duration
}

// New validates local configuration without contacting the service.
// Construction is not readiness: the first evaluation establishes reachability.
// Redirects and ambient proxy/provider configuration are not used.
func New(ctx context.Context, config Config) (*openfeature.Evaluator, error) {
	endpoint, err := url.Parse(config.URL)
	if err != nil || !config.Scope.Valid() || config.Timeout <= 0 || endpoint.Hostname() == "" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || strings.Contains(config.URL, "#") || endpoint.Opaque != "" {
		return nil, flags.ErrInvalid
	}
	if config.Insecure {
		if endpoint.Scheme != "http" || config.CertificatePath != "" || (endpoint.Hostname() != "localhost" && !net.ParseIP(endpoint.Hostname()).IsLoopback()) {
			return nil, flags.ErrInvalid
		}
	} else if endpoint.Scheme != "https" {
		return nil, flags.ErrInvalid
	}
	if strings.ContainsFunc(config.BearerToken, func(c rune) bool { return c < 33 || c > 126 }) {
		return nil, flags.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var roots *x509.CertPool
	if config.CertificatePath != "" {
		certificate, err := os.ReadFile(config.CertificatePath)
		if err != nil {
			return nil, flags.ErrInvalid
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(certificate) {
			return nil, flags.ErrInvalid
		}
	}
	transport := &http.Transport{
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
		TLSHandshakeTimeout: config.Timeout, ResponseHeaderTimeout: config.Timeout,
		IdleConnTimeout: 30 * time.Second, ForceAttemptHTTP2: true,
	}
	client := &http.Client{Transport: transport, Timeout: config.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	options := []ofrepsdk.Option{ofrepsdk.WithClient(client)}
	if config.BearerToken != "" {
		options = append(options, ofrepsdk.WithBearerToken(config.BearerToken))
	}
	provider := &ofrepProvider{Provider: ofrepsdk.NewProvider(endpoint.String(), options...), transport: transport}
	evaluator, err := openfeature.New(ctx, config.Scope, provider, config.Timeout)
	if err != nil {
		transport.CloseIdleConnections()
	}
	return evaluator, err
}

type ofrepProvider struct {
	*ofrepsdk.Provider
	transport *http.Transport
}

func (p *ofrepProvider) Init(sdk.EvaluationContext) error { return nil }
func (p *ofrepProvider) Shutdown()                        { p.transport.CloseIdleConnections() }

func (p *ofrepProvider) BooleanEvaluation(ctx context.Context, key string, fallback bool, subject sdk.FlattenedContext) sdk.BoolResolutionDetail {
	// The upstream provider joins the key into a URL path without escaping it.
	// Restrict this adapter's keys to a single, unambiguous path segment.
	if key == "" || key == "." || key == ".." || strings.ContainsFunc(key, func(c rune) bool {
		return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.')
	}) {
		return sdk.BoolResolutionDetail{Value: fallback, ProviderResolutionDetail: sdk.ProviderResolutionDetail{
			ResolutionError: sdk.NewInvalidContextResolutionError("invalid flag key"),
		}}
	}
	return p.Provider.BooleanEvaluation(ctx, key, fallback, subject)
}
