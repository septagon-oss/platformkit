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

// faultKeys is the inventory of the refusal codes kit/httpx publishes that this shell
// ships a sentence for, and the catalogue gate in catalogue_test.go reads it as one: a
// code in the kernel with no key beside it is a refusal a Portuguese tenant is answered
// in English, and nobody has to notice.
//
// The code is namespaced, because a kernel refusal and a module's own copy share one
// catalog and "AUTH_DENIED" on its own is a name either of them could want — and because
// this shell claims the `fault.` prefix against every catalogue that comes after it
// (kit/locale/providers/xtext refuses a later source a key under it), the codes named
// here are the codes allowed to be shown under that prefix. A module with a page of its
// own, and so with copy for refusals only it makes, names its entry on the refusal itself
// instead: see problem.Problem's Key and sentence. Two codes are absent here on purpose
// rather than by oversight, for one reason: CodeWriteElsewhere names the address the write
// belongs at and CodePlanExcludes names the feature to ask the plan for, and a sentence
// that carries something the caller has to *have* cannot be replaced by one that only
// describes it — this mechanism swaps a sentence and does not interpolate an argument. Neither is carried
// by any catalogue either, which is the same refusal stated as data.
var faultKeys = map[string]string{
	httpx.CodeAnonymous:         "fault.AUTH_ANONYMOUS",
	httpx.CodeDenied:            "fault.AUTH_DENIED",
	httpx.CodeNotOperator:       "fault.AUTH_NOT_OPERATOR",
	httpx.CodeUndeclared:        "fault.AUTH_UNDECLARED",
	httpx.CodeNoTenant:          "fault.AUTH_NO_TENANT",
	httpx.CodePrincipalChanged:  "fault.AUTH_PRINCIPAL_CHANGED",
	httpx.CodeCSRFOrigin:        "fault.CSRF_ORIGIN",
	httpx.CodePublicSetsACookie: "fault.PUBLIC_SETS_A_COOKIE",
	httpx.CodeLimitExhausted:    "fault.LIMIT_EXHAUSTED",
}

// refusalParts is every catalogue key the refusal page looks up besides the
// verdict's own sentence: the four parts of a grant denial, the row denial's one,
// and the confirmation the ask lands on. The test that reads this list is the
// gate that keeps a part from shipping in English to a tenant served in
// Portuguese, which is the defect the whole page exists to close. Each of these
// lines is copy this package writes, so each has an English entry in
// messages/en.json as well and no literal of one survives in this file.
var refusalParts = []string{
	"fault.missing", "fault.granter", "fault.ask", "fault.holds_the_row",
	"fault.sent", "fault.sent_body",
	// The five words the generic verdict page adds above its sentence. They are
	// in this list rather than in the page because the gate that reads the list is
	// what refuses a Portuguese tenant an English label — and a person who is
	// offered "Try again" in English on a page otherwise in Portuguese is exactly
	// the defect this page exists to close, wearing a smaller hat.
	"fault.retry", "fault.retry_after", "fault.back", "fault.sign_in", "fault.request_reference",
}

// faultKey is the catalog key of the sentence a refusal is shown in, and whether
// there is one to look for at all. It is the answer for a refusal that names no key of
// its own — every guard's, and the verdicts nobody coded.
//
// A guard writes "<CODE>: <sentence>", so the prefix is the lookup and not the
// copy: the code is in the JSON body and in the log line whatever the page says in
// response, and a shell that ships the sentence for a code answers a refusal in the
// language it ships it in. A code the table does not carry is left exactly as
// written — an unlisted code is a sentence this package has not read, and a shell's
// generic copy standing in for it would hide whatever that sentence says.
//
// A refusal with no code at all is keyed by its verdict, and only for the four
// verdicts whose sentence this layer writes because nobody else wrote it: the 404
// of an address nobody mounted, the 405 of an address that does not take the verb
// it was asked with, the 500 of a handler that broke, and the 503 of a guard whose
// own decision could not be made. Every one of those four sentences is kit/httpx's
// own, written for the page this package renders it on.
//
// A 400, a 409 or a 422 is not one of them: it carries a sentence about the
// caller's own request, written by a module or by the decoder, and translating
// that is the writer's share — a sentence about the status would paper over the
// one thing the caller could act on. The 405 belongs with the 404 and the 500 on
// that same ground and not beside the 400: "this address does not accept POST
// requests" is the kernel's own words about the address and not about the
// caller's request, and it is the refusal a derived client meets most often of
// all — the catalog's write_path exists because the kernel keeps refusing clients
// at the wrong verb.
//
// The 503 joins the 500 on the same ground as the other three: the sentences are
// the kernel's about its own outage ("authorization is temporarily unavailable",
// "the plan could not be read right now", "this host cannot be resolved right
// now"), and a person cannot act on which subsystem is down — the guard logs each
// one with its reason and sends Retry-After, so what the page owes the person is
// that it is our side and it is for a moment. Which of them it was stays in the
// problem document a monitor reads. Left untranslated it is the one refusal a
// tenant served in Portuguese is handed in English on an outage — the outage being
// the moment a person is most likely to be reading it.
func faultKey(detail string, status int) (key string, lookup bool) {
	if code, _, named := strings.Cut(detail, ": "); named {
		key, shipped := faultKeys[code]
		return key, shipped
	}
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusInternalServerError, http.StatusServiceUnavailable:
		return "fault." + strconv.Itoa(status), true
	}
	return "", false
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
		detail, key := p.Detail, p.Key
		if p.Diagnostic {
			detail = ""
		}
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
			v = fault(r, status, detail, key, loc, requestID(p.Instance), s, retryAfter(w.Header()))
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

// fault is the refusal document, with the three things a person in front of one
// actually needs: what to do next, and the reference an operator can find the
// request by in a log. The reference is the same URN the JSON body carries, so a
// screenshot and a log line agree — which matters most for the failures that are
// nobody's fault and still happened.
//
// # Which doors appear on it, and why that is a rule and not a taste
//
// Every affordance below exists iff the request actually offers it, because a link
// to somewhere a person cannot get is worse than no link: it is the page lying.
//
//   - Retry is the request's own address, and it appears for the two verdicts that
//     are "our side, and for a moment" (503, 429) — and only on a method an anchor
//     can re-issue. A POST gets the wait, never the button: a link cannot resend a
//     registration, and a page that pretends otherwise throws away what the person
//     typed. Their own Back button resends it.
//   - The wait sentence appears whenever a guard named a number, whatever the
//     method, because the number is true either way. Retry-After as an HTTP-date is
//     read as absent (retryAfter): this kernel only ever writes delta-seconds, and
//     guessing at a date would be the page inventing a wait.
//   - Back is the referrer, and only a path on this site — an off-site referrer
//     would be an invitation to bounce somebody else's traffic, and a full URL in
//     an href is an open redirect wearing a label.
//   - Home is the composition's Shell.Back, and Sign in is the chrome's own address
//     on the one verdict signing in changes. Both are offered only where a shell
//     mounted them; a control for a door nobody hung is the same lie.
//   - The reference appears whenever the verdict carried an instance, which is every
//     page this renders.
//
// The sentence is the shell's own when the shell's catalog holds this refusal, and the
// kernel's English one when it does not. The declared language follows whichever of those
// is actually on the page and not the negotiation: a refusal the catalog has no sentence
// for is English copy whatever the caller asked for, and a document that declares
// Portuguese over an English line lies in the same shape as the page that was English to
// everybody.
//
// What the translation replaces is the sentence and not the guard's whole line. A guard
// writes "<CODE>: <sentence>" and the code is the half of that a person reads back to
// support and an operator greps a log for; it is in the problem document and the log
// line whatever the page says, and dropping it here would leave the page the only place
// the refusal has no name. So the code is kept and the copy after it is what changes.
// deniedPermissionKey is the catalogue key of the sentence for a missing grant, with
// the permission as its one argument. Its English half lives in messages/en.json like
// every other line this package writes: it is the one sentence that says what the
// short catalogue sentence drops, and it is copy rather than code.
const deniedPermissionKey = "fault.AUTH_DENIED.permission"

func fault(r *http.Request, status int, detail, key string, loc *Locale, reference string, s Shell, wait int) View {
	line := detail
	if strings.TrimSpace(line) == "" {
		// A 500 carries no detail on purpose, and neither does a diagnostic: the reason is
		// in the log, not for the browser. The sentence has to be true and useful without
		// it — and when the catalogue speaks this verdict, which it does for both of the
		// languages this package ships, that copy replaces this line below.
		line = "Something went wrong while handling this."
	}
	line, language := sentence(line, loc, status, detail, key)
	// A missing grant names the grant and who can give it (UX walkthroughs, 2026-09-30: an editor who signed
	// in met "Não pode fazer isto." and could not say what to ask for; the administrator helping her could not
	// tell what to give). The guard already wrote the permission into its detail and the translated sentence
	// dropped it. A catalogue with the full sentence answers with it; one that carries only the short sentence
	// keeps its language and gains the permission, so no shell answers a denial in two languages.
	if code, rest, named := strings.Cut(detail, ": "); named && code == httpx.CodeDenied {
		if permission, ok := strings.CutPrefix(rest, "this operation requires "); ok && permission != "" {
			words := sprintf(own(deniedPermissionKey), permission)
			switch {
			case loc != nil && (loc.Language == sourceLanguage ||
				loc.Text(deniedPermissionKey, words, permission) != words):
				line, language = code+": "+loc.Text(deniedPermissionKey, words, permission), loc.Language
			case language != "":
				line = line + " (" + permission + ")" // the shell's own short sentence, in its own language
			default:
				line = code + ": " + words
			}
		}
	}
	p := document.FaultProps{
		Status: status, Title: http.StatusText(status), Sentence: line,
		Home: s.Back, HomeLabel: s.BackLabel,
		BackLabel: word0(loc, "fault.back"),
	}
	// A handler's own 4xx arrives through Serve, which hands the request when it
	// can (httpx.RequestFrom) — and a page rendered where no request was carried
	// gets the verdict and the ways on that do not need one. Nothing here guesses
	// at an address it could not read.
	if r != nil {
		p.Back = cameFrom(r)
		if transient(status) && safeToResend(r.Method) {
			p.Retry = (&url.URL{Path: r.URL.Path, RawQuery: r.URL.RawQuery}).String()
			p.RetryLabel = word0(loc, "fault.retry")
		}
	}
	if wait > 0 {
		// One sentence, and the label is a sentence with the number in it rather than
		// a label beside a bare integer: "3" on its own tells nobody anything.
		p.RetryAfter, p.RetryAfterLabel = wait, label(loc, "fault.retry_after", wait)
	}
	if s.Chrome.SignIn != "" && status == http.StatusUnauthorized {
		p.SignIn, p.SignInLabel = s.Chrome.SignIn, word0(loc, "fault.sign_in")
	}
	if reference != "" {
		// The reference left the sentence, where it could not be selected cleanly and
		// a screen reader read it as the tail of an apology. It is a value with a name,
		// because the one thing a person does with it is hand it to somebody else.
		p.Reference = reference
		p.ReferenceLabel = word0(loc, "fault.request_reference")
	}
	v := document.Fault(p)
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

// sentence is the refusal's own line, in the request's language when this shell
// ships the copy for it and in the guard's English when it does not, and the
// language actually on the page ("" for English). The code is kept and the copy
// after it is what changes — see fault.
//
// The source language is the one language that gets both lines. Every other
// language reads the refusal in the sentence its own catalogue carries, which is
// what the key exists for; English *is* the guard's sentence, so a shipped entry
// there is copy beside the line rather than a replacement for it, and the
// permission, the subsystem or the address the guard wrote into its detail stays
// on the page. A deployment whose English copy differs from the guard's wording
// says its own and the guard's, and both name the same verdict.
func sentence(line string, loc *Locale, status int, detail, key string) (text, language string) {
	text = line
	if loc == nil {
		return text, ""
	}
	if key == "" {
		var lookup bool
		key, lookup = faultKey(detail, status)
		if !lookup {
			return text, ""
		}
	}
	shipped := loc.Text(key, line)
	if shipped == line {
		return text, ""
	}
	_, guardSaid := strings.CutPrefix(loc.Language, sourceLanguage)
	// The guard's own line stands beside the shipped copy only where the guard wrote one
	// for this page. A refusal with no detail to show — a diagnostic, or a 500 whose
	// reason is in the log — has already said that its sentence is not the person's, and
	// echoing an empty detail after the catalogue's line would be a dash over nothing.
	if detail == "" {
		return shipped, loc.Language
	}
	if code, rest, named := strings.Cut(detail, ": "); named {
		if guardSaid {
			return code + ": " + shipped + " " + sourceMark + " " + rest, loc.Language
		}
		return code + ": " + shipped, loc.Language
	}
	if guardSaid {
		return shipped + " " + sourceMark + " " + line, loc.Language
	}
	return shipped, loc.Language
}

// sourceLanguage is the language this package's copy is authored in — the one
// xtext.Load is handed as the fallback, and the language the guards' own sentences
// are written in. It is a name rather than an absence because both catalogues now
// carry every key, so "the source language" is a fact to state and not a hole in a
// file to infer from.
const sourceLanguage = "en"

// sourceMark is what stands between the shipped copy and the guard's own line on a
// source-language refusal: an em dash, because the two halves are written by two
// owners and read as two sayings of the same verdict.
const sourceMark = "\u2014"

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
	if language != "" && reference != "" {
		line = line + " (request " + reference + ")"
	} else if reference != "" {
		line = detail + " (request " + reference + ")"
	}
	p := document.RefusalProps{Status: status, Title: http.StatusText(status),
		Sentence: line, Home: s.Back, HomeLabel: s.BackLabel}
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
