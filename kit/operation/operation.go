// Package operation defines portable intent identity and outcome values.
// It owns no request, transaction, clock or persistence.
package operation

import "strings"

// Outcome is an authoritative operation result. Empty means no result is known.
type Outcome string

const (
	Succeeded       Outcome = "succeeded"
	Failed          Outcome = "failed"
	PendingApproval Outcome = "pending_approval"
)

// Valid reports whether the outcome is one of the three known results.
func (o Outcome) Valid() bool { return o == Succeeded || o == Failed || o == PendingApproval }

// Identity identifies one admitted intent independently of its calls and events.
// References and tenant/person identifiers are UUID strings validated at boundaries.
// An empty person means no authenticated person initiated the operation.
type Identity struct {
	ID       string
	Ref      string
	Tenant   string
	Person   string
	Outcome  Outcome
	Revision int64
}

// MaxTokenLength is the shared bound for an operation ID token.
const MaxTokenLength = 128

// ValidToken reports whether id is 1–128 RFC 9110 token bytes, without quotes.
func ValidToken(id string) bool {
	if len(id) == 0 || len(id) > MaxTokenLength {
		return false
	}
	for i := range len(id) {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0 {
			continue
		}
		return false
	}
	return true
}
