package contracts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/septagon-oss/platformkit/kit/limit"
)

// The lockout: ten failures for one address in fifteen minutes, and that
// address stops being tried until the window passes.
//
// Ten is enough that a person who has forgotten which of their passwords this
// is does not lock themselves out, and few enough that an online guessing
// attack is not worth running. Fifteen minutes is short enough that the person
// who did lock themselves out can have a coffee.
const (
	MaxAttempts   = 10
	AttemptWindow = 15 * time.Minute

	// MaxSources is how many distinct addresses may fail against one account
	// inside a window before the lockout stops being a lockout.
	//
	// The lockout above is a denial of service somebody else can trigger. Ten
	// wrong passwords for ada@acme.example.com locks Ada out, and an attacker
	// who wants Ada locked out needs nothing but her address — from a botnet,
	// once every fifteen minutes, for as long as they care to. The failures
	// coming from many addresses is what tells the two apart: a person
	// mistyping their password is one machine, and a distributed lockout attack
	// is not. Past this many sources the account gets a delay instead of a
	// refusal, so the attack costs the attacker time and costs Ada nothing but
	// two seconds when she gets it right.
	//
	// Three, because a person legitimately fails from a laptop, a phone and an
	// office network, and rarely from a fourth inside a quarter of an hour.
	MaxSources = 3

	// SoftDelay is what an account under a distributed attack gets instead of a
	// refusal. Two seconds is nothing to a person typing a password and is the
	// difference between an online guessing attack running at the speed of the
	// network and running at half an attempt a second per connection.
	SoftDelay = 2 * time.Second

	// SourceAttempts is the per-address limit, which is the half the per-account
	// one cannot cover: one machine working through a list of addresses fails
	// once against each and never trips an account's counter. It is higher than
	// MaxAttempts because one address is legitimately many people — an office,
	// a mobile carrier's NAT — and the number is what an attacker gets rather
	// than what anybody needs.
	SourceAttempts = 50

	// ResetRequests is how many forgotten-password links one address may ask
	// for inside a window.
	//
	// The route it guards is public, answers the same either way, and costs a
	// mail: without a cap, twenty-three requests were twenty-three mails to
	// somebody who asked for none, which is a mail relay's reputation spent by
	// a stranger with a browser. It counts the address asking and never the
	// address asked about, so the cap cannot be used to tell an account that
	// exists from one that does not — that is the whole property the route is
	// built around.
	//
	// Ten, because a person who has lost a password asks two or three times and
	// an office behind one NAT is several people doing that at once. It is the
	// default of AccountRequestBudget.Quota: a deployment names its own in
	// auth.account_requests, and a development instance names a large one.
	ResetRequests = 10

	// ResetRedemptions is how many reset tokens one address may present inside
	// a window.
	//
	// Asking for a link was capped and spending one was not, so the token
	// itself could be guessed at the speed of the network. It is 256 bits from
	// crypto/rand, so guessing it is not a real attack — but "the number is
	// large" is the only thing that was stopping it, and a cap costs nothing
	// and stops the traffic as well as the guess.
	//
	// Twenty, because a person follows a link once, occasionally twice when the
	// first attempt fails validation, and an office behind one NAT is several
	// of them at once.
	ResetRedemptions = 20

	// AssertionsPerAddress is how many SAML assertions one address may present
	// at the assertion consumer service inside AssertionWindow.
	//
	// The ACS is the only anonymous POST this module serves, and it is the one
	// sign-in leg the kernel's own limit does not reach: `kit/httpx`'s write
	// limiter runs on the Public surface and this leg is mounted on the app
	// surface beside the OIDC legs, so nothing counted it and every POST bought
	// an XML parse and an RSA verification. Sixty a minute is the bound the
	// kernel sets for the public forms it does count (`kit/httpx/surfaces.go`
	// `publicWriteLimit`, `publicWriteWindow`), taken rather than invented: the
	// same shape of caller, the same "an office behind one NAT is several people
	// at once" that made ten and twenty the numbers above, and a number that is
	// not this module's own to argue about.
	//
	// It is a bound on cost, not on trust: a spent assertion is refused by its
	// own row whatever the counter says, and a person who presents one after
	// being refused here presents it again a minute later and gets in.
	AssertionsPerAddress = 60

	// AssertionWindow is the minute those sixty assertions are counted over —
	// the window of the bound above, and deliberately not AttemptWindow: sixty
	// presentations spread over fifteen minutes is a Monday morning at one
	// company's front door, not an attack.
	AssertionWindow = time.Minute

	// PasskeyPrompts is how many passkey ceremonies one address may begin inside
	// a window.
	//
	// Every one of these legs is public and every one writes a row: the nonce a
	// signature has to match. Uncapped, a stranger with a browser grew the table
	// one row per request, and the second-factor leg did the password hashing for
	// an address nobody knows on top of it. The row is swept once its two-minute
	// window closes (Service.Purge), which bounds the damage a limited run does;
	// the cap is what stops the run.
	//
	// Twenty, for the same two reasons as ResetRedemptions: a person who cannot
	// get their phone to answer begins again, and one office address is several
	// people. That is also the residual, stated: a large office behind one NAT
	// signing everybody in with a passkey at nine in the morning will find this
	// cap, and the answer is the deployment counting the real client address
	// rather than the proxy's, which is what ClientOf documents.
	PasskeyPrompts = 20

	// MailDeliveryAsks is how many delivery questions one address may ask in a
	// window — the shell's read of what became of the mail its own call asked
	// for.
	//
	// It is its own bucket rather than ResetRedemptions because the two are
	// spent by different acts and one of them is a page that is merely open.
	// Shared, a shell that polls the record a few times per acknowledgment would
	// eat the redemptions that let the person spend the link when it arrives,
	// and a stranger could lock an address's people out of their own reset by
	// asking about mail they never requested. Shared the other way — counted as
	// a forgotten-password request — it would spend the cap on asking for the
	// link on reading about one.
	//
	// Sixty, because one acknowledgment costs the shell three asks, a person
	// who is waiting may submit twice, and an office behind one NAT is several
	// of them at once.
	MailDeliveryAsks = 60
)

// Limiter counts failed logins, per account and per source address, in the one
// counter every replica shares (kit/limit).
//
// Two counters, because one of them is a weapon. A per-account lockout is a
// denial of service anybody can trigger against anybody whose address they
// know, and the fix is not to remove it — a guessing attack against one account
// is the common case — but to tell the two apart. Failures against one account
// from many addresses are somebody attacking the lockout, and that account gets
// a delay rather than a refusal; failures from one address against many
// accounts are somebody working through a list, and that address gets the same.
//
// # Counting distinct addresses without a set
//
// MaxSources asks how many places one account has failed from, and the store
// counts events rather than remembering them. So the first failure of an
// account-and-address pair inside a window — a limit of one on the pair's own
// key — is what raises the account's source count. A hundred failures from one
// address raise it once, which is the question being asked.
//
// # The residual, stated
//
// Only the distributed lockout is defused. Ten wrong passwords for
// ada@acme.example.com from one address still lock Ada out for fifteen minutes,
// and an attacker who wants exactly that needs nothing but her address and one
// machine. MaxSources does not help: one source is not many. That is accepted,
// not overlooked, and it is accepted because the alternative is worse — an
// account with no lockout is an account a botnet guesses at the speed of the
// network, and the common attack is guessing rather than griefing. What is
// bought back is the scale of it: the attacker has to spend fifteen minutes per
// account per window, they are recorded in auth.login_failed with Locked set
// while they do it, and the person they are locking out is told why by
// ErrTooManyAttempts rather than left thinking their password stopped working.
// Closing it properly means proving the caller is not the account's owner —
// a second factor, or a challenge — and neither exists here yet.
//
// A limiter that cannot reach its store allows the attempt and says so in the
// log. It is the right way for this one to fail: the alternative is an
// installation nobody can sign in to because a counter table is unreachable,
// and every refusal this makes is a refusal the password check would make
// anyway. Which stores those are is stated once, in kit/limit/README.md: an
// attempt the counter did not record, because the key's row was still busy with
// it, arrives as a refusal carrying no error, and a refusal is never allowed
// here.
//
// It lives in contracts rather than in internal/ because the lockout is part of
// what Login promises, so the fake keeps it too and the conformance suite can
// check that both do.
type Limiter struct {
	store limit.Limiter
	// requests is the account-request budget with its defaults applied, set at
	// construction and never written again, so no request reads a half-made one.
	requests AccountRequestBudget
}

// NewLimiter returns a limiter over a store with the default account-request
// budget: kit/limit's Postgres one in the application, its memory one in a fake.
func NewLimiter(store limit.Limiter) *Limiter {
	return NewBudgetedLimiter(store, AccountRequestBudget{})
}

// NewBudgetedLimiter is NewLimiter whose Requested spends requests, each zero
// field given its default. requests is one Check accepted: the composition's
// refusal is auth's, and this does not repeat it.
func NewBudgetedLimiter(store limit.Limiter, requests AccountRequestBudget) *Limiter {
	if requests.Quota == 0 {
		requests.Quota = ResetRequests
	}
	if requests.Window == 0 {
		requests.Window = AttemptWindow
	}
	return &Limiter{store: store, requests: requests}
}

// AccountRequestBudget is how many account requests one address may make inside
// Window: the quota and window of the counter AccountRequestsPolicyName names. A
// zero field is its default — ResetRequests, AttemptWindow — so the zero value is
// the budget every deployment had before it was configurable.
type AccountRequestBudget struct {
	Quota  int
	Window time.Duration
}

// Check refuses a budget the counter cannot keep: a negative quota (every
// request refused) or window (no request counted), a quota over limit.MaxQuota,
// which the counter could never refuse past, and a window longer than
// limit.MaxWindow, which the sweep would end before the clock did. Zero fields
// are not refused: they are the default.
func (b AccountRequestBudget) Check() error {
	switch {
	case b.Quota < 0:
		return fmt.Errorf("the quota is %d; it is how many account requests one address may make in a window, so at least 1", b.Quota)
	case b.Quota > limit.MaxQuota:
		return fmt.Errorf("the quota is %d; kit/limit counts to at most %d, and a larger quota could never be refused past", b.Quota, limit.MaxQuota)
	case b.Window < 0:
		return fmt.Errorf("the window is %s; it is a positive duration, like %s", b.Window, AttemptWindow)
	case b.Window > limit.MaxWindow:
		return fmt.Errorf("the window is %s; kit/limit keeps a counter for at most %s, and a longer window would be forgotten before it closed", b.Window, limit.MaxWindow)
	}
	return nil
}

// The keys, each namespaced by what it counts. kit/limit puts the tenant in
// front of them, so these say nothing about which customer is being attacked.
// The separator inside a compound key is a space, which no address contains.
//
// The address is hashed and not spelled. platformkit_limits is an ordinary
// table with an ordinary backup, and a key of "auth/account/ada@acme.example"
// made it a list of every address that has ever failed a sign-in here —
// readable by the owner role, by whoever restores a dump, and by anybody who
// gets a look at either. The counter needs the address to be the same string
// twice and nothing else, which is exactly what a hash is.
func accountKey(email string) string   { return "auth/account/" + emailHash(email) }
func sourcesKey(email string) string   { return "auth/sources/" + emailHash(email) }
func sourceKey(ip string) string       { return "auth/source/" + ip }
func pairKey(email, ip string) string  { return "auth/pair/" + emailHash(email) + " " + ip }
func forgotKey(ip string) string       { return "auth/forgot/" + ip }
func redeemKey(ip string) string       { return "auth/redeem/" + ip }
func mailAskKey(ip string) string      { return "auth/mail-ask/" + ip }
func notedKey(email, ip string) string { return "auth/noted/" + emailHash(email) + " " + ip }
func promptKey(ip string) string       { return "auth/passkey-prompt/" + ip }

// acsKey is one address knocking on the assertion consumer service. The assertion
// itself never enters the key: it is somebody else's credential, its id is a value that
// appears in a log line elsewhere, and a counter keyed by it would be a list of every
// assertion ever presented here.
func acsKey(ip string) string { return "auth/saml-acs/" + ip }

// AssertionPresented counts one assertion presented from this address at the assertion
// consumer service and reports whether it is within AssertionsPerAddress. It runs ahead
// of the parse and the signature, which is the whole of its point: the refusal has to
// cost less than the thing it is refusing.
//
// The address is the one asking and no identity is consulted, because there is none yet —
// which is also why an address that cannot be trusted is not: `ClientOf` takes the peer
// address and no forwarded header, for the reason it gives.
//
// A counter that cannot be read allows the presentation and says so, which is the answer
// every other limiter here gives: an installation nobody can sign in to because a counter
// table blinked is a worse failure than traffic this limit would not have stopped.
func (l *Limiter) AssertionPresented(ctx context.Context, ip string) bool {
	ok, _, err := l.store.Allow(ctx, acsKey(ip), AssertionsPerAddress, AssertionWindow)
	if err != nil {
		slog.ErrorContext(ctx, "auth: the assertion limit could not be read; the presentation is allowed",
			"error", err)
		return true
	}
	return ok
}

// emailHash is the address as a key: SHA-256 of EmailKey, in hex.
//
// It is not a secret and it is not pretending to be one — an attacker with the
// table and a list of addresses can hash the list — so there is no salt and no
// work factor, and adding either would be claiming a property this does not
// have. What it buys is that the table is no longer a list of addresses to
// read, which is the whole of the finding.
func emailHash(email string) string {
	sum := sha256.Sum256([]byte(EmailKey(email)))
	return hex.EncodeToString(sum[:])
}

// Verdict is what the limiter says about one attempt before it is made.
type Verdict int

const (
	// Allow: try the password.
	Allow Verdict = iota
	// Delay: try the password, after SoftDelay.
	//
	// It is the answer to an account whose failures came from many addresses,
	// and to an address that has failed against many accounts. Neither can be
	// refused outright: refusing the first is the lockout an attacker triggers
	// on somebody else's behalf, and refusing the second locks out an office
	// behind one NAT. A delay costs the person one pause and costs an attacker
	// the whole rate of their attack.
	Delay
	// Refuse: too many failures for this one account from one or two places,
	// which is somebody guessing a password rather than somebody attacking the
	// lockout. The caller gets ErrTooManyAttempts.
	Refuse
)

// Check is what the limiter says about an attempt on this address from this one, and
// the account's own counter as the read that decided it described it: the quota, what
// is left under it, and what is left of the window. It reads and does not record;
// Failed and Succeeded are the writes.
//
// The numbers come back with the verdict for the same reason kit/limit answers a
// Reserve in one call: an answer refused on this counter has to name it, and a reading
// taken after the refusal is a second look at a row somebody else has moved. A counter
// that could not be read decides Allow and reports no Decision at all, which is
// Limiter's stated failure mode rather than a new one.
//
// What is named is the account's counter, and only that one. The per-address and
// per-account-source counters are what tell a person from a botnet, and an answer that
// named them would tell whoever is running the botnet how much of it is left to spend:
// they decide quietly, and the Delay they earn is the whole of what a caller hears
// about them.
func (l *Limiter) Check(ctx context.Context, email, ip string) (Verdict, Decision) {
	attempts, left, counted := l.read(ctx, accountKey(email))
	verdict := Allow
	switch {
	case attempts >= MaxAttempts:
		if l.count(ctx, sourcesKey(email)) > MaxSources {
			// Locked, but by a crowd: this is somebody trying to lock the account's
			// owner out, and answering "too many attempts" is doing it for them.
			verdict = Delay
		} else {
			verdict = Refuse
		}
	case ip != "" && l.count(ctx, sourceKey(ip)) >= SourceAttempts:
		// One machine working through a list of addresses. Each account's own
		// counter is untouched, so this is the only thing that sees it.
		verdict = Delay
	}
	if !counted {
		return verdict, Decision{}
	}
	return verdict, Decision{
		Policy:    Policy{Name: LoginAttemptsPolicyName, Quota: MaxAttempts, Window: AttemptWindow},
		Allowed:   verdict != Refuse,
		Remaining: max(MaxAttempts-attempts, 0),
		Left:      left,
	}
}

// Failed records one failure, from one address, against one account.
func (l *Limiter) Failed(ctx context.Context, email, ip string) {
	l.record(ctx, accountKey(email), MaxAttempts)
	if ip == "" {
		return
	}
	// The first failure of this pair in this window, and only it, is a new
	// place this account has been attacked from.
	if l.within(ctx, pairKey(email, ip), 1) {
		l.record(ctx, sourcesKey(email), MaxSources)
	}
	l.record(ctx, sourceKey(ip), SourceAttempts)
}

// Succeeded forgets an account's failures: somebody proved they are who they
// said. The addresses stay — one correct password out of fifty attempts is what
// a successful guessing run looks like — and so does the count of places this
// account has been attacked from, because a sign-in is not evidence that the
// attack stopped. The consequence is the safe one: a lockout that follows is a
// delay rather than a refusal.
func (l *Limiter) Succeeded(ctx context.Context, email string) {
	if err := l.store.Forget(ctx, accountKey(email)); err != nil {
		slog.ErrorContext(ctx, "auth: the limiter could not forget an account", "error", err)
	}
}

// Requested counts one account request from an address against the configured
// budget and reports the cap that decided it, with its numbers. The address is
// the one asking; the address being asked about is never counted, because a cap
// that depended on it would answer the question the route exists not to answer.
func (l *Limiter) Requested(ctx context.Context, ip string) Decision {
	return l.reserve(ctx, AccountRequestsPolicyName, forgotKey(ip), l.requests.Quota, l.requests.Window)
}

// VerificationMail reserves one resend per tenant/mailbox before any lookup.
// A denied reservation receives the same public acknowledgment as an accepted
// one. Unlike login's availability policy, a failed mail limiter fails closed.
func (l *Limiter) VerificationMail(ctx context.Context, email string) (bool, error) {
	key := "auth/verification-mail/" + emailHash(email)
	allowed, _, _, err := l.store.Reserve(ctx, key, 1, VerificationResendInterval)
	return allowed, err
}

// Redeemed counts one reset-token redemption from an address and reports the cap
// that decided it, with its numbers. It counts the address presenting the token and
// never the token, for the reason Requested counts the address asking: a cap that
// depended on what was presented would answer a question about it.
func (l *Limiter) Redeemed(ctx context.Context, ip string) Decision {
	return l.reserve(ctx, ResetRedemptionsPolicyName, redeemKey(ip), ResetRedemptions, AttemptWindow)
}

// Prompted counts one passkey ceremony begun from an address and reports whether
// it is within PasskeyPrompts for this window. It counts the address that asked
// for the prompt and never the address the prompt was asked about: the second of
// the two would turn the cap into the account-enumeration question the ceremony's
// begin leg exists not to answer.
func (l *Limiter) Prompted(ctx context.Context, ip string) bool {
	return l.within(ctx, promptKey(ip), PasskeyPrompts)
}

// AskedAboutMail counts one read of what became of the mail one of this
// address's own calls asked for, and reports whether it is within
// MailDeliveryAsks for this window. It counts the address asking and never the
// request it asks about, for the reason every other public cap here does: a cap
// that depended on which request was named would be an answer about it.
func (l *Limiter) AskedAboutMail(ctx context.Context, ip string) bool {
	return l.within(ctx, mailAskKey(ip), MailDeliveryAsks)
}

// Noted reports whether a refusal for this account from this address is worth
// writing down, which is the first one in the window and no more.
//
// A locked-out account under a script is refused before anything is checked,
// and recording each refusal was two audit rows per attempt for a fact that
// does not change: the first one says the account is under attack and the
// nine hundredth says it still is. The failures themselves are still recorded
// one for one — those are attempts that were actually made.
func (l *Limiter) Noted(ctx context.Context, email, ip string) bool {
	return l.within(ctx, notedKey(email, ip), 1)
}

// count is how many events a key has in the window, or zero when the store
// cannot be reached: a limiter that refused what it could not count would make
// an unreachable table an installation nobody can sign in to.
func (l *Limiter) count(ctx context.Context, key string) int {
	n, _, _ := l.read(ctx, key)
	return n
}

// read is a window's own account of itself: how many events it holds, how long it has
// left, and whether the counter answered at all. Every caller of this is a check on a
// limit, and a check that could not be made decides nothing and states nothing — which
// is the zero of every number here, not a count of zero events.
func (l *Limiter) read(ctx context.Context, key string) (int, time.Duration, bool) {
	n, left, err := l.store.Count(ctx, key, AttemptWindow)
	if err != nil {
		slog.ErrorContext(ctx, "auth: the limiter could not be read; the attempt is allowed", "key", key, "error", err)
		return 0, 0, false
	}
	return n, left, true
}

// within records one event and reports whether the key is still inside max. The
// quota the statement also reports is dropped here: the caps that answer on the wire
// keep their own reading (Requested, Redeemed), and this is the lockout's, whose
// verdict Check reads out of a count taken separately — a number nothing reads is a
// number nothing keeps honest.
func (l *Limiter) within(ctx context.Context, key string, max int) bool {
	ok, _, _, err := l.store.Reserve(ctx, key, max, AttemptWindow)
	if err != nil {
		slog.ErrorContext(ctx, "auth: the limiter could not be written; the attempt is allowed", "key", key, "error", err)
		return true
	}
	return ok
}

// reserve is within with the numbers kept: the decision, the quota left and the
// window left, all three out of the one statement that recorded the event. They are
// kept because a route that refuses on a counter has to say which counter it refused
// on and for how long, and a limit stated from a reading taken after the decision is a
// guess — which is kit/limit's reason to answer in one call rather than two. The
// window is the caller's, because one of these caps is configured and the rest are not.
//
// A store that cannot be reached gives the zero Decision, not a refusal: that is
// Limiter's stated failure mode, and the zero value cannot be "not allowed" without
// turning an unreachable counter table into an installation nobody can use. A handler
// reads Decided before it states anything.
func (l *Limiter) reserve(ctx context.Context, name, key string, max int, window time.Duration) Decision {
	allowed, remaining, left, err := l.store.Reserve(ctx, key, max, window)
	if err != nil {
		slog.ErrorContext(ctx, "auth: the limiter could not be written; the attempt is allowed", "key", key, "error", err)
		return Decision{}
	}
	return Decision{
		Policy:    Policy{Name: name, Quota: max, Window: window},
		Allowed:   allowed,
		Remaining: remaining,
		Left:      left,
	}
}

// Policy is one cap as the response names it: a name, a quota, and the window the
// quota is measured over.
type Policy struct {
	Name   string
	Quota  int
	Window time.Duration
}

// Decision is one cap's answer with the numbers that came out of the statement that
// decided it. It is a type of this module rather than four return values because the
// three numbers belong to one another — a quota stated beside a window nobody counted
// is the mistake the port exists to prevent — and because the zero value has to mean
// "the counter never answered", which one bool cannot say.
type Decision struct {
	Policy    Policy
	Allowed   bool
	Remaining int
	Left      time.Duration
}

// Decided reports whether a counter answered at all. False is the store that could not
// be reached: the attempt is allowed and the answer states no limit, because the only
// numbers on offer are ones nobody read.
func (d Decision) Decided() bool { return d.Policy.Name != "" }

// record counts an event whose verdict is read separately, because the lockout
// has three answers rather than two: what a failure is worth is Check's to say,
// after the count that Failed has just raised.
func (l *Limiter) record(ctx context.Context, key string, max int) { _ = l.within(ctx, key, max) }

// EmailKey is the address as this module counts it: one that differs only in
// case or in whitespace is the same account being attacked, and the same
// account in an audit trail. It is exported because the event that records a
// failure has to spell the address the same way the limiter counted it.
func EmailKey(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// The two caps whose answer goes on the wire, under the names the wire gives them.
//
// A policy name belongs to a counter and not to a route, because `RateLimit-Policy`
// promises to stay the same across a sequence of responses: the cap behind Requested
// is spent by the forgotten-password route, the account-request route and the
// verification resend alike, and a client told three names for one window has been
// told three limits it is not under.
const (
	AccountRequestsPolicyName  = "auth-account-requests"
	ResetRedemptionsPolicyName = "auth-reset-redemptions"
	// LoginAttemptsPolicyName is the lockout itself: MaxAttempts failures against one
	// account inside AttemptWindow. It is one name for the two doors that spend it —
	// the password check and the second-factor leg, which are the same account under
	// the same script — and the address's own counters are named nowhere, because an
	// answer that stated them would state how much of an attack is still affordable.
	LoginAttemptsPolicyName = "auth-login-attempts"
)
