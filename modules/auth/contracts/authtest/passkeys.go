package authtest

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// The fake's factor and ceremony store.
//
// What it holds is the shape of the two tables, not their contents: a factor row
// names a kind and a label, and a ceremony row names the door it was begun at and
// when it stopped being answerable. Nothing here holds a secret, a public key or a
// signature, because a fake that could check a signature would be a second
// implementation of WebAuthn — a rebuilt standard — and the rules it would then be
// proving are its own arithmetic rather than the contract's.
//
// The credential id is the one piece of ceremony material the fake reads. It is
// public by design (it identifies a public key and spends nothing), it is what the
// row is unique on, and a store that could not tell two authenticators apart could
// not decide "one authenticator is one person's factor" at all. The response body
// is read as `{"id": "…"}` and no other member is consulted: the assertion
// statements, the client data and the attestation object go past the fake
// unexamined, and RunPasskeys hands a case's answer through the fixture's Answer
// seam so a signature-checking implementation can supply its own body here.
type fakeFactor struct {
	id, user uuid.UUID
	kind     string
	name     string
	cred     string
	at       time.Time
}

type fakeCeremony struct {
	id     uuid.UUID
	kind   string
	user   uuid.UUID
	expire time.Time
}

// The two kinds Factor.Kind takes, spelled as the published schema spells them
// (enums:"totp,passkey"). The columns they come from belong to the module; the
// words are here because a consumer standing a factor up in the fake needs one.
const (
	FactorTOTP    = "totp"
	FactorPasskey = "passkey"
)

// SetPasskeySignIn is the tenant's own row at the usernameless door — the fact
// BeginPasskeySignIn reads. The real service keeps it in passkey_settings, written
// today only by an operator's SQL; the fake holds the same one boolean so a
// consumer can test the door either way without a database.
func (f *Fake) SetPasskeySignIn(enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.passkeySignIn = enabled
}

// AddFactor stands in for a factor row this person holds without running the
// ceremony or the secret enrolment that would have made it. A TOTP row is the
// case that needs it: the last-factor rule counts kinds rather than enrolment
// paths, and a suite that could only add a passkey could not say so.
func (f *Fake) AddFactor(user uuid.UUID, kind, name, credential string) uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := uuid.New()
	f.factors[id] = &fakeFactor{id: id, user: user, kind: kind, name: name, cred: credential, at: db.Now()}
	return id
}

// ExpireCeremony stands the clock past the window for one begun ceremony, which
// is how a case asks "and the prompt took too long" without waiting two minutes
// for a row the contract says lives exactly that long. The real service's harness
// answers the same request with an UPDATE of expires_at.
func (f *Fake) ExpireCeremony(ceremony uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if row, ok := f.ceremonies[ceremony]; ok {
		row.expire = db.Now().Add(-time.Second)
	}
}

// BeginPasskeyRegistration mirrors the real command: a nonce row and nothing
// else, so a tab closed mid-enrolment leaves no factor.
func (f *Fake) BeginPasskeyRegistration(_ context.Context, _ db.Tx[db.Tenant], userID uuid.UUID) (*contracts.PasskeyChallenge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.begin(contracts.PasskeyCeremonyRegister, userID), nil
}

// BeginPasskeyAssertion mirrors the real command's indifference: the address is
// not looked up, so the request costs the same and the options look the same
// whether or not it holds a passkey. The ceremony keeps no address either — the
// door is the row's fact, and whose credential answers is decided at the answer.
func (f *Fake) BeginPasskeyAssertion(_ context.Context, _ db.Tx[db.Tenant], _ string) (*contracts.PasskeyChallenge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.begin(contracts.PasskeyCeremonySecondFactor, uuid.Nil), nil
}

// BeginPasskeySignIn is the usernameless door, including the one thing it refuses
// on its own: this tenant's row.
func (f *Fake) BeginPasskeySignIn(_ context.Context, _ db.Tx[db.Tenant]) (*contracts.PasskeyChallenge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.passkeySignIn {
		return nil, contracts.ErrPasskeySignInOff
	}
	return f.begin(contracts.PasskeyCeremonySignIn, uuid.Nil), nil
}

// begin is the three doors' shared row. The challenge is random bytes, which is
// all a fake needs: nothing here is signed, and a challenge two ceremonies shared
// would hide the very thing the door is a fact about.
func (f *Fake) begin(kind string, user uuid.UUID) *contracts.PasskeyChallenge {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		panic("auth fake: no challenge bytes: " + err.Error())
	}
	id := uuid.New()
	f.ceremonies[id] = &fakeCeremony{
		id: id, kind: kind, user: user, expire: db.Now().Add(contracts.PasskeyChallengeWindow),
	}
	options, _ := json.Marshal(map[string]any{
		"challenge": base64.RawURLEncoding.EncodeToString(nonce),
		"timeout":   contracts.PasskeyChallengeWindow.Milliseconds(),
	})
	return &contracts.PasskeyChallenge{Ceremony: id, Options: options}
}

// FinishPasskeyRegistration enrols the credential whose id the answer carries,
// for whoever the ceremony was begun for — not for whoever is asking, which is the
// whole of why a ceremony has an owner.
func (f *Fake) FinishPasskeyRegistration(_ context.Context, _ db.Tx[db.Tenant], userID, ceremony uuid.UUID, response json.RawMessage, name string) (*contracts.Factor, error) {
	f.mu.Lock()
	row := f.spend(ceremony, userID, contracts.PasskeyCeremonyRegister)
	if row == nil {
		f.mu.Unlock()
		return nil, contracts.ErrCredentials
	}
	cred := credentialIn(response)
	if cred == "" {
		f.mu.Unlock()
		return nil, contracts.ErrCredentials
	}
	for _, held := range f.factors {
		if held.cred == cred && held.user != userID {
			f.mu.Unlock()
			return nil, contracts.ErrPasskeyExists
		}
	}
	factor := &fakeFactor{id: uuid.New(), user: userID, kind: FactorPasskey,
		name: trimName(name), cred: cred, at: db.Now()}
	f.factors[factor.id] = factor
	f.mu.Unlock()
	f.record(contracts.EventFactorEnrolled)
	return factor.view(), nil
}

// FinishPasskeyAssertion is the answer to either sign-in door. The nonce goes
// first — a refusal consumes the attempt rather than leaving a signature to be
// tried again — then the credential says whose session this is, and the door the
// row names decides whether a first factor was ever offered.
func (f *Fake) FinishPasskeyAssertion(ctx context.Context, tx db.Tx[db.Tenant], ceremony uuid.UUID, response json.RawMessage, from contracts.Client) (*contracts.Session, *contracts.Identity, error) {
	f.mu.Lock()
	row := f.spend(ceremony, uuid.Nil, contracts.PasskeyCeremonySignIn, contracts.PasskeyCeremonySecondFactor)
	f.mu.Unlock()
	if row == nil {
		return nil, nil, contracts.ErrCredentials
	}
	if row.kind == contracts.PasskeyCeremonySecondFactor && !f.proved(row) {
		return nil, nil, contracts.ErrCredentials
	}
	owner := uuid.Nil
	f.mu.Lock()
	for _, held := range f.factors {
		if held.cred == credentialIn(response) {
			owner = held.user
			break
		}
	}
	f.mu.Unlock()
	if owner == uuid.Nil {
		return nil, nil, contracts.ErrCredentials
	}
	user, err := f.Users.Get(ctx, tx, owner)
	if err != nil {
		return nil, nil, contracts.ErrCredentials
	}
	f.record(contracts.EventFactorUsed)
	return f.open(ctx, tx, user, from)
}

// ListFactors spans both kinds, newest first, and carries no material.
func (f *Fake) ListFactors(_ context.Context, _ db.Tx[db.Tenant], userID uuid.UUID) ([]*contracts.Factor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*contracts.Factor
	for _, held := range f.factors {
		if held.user == userID {
			out = append(out, held.view())
		}
	}
	slices.SortFunc(out, func(a, b *contracts.Factor) int {
		if a.EnrolledAt.Equal(b.EnrolledAt) {
			return 0
		}
		if a.EnrolledAt.After(b.EnrolledAt) {
			return -1
		}
		return 1
	})
	return out, nil
}

// WithdrawFactor refuses the last one of any kind: the count spans the tables,
// which is the difference between rule 8 and a per-kind count that lets a
// passkey-only account delete its only way in.
func (f *Fake) WithdrawFactor(_ context.Context, _ db.Tx[db.Tenant], userID, factor uuid.UUID) error {
	f.mu.Lock()
	var held []*fakeFactor
	for _, row := range f.factors {
		if row.user == userID {
			held = append(held, row)
		}
	}
	if !slices.ContainsFunc(held, func(row *fakeFactor) bool { return row.id == factor }) {
		f.mu.Unlock()
		return crud.ErrNotFound
	}
	if len(held) == 1 {
		f.mu.Unlock()
		return contracts.ErrLastFactor
	}
	delete(f.factors, factor)
	f.mu.Unlock()
	f.record(contracts.EventFactorWithdrawn)
	return nil
}

// RotateRecoveryCodes refuses a person with no factor, whatever kind it would
// have been: a code with nothing to stand in for is a second password.
func (f *Fake) RotateRecoveryCodes(_ context.Context, _ db.Tx[db.Tenant], userID uuid.UUID) ([]string, error) {
	f.mu.Lock()
	any := false
	for _, held := range f.factors {
		if held.user == userID {
			any = true
			break
		}
	}
	f.mu.Unlock()
	if !any {
		return nil, contracts.ErrNoFactor
	}
	codes := make([]string, 0, contracts.RecoveryCodes)
	for range contracts.RecoveryCodes {
		codes = append(codes, newCode())
	}
	f.record(contracts.EventRecoveryCodesIssued)
	return codes, nil
}

// spend deletes the ceremony and reports it, but only for a row that is still
// here, still unexpired, of one of the doors the caller answers, and begun for
// this person when the door has one. Each of those refusals is the same answer,
// and the row is gone either way: the attempt is spent, not left to be tried
// against the next door.
func (f *Fake) spend(id, user uuid.UUID, kinds ...string) *fakeCeremony {
	row, ok := f.ceremonies[id]
	if !ok || !row.expire.After(db.Now()) {
		delete(f.ceremonies, id)
		return nil
	}
	if user != uuid.Nil && row.user != user {
		return nil
	}
	if !slices.Contains(kinds, row.kind) {
		return nil
	}
	delete(f.ceremonies, id)
	return row
}

// proved is the fake's half of RequireFirstFactorProof: this second-factor
// ceremony was refused by a Login that minted the window. The fake mints nothing
// yet — its Login still opens the session a factor should hold at — so this
// reports false, and the case that would answer true is the one named as not
// delivered in RunPasskeys.
func (f *Fake) proved(*fakeCeremony) bool { return false }

// credentialIn reads the one member the fake consults. Anything unparseable is
// nobody's credential, which is the same answer as an unknown one.
func credentialIn(response json.RawMessage) string {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response, &body); err != nil {
		return ""
	}
	return body.ID
}

// maxFactorName is the label's length, the same 40 the published schema and the
// real command's column both spell.
const maxFactorName = 40

func trimName(name string) string {
	name = strings.TrimSpace(name)
	if len([]rune(name)) <= maxFactorName {
		return name
	}
	return string([]rune(name)[:maxFactorName])
}

// newCode is one recovery code: eight base32 characters, which is what the real
// service issues and what a consumer's screen has to fit.
func newCode() string {
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		panic("auth fake: no code bytes: " + err.Error())
	}
	return hex.EncodeToString(nonce)
}

// view is the row as the contract shows it: a kind, a label and a date, and no
// material a client could use.
func (h *fakeFactor) view() *contracts.Factor {
	return &contracts.Factor{ID: h.id, Kind: h.kind, Name: h.name, EnrolledAt: h.at}
}
