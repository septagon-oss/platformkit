package httpx

// access.go is the sequel to a refusal: the person who was refused asks for the
// grant, and the people who hold role management are told.
//
// It lives here, beside the refusal, because everything it names belongs to the
// request that was refused: the permission key from the declared catalogue, the
// address it was refused at, the principal and the tenant of that request. No
// module can own it — no module ran, which is the same sentence
// kit/app/denial.go writes about security.denied — and a module named `access`
// would be a fifth manifest answering two questions whose owners are already
// named: kit/httpx for the refusal and modules/user for who holds a role.
//
// What the capability cannot do itself is reach anybody: the people holding role
// management are the auth module's roles and the user module's rows, and a notice
// is the notification module's table. So the reach is a port, satisfied at
// composition in the manner of usercontracts.Administration and
// notificationcontracts.RecipientLookup, and a composition that wires no door
// draws no ask control — a button whose action nobody mounted is a lie about a
// door.
//
// The ask is never a way in. It writes no role, no session, no grant and no
// revision; the next request is refused exactly as before until somebody who may
// grant it does so through the user module's own door.

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The two counters of an ask, and why there are two. The first is the brief's
// limit — one person may ask for one grant three times in six hours. The second
// is the shape the first cannot see: the same person refused at ten addresses,
// each of which is under the per-permission ceiling. Both are scoped to the
// tenant by kit/limit itself (the tenant is the first field of every key).
const (
	askPerPermission = 3
	askPerPerson     = 10
	askWindow        = 6 * time.Hour
	askPermissionKey = "access-request"
)

// AccessNotice is one ask, addressed to one person who may grant it. The words
// and the deep link are the composition's, because only the product knows that a
// person's roles are shown at /app/user/users/<id>; the kernel names the facts
// and nothing else. Language is the refused request's negotiated tag — "" when
// the shell composes no catalogue — which the composition may trade for the
// recipient's own preference.
type AccessNotice struct {
	To          uuid.UUID
	Requester   uuid.UUID
	Permission  string
	Label       string
	RefusedPath string
	Language    string
}

// AskForAccess is the reach: who to tell, and how. An error from Recipients is
// never read as an empty answer, and an empty answer is not an error — asking in
// a tenant with nobody holding role management still records the ask, because
// refusing the person for their tenant's shape is the wrong verdict.
type AskForAccess interface {
	// Recipients is this tenant's people whose role grants role management:
	// ids, each once, deduplicated across the roles they hold.
	Recipients(ctx context.Context, tx db.Tx[db.Tenant]) ([]uuid.UUID, error)
	// Tell writes one notice. It runs in the request's own transaction, so the
	// notices and the event commit together or not at all.
	Tell(ctx context.Context, tx db.Tx[db.Tenant], n AccessNotice) error
}

// AccessAsk is what the person asks in: which grant, and where they were
// refused. The permission names a declared grant and nothing else — a body
// naming a role, a person or a target is not ignored, it is refused, because the
// only thing this command may choose is who was told.
type AccessAsk struct {
	Permission string
	Path       string
}

// AccessRecord is the ask the kernel records: AccessAsk answered with how many
// people were told. Notified is in the payload because "sent" is only true of the
// notices that were written, and a 0 is a fact an operator can read out of the
// trail rather than a page that claimed something it did not do.
type AccessRecord struct {
	Permission string
	Label      string
	Method     string
	Path       string
	RequestID  string
	UserID     uuid.UUID
	Notified   int
}

// accessDoor is the request's way to the port. It travels on the context rather
// than being imported by the layer that mounts the page door, because ui may not
// import kit/app and kit/httpx may not import ui: the two doors — the JSON one
// kit/app mounts and the browser one ui/page mounts — call this, so neither is a
// second implementation of the command (docs/adr/0007).
type accessDoor struct {
	ask       AskForAccess
	limiter   WriteLimiter
	record    func(context.Context, AccessRecord)
	catalogue func() []tenancy.Grant
}

type accessKey struct{}

func withAccessDoor(ctx context.Context, d *accessDoor) context.Context {
	return context.WithValue(ctx, accessKey{}, d)
}

// CanAsk reports whether this composition wired the door. The refusal page asks,
// so a shell that wired nothing draws no form instead of drawing one that 404s.
func CanAsk(ctx context.Context) bool {
	d, ok := ctx.Value(accessKey{}).(*accessDoor)
	return ok && d != nil
}

// Ask records one request for a missing grant and tells the people who may give
// it. It answers nil when the ask was written — notices, event and all, in the
// request's own transaction — and a *problem.Problem the caller can act on
// otherwise, with nothing written.
func Ask(ctx context.Context, in AccessAsk) error {
	d, ok := ctx.Value(accessKey{}).(*accessDoor)
	if !ok || d == nil {
		return problem.New(http.StatusServiceUnavailable, "this application offers no way to ask for access")
	}
	tx, ok := TxFrom(ctx)
	if !ok {
		return problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
	}
	p, ok := tenancy.PrincipalFrom(ctx)
	if !ok || p.UserID == uuid.Nil {
		// The guard answers an anonymous caller before this runs; reaching here
		// without a principal means the door was mounted outside the chain.
		return problem.New(http.StatusForbidden, CodeDenied+": this request names nobody to ask as")
	}

	// The permission has to be one the composition defines. A key nobody
	// declares names no grant, and a notification built from it would tell a
	// manager to give out a token that grants nothing.
	label := ""
	for _, g := range d.catalogue() {
		if g.Permission == in.Permission {
			label = g.Label
			break
		}
	}
	if in.Permission == "" || label == "" {
		return problem.New(http.StatusUnprocessableEntity,
			"permission "+in.Permission+" names no access this application defines")
	}
	if in.Path != "" && !LocalPath(in.Path) {
		// The same rule a notification link carries (modules/notification): a
		// notice may carry a path within this application and never an absolute
		// URL, which is what turns a notice into a link to somewhere else.
		return problem.New(http.StatusUnprocessableEntity,
			"a link is a path within this application, and "+in.Path+" is not")
	}

	// Then the limit. Two counters, both keyed by the person, both tenant-scoped
	// by kit/limit itself. A failure to reach the limiter is an outage and not an
	// allowance: the auth lockout fails open because a false lockout stops a
	// person working, and a false allowance here would mail every administrator in
	// a tenant as many times as the database blinks.
	if err := d.count(ctx, in.Permission, p.UserID); err != nil {
		return err
	}

	recipients, err := d.ask.Recipients(ctx, tx)
	if err != nil {
		// Our side, for a moment: the same shape as an authorizer that cannot
		// decide, and nothing was written.
		return problem.New(http.StatusServiceUnavailable, "the people who grant access could not be reached right now")
	}
	for _, to := range recipients {
		if to == p.UserID {
			// Asking your administrator when you are one is not a notice.
			continue
		}
		if err := d.ask.Tell(ctx, tx, AccessNotice{
			To: to, Requester: p.UserID, Permission: in.Permission, Label: label,
			RefusedPath: in.Path, Language: RefusalLanguage(ctx),
		}); err != nil {
			return err
		}
	}
	if d.record != nil {
		req, _ := RequestFrom(ctx)
		method, path := "", in.Path
		if req != nil {
			method = req.Method
		}
		d.record(ctx, AccessRecord{
			Permission: in.Permission, Label: label, Method: method, Path: path,
			RequestID: requestIDFrom(ctx), UserID: p.UserID, Notified: len(recipients),
		})
	}
	return nil
}

// count spends both counters, and answers the refusal a spent one earns. The
// counters are written by kit/limit in a detached cross-tenant transaction, so an
// attempt stays counted even when the request's own transaction rolls back: a
// caller cannot earn unlimited asks by causing the write to fail.
func (d *accessDoor) count(ctx context.Context, permission string, user uuid.UUID) error {
	if d.limiter == nil {
		return nil
	}
	// The attempt must outlive a rollback, so the counters do not run against the
	// request's own transaction.
	meter := context.WithoutCancel(ctx)
	id := user.String()
	for _, c := range []struct {
		key   string
		limit int
	}{
		{askPermissionKey + "/" + id + "/" + permission, askPerPermission},
		{askPermissionKey + "/" + id, askPerPerson},
	} {
		ok, after, err := d.limiter.Allow(meter, c.key, c.limit, askWindow)
		if err != nil {
			// An outage is never an allowance here: see the note above.
			return problem.New(http.StatusServiceUnavailable, "access requests cannot be counted right now")
		}
		if !ok {
			return tooMany(after)
		}
	}
	return nil
}

func tooMany(after time.Duration) error {
	seconds := int64(after.Seconds()) + 1
	return problem.New(http.StatusTooManyRequests,
		CodeLimitExhausted+": you have asked for this too often; try again in "+strconv.FormatInt(seconds, 10)+" seconds")
}

// RefusalLanguage is the tag this request's refusal is answered in, or "". It is
// set by the page layer, which is where a language is negotiated; a JSON ask
// carries none, and the composition then words the notice in the recipient's own
// preference.
func RefusalLanguage(ctx context.Context) string {
	tag, _ := ctx.Value(refusalLanguageKey{}).(string)
	return tag
}

type refusalLanguageKey struct{}

// WithRefusalLanguage records the negotiated tag for the layers below the page.
func WithRefusalLanguage(ctx context.Context, tag string) context.Context {
	return context.WithValue(ctx, refusalLanguageKey{}, tag)
}
