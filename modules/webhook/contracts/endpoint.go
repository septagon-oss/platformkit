// Package contracts is everything another module, an app or a test may know
// about outbound webhooks: the two entities, the events, the permissions, the
// Service interface and the two ports this module needs from the composition.
//
// A webhook endpoint is an HTTPS address a tenant registered so that a machine
// of its own can hear what happened here. Its point is that the customer never
// polls: the platform already knows an order was paid, and this module is the
// way that fact leaves.
package contracts

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"database/sql/driver"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/events"
)

// The two lifecycle states an endpoint is in. Only a route moves an endpoint
// between them — the generic PATCH refuses the field — because a disabled
// endpoint stops receiving, and a change that stops the flow of events to a
// customer has to be an act somebody performed rather than a field somebody
// happened to send.
const (
	StatusEnabled  = "enabled"
	StatusDisabled = "disabled"
)

var statuses = []string{StatusEnabled, StatusDisabled}

// The three reasons an endpoint stopped. They are words an administrator reads,
// written where the decision was made, and nothing branches on them.
const (
	ReasonDisabledByAdmin  = "disabled by an administrator"
	ReasonNoDeliveryIn3d   = "no delivery succeeded for 3 days"
	ReasonEndpointDeleted  = "the endpoint was deleted"
	ReasonRetryWindowClose = "the retry window closed"
	ReasonAddressRefused   = "the address is not a public endpoint"
	ReasonLeaseExpired     = "no answer before the lease expired"
	// ReasonEndpointDisabled is the end an attempt comes to because the endpoint it
	// was addressed to is disabled — the row arrived after the disablement's own
	// transaction had ended every queued one, or was already on the wire when the
	// administrator clicked. Either way the ladder stops here and the log says why,
	// which is a different sentence from "the retry window closed".
	ReasonEndpointDisabled = "the endpoint is disabled"
)

// MaxName is the length of the longest name a caller may send, in characters.
const MaxName = 120

// EventNames is the list of event types an endpoint subscribes to. It is a
// named type so the column codec lives beside the field that uses it, the way
// user.Roles and billing.Features do: one text[] column, one Go slice.
type EventNames []string

// Value and Scan are the driver codec: the column is text[], and the operator
// that matters is `events @> ARRAY[name]`, which is what the fan-out matches an
// incoming event against.
func (e EventNames) Value() (driver.Value, error) { return pq.StringArray(e).Value() }

// Scan reads the column back.
func (e *EventNames) Scan(src any) error { return (*pq.StringArray)(e).Scan(src) }

// Endpoint is one HTTPS subscriber a tenant registered.
//
// The struct is the whole surface: the json tags are the API, the gorm tags are
// the table, and crud.Base contributes the id, the timestamps, the soft delete
// and the tenant column row-level security matches on.
//
// The signing secret is deliberately not a field of this entity. It lives in
// webhook_endpoint_secrets, which no screen and no read route reads, because
// this struct is rendered as a form by the generated screen and is carried in
// webhook.endpoint.created: a secret column here would be a secret on a screen
// and a secret in the audit trail. See internal/secret.go for the wrap and the
// two routes that answer with the plaintext once.
type Endpoint struct {
	crud.Base

	// Name is what the administrator called it, and what the list screen and
	// the disablement notice show. It is the only field the generic PATCH
	// takes: renaming a subscriber is not a rule about state.
	Name string `json:"name" gorm:"type:varchar(120);not null" validate:"required" doc:"What this endpoint is called" minLength:"1" maxLength:"120" example:"Ops paging service"`

	// URL is the destination: https, no userinfo, no query, no fragment. It is
	// immutable to the generic PATCH because changing it moves every future
	// delivery of this endpoint, and POST /endpoints/{id}/target re-checks the
	// address policy on the way in. Normalised on every write.
	URL string `json:"url" gorm:"type:varchar(2048);not null" validate:"required" ui:"widget:url" doc:"HTTPS address events are POSTed to" example:"https://hooks.example.com/platformkit"`

	// Events are the event types this endpoint wants, each one a name the
	// composition can emit. Immutable to the PATCH for the same reason URL is;
	// the same command sets both.
	// No widget, and none is missing: `ui/forms` draws no control for a list of
	// names, and a widget named that no screen can draw is refused at mount rather
	// than shipped. The list is hidden from the table and read on the detail page as
	// text; changing it is the `target` command, which re-checks every name against
	// what this composition emits — a check a form control could not do on its own.
	Events EventNames `json:"events" gorm:"column:events;type:text[];not null" validate:"required" ui:"hide:list" doc:"Event types delivered to this address"`

	// Status is the lifecycle. Set by Disable and Enable, and by the fault rule
	// in the delivery outcome transaction.
	Status string `json:"status" gorm:"type:varchar(16);not null;default:'enabled'" enum:"enabled,disabled" ui:"widget:select" doc:"Whether this endpoint receives events" default:"enabled" required:"false" readOnly:"true"`

	// DisabledAt and DisabledReason say when and why, for the row of the log a
	// human reads first. Both are empty while the endpoint is enabled.
	DisabledAt     *time.Time `json:"disabledAt,omitempty" gorm:"type:timestamptz" ui:"widget:datetime;hide:list" doc:"When this endpoint was disabled" readOnly:"true"`
	DisabledReason string     `json:"disabledReason,omitempty" gorm:"type:text;not null;default:''" ui:"hide:list" doc:"Why this endpoint was disabled" readOnly:"true"`

	// LastSuccessAt is the last delivery that got a 2xx. It is the field that
	// answers "is this thing still alive" without reading the log.
	LastSuccessAt *time.Time `json:"lastSuccessAt,omitempty" gorm:"type:timestamptz" ui:"widget:datetime" doc:"When a delivery to this endpoint last succeeded" readOnly:"true"`

	// FailingSince is the 3-day clock: the first failure after the last
	// success, so an endpoint that fails for three days is a fact the database
	// holds rather than a scan of the log on every tick. It is not a field of
	// the API — it names no actor and nobody reads it in a screen — so it is
	// absent from the JSON entirely.
	FailingSince *time.Time `json:"-" gorm:"type:timestamptz"`

	// CreatedBy is the person who registered the endpoint. It is who a
	// disablement notice addresses, and it is stamped by the create's own
	// actor rather than sent by the caller.
	CreatedBy uuid.UUID `json:"createdBy" gorm:"column:created_by;type:uuid;not null" required:"false" readOnly:"true" doc:"The user who registered this endpoint" format:"uuid"`
}

// TableName pins the table, so the entity and migrations/000044 agree.
func (Endpoint) TableName() string { return "webhook_endpoints" }

// IsEnabled reports whether events arriving now would be delivered. A disabled
// endpoint receives nothing, and the fan-out's WHERE clause is the one place
// that decides it.
func (e *Endpoint) IsEnabled() bool { return e.Status == StatusEnabled }

// Validate is the entity's own check, run by kit/crud at every door, whichever
// route the write came through. It refuses what is wrong with *this* value and
// normalises what a caller and the platform must not disagree about.
//
// What it cannot check is the deployment's address policy, because a deployment
// level flag is not a fact an entity may hold — a tenant that could set it could
// open the SSRF gate for itself. So Validate refuses the addresses no
// deployment ever dials (a link-local or metadata literal is a destination the
// platform will never POST to, whatever the flag says), and the private and
// loopback question is asked by internal.Service.Create and Service.Target,
// which hold the flag, and again on every attempt by the dial gate, which reads
// the resolved address. A PATCH cannot reach the field, which is why URL is
// Immutable.
func (e *Endpoint) Validate(context.Context) error {
	e.Name = strings.TrimSpace(e.Name)
	if e.Name == "" {
		return fmt.Errorf("an endpoint needs a name")
	}
	if len([]rune(e.Name)) > MaxName {
		return fmt.Errorf("name must have at most %d characters", MaxName)
	}

	normalized, err := NormalizeURL(e.URL)
	if err != nil {
		return err
	}
	e.URL = normalized

	if len(e.Events) == 0 {
		return fmt.Errorf("an endpoint needs at least one event type")
	}
	seen := make(map[string]bool, len(e.Events))
	for _, name := range e.Events {
		if !ValidEventName(name) {
			return fmt.Errorf("%q is not an event name", name)
		}
		if seen[name] {
			return fmt.Errorf("event type %q is listed twice", name)
		}
		seen[name] = true
	}

	if !slices.Contains(statuses, e.Status) {
		return fmt.Errorf("status %q is not a lifecycle state", e.Status)
	}
	if e.Status == StatusDisabled && e.DisabledAt == nil {
		return fmt.Errorf("a disabled endpoint needs the moment it was disabled")
	}
	return nil
}

// NormalizeURL is the one spelling an address is stored in: lower-case scheme
// and host, the default port dropped, nothing else changed. It refuses what is
// not a destination — anything with a scheme but https, a userinfo (an
// embedded credential is not a destination either), a query (an endpoint address
// is not a call with arguments), a fragment, or an empty host.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("an endpoint needs a URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("the URL does not parse: %v", err)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("the URL must be https, not %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("the URL has no host")
	}
	if u.User != nil {
		return "", fmt.Errorf("the URL must not carry a user name or password")
	}
	if u.RawQuery != "" || u.ForceQuery {
		return "", fmt.Errorf("the URL must not carry a query")
	}
	if u.Fragment != "" {
		return "", fmt.Errorf("the URL must not carry a fragment")
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		host = u.Hostname()
		port = u.Port()
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host == "" {
		return "", fmt.Errorf("the URL has no host")
	}
	// The default port is not stored: https://example.com and
	// https://example.com:443 are one destination and must be one string in the
	// table, or two rows that mean the same thing would compare unequal. Anything
	// else is kept — a receiver behind a port is a receiver.
	//
	// The bracket goes on before the port, never around a string that already
	// carries one: an IPv6 literal is a host that contains colons, and the colon
	// that separates a port is not part of the address. `JoinHostPort` is the
	// standard library's own answer to which is which — it brackets a host holding
	// a colon and leaves a hostname alone — and an address kept as `[::1:32943]`
	// instead of `[::1]:32943` is not an address at all, so a receiver on an IPv6
	// port would have been refused at the write, on a URL its caller never sent.
	if port != "" && port != "443" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	// A path is kept exactly as it was sent, including a trailing slash: /hooks/ is
	// one receiver's 204 and /hooks another's 404, and a normalisation that trimmed
	// the difference away would register a destination the caller did not name and
	// then drop every event the receiver refused for it. Only a lone "/" is dropped,
	// which is not a different destination: an HTTP/1.1 request for the empty path
	// is sent as "GET /", so https://host and https://host/ are one address on the
	// wire and must be one string in the table.
	if u.Path == "/" {
		u.Path = ""
	}
	u.Scheme, u.Host = "https", host
	return u.String(), nil
}

// ValidEventName is the grammar of an event name, which the module can check on
// its own. Whether a name is one this composition emits is the Catalogue port's
// question, asked by the service that holds it.
func ValidEventName(name string) bool { return events.ValidName(name) }

// RefusedAddressLiteral reports the literal host addresses no deployment of this
// platform dials: the unspecified address, multicast, and every link-local
// network — which carries each cloud provider's metadata address. The
// private-range question — which a deployment may open for local use — is
// internal.AddressPolicy's, because it needs the deployment's flag.
func RefusedAddressLiteral(host string) bool {
	ip, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return false
	}
	ip = ip.WithZone("")
	return ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		slices.ContainsFunc(refusedRanges, func(r netip.Prefix) bool { return r.Contains(ip) })
}

// refusedRanges are the networks no deployment dials whatever its flag says:
// the link-local half of 169.254.0.0/16, which carries every cloud provider's
// metadata address, and IPv6's fe80::/10.
var refusedRanges = []netip.Prefix{
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("fe80::/10"),
}
