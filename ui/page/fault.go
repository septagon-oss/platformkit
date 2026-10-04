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
// A shell that ships no catalog is shown the kernel's English sentence and keeps declaring
// "en", which is what it always did and what the copy still is. Which languages a
// deployment's failure page speaks is the composition's, as it is for every page here:
// this file holds the mechanism and the English, and no other language.

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/document"
)

// faultKeys is the one table from a refusal the kernel published to the catalog key this
// shell's sentence for it lives under.
//
// The code is namespaced, because a kernel refusal and a module's own copy share one
// catalog and "AUTH_DENIED" on its own is a name either of them could want. A code with no
// entry is shown the kernel's sentence, which is the same answer a shell with no catalog
// gets — and two codes are absent on purpose rather than by oversight, for one reason:
// CodeWriteElsewhere names the address the write belongs at and CodePlanExcludes names
// the feature to ask the plan for, and a sentence that carries something the caller has
// to *have* cannot be replaced by one that only describes it — this mechanism swaps a
// sentence and does not interpolate an argument.
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
// Portuguese, which is the defect the whole page exists to close.
//
// `fault.back` is the shell's own way out. Its words belong to the composition — a
// product sends a stranger to its sign-in and a public site sends them home — so the
// shell's label is the readable fallback and the catalogue carries the translation,
// the same arrangement the verdict's sentence has with the guard's line.
var refusalParts = []string{
	"fault.missing", "fault.granter", "fault.ask", "fault.holds_the_row",
	"fault.sent", "fault.sent_body", "fault.back",
}

// backKey is that label; asked wherever the shell offers its way out.
const backKey = "fault.back"

// titleSuffix turns a verdict's sentence key into that verdict's heading: the two
// halves of one page, one key apart, so the catalogue is read top to bottom as the
// page is.
const titleSuffix = ".title"

// statusTitle is the refusal's heading in the request's language. What it says when
// the catalogue has nothing for the verdict is what it always said — the status text
// this server registers — because a heading invented for one verdict and not another
// would name a cause nobody withheld.
func statusTitle(loc *Locale, status int, detail string) string {
	english := http.StatusText(status)
	key, lookup := faultKey(detail, status)
	if !lookup {
		return english
	}
	return word0(loc, key+titleSuffix, english)
}

// backWay is the shell's label, translated the way the heading is.
func backWay(loc *Locale, label string) string { return word0(loc, backKey, label) }

// faultTitleKeys is every heading key this page can ask for: each key faultKey can
// return, with the suffix on the end. catalogue_test.go reads it rather than a list,
// so the day a tenth code or a fifth status joins the page its heading is demanded of
// the catalogue in the same commit — which is the rule that already holds for the
// sentences and the whole reason a heading was ever the English one.
func faultTitleKeys() []string {
	seen := map[string]bool{}
	for _, key := range faultKeys {
		seen[key] = true
	}
	for code := 100; code < 600; code++ {
		if key, lookup := faultKey("", code); lookup {
			seen[key] = true
		}
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key+titleSuffix)
	}
	sort.Strings(out)
	return out
}

// faultKey is the catalog key of the sentence a refusal is shown in, and whether
// there is one to look for at all.
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
		ctx := r.Context()
		req := read(ctx, s.Chrome)
		loc := refusalLocale(s.Messages, r, req.Tenant)
		// The frame is given the negotiated language for the same reason the sentence is:
		// a shell whose chrome has labels of its own renders them here as it does on every
		// page Serve mounts, and not in English because the request was refused.
		req.Locale = loc
		v, ok := granted(r, loc, status, p.Detail, requestID(p.Instance), s)
		if !ok {
			v = fault(status, p.Detail, loc, requestID(p.Instance), s.Back, s.BackLabel)
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

// fault is the refusal document, with the two things a person in front of one actually
// needs: what to do next, and the reference an operator can find the request by in a
// log. The reference is the same URN the JSON body carries, so a screenshot and a log
// line agree — which matters most for the failures that are nobody's fault and still
// happened.
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
// deniedPermission is the English sentence for a missing grant; the catalogue carries it as
// deniedPermissionKey, with the permission as its one argument.
const (
	deniedPermission    = "You can't do this. You need the %s permission for it. Anyone who manages roles here can grant it: ask your administrator."
	deniedPermissionKey = "fault.AUTH_DENIED.permission"
)

func fault(status int, detail string, loc *Locale, reference, back, backLabel string) View {
	line := detail
	if strings.TrimSpace(line) == "" {
		// A 500 carries no detail on purpose: the reason is in the log, not for the
		// browser. The sentence has to be true and useful without it.
		line = "Something went wrong while handling this."
	}
	line, language := sentence(line, loc, status, detail)
	// A missing grant names the grant and who can give it (UX walkthroughs, 2026-09-30: an editor who signed
	// in met "Não pode fazer isto." and could not say what to ask for; the administrator helping her could not
	// tell what to give). The guard already wrote the permission into its detail and the translated sentence
	// dropped it. A catalogue with the full sentence answers with it; one that carries only the short sentence
	// keeps its language and gains the permission, so no shell answers a denial in two languages.
	if code, rest, named := strings.Cut(detail, ": "); named && code == httpx.CodeDenied {
		if permission, ok := strings.CutPrefix(rest, "this operation requires "); ok && permission != "" {
			words := fmt.Sprintf(deniedPermission, permission)
			switch {
			case loc != nil && loc.Text(deniedPermissionKey, words, permission) != words:
				line, language = code+": "+loc.Text(deniedPermissionKey, words, permission), loc.Language
			case language != "":
				line = line + " (" + permission + ")" // the shell's own short sentence, in its own language
			default:
				line = code + ": " + words
			}
		}
	}
	if reference != "" {
		// Every verdict carries the reference, and most especially the ones the person
		// can do nothing about: a 403 they may be able to fix themselves, a 500 they can
		// only report. This line used to sit in the else-branch above, which left the 500
		// page — the one page where a reference is the entire value of the visit — with
		// an apology and nothing to quote.
		line = line + " (request " + reference + ")"
	}
	v := document.Fault(status, statusTitle(loc, status, detail), line, back, backWay(loc, backLabel))
	if language != "" {
		v.Language = language
	}
	return v
}

// sentence is the refusal's own line, in the request's language when this shell
// ships the copy for it and in the guard's English when it does not, and the
// language actually on the page ("" for English). The code is kept and the copy
// after it is what changes — see fault.
func sentence(line string, loc *Locale, status int, detail string) (text, language string) {
	text = line
	if loc == nil {
		return text, ""
	}
	key, lookup := faultKey(detail, status)
	if !lookup {
		return text, ""
	}
	translated := loc.Text(key, line)
	if translated == line {
		return text, ""
	}
	if code, _, named := strings.Cut(detail, ": "); named {
		translated = code + ": " + translated
	}
	return translated, loc.Language
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
	line, language := sentence("", loc, status, detail)
	if line == "" {
		line = strings.TrimPrefix(detail, ref.Code+": ")
	}
	if language != "" && reference != "" {
		line = line + " (request " + reference + ")"
	} else if reference != "" {
		line = detail + " (request " + reference + ")"
	}
	p := document.RefusalProps{Status: status, Title: statusTitle(loc, status, detail),
		Sentence: line, Home: s.Back, HomeLabel: backWay(loc, s.BackLabel)}
	if ref.Code == httpx.CodePolicyDenied {
		p.Missing, _ = word(loc, "fault.holds_the_row",
			"You already hold what this needs; this row's own rule refused it: %s", ref.Reason)
	} else {
		p.Missing, _ = word(loc, "fault.missing", "What is missing: %s",
			word0(loc, "permission."+ref.Permission, ref.Label))
		p.Granter, _ = word(loc, "fault.granter",
			"Ask your administrator — anyone whose role grants them %s.",
			word0(loc, "permission."+s.Granter.Permission, s.Granter.Label))
		// Both the shell's address and a wired door: a composition that supplies
		// the page route but no reach would draw a form that refuses.
		if s.Ask != "" && httpx.CanAsk(r.Context()) {
			p.Ask, p.AskLabel = s.Ask, word0(loc, "fault.ask", "Ask for access")
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

// word is one line of the refusal page through the shell's catalogue, and
// whether the catalogue actually spoke it — a page the catalogue has no copy for
// is English and must not declare Portuguese.
func word(loc *Locale, key, fallback string, args ...any) (string, bool) {
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

// word0 is a line already assembled, or one whose translation is not worth
// recording: the refusal page's own labels.
func word0(loc *Locale, key, text string) string {
	if loc == nil {
		return text
	}
	return loc.Text(key, text)
}

// requestID is the instance URN read back into the bare identifier, which is what a
// human reads aloud to a human reading a log.
func requestID(instance string) string {
	if instance == "" {
		return ""
	}
	return strings.TrimPrefix(instance, "urn:request:")
}
