package internal

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// errAddressRefused is the one error the dial gate returns that means "never
// try this again": the attempt it happens on is dropped rather than failed, no
// retry is queued, and the endpoint is not disabled. An address this deployment
// will not open a socket to is not a fault that might clear; retrying it for
// three days would spend somebody's queue to learn nothing.
var errAddressRefused = errors.New("webhook: the address is not a public endpoint")

// errSchemeDowngrade is the redirect half of the same decision, and it is
// refused for the reason the signature exists at all: the endpoint was
// registered as an HTTPS address, and a receiver that hands the attempt to a
// plain-HTTP one has moved where the signed event and its body are read. The
// hop is refused rather than the event sent unlabelled, because the receiver
// that asked for TLS is the receiver whose administrator would be surprised to
// find the payload on the wire in the clear.
var errSchemeDowngrade = errors.New("webhook: the endpoint redirects to a plain HTTP address")

// RedirectRefused reports whether a send failure is that refusal, which the
// outcome transaction reads as an attempt that must never be sent again.
func RedirectRefused(err error) bool { return errors.Is(err, errSchemeDowngrade) }

// AddressPolicy is the SSRF gate, one value read by both checks so they cannot
// disagree: the write that stores an address and the dial that opens a socket to
// it.
//
// AllowLocal is deployment-level. There is no column, no field and no route that
// carries it, so a tenant cannot open the gate for itself; it exists because a
// developer's laptop and this repository's own tests run receivers on 127.0.0.1,
// and a module that made that impossible would be a module nobody could try.
//
// What it does not reach is the always-refused class below, and the reason is the
// name on the door: the flag says "this deployment dials a local receiver", not
// "this deployment will POST to its own cloud provider's metadata service".
type AddressPolicy struct {
	AllowLocal bool

	// Resolver is how a hostname becomes addresses. nil is the system's.
	Resolver *net.Resolver

	// Roots is the trust store TLS verifies against. nil is the platform's own.
	Roots *x509.CertPool
}

// alwaysRefused are the addresses no deployment of this platform dials: the
// unspecified address, multicast, and every link-local network — 169.254.0.0/16
// and fe80::/10 — which is where each cloud provider's instance metadata service
// lives. A request that reaches one of those from a tenant-supplied URL is the
// attack this whole file exists to stop, whatever the deployment's flag.
var alwaysRefused = []netip.Prefix{
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("::/128"),
}

// localRefused are the ranges a deployment may open: loopback, the RFC1918 and
// unique-local private ranges, and 100.64.0.0/10 — the carrier-grade NAT block,
// which netip's own IsPrivate does not cover and which is a private network in
// every sense that matters to a webhook.
var localRefused = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("fc00::/7"),
}

// refusal names the rule this address breaks, or "" for "dial it". It reads only
// an address, which is what lets the write-time check on a literal and the
// dial-time check on a resolved address be the same function.
func (p AddressPolicy) refusal(ip netip.Addr) string {
	ip = ip.WithZone("")
	if !ip.IsValid() {
		return "the address is not an address"
	}
	// An IPv4 address written inside an IPv6 one is the same address, and a
	// policy that checked only the v6 form would let ::ffff:169.254.169.254
	// through the very check it exists for.
	ip = ip.Unmap()
	for _, r := range alwaysRefused {
		if r.Contains(ip) {
			return fmt.Sprintf("%s is a link-local or unspecified address", ip)
		}
	}
	if ip.IsMulticast() {
		return fmt.Sprintf("%s is a multicast address", ip)
	}
	if ip.IsLoopback() || ip.IsPrivate() || containsAny(localRefused, ip) {
		if p.AllowLocal {
			return ""
		}
		return fmt.Sprintf("%s is not a public address", ip)
	}
	return ""
}

func containsAny(set []netip.Prefix, ip netip.Addr) bool {
	for _, r := range set {
		if r.Contains(ip) {
			return true
		}
	}
	return false
}

// Refuse is the write-time half: the reason this stored address could never be
// dialled, or "" when it could. It runs on the literal, which is all a write has
// — a name that resolves to 127.0.0.1 tomorrow is the dial gate's to catch, and
// it does, on every attempt, from the resolved answer.
func (p AddressPolicy) Refuse(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "the URL does not parse"
	}
	host := strings.Trim(u.Hostname(), "[]")
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return "" // a hostname is checked when it is resolved, which is every time
	}
	return p.refusal(ip)
}

// transport is the client's transport: every socket goes through the gate, and
// so does every redirect hop, because CheckRedirect follows inside the same
// transport and the gate reads the address it is dialling rather than the URL it
// was handed.
func (p AddressPolicy) transport(timeout time.Duration) *http.Transport {
	resolver := p.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &http.Transport{
		DialContext:           p.dialContext(resolver),
		TLSClientConfig:       &tls.Config{RootCAs: p.Roots, MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

// refuseHop is the redirect gate, asked of every hop before the socket opens:
// the same policy value that decides the dial decides the redirect, so the two
// cannot disagree about what this deployment will carry a signed event over.
//
// The rule is one sentence — an event asked for of an HTTPS address is not
// carried to a plain-HTTP one. AllowLocal does not weaken it: the flag says
// "this deployment dials a local receiver", and the test's own loopback HTTP
// server is exactly the address the flag permits *as a destination* and still
// refuses *as a downgrade*. A redirect that stays on TLS, to any host the dial
// gate will open, is followed up to maxRedirects hops; past that the caller
// sees the last redirect answer, which is a status like any other.
func (p AddressPolicy) refuseHop(to *url.URL, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return http.ErrUseLastResponse
	}
	if to == nil || len(via) == 0 {
		return nil
	}
	if via[0].URL.Scheme == "https" && to.Scheme != "https" {
		return fmt.Errorf("%w: %s", errSchemeDowngrade, to)
	}
	return nil
}

// dialContext resolves first and refuses by the answer, then dials that address
// rather than the name again: a second lookup would be a second decision, and the
// rebinding an attacker waits for is exactly the gap between the two.
func (p AddressPolicy) dialContext(resolver *net.Resolver) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		target := addr
		if ip, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
			if reason := p.refusal(ip); reason != "" {
				return nil, fmt.Errorf("%w: %s", errAddressRefused, reason)
			}
		} else {
			ips, err := resolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("webhook: %s does not resolve: %w", host, err)
			}
			var refused []string
			for _, candidate := range ips {
				addr, err := netip.ParseAddr(candidate.IP.String())
				if err != nil {
					refused = append(refused, candidate.IP.String()+" is not an address this gate can read")
					continue
				}
				if reason := p.refusal(addr); reason != "" {
					refused = append(refused, reason)
					continue
				}
				target = net.JoinHostPort(candidate.IP.String(), port)
				break
			}
			if target == addr {
				if len(refused) == 0 {
					refused = []string{"it has no address"}
				}
				return nil, fmt.Errorf("%w: %s: %s", errAddressRefused, host, strings.Join(refused, "; "))
			}
		}
		return dialer.DialContext(ctx, network, target)
	}
}

// AddressRefused reports whether a send failure is the policy's refusal rather
// than somebody's outage, which is the difference between an attempt that is
// dropped and one that joins the retry ladder.
func AddressRefused(err error) bool { return errors.Is(err, errAddressRefused) }
