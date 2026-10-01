package httpx

// refusal.go carries *which grant was missing* as a fact of the request.
//
// Before this, a refusal reached the page as a sentence: authorize.go wrote
// "this operation requires task:read" and ui/page looked up only the code,
// replacing the words after the colon. A page that wanted the permission had to
// parse English to get it, and POLICY_DENIED's sentence names no permission at
// all, so there was nothing to parse. The verdict therefore travelled as prose
// and the one thing a refused person could act on — the grant they lack — was
// only ever in a log line.
//
// So a guard records the fact here, in the note `carry` put on the request, and
// both consumers read the same struct: the audit hook, which now says which
// permission was missing rather than only the sentence that said so, and the
// refusal page, which renders it. It is a pointer inside the context value
// precisely because the guard holds a huma.Context and the renderer holds the
// *http.Request unwrapped from it: huma.WithContext does not propagate to that
// unwrap, so a value put on the huma context would be invisible to the page.

import (
	"context"
	"sync"
)

// Refusal is one refusal, as the request knows it. Which half is filled tells
// the reader which question was answered no: CodeDenied asks the grant question
// (Permission, Label) and CodePolicyDenied asks the row question (Action,
// ResourceKind, ResourceID, Reason, Revision). Method and Path name the address
// that refused — the page must not offer it as a way on.
type Refusal struct {
	Code string

	Permission string
	// Label is the grant in the words of the module that defines it, read from
	// the declared catalogue. Empty means the composition declared no label for
	// that key, which module.Validate makes unshippable.
	Label string

	Action       string
	ResourceKind string
	ResourceID   string
	Reason       string
	Revision     string

	Method string
	Path   string
}

// refusalNote is the request's slot. The first guard to refuse fills it; a
// second refusal in one request (the policy hook after a grant was held, say)
// does not overwrite what the caller was actually answered with.
type refusalNote struct {
	mu     sync.Mutex
	ref    Refusal
	filled bool
}

func (n *refusalNote) record(r Refusal) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.filled {
		return
	}
	n.ref, n.filled = r, true
}

func (n *refusalNote) read() (Refusal, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.ref, n.filled
}

func (n *refusalNote) grant() (permission, label string) {
	r, ok := n.read()
	if !ok {
		return "", ""
	}
	return r.Permission, r.Label
}

// refusalKey carries the note past huma's routing. See carry.
type refusalKey struct{}

// withRefusalNote allocates the slot, once per request, beside the request
// itself. A request no guard refuses never reads it.
func withRefusalNote(ctx context.Context) context.Context {
	return context.WithValue(ctx, refusalKey{}, new(refusalNote))
}

// Refused is the refusal this request was given, and whether a guard recorded
// one at all. A 404, a cross-site write and a handler's own 4xx answer false,
// and the page those verdicts get is what it always was, byte for byte.
func Refused(ctx context.Context) (Refusal, bool) {
	n, ok := ctx.Value(refusalKey{}).(*refusalNote)
	if !ok {
		return Refusal{}, false
	}
	return n.read()
}

// noteRefused records the fact a guard decided. It is unexported because the
// only guards that may write it are the two in authorize.go: the grant question
// and the row question. Anything else that refuses a request refuses something
// this struct cannot describe.
func noteRefused(ctx context.Context, r Refusal) {
	if n, ok := ctx.Value(refusalKey{}).(*refusalNote); ok {
		n.record(r)
	}
}

// grantLabel is the declared words for one permission, or "" when the
// composition declared none — which boot refuses (module.Validate).
func (a *API) grantLabel(permission string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, g := range a.declared {
		if g.Permission == permission {
			return g.Label
		}
	}
	return ""
}
