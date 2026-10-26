package page

// The page a person gets when the kernel refused before any handler ran.
//
// ui/page has always made a document for a handler's own 4xx — Serve turns a returned
// problem into Fault. What it never had was the other half: a cross-site write refused
// by the CSRF guard, a handler that panicked, a request refused by a guard that runs
// ahead of routing. Those never reach a handler, so they answered a browser with a JSON
// body and a request id that could not be selected from a JSON blob in a window frame.
//
// FaultHandler is how the presentation layer closes that. The application registers it
// once at httpx.New, and every guard in the kernel then answers a navigating client with
// this shell's page — same chrome, same stylesheet, same way back, and the verdict's own
// status rather than a 200 that says "Forbidden" in it.
//
// # What the page says, in what language
//
// A refusal has one value and two shapes (docs/adr/0015): the problem document a program
// reads and the sentence a person is shown. The first is the same in every language
// because a program reads a code; the second used to be English by construction, because
// the sentence was a string literal in the guard that made it. The guards publish
// the codes they travel in (httpx's Code* constants) and every guard that refuses
// a request a person could be looking at answers through API.refuse, which is what
// makes this table worth filling in the first place: a code no page is ever shown
// for is a key nobody can ship copy under. The page is then negotiated from the
// request through the same contract every other page of this package uses —
// Shell.Messages and SelectLocale and Formatter.Text, and the same two headers
// Serve writes for a page it translated.
//
// A shell that ships no catalog is shown the source-language sentence and keeps
// declaring "en", which is what it always did and what the copy still is. Which
// languages a deployment's failure page speaks is the composition's, as it is for
// every page here: this file holds the mechanism, messages/ holds the copy — the
// English of the lines this package writes itself, and the Portuguese of every line
// including a guard's, whose English is the guard's own sentence.

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/document"
)

// backWay is the shell's label, translated the way the heading is — but only for the
// destination this catalogue words (homeKey), which is the one the shell labelled with
// the very English the catalogue carries for it. A shell whose link leads somewhere else, or
// whose words are its own, has its own label back: translating it here would tell a
// person one destination while the href carried another, and the translation is
// offered only for the destination these words name.
func backWay(loc *Locale, label string) string {
	if label != own(homeKey) {
		return label
	}
	if words := strings.TrimSpace(word0(loc, homeKey)); words != "" {
		return words
	}
	return label
}

// shown is the half of a refusal this shell may repeat to the person in front of it:
// everything the writer said about their own request, and nothing it marked an operator's.
//
// A Diagnostic Detail names a host, a query, a path or a driver's message. The problem
// document a monitor diffs and the log line keep it; the page is left with the verdict, the
// reference the person can quote back and the catalogue's sentence for the verdict (faultKey
// keys that case for every status whose sentence this package writes). Both doors ask through
// this one function: FaultHandler, which renders a guard's refusal ahead of any handler, and
// Serve, which renders a handler's own 4xx as markup — and a response that is already HTML has
// no later stage that could take the detail back out of it, which is why the judgement cannot
// live on one side of the pair. See (*API).renegotiate for the JSON door's half.
func shown(p *problem.Problem) string {
	if p == nil || p.Diagnostic {
		return ""
	}
	return p.Detail
}

// refusalLocale is the language this refusal is answered in, or nil for a shell that ships
// no catalog at all.
//
// It is the same contract every other page of this package uses — the tenant's set, the
// caller's header filtered against it, the tenant's default behind that — and the tenant
// arrives because the kernel resolves the address's host before it renders a refusal as a
// page (kit/httpx's withHostTenant), which it may, because a host is a fact a refused
// request still has. What a guard has no access to is everything Shell.Locale reads: a
// URL, an account, a stored preference, each of which needs something this response is the
// refusal to have — a session that was accepted, an open transaction, a handler to run at
// all. Those are absent whatever the verdict, and the caller's own header is what is left
// of the preference list — which is also exactly what makes Vary: Accept-Language the true
// thing to say about the response.
func refusalLocale(m Messages, r *http.Request, tenant tenancy.Tenant) *Locale {
	if m == nil {
		return nil
	}
	// The tenant the request resolved to: the languages it is served in are as much a
	// fact about a refusal as about any other page, and a tenant that declared one
	// language was not refusing to be answered in another. An address whose host names no
	// tenant — a domain nobody serves, a database that could not say — has no declaration
	// to filter by, and the deployment's own catalog is then the only one standing.
	loc := SelectLocale(m, TenantPreferences(Request{Tenant: tenant}, r.Header.Get("Accept-Language"))...)
	return &loc
}

// FaultHandler builds the kernel's failure renderer for one shell. Back and BackLabel
// are the shell's own, so the page offers the way out that exists in that application:
// a generated admin shell sends a stranger to its sign-in; a public site sends them home.
//
// It returns false — leaving the JSON body — for a shell with no frame, because a page
// with no chrome is not a page and a kernel that invented one here would be designing
// interface in the wrong package.
func FaultHandler(s Shell) httpx.Fault {
	return func(w http.ResponseWriter, r *http.Request, p *problem.Problem) bool {
		if s.Frame == nil || p == nil {
			return false
		}
		status := p.Status
		if status == 0 {
			status = http.StatusInternalServerError
		}
		// Two refusals do not answer a person with their Detail. A Diagnostic one names
		// something only an operator can act on, so the page is left with the verdict and
		// the catalogue's sentence for it; the detail stays in the problem body and the log
		// line. A refusal that names a Key is a writer that ships copy for its own verdict
		// and wants it resolved in the reader's language rather than repeated in one.
		detail, key := shown(p), p.Key
		ctx := r.Context()
		req := read(ctx, s.Chrome)
		loc := refusalLocale(s.Messages, r, req.Tenant)
		// The frame is given the negotiated language for the same reason the sentence is:
		// a shell whose chrome has labels of its own renders them here as it does on every
		// page Serve mounts, and not in English because the request was refused.
		req.Locale = loc
		v, ok := granted(r, loc, status, detail, requestID(p.Instance), s)
		if !ok {
			// Retry-After is read here, before the status is written, because after
			// WriteHeader the guard's header is the one thing a rewrite of this function
			// could lose sight of — and it is the only number the page is allowed to say.
			v = fault(verdict{status: status, detail: detail, key: key,
				reference: requestID(p.Instance), loc: loc,
				back: s.Back, backLabel: s.BackLabel, path: r.URL.Path,
				ask: r, signIn: s.Chrome.SignIn, wait: retryAfter(w.Header())})
		}

		body := s.Frame(ctx, req, v.Body)
		out, err := Render(Document(s.Chrome, req, v, body), status)
		if err != nil {
			// The document could not be built. Say nothing about why to the person and
			// let the kernel's JSON answer, which is honest about being a failure.
			return false
		}
		w.Header().Set("Content-Type", out.ContentType)
		if out.CacheControl != "" {
			w.Header().Set("Cache-Control", out.CacheControl)
		}
		if loc != nil {
			// The two headers every page of a translated shell carries (see Serve), for
			// the reason every page carries them: the copy is this language and a cache
			// that ignores Accept-Language would answer a Portuguese refusal to the next
			// person who asked in English.
			w.Header().Set("Content-Language", v.Language)
			w.Header().Set("Vary", "Accept-Language")
		}
		w.WriteHeader(status)
		_, _ = w.Write(out.Body)
		return true
	}
}

// verdict is one refusal as the page sees it: the status and the line its writer wrote,
// the language it is answered in, the reference an operator greps a log for, the shell's
// own way out and the address the person was at, and what the page was about when a
// generated screen said so. key is a refusal's own catalogue key where its writer ships
// copy for it ("" wherever this layer's table decides), and wait is the guard's own
// Retry-After in whole seconds. ask is the request the refusal answers, when one was
// carried: the referrer, the method and the address a page can offer again come from it,
// and a page rendered where no request was carried gets the verdict and the ways on that
// do not need one. Nothing here guesses at an address it could not read.
type verdict struct {
	status                        int
	detail, key, reference        string
	back, backLabel, path, signIn string
	wait                          int
	sub                           Subject
	loc                           *Locale
	ask                           *http.Request
}

// fault is the refusal document: what happened, in the language this shell speaks it, and
// every way on that this request actually offers — this address again for a verdict that
// may pass on a second attempt, the list a record belongs to, the page the visitor came
// from, the destination the composition owns, sign in for the one verdict signing in
// changes, and the reference an operator finds the request by in a log. A control for a
// door nobody mounted is the page lying, so each appears only where the request offers
// one: a POST gets the wait and never a link that could not resend it, and an off-site
// referrer is no way back (cameFrom, retryAfter).
//
// The sentence is one of three things: this layer's own words where the page declared a
// subject or the verdict is a plan exclusion (plainWords), the shell's catalogue for the
// code the guard travels under, or the guard's line as written — because a refusal nobody
// has read is never smoothed over. The declared language follows whichever is on the page,
// not the negotiation. What a translation replaces is the sentence, never the code: the
// code is the half a person reads back to support and an operator greps a log for, so it
// stays — at the head of the line where the guard wrote it, on the data line below where
// this layer wrote the sentence in its place.
func fault(rf verdict) View {
	plain, english, plainArgs, retryTo := plainWords(rf.sub, rf.status, rf.detail)
	key := plain
	if key == "" {
		key = rf.key
	}
	line, language := rf.detail, ""
	if strings.TrimSpace(line) == "" {
		// A 500 carries no detail on purpose, and neither does a diagnostic: the reason is
		// in the log, not for the browser. The sentence has to be true and useful without
		// it — and when the catalogue speaks this verdict, which it does for both of the
		// languages this package ships, that copy replaces this line below.
		line = "Something went wrong while handling this."
	}
	code, _, prefixed := strings.Cut(rf.detail, ": ")
	shownCode := ""
	if plain != "" {
		var spoken bool
		if line, spoken = speak(rf.loc, plain, english, plainArgs...); spoken {
			language = rf.loc.Language
		}
		if prefixed {
			shownCode = code
		}
	} else {
		line, language = sentence(line, rf.loc, rf.status, rf.detail, rf.key)
		if rf.key == "" {
			line, language = missingGrant(line, language, rf.loc, prefixed, code, rf.detail)
		}
	}
	p := document.FaultProps{
		// The heading and the way on are the catalogue's, in both shapes of this
		// page: a heading that comes from net/http and a home label that comes from
		// the shell are words no translator can reach, and this page is read when a
		// person least wants to be reading it.
		Status: rf.status, Title: statusTitle(rf.loc, rf.status, rf.detail, rf.sub),
		Sentence: line, Tone: toneOf(key),
		Way: rf.back, WayLabel: backWay(rf.loc, rf.backLabel),
		BackLabel: word0(rf.loc, backKey),
	}
	if rf.reference != "" {
		p.Reference = rf.reference
		p.ReferenceLabel = strings.TrimSpace(word0(rf.loc, referenceLabelKey))
	}
	p.Code = shownCode
	// The way out: a subject page sends a person back to the list the item belongs to,
	// a verdict that may pass on a second attempt sends them to this address again, and
	// everything else takes the shell's way out, which is the one it promised.
	switch {
	case (key == recordMissingKey || key == recordDeniedKey) && httpx.LocalPath(rf.sub.List):
		p.Way, p.WayLabel = rf.sub.List, label(rf.loc, backToKey, rf.sub.Plural)
	case retryTo && httpx.LocalPath(rf.path):
		p.Retry, p.RetryLabel = rf.path, word0(rf.loc, retryKey)
	}
	if rf.ask != nil {
		p.Back = cameFrom(rf.ask)
		if p.Retry == "" && transient(rf.status) && safeToResend(rf.ask.Method) {
			p.Retry = (&url.URL{Path: rf.ask.URL.Path, RawQuery: rf.ask.URL.RawQuery}).String()
			p.RetryLabel = word0(rf.loc, retryKey)
		}
	}
	if rf.wait > 0 {
		// One sentence, and the label is a sentence with the number in it rather than
		// a label beside a bare integer: "3" on its own tells nobody anything.
		p.RetryAfter, p.RetryAfterLabel = rf.wait, label(rf.loc, "fault.retry_after", rf.wait)
	}
	if rf.signIn != "" && rf.status == http.StatusUnauthorized {
		p.SignIn, p.SignInLabel = rf.signIn, word0(rf.loc, "fault.sign_in")
	}
	v := document.FaultPage(p)
	if language != "" {
		v.Language = language
	}
	return v
}

// transient is the verdict that says "our side, and for a moment": the two the kernel
// writes when something it depends on could not be consulted, or answered too often.
// A 500 is not one of them — waiting does not fix a handler that is broken — and
// neither is a 404, which is immutable whatever the person does but change the address.
func transient(status int) bool {
	return status == http.StatusServiceUnavailable || status == http.StatusTooManyRequests
}

// safeToResend is the methods an anchor can re-issue by asking for the same address
// again. A POST is not one: the page would promise to press a button for a person
// whose button carried a form.
func safeToResend(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

// cameFrom is the visitor's way back, or "" when there is no honest one: only a
// referrer that is a path on this site, checked the same way every other address a
// page may point at is checked (httpx.LocalPath). An off-site referrer is not this
// page's business, and an absolute URL in an href is an open redirect.
func cameFrom(r *http.Request) string {
	ref := r.Referer()
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil || (u.Host != "" && httpx.HostOnly(u.Host) != httpx.HostOnly(r.Host)) {
		return ""
	}
	if !httpx.LocalPath(u.Path) {
		return ""
	}
	return (&url.URL{Path: u.Path, RawQuery: u.RawQuery}).String()
}

// retryAfter is the guard's own wait, read off the response header in whole seconds.
// Only delta-seconds count: RFC 9110 allows an HTTP-date as well, and this kernel
// never writes one (every Retry-After it sets is a strconv'd integer), so a date here
// would be a number this page has no business guessing.
func retryAfter(h http.Header) int {
	secs, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After")))
	if err != nil || secs < 0 {
		return 0
	}
	return secs
}

// granted is the refusal page for the two verdicts a person can be told more
// about: the grant question (AUTH_DENIED) and the row question (POLICY_DENIED).
// Every other verdict — a 404, a cross-site write, a handler that panicked, a
// plan that does not include a feature — is answered by the page below, byte for
// byte as it always was, because the parts below would name a grant nobody
// withheld.
//
// The order is the one the brief walks: the verdict, code first, as the page has
// always shown it; what is missing, in the words of the module that defines it;
// who can grant it, as a role and never as a list of people; and the way on,
// which is never the address that refused. The ask control is drawn only where the
// composition mounted the door it posts to.
func granted(r *http.Request, loc *Locale, status int, detail, reference string, s Shell) (document.View, bool) {
	ref, ok := httpx.Refused(r.Context())
	if !ok || status != http.StatusForbidden ||
		(ref.Code != httpx.CodeDenied && ref.Code != httpx.CodePolicyDenied) {
		return document.View{}, false
	}
	line, language := sentence("", loc, status, detail, "")
	if line == "" {
		line = strings.TrimPrefix(detail, ref.Code+": ")
	}
	if language == "" && reference != "" {
		// No sentence from the catalogue: the guard's whole line, code first, exactly
		// as the page has always shown it. The reference itself is said below.
		line = detail
	}
	p := document.RefusalProps{Status: status, Title: statusTitle(loc, status, detail, Subject{}),
		Sentence: line, Home: s.Back, HomeLabel: backWay(loc, s.BackLabel)}
	if reference != "" {
		p.Reference = reference
		if words := strings.TrimSpace(word0(loc, referenceKey)); words != "" {
			p.ReferenceWords = words
		} else {
			p.ReferenceWords = referenceWords
		}
	}
	if ref.Code == httpx.CodePolicyDenied {
		p.Missing, _ = word(loc, "fault.holds_the_row", ref.Reason)
	} else {
		p.Missing, _ = word(loc, "fault.missing",
			named(loc, "permission."+ref.Permission, ref.Label))
		p.Granter, _ = word(loc, "fault.granter",
			named(loc, "permission."+s.Granter.Permission, s.Granter.Label))
		// Both the shell's address and a wired door: a composition that supplies
		// the page route but no reach would draw a form that refuses.
		if s.Ask != "" && httpx.CanAsk(r.Context()) {
			p.Ask, p.AskLabel = s.Ask, word0(loc, "fault.ask")
			p.Permission, p.Path = ref.Permission, ref.Path
		}
	}
	v := document.Refusal(p)
	if language != "" {
		// The verdict's own line is this language, which is the line the page is
		// read for; a part the catalogue has no copy for is English beside it, as
		// an untranslated screen is. Declaring the negotiation rather than the
		// lowest common denominator is what every other page of this package does.
		v.Language = language
	}
	return v, true
}

// word is one line of the refusal page through the shell's catalogue, and whether
// the catalogue actually spoke it — a page the catalogue has no copy for is English
// and must not declare Portuguese. The English behind a key is messages/en.json's,
// read by own, so a line this package writes exists in the catalogue files and
// nowhere in this file.
func word(loc *Locale, key string, args ...any) (string, bool) {
	fallback := own(key)
	if loc == nil {
		return sprintf(fallback, args...), false
	}
	out := loc.Text(key, fallback, args...)
	return out, out != sprintf(fallback, args...)
}

// sprintf is the fallback formatting Locale.Text itself applies to a key no
// catalogue answers, applied here so "did the catalogue speak?" is a fair test.
func sprintf(format string, args ...any) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

// word0 is a line with no arguments to interpolate: the refusal page's own labels.
func word0(loc *Locale, key string) string {
	out, _ := word(loc, key)
	return out
}

// named is a line whose text arrives with the refusal — a permission's own label,
// which the catalog answer for it and the grant table both read — so there is no
// copy of it in this package's catalogue to fall back on.
func named(loc *Locale, key, text string) string {
	if loc == nil {
		return text
	}
	return loc.Text(key, text)
}

// label is one of the page's own short lines through the catalogue, with the
// arguments the copy asks for. Whether the catalogue spoke it is not recorded
// here because the verdict's sentence already decides the declared language, and
// the rule that decides which of the two is on the page is stated above: a part
// the catalogue has no copy for is English beside it, as an untranslated screen is.
func label(loc *Locale, key string, args ...any) string {
	out, _ := word(loc, key, args...)
	return out
}

// requestID is the instance URN read back into the bare identifier, which is what a
// human reads aloud to a human reading a log.
func requestID(instance string) string {
	if instance == "" {
		return ""
	}
	return strings.TrimPrefix(instance, "urn:request:")
}
