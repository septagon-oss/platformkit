package page

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Navigation is what a shell may show a caller: every module's nav entries,
// against what the application actually serves and which permissions are the
// operator's. It is built once, at mount, from values — and never changed. It
// used to be two maps on the shell, filled in after the routes were mounted,
// which is a value that is wrong until a moment nobody can point at.
type Navigation struct {
	entries  []module.NavEntry
	served   map[string]bool
	operator map[string]bool
}

// NewNavigation takes the entries, the paths a GET answers, and the grants the
// routes declared; the operator flag on a grant is what says an entry is the
// installation's rather than a customer's.
func NewNavigation(entries []module.NavEntry, served []string, required []tenancy.Grant) Navigation {
	n := Navigation{entries: entries, served: map[string]bool{}, operator: map[string]bool{}}
	for _, p := range served {
		n.served[p] = true
	}
	for _, g := range required {
		if g.Operator {
			n.operator[g.Permission] = true
		}
	}
	return n
}

// Served is every path the recorded operations answer a GET on. A shell adds
// the paths it is about to mount itself, since it is composed last and nothing
// else will record them for it.
func Served(recorded []*huma.Operation) []string {
	var out []string
	for _, op := range recorded {
		if op.Method == http.MethodGet {
			out = append(out, op.Path)
		}
	}
	return out
}

// Unserved is the entries no GET answers: a mistake in a module's manifest,
// for the shell to report once at boot rather than render every day.
func (n Navigation) Unserved() []module.NavEntry {
	var out []module.NavEntry
	for _, e := range n.entries {
		if !n.served[e.Path] {
			out = append(out, e)
		}
	}
	return out
}

// Visible is what this caller may follow: served; not the operator's when the
// tenant is a customer's, for the same reason the kernel refuses before it asks
// the Authorizer; and allowed by the same Authorizer the routes enforce with,
// so a link that is shown is a link that works. A request that resolved no
// tenant is asked nothing beyond served, which is what an unresolved host gets
// everywhere else too.
func (n Navigation) Visible(ctx context.Context, t tenancy.Tenant, authorize httpx.Authorizer) []module.NavEntry {
	var out []module.NavEntry
	for _, e := range n.entries {
		if !n.served[e.Path] {
			continue
		}
		grant := tenancy.Grant{Permission: e.Permission, Operator: n.operator[e.Permission]}
		if grant.Operator && !t.Operator {
			continue
		}
		if t.ID != uuid.Nil && authorize != nil {
			allowed, err := authorize.Allowed(ctx, t, grant)
			if err != nil || !allowed {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}
