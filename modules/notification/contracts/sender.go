package contracts

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// The two states a tenant's sender can be in. There is no third: a sender that
// is withdrawn is deleted, and deleting the tenant's only verified one is
// refused (Senders.Delete) rather than represented by a state that means
// "mail is off now" and reads like a switch nobody asked for.
const (
	// SenderPending is saved and not believed: the row exists, DNS does not
	// (yet), and every mail it would send is suppressed with a reason.
	SenderPending = "pending"
	// SenderVerified is believed: an operator or a check has seen the record
	// this row claims.
	SenderVerified = "verified"
)

// Sender is the address this tenant presents itself as, and the proof it has to
// have before anything is sent from it. One row per tenant, in this module's
// table, read under the recipient's own transaction — never from config: one
// binary serves many tenants and the name in a mail header belongs to one of
// them, decided per request.
//
// The envelope sender stays the deployment's (config's mail.from, still one bare
// address, still refused with a display name, because that is what MAIL FROM is).
// What lives here is the *header* — `From: "Ada's Clinic" <care@clinic.example>` —
// and the DKIM pair that has to exist for the far end to believe it. That is why
// kit/config's refusal is untouched by this type: the two addresses answer
// different questions, and a display name is legal in one and a bug in the other.
//
// The private key is not a column. A row would put signing key material in a
// table that every backup copies; the composition reads the key for this
// domain out of its own configuration and hands it to the email provider, which
// is the only thing that ever sees it. A verified sender whose key the
// deployment does not have is suppressed with a reason naming the deployment.
type Sender struct {
	crud.Base

	// Domain is the domain that signs, which is the domain of FromAddress:
	// DKIM signs the domain in the header, so a row pairing one domain with
	// another's address would be a signature the far end cannot find.
	Domain string `json:"domain" gorm:"type:text;not null" validate:"required" maxLength:"253" doc:"The domain that signs" example:"clinic.example"`
	// Selector is the DNS label the public key hangs off. Two selectors per
	// domain let a key rotate without a gap: the old one keeps signing while the
	// new one publishes.
	Selector string `json:"selector" gorm:"type:text;not null" validate:"required" maxLength:"63" doc:"The DKIM selector" example:"selector1"`
	// FromName is the display name — what a person sees in the list, and the
	// one thing in a mail header a tenant is entitled to choose. It carries no
	// angle brackets and no line break, because both would let a tenant put a
	// second header in somebody's mail.
	FromName string `json:"fromName" gorm:"column:from_name;type:text;not null" validate:"required" maxLength:"200" doc:"The display name in the From header" example:"Ada's Clinic"`
	// FromAddress is the mailbox in the From header, and its domain is Domain.
	FromAddress string `json:"fromAddress" gorm:"column:from_address;type:text;not null" validate:"required" format:"email" maxLength:"320" doc:"The address mail arrives as" example:"care@clinic.example"`
	// ReplyTo is where a person's answer goes. Empty means the From address,
	// which is what the far end does anyway; a tenant that wants answers at a
	// helpdesk says so here.
	ReplyTo string `json:"replyTo,omitempty" gorm:"type:text;not null;default:''" format:"email" maxLength:"320" doc:"Where an answer goes"`
	// Status is pending or verified, and nothing sets it but Verify and the
	// command that saved this row. A caller may not post it: a tenant that
	// could declare itself verified is a tenant that needs no DNS.
	Status string `json:"status" gorm:"type:text;not null;default:'pending'" doc:"Whether this sender is believed" enum:"pending,verified" readOnly:"true"`
	// Token is the random value the domain must publish, at
	// _platformkit-verify.<domain>, for the check to match. It is minted when
	// the row is saved and changes when the domain does, and it is what makes
	// "verify this domain" an instruction a customer can carry to their DNS
	// rather than a favour the installation does them.
	Token string `json:"token,omitempty" gorm:"type:text;not null;default:''" maxLength:"64" readOnly:"true" doc:"The value to publish for the domain to be verified"`
	// Proof is what the check saw, in one line, so an audit question about a
	// sender verified two years ago has an answer that is not a guess.
	Proof string `json:"proof,omitempty" gorm:"type:text;not null;default:''" maxLength:"500" readOnly:"true" doc:"What the verification saw"`
	// VerifiedAt is when Status became verified, nil until it did.
	VerifiedAt *time.Time `json:"verifiedAt,omitempty" gorm:"type:timestamptz" readOnly:"true" doc:"When this sender was verified"`
	// Key is the PEM the composition holds for Domain+Selector and no
	// column of anything: nil means this deployment cannot sign as this sender,
	// which suppresses its mail rather than sending it unsigned.
	Key []byte `json:"-" gorm:"-"`
}

// TableName pins the table, so the entity and migrations/000029 agree.
func (Sender) TableName() string { return "notification_senders" }

// Validate is the entity's own check, run by kit/crud on every write whichever
// door it came through, and it is the reason a tenant cannot save a sender that
// will fail hours later in somebody else's log.
func (s *Sender) Validate(context.Context) error {
	s.Domain, s.Selector = trimASCII(s.Domain), trimASCII(s.Selector)
	s.FromName, s.FromAddress, s.ReplyTo = trimASCII(s.FromName), trimASCII(s.FromAddress), trimASCII(s.ReplyTo)
	switch {
	case s.Domain == "":
		return fmt.Errorf("a sender signs for a domain")
	case !validDomain(s.Domain):
		return fmt.Errorf("%q is not a domain name", s.Domain)
	case s.Selector == "" || !validSelector(s.Selector):
		return fmt.Errorf("%q is not a DKIM selector: lower letters, digits and hyphens", s.Selector)
	case s.FromName == "":
		return fmt.Errorf("a sender has a name to show")
	case strings.ContainsAny(s.FromName, "<>\r\n\""):
		return fmt.Errorf("a sender's name carries no brackets, quotes or line breaks")
	case s.FromAddress == "":
		return fmt.Errorf("a sender has an address")
	}
	a, err := mail.ParseAddress(s.FromAddress)
	if err != nil || a.Address != s.FromAddress || a.Name != "" {
		return fmt.Errorf("a sender's address is one bare address, and %q is not", s.FromAddress)
	}
	if at := strings.IndexRune(a.Address, '@'); at < 0 || !strings.EqualFold(a.Address[at+1:], s.Domain) {
		return fmt.Errorf("%s belongs to another domain than %s, which its DKIM record cannot sign", s.FromAddress, s.Domain)
	}
	if s.ReplyTo != "" {
		r, err := mail.ParseAddress(s.ReplyTo)
		if err != nil || r.Address != s.ReplyTo || r.Name != "" {
			return fmt.Errorf("a reply-to is one bare address, and %q is not", s.ReplyTo)
		}
	}
	if s.Status != SenderPending && s.Status != SenderVerified {
		return fmt.Errorf("a sender is pending or verified, not %q", s.Status)
	}
	return nil
}

// CanSend is the whole of the email gate in one call, for the fake and the
// service alike: a tenant sends mail when it has a sender, that sender is
// believed, and this deployment holds the key to sign as it.
func (s *Sender) CanSend() bool {
	return s != nil && s.Status == SenderVerified && len(s.Key) > 0
}

// DKIMName is the DNS name the public key must be published at.
func (s *Sender) DKIMName() string {
	if s == nil {
		return ""
	}
	return s.Selector + "._domainkey." + s.Domain
}

// VerificationName is the DNS name the Token must be published at — the TXT
// record a customer's own administrator can set, which is what makes
// verification something a tenant can do rather than wait for.
func (s *Sender) VerificationName() string {
	if s == nil {
		return ""
	}
	return "_platformkit-verify." + s.Domain
}

// Header is the From header a message carries: the name and the address in the
// form RFC 5322 wants. A name with an apostrophe in it is quoted, which is why
// this goes through net/mail rather than fmt.Sprintf.
func (s *Sender) Header() string {
	if s == nil {
		return ""
	}
	return (&mail.Address{Name: s.FromName, Address: s.FromAddress}).String()
}

// Senders is how this module reads the tenant of a request or an event: the
// sender, or nil when the tenant never set one, which is an answer and not an
// error. The lookup runs in the caller's own transaction, so what it reads is
// the one tenant's row the policy shows it.
type Senders interface {
	For(ctx context.Context, tx db.Tx[db.Tenant]) (*Sender, error)
}

// SenderAdmin is what an administrator of the tenant does about it. Both
// commands recheck the actor's grant and the tenant's own row inside their
// transaction, and a refusal writes nothing, publishes nothing and returns no
// stale row.
type SenderAdmin interface {
	// Put saves the tenant's sender, creating it or replacing what is there.
	// Status is never taken from the caller: a row whose domain or selector
	// differs from the verified one goes back to pending, because the proof is
	// about the pair it was made for, and a row that changes only the display
	// name keeps being believed. It publishes notification.sender_set.
	Put(ctx context.Context, tx db.Tx[db.Tenant], s Sender) (*Sender, error)

	// Verify consults the composition's SenderVerifier and, when it is
	// satisfied, records the proof and the moment and publishes
	// notification.sender_verified. Already-verified changes nothing and
	// publishes nothing. A refusal leaves the row pending and says why in the
	// error, which is the sentence the operator needs.
	Verify(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*Sender, error)

	// Delete removes a sender that is not believed. Removing the tenant's only
	// verified one is refused — correctable by putting its replacement first —
	// because the write that takes the last one away leaves a tenant's people
	// unable to be told anything by mail, and finding none is a query.
	Delete(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error
}

// SenderVerifier is the composition's answer to "does this domain really say
// so". The module asks, and never reads DNS itself: what a proof is — the TXT
// record at the VerificationName matching the Token, a TXT record at DKIMName,
// a human ticking a box in a deployment with no resolver — is the product's
// decision, and it is the one fact in this file the kernel cannot own.
//
// The proof string it returns is stored, so a verifier that stores what it saw
// is auditable a year later; an empty proof is a verifier that cannot explain
// itself and should not be composed.
type SenderVerifier interface {
	Verify(ctx context.Context, s Sender) (proof string, err error)
}

// validDomain is a dotted set of labels with no empty label, no trailing dot
// (DNS accepts one and a mail header does not mean it) and no port, slash or
// space. It is deliberately not a full RFC check: what it must not accept is a
// string a resolver could not use, and what it must accept is the tenant's own
// host name, which modules/tenant already checked when it claimed it.
func validDomain(s string) bool {
	if s == "" || s[len(s)-1] == '.' || strings.ContainsAny(s, "@/ \t") {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || !oneOf(label, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-") {
			return false
		}
	}
	return true
}

// validSelector is one DNS label, lower-case: a selector that has to be
// lower-cased to work is a selector that will not be found.
func validSelector(s string) bool {
	return len(s) <= 63 && oneOf(s, "abcdefghijklmnopqrstuvwxyz0123456789-")
}

func oneOf(s, set string) bool {
	for i := 0; i < len(s); i++ {
		if !strings.ContainsRune(set, rune(s[i])) {
			return false
		}
	}
	return len(s) > 0
}
