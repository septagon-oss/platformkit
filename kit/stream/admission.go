package stream

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// normalise turns whatever the query said into event names: every member split on
// commas, every part trimmed, empties and repeats gone, and the first part that is
// not an event name named rather than dropped. Both spellings of a repeated
// parameter — ?topics=a&topics=b and ?topics=a,b — mean the same thing.
func normalise(raw []string) (names []string, bad string) {
	seen := map[string]bool{}
	for _, member := range raw {
		for _, part := range strings.Split(member, ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				continue
			}
			if !transport.ValidName(name) {
				return nil, name
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	return names, ""
}

// admit checks operator scope before asking Authority about the session and
// topic grants. Authority receives a detached context so its short transaction
// cannot retain the completed request transaction for the connection's lifetime.
func (h *Hub) admit(ctx context.Context, req Request, topics []Topic) error {
	asks, outside := askable(req.Tenant, topics)
	if len(outside) > 0 {
		return &Refusal{Status: http.StatusForbidden,
			Detail: httpx.CodeNotOperator + fmt.Sprintf(": the topic %s requires %s, which is the operator's and this is not the operator's tenant", named(topics, outside[0]), outside[0])}
	}
	renewal, err := h.authority.Renew(db.Detached(ctx), req.Tenant, req.HTTP, asks)
	if err != nil {
		return &Refusal{Status: http.StatusServiceUnavailable,
			Detail: CodeUnavailable + ": this application cannot check what this caller may watch right now"}
	}
	if !renewal.SignedIn {
		return &Refusal{Status: http.StatusUnauthorized,
			Detail: httpx.CodeAnonymous + ": no session answers for this caller"}
	}
	for _, t := range topics {
		if t.Policy.Kind() != module.KindGrant {
			continue
		}
		if slices.Contains(renewal.Refused, t.Policy.Grant()) {
			return &Refusal{Status: http.StatusForbidden,
				Detail: httpx.CodeDenied + fmt.Sprintf(": the topic %s requires %s, which this caller does not hold", t.Event, t.Policy.Grant())}
		}
	}
	return nil
}

// named is the first topic whose policy names this grant, for the refusal that has to
// say which topic it refused.
func named(topics []Topic, grant string) string {
	for _, t := range topics {
		if t.Policy.Kind() == module.KindGrant && t.Policy.Grant() == grant {
			return t.Event
		}
	}
	return ""
}

// askable splits one connection's topics into the permissions its tenant's authorizer
// may be asked about, and the permissions that tenant may not exercise at all. The
// second half is the declaration's operator boundary, and it is answered here rather
// than by the caller for the same reason the connect check asks the same Authority the
// heartbeat does: the two decisions are one decision, and a tenant's authorizer
// answering about a permission that is not the tenant's is a question it cannot answer
// honestly — the module's own authorizer reads its tenant's roles and reads them as
// its tenant's. One permission is one question however many topics name it.
func askable(tenant tenancy.Tenant, topics []Topic) (asks []string, outside []string) {
	for _, t := range topics {
		if t.Policy.Kind() != module.KindGrant {
			continue
		}
		grant := t.Policy.Grant()
		if slices.Contains(asks, grant) || slices.Contains(outside, grant) {
			continue
		}
		if t.Operator && !tenant.Operator {
			outside = append(outside, grant)
			continue
		}
		asks = append(asks, grant)
	}
	return asks, outside
}

// refused is the log line behind every refusal: who asked, what for, and the fact
// that answers it. A stream is not a state change, so this — and not an audit row —
// is the trail a connection leaves.
func (h *Hub) refused(ctx context.Context, req Request, topics []string, why string) {
	h.log.WarnContext(ctx, "stream: a live connection was refused",
		"tenant", req.Tenant.ID, "user", req.Principal.UserID, "topics", strings.Join(topics, ","), "why", why)
}
