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
// reads, the same in every language because a program reads a code, and the sentence a
// person is shown, which used to be English by construction because it was a string
// literal in the guard that made it. The guards publish the codes they travel in (httpx's
// Code* constants) and every guard that refuses a request a person could be looking at
// answers through API.refuse — which is what makes this table worth filling: a code no
// page is ever shown for is a key nobody can ship copy under. The page is negotiated from
// the request through the contract every other page here uses (Shell.Messages,
// SelectLocale, Formatter.Text, and the two headers Serve writes for a translated page).
//
// A shell that ships no catalog is shown the kernel's English and keeps declaring "en".
// Which languages a deployment's failure page speaks is the composition's, as it is for
// every page here: this file holds the mechanism and the English, and no other language.
//
// The person the guard refused is the person the page is written for: it asks the same
// Shell.Locale every served page asks, so a choice the account made is what a refusal is
// answered in, and only a caller with no accepted session to read a choice from falls
// back to the language their request asked for.

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	kitfault "github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/ui/document"
)

// faultKeys is the one table from a refusal the kernel published to the catalog key this
// shell's sentence for it lives under.
//
// The code is namespaced, because a kernel refusal and a module's own copy share one
// catalog and "AUTH_DENIED" on its own is a name either of them could want. A code with no
// entry is shown the kernel's sentence, which is the same answer a shell with no catalog
// gets — and CodeWriteElsewhere is absent on purpose: its sentence names the address the
// write belongs at, which is the one thing the caller has to act on, so it is left standing
// whole. CodePlanExcludes is the one code whose sentence this layer replaces outright: the
// guard's line names the plan feature's slug, and a slug is what the plan's owner reads,
// not what a person at the page can ask for. See plainWords.
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
	// A plan exclusion is a verdict the application reached by working as designed, so it
	// is worded here as the plan's absence rather than as the guard's line about a slug.
	httpx.CodePlanExcludes: planKey,
	// The one refusal a module's own door publishes: a restore asked for a version
	// this record has never held. It is the kit's sentence about the kit's own door,
	// which is why it has a code, and a code is what a catalogue can be asked about.
	httpx.CodeVersionUnusable: "fault.VERSION_UNUSABLE",
}

// Subject is what a generated page is about: the entry it shows or lists, by the
// name its own catalogue gives it, and the collection a person returns to when the
// read that filled it was refused.
//
// A page that declares none is refused about an address, which is what a refusal page has
// always said, because nothing told it what the address held. A page that declares one can
// say what happened to the thing the person came for and link back to the list it belongs
// to. The names arrive from the caller's own catalogue (entity.EntryHints), never from a
// slug: a resource slug on a person's page is a developer fact.
type Subject struct {
	// Singular and Plural are the entry's readable names in the page's language, the
	// two ways the same noun is read ("task", "tasks"). A failed list read names the
	// plural, a failed record read the singular, and the way back names the collection.
	Singular, Plural string
	// List is the collection's own address, as the kernel derived it. It is used only
	// once it passes the kernel's local-path rule, and empty leaves the shell's way out.
	List string
	// Record says this page is one entry rather than the list. It decides which name a
	// failed read speaks, and whether a 404 is about an item or an address.
	Record bool
}

// declared is whether this page said what it is about. A subject with no noun names
// nothing, and the page is then answered as one that declared none.
func (s Subject) declared() bool { return s.Singular != "" || s.Plural != "" }

// The subject sentence keys, and the two ways out they offer: copy this package writes for
// a verdict no guard worded, so they carry no code prefix and the catalogue gate that reads
// faultSubjectKeys is what ships them in every language the deployment speaks.
const (
	loadFailedKey    = "fault.load_failed"
	loadFailedOneKey = "fault.load_failed.one"
	recordMissingKey = "fault.record.missing"
	recordDeniedKey  = "fault.record.denied"
	retryKey         = "fault.retry"
	backToKey        = "fault.back_to"
	saveFailedKey    = "fault.save_failed"
)

// saveFailed is what a person is told when a write broke on the server. The line the
// failure carries is a driver's text or a subsystem's name, which is what an operator
// greps a log for and not a reason anyone at a form can act on. The write is not there:
// a refusal rolls the request's transaction back, and the sentence says so.
const saveFailed = "We couldn't save this. Nothing was saved."

// planKey is the verdict a plan exclusion travels under. It is the one published code
// whose sentence this layer replaces outright rather than translating after the code:
// the guard's line names the feature slug the tenant's plan does not carry, which is a
// fact for the plan's owner and not a sentence a person can act on.
const planKey = "fault." + httpx.CodePlanExcludes

// faultTitles is what each verdict of the table is called, in the source language. It
// exists because http.StatusText names the verdict of the protocol rather than what
// happened: "Internal Server Error" and "Payment Required" are a log line's words put in
// front of a person, and the plan line names a purchase nobody in front of the page is
// making.
//
// A verdict absent here keeps http.StatusText: until this package has read a refusal it
// has no truer words than the verdict's own name.
var faultTitles = map[string]string{
	"fault.404":                 "Page not found",
	"fault.405":                 "This page can't take that request",
	"fault.500":                 "Something went wrong",
	"fault.503":                 "Not available right now",
	"fault." + httpx.CodeDenied: "You don't have access",
	planKey:                     "Not in this plan",
	saveFailedKey:               "We couldn't save this",
	loadFailedKey:               "We couldn't load this",
	loadFailedOneKey:            "We couldn't load this",
	recordMissingKey:            "No longer available",
	recordDeniedKey:             "Access withdrawn",
}

// faultSubjectKeys is every sentence key a page that declared its subject can ask for,
// and faultTitleKeys every heading key. The package's catalogue gate reads both, so a subject
// sentence cannot ship in English to a tenant served in Portuguese — which matters
// precisely because no faultKey call can produce one: the gate that reads faultKey for
// every status could never ask about these.
func faultSubjectKeys() []string {
	return []string{loadFailedKey, loadFailedOneKey, recordMissingKey, recordDeniedKey}
}

// faultFormKeys is every sentence this package writes for a refusal a generated form
// shows which no faultKey call can produce: the write that broke on the server, whose
// sentence belongs to this layer wherever it is shown, subject declared or not.
func faultFormKeys() []string {
	return []string{saveFailedKey}
}

// plainWords is the row of the copy table a refusal is shown in when this layer writes
// the sentence itself: a guard's line for these verdicts names a subsystem or a plan
// slug, and retryTo says whether the honest way out is to ask again at this address
// rather than to leave it. An empty key means the writer's own line stands — a generic
// sentence standing in for an unread one hides the one thing the caller could act on.
func plainWords(sub Subject, status int, detail string) (key, english string, args []any, retryTo bool) {
	if code, _, named := strings.Cut(detail, ": "); named && code == httpx.CodePlanExcludes {
		return planKey, "This isn't included in this workspace's plan.", nil, false
	}
	if !sub.declared() {
		return "", "", nil, false
	}
	switch {
	case sub.Record && status == http.StatusNotFound:
		return recordMissingKey, "This item is no longer available.", nil, false
	case sub.Record && status == http.StatusForbidden && !strings.Contains(detail, ": "):
		// A refusal that carries a published code says something of its own about the
		// row — a rule it broke, a grant that is missing — and that sentence stays.
		return recordDeniedKey, "You no longer have access to this item.", nil, false
	case status >= http.StatusInternalServerError:
		if sub.Record {
			return loadFailedOneKey, "We couldn't load this %s.", []any{sub.Singular}, true
		}
		return loadFailedKey, "We couldn't load %s.", []any{sub.Plural}, true
	}
	return "", "", nil, false
}

// toneOf is how loudly a verdict is announced. A plan exclusion is not a fault — the
// application is working exactly as the plan it was bought on does — and drawing it in
// the same voice as a broken handler is what makes a person think they lost something.
func toneOf(key string) string {
	if key == planKey {
		return document.ToneInfo
	}
	return ""
}

// refusalParts is every catalogue key the refusal page looks up besides the
// verdict's own sentence: the four parts of a grant denial, the row denial's one,
// and the confirmation the ask lands on. The test that reads this list is the
// gate that keeps a part from shipping in English to a tenant served in
// Portuguese, which is the defect the whole page exists to close.
//
// `fault.back` names one way out — back to the workspace — and `fault.request`
// names the identifier every refusal carries. Both are copy; what they name is not.
//
// The shell's own label is the readable fallback and the catalogue carries the
// translation, the same arrangement the verdict's sentence has with the guard's
// line — but a translation is offered only for the destination these words name.
// A shell that says something else (a public site sends the visitor to its home,
// not to a workspace) keeps its own words: see backWay.
var refusalParts = []string{
	"fault.missing", "fault.granter", "fault.ask", "fault.holds_the_row",
	"fault.sent", "fault.sent_body", "fault.back", referenceKey,
	// The two ways out a page with a subject offers, and the verdicts no guard wrote a
	// sentence for, which are this package's own copy in both languages.
	retryKey, backToKey,
}

// faultSubjectWayKeys are the way-out labels a subject page draws, read with the
// sentences they sit beside.
func faultSubjectWayKeys() []string { return []string{retryKey, backToKey} }

// backKey is the way out this package's catalogue words; asked wherever the shell
// offers its way out and means the workspace.
const backKey = "fault.back"

// backWayOut is the English of the destination `backKey` names. It is not a copy
// default the shell may overrule with anything: the catalogue's translation names
// that destination, so a shell offering a different one gets its own label back
// untranslated rather than a sentence that misnames where its link leads.
const backWayOut = "Back to the workspace"

// referenceKey is the words that name the request's own identifier on the page.
const referenceKey = "fault.reference"

// referenceWords is the English of that label. The identifier itself is data, so it
// is rendered beside these words and never inside them: one string holding both is
// reported as untranslated copy that no translator can finish, and a pseudo-locale
// marks and accents the identifier with the sentence around it, which is the one
// value on the page a person has to be able to read back digit for digit.
const referenceWords = "(request"

// titleSuffix turns a verdict's sentence key into that verdict's heading: the two
// halves of one page, one key apart, so the catalogue is read top to bottom as the
// page is.
const titleSuffix = ".title"

// faultTitleStatuses is every verdict this installation answers with that no guard wrote
// a sentence for and this layer does not word either: the 400 of a body the decoder
// refused, the 409 of a row that moved, the 422 of a field. Their sentence belongs to
// whoever wrote it, so they are absent from faultKey on purpose; their *name* is still
// copy, and each is asked of the reader's language under `fault.<status>.title`, with
// net/http's name as the fallback a deployment that ships no copy is shown.
//
// The four verdicts this layer does word — the 404, the 405, the 500, the 503 — are not
// here: they are named by faultTitles beside the sentence this package holds for them.
var faultTitleStatuses = []int{
	http.StatusBadRequest,
	http.StatusUnauthorized,
	http.StatusForbidden,
	http.StatusConflict,
	http.StatusRequestEntityTooLarge,
	http.StatusUnprocessableEntity,
	http.StatusTooManyRequests,
}

// faultTitleName is what a refusal page calls a verdict this package holds no sentence
// for: net/http's name for the status, asked of the reader's language first. A shell with
// no catalogue, and a status no page of this installation answers with, are named as they
// always were.
func faultTitleName(loc *Locale, status int) string {
	english := http.StatusText(status)
	if loc == nil {
		return english
	}
	return loc.Text("fault."+strconv.Itoa(status)+titleSuffix, english)
}

// statusTitle is the refusal's heading in the request's language. A verdict this layer
// words is called what this package calls it; one it does not is called what the status
// is called — in the reader's language, because a heading invented for one verdict and
// not another would name a cause nobody withheld.
func statusTitle(loc *Locale, status int, detail string, sub Subject) string {
	if key, _, _, _ := plainWords(sub, status, detail); key != "" {
		return word0(loc, key+titleSuffix, faultTitles[key])
	}
	english := http.StatusText(status)
	key, lookup := faultKey(detail, status)
	if !lookup {
		return faultTitleName(loc, status)
	}
	if titled, named := faultTitles[key]; named {
		english = titled
	}
	return word0(loc, key+titleSuffix, english)
}

// backWay is the shell's label, translated the way the heading is — but only for
// the way out this catalogue words. A shell whose link leads somewhere else has
// words of its own, and translating them here would tell a person one destination
// while the href carried another.
func backWay(loc *Locale, label string) string {
	if label != backWayOut {
		return label
	}
	return word0(loc, backKey, label)
}

// referenceSaid is the identifier line: the words that name it, then the identifier
// itself, and then the refusal's own code when the sentence above stopped carrying it.
// Each is its own piece of the line — the words are a translator's, the identifier is a
// row in a log and the code is a grep pattern — so they are three strings on the page
// and not one sentence no translation can finish.
func referenceSaid(loc *Locale, reference, code string) g.Node {
	parts := []g.Node{g.Text(word0(loc, referenceKey, referenceWords)), g.Text(" "), h.Code(g.Text(reference))}
	if code != "" {
		parts = append(parts, g.Text(" · "), h.Code(g.Text(code)))
	}
	return h.P(append(parts, g.Text(")"))...)
}

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
	for _, key := range faultSubjectKeys() {
		seen[key] = true
	}
	for _, key := range faultFormKeys() {
		seen[key] = true
	}
	for _, status := range faultTitleStatuses {
		seen["fault."+strconv.Itoa(status)] = true
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
// It is the contract every other page of this package uses, asked in the same order: the
// tenant's set as the ceiling, the language Shell.Locale prefers for this caller, the
// caller's header behind that, the tenant's default last. The resolver is the one Serve
// asks, because a verdict changes nothing about who is standing there — and what the guards
// ahead of routing cannot reach (an unsigned caller, an address that names no tenant, a
// public face that parses no cookie) is answered "" by that resolver and negotiated from the
// header, which is what keeps Vary: Accept-Language the truth here too.
func (s Shell) refusalLocale(ctx context.Context, r *http.Request, req Request) *Locale {
	if s.Messages == nil {
		return nil
	}
	// The tenant the request resolved to: the languages it is served in are as much a
	// fact about a refusal as about any other page, and a tenant that declared one
	// language was not refusing to be answered in another. An address whose host names no
	// tenant — a domain nobody serves, a database that could not say — has no declaration
	// to filter by, and the deployment's own catalog is then the only one standing.
	var preferred string
	if s.Locale != nil {
		preferred = s.Locale(ctx, req)
	}
	loc := SelectLocale(s.Messages, TenantPreferences(req, preferred, r.Header.Get("Accept-Language"))...)
	return &loc
}

// shellWay is the shell's way out, in the reader's language: the words under the key the
// shell named, or — with no key — the destination this package's own catalogue words.
//
// A label is copy, and copy is the catalogue's: the shell's own words are the fallback a
// deployment that ships no copy for the key is shown.
func shellWay(loc *Locale, key, label string) string {
	if key == "" {
		// The shell named no catalogue entry for its way out. This package still words
		// the destination its own label names, and carries the translation of that one
		// destination under fault.back — so the label is asked of the reader's language
		// here rather than printed as the composition wrote it. A shell whose words name
		// somewhere else keeps them untranslated: see backWay.
		return backWay(loc, label)
	}
	return word0(loc, key, label)
}

// FaultHandler builds the kernel's failure renderer for one shell. Back and BackLabel are
// the shell's own, so the page offers the way out that exists in that application: a
// generated admin shell sends a stranger to its sign-in; a public site sends them home.
// BackKey, when the shell names one, is the entry that label is worded under — the way out
// in the reader's language rather than in the language whoever wired the shell writes.
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
		loc := s.refusalLocale(ctx, r, req)
		// The frame is given the negotiated language for the same reason the sentence is:
		// a shell whose chrome has labels of its own renders them here as it does on every
		// page Serve mounts, and not in English because the request was refused.
		req.Locale = loc
		v, ok := granted(r, loc, status, p.Detail, requestID(p.Instance), s)
		if !ok {
			v = fault(verdict{status: status, detail: p.Detail, reason: p.Refusal, loc: loc,
				reference: requestID(p.Instance), back: s.Back, backLabel: s.BackLabel,
				backKey: s.BackKey, path: r.URL.Path})
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
			// The two headers every page of a translated shell carries (see Serve): the
			// copy is this language, and a cache has to key on what moved it. An account
			// preference outranks the caller's header, and only a caller with an accepted
			// session has one to be moved by — the surface kit/httpx already answers
			// no-store, so what Vary cannot fully say, the session policy already keeps.
			w.Header().Set("Content-Language", v.Language)
			w.Header().Set("Vary", "Accept-Language")
		}
		w.WriteHeader(status)
		_, _ = w.Write(out.Body)
		return true
	}
}

// refusal is one verdict as the page sees it: the status and the guard's line, the
// language it is answered in, the reference an operator greps a log for, the shell's own
// way out and the address the person was at, and what the page was about when a
// generated screen said so.
type verdict struct {
	status                                   int
	detail, reference, back, backLabel, path string
	// backKey is the catalogue entry backLabel is worded under, when the shell named one.
	backKey string
	loc     *Locale
	sub     Subject
	// reason is the refusal's own sentence where its writer named one — see sentence.
	reason *kitfault.Reason
}

// fault is the refusal document, with what a person in front of one needs and the
// reference an operator can find the request by in a log. The reference is the same URN
// the JSON body carries, so a screenshot and a log line agree.
//
// The sentence is one of three things, in this order: the words this package holds for
// the verdict when the page declared a subject or the verdict is a plan exclusion, the
// shell's own copy when its catalogue holds the code the guard travels under, and the
// guard's line as written. The first is why a person at a record is told about the record
// and not about HTTP; the last is why a refusal nobody has read is never smoothed over.
//
// The declared language follows whichever sentence is on the page, not the negotiation: a
// refusal the catalogue has no sentence for is English copy whatever was asked for, and a
// document declaring Portuguese over an English line mislabels the words on the page.
//
// What a translation replaces is the sentence, and the code goes on the data line below
// rather than at its head: a guard writes "<CODE>: <sentence>", and the code is the half
// a person reads back to support and an operator greps a log for, so it stays on the page
// whatever the copy says.
// deniedPermission is the English sentence for a missing grant; the catalogue carries it
// as deniedPermissionKey, with the permission as its one argument.
const (
	deniedPermission    = "You can't do this. You need the %s permission for it. Anyone who manages roles here can grant it: ask your administrator."
	deniedPermissionKey = "fault.AUTH_DENIED.permission"
)

func fault(rf verdict) View {
	key, english, args, retryTo := plainWords(rf.sub, rf.status, rf.detail)
	line, language := rf.detail, ""
	if strings.TrimSpace(line) == "" {
		// A 500 carries no detail on purpose: the reason is in the log, not for the
		// browser. The sentence has to be true and useful without it.
		line = "Something went wrong while handling this."
	}
	code, _, prefixed := strings.Cut(rf.detail, ": ")
	if key == "" {
		line, language = sentence(line, rf.loc, rf.status, rf.detail, rf.reason)
		// A missing grant names the grant and who can give it: an editor who signed in met
		// "Não pode fazer isto." and could not say what to ask for, and the administrator
		// helping her could not tell what to give. The guard already wrote the permission
		// into its detail and the short translated sentence dropped it. A catalogue with the
		// full sentence answers with it; one that carries only the short sentence keeps its
		// language and gains the permission, so no shell answers a denial in two languages.
		if prefixed && code == httpx.CodeDenied {
			if permission, ok := strings.CutPrefix(strings.TrimPrefix(rf.detail, code+": "), "this operation requires "); ok && permission != "" {
				words := fmt.Sprintf(deniedPermission, permission)
				switch {
				case rf.loc != nil && rf.loc.Text(deniedPermissionKey, words, permission) != words:
					line, language = code+": "+rf.loc.Text(deniedPermissionKey, words, permission), rf.loc.Language
				case language != "":
					line = line + " (" + permission + ")" // the shell's own short sentence, in its own language
				default:
					line = code + ": " + words
				}
			}
		}
	}
	// The way out. A subject page sends a person back to the list the item belongs to;
	// a verdict that may pass on a second attempt sends them to this address again;
	// everything else takes the shell's way out, which is the one it promised.
	way, wayLabel := rf.back, shellWay(rf.loc, rf.backKey, rf.backLabel)
	switch {
	case (key == recordMissingKey || key == recordDeniedKey) && httpx.LocalPath(rf.sub.List):
		way = rf.sub.List
		wayLabel, _ = word(rf.loc, backToKey, "Back to %s", rf.sub.Plural)
	case retryTo && httpx.LocalPath(rf.path):
		way, wayLabel = rf.path, word0(rf.loc, retryKey, "Try again")
	}
	if key != "" {
		text, spoken := word(rf.loc, key, english, args...)
		line, language = text, ""
		if spoken {
			language = rf.loc.Language
		}
	}
	// The code leaves the sentence only where this package wrote the sentence, and then
	// lands on the data line: a refusal whose copy this is not keeps its line whole.
	shown := ""
	if key != "" && prefixed {
		shown = code
	}
	v := document.FaultPage(document.FaultProps{Status: rf.status, Title: statusTitle(rf.loc, rf.status, rf.detail, rf.sub),
		Sentence: line, Tone: toneOf(key), Way: way, WayLabel: wayLabel})
	if reference := rf.reference; reference != "" || shown != "" {
		v.Body = append(v.Body, referenceSaid(rf.loc, reference, shown))
	}
	if language != "" {
		v.Language = language
	}
	return v
}

// RefusalWords is the heading and sentence ui/page would put on a refusal page for this
// verdict, in the language `loc` speaks (nil for the kernel's English), the tone the
// verdict is announced in, and whether these are this layer's own words at all.
//
// ok is false for every refusal whose sentence belongs to whoever wrote it — a row rule,
// a field the caller sent, a grant named by its permission — and a caller that shows the
// line then shows what the writer wrote, untranslated and unsoftened. A form that was
// refused on submit uses this to stop printing a code and a problem's `detail` where a
// person is reading for a reason.
//
// A verdict of 500 or above is the exception and is always worded: whatever the writer
// put in the detail is the reason the server broke, and that is a log line. The subject
// rows above still win where a page declared one, because "We couldn't load tasks." says
// more than "We couldn't save this." about a read.
func RefusalWords(loc *Locale, status int, detail string) (title, sentence, tone string, ok bool) {
	key, english, args, _ := plainWords(Subject{}, status, detail)
	if key == "" && status >= http.StatusInternalServerError {
		key, english = saveFailedKey, saveFailed
	}
	if key == "" {
		return "", "", "", false
	}
	text, _ := word(loc, key, english, args...)
	return word0(loc, key+titleSuffix, faultTitles[key]), text, toneOf(key), true
}

// sentence is the refusal's own line, in the request's language when this shell
// ships the copy for it and in the guard's English when it does not, and the
// language actually on the page ("" for English). The code is kept and the copy
// after it is what changes — see fault.
//
// There are two ways a sentence exists to be changed. A guard's own refusal is keyed by
// the code it published, because the guard wrote both the code and the line after it and
// this package ships the copy. A refusal that arrives with a sentence of its own
// (`reason`) is keyed by the entry its writer named, and is answered only when the line
// being shown *is* that sentence: a refusal wrapped in somebody's further prose keeps
// that prose rather than have a sentence swapped under the reader's eyes.
func sentence(line string, loc *Locale, status int, detail string, reason *kitfault.Reason) (text, language string) {
	text = line
	if loc == nil {
		return text, ""
	}
	if reason != nil && reason.Key != "" && line == reason.Error() {
		if said := loc.Text(reason.Key, reason.Error(), reason.Args...); said != reason.Error() {
			return said, loc.Language
		}
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
	line, language := sentence("", loc, status, detail, nil)
	if line == "" {
		line = strings.TrimPrefix(detail, ref.Code+": ")
	}
	if language == "" && reference != "" {
		// No sentence from the catalogue: the guard's whole line, code first, exactly
		// as the page has always shown it. The reference itself is said below.
		line = detail
	}
	p := document.RefusalProps{Status: status, Title: statusTitle(loc, status, detail, Subject{}),
		Sentence: line, Home: s.Back, HomeLabel: shellWay(loc, s.BackKey, s.BackLabel)}
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
	if reference != "" {
		v.Body = append(v.Body, referenceSaid(loc, reference, ""))
	}
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
