package page

// The words a refusal is shown in: which sentence, under which heading, in which tone,
// in which language.
//
// fault.go builds the page — the verdict, the ways out the request actually offers, the
// reference a person reads back. This file holds the other decision, and the two are
// asked by different things: the page moves when the kernel offers a new way out, and
// these tables move when a guard publishes a new code or a deployment ships new copy.
// Every key named here is read by the package's catalogue gate, which is what
// keeps a sentence this package writes from shipping in English to a tenant served in
// Portuguese.

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/ui/document"
)

// faultKeys is the inventory of the refusal codes kit/httpx publishes that this shell
// ships a sentence for, and the catalogue test reads it as one: a code in the kernel with
// no key beside it is a refusal a Portuguese tenant is answered in English, and nobody
// has to notice.
//
// The code is namespaced because a kernel refusal and a module's own copy share one
// catalog, and because this shell claims the `fault.` prefix against every catalogue that
// follows it: the codes named here are the ones it may show. A module with a page of its
// own names its entry on the refusal instead (see problem.Problem's Key).
// CodeWriteElsewhere is absent on purpose: its sentence names the address the write
// belongs at, the one thing the caller has to act on, and this mechanism swaps a sentence
// without passing an argument. CodePlanExcludes is the one code whose sentence this layer
// replaces outright: the guard's line names a plan slug the plan's owner reads and a
// person at the page cannot ask for. See plainWords.
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
	// this record has never held. Everything else about a 422 stays the writer's
	// English sentence, because it names a field of the caller's own request; this
	// one is the kit's own sentence about its own door, which is why it has a code,
	// and a code is what a catalogue can be asked about.
	httpx.CodeVersionUnusable: "fault.VERSION_UNUSABLE",
}

// Subject is what a generated page is about: the entry it shows or lists, by the
// name its own catalogue gives it, and the collection a person returns to when the
// read that filled it was refused.
//
// A page that declares none is refused about an address, which is what a refusal page
// has always said, because nothing told it what the address held. A page that declares
// one can say what happened to the thing the person came for and link back to the list
// it belongs to. The names arrive from the caller's own catalogue (entity.EntryHints),
// never from a slug: a resource slug on a person's page is a developer fact.
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

// The subject sentence keys, and the two ways out they offer. These are the copy this
// package writes for a verdict no guard wrote a sentence for, so they carry no code
// prefix and the catalogue gate that reads faultSubjectKeys is what ships them in every
// language the deployment speaks.
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
// failure carries is a driver's text or a subsystem's name — "pq: could not serialize
// access", "authorization is temporarily unavailable" — which is what the operator
// greps a log for and not a reason anyone at a form can act on. The write is not
// there: a refusal rolls the request's transaction back, so nothing of the submission
// was stored, and the sentence says so rather than leaving it unknown.
const saveFailed = "We couldn't save this. Nothing was saved."

// planKey is the verdict a plan exclusion travels under. It is the one published code
// whose sentence this layer replaces outright rather than translating after the code:
// the guard's line names the feature slug the tenant's plan does not carry, which is a
// fact for the plan's owner and not a sentence a person can act on.
const planKey = "fault." + httpx.CodePlanExcludes

// faultTitles is what each verdict of the table is called, in the source language. It
// exists because http.StatusText names the verdict of the protocol rather than what
// happened: "Internal Server Error" and "Payment Required" are a log line's words put
// in front of a person, and the plan line names a purchase nobody in front of the page
// is making.
//
// A verdict absent here keeps http.StatusText: a heading invented for one verdict and
// not another would name a cause nobody withheld, and until this package has read a
// refusal it has no truer words than the verdict's own name.
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

// faultSubjectWayKeys are the way-out labels a subject page draws, read with the
// sentences they sit beside.
func faultSubjectWayKeys() []string { return []string{retryKey, backToKey} }

// plainWords is the row of the copy table a refusal is shown in when this layer writes
// the sentence itself: a guard's line for these verdicts names a subsystem or a plan
// slug, and retryTo says whether the honest way out is to ask again at this address
// rather than to leave it. An empty key means the writer's own line stands, which is
// what every refusal this package has not read gets: a generic sentence standing in for
// an unread one hides the one thing the caller could act on.
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
// Portuguese, which is the defect the whole page exists to close. Each of these
// lines is copy this package writes, so each has an English entry in
// messages/en.json as well and no literal of one survives in this file.
//
// The verdict's heading is copy this page asks for too: faultTitleKeys spells the
// key of every heading faultKey can name, so the catalogue is read top to bottom as
// the page is. What it says where the catalogue has nothing for a verdict is what it
// always said — the status text this server registers — because a heading invented
// for one verdict and not another would name a cause nobody withheld.
var refusalParts = []string{
	"fault.missing", "fault.granter", "fault.ask", "fault.holds_the_row",
	"fault.sent", "fault.sent_body",
	// The one refusal a page makes about the bytes it was handed, which no module
	// owns because the door that reads a form is the shell's shape rather than any
	// one screen's. It is in this list rather than in faultKey's status table on
	// purpose: a 422 is keyed by what the decoder could not do, not by its number
	// (faultKey says why the number is the wrong key here), and the sentence below
	// is the same at every door that reads a url-encoded body.
	"fault.form_unreadable",
	// The five words the generic verdict page adds above its sentence. They are
	// in this list rather than in the page because the gate that reads the list is
	// what refuses a Portuguese tenant an English label — and a person who is
	// offered "Try again" in English on a page otherwise in Portuguese is exactly
	// the defect this page exists to close, wearing a smaller hat.
	"fault.retry", "fault.retry_after", "fault.back", "fault.sign_in",
	homeKey, referenceKey, referenceLabelKey,
}

// backKey is the way out this package's catalogue words: the page's own way back,
// asked wherever a refusal offers one and names where its link leads.
const backKey = "fault.back"

// homeKey is the words for the destination the composition's own shell points its
// way out at. The label arrives from the application (a Shell's BackLabel, written in
// apps/platformkit) rather than from this package, and these words live here only so
// a translator can reach the one label the reference application ships: backWay hands
// back any other wording untouched, because naming a destination this key does not
// name would tell a person one place while the href carried another.
const homeKey = "fault.home"

// referenceKey is the words that name the request's own identifier where the
// refusal page says it inside its sentence — the grant question's page, whose line
// reads "… (request 1111aaaa-…)".
const referenceKey = "fault.reference"

// referenceLabelKey is the name the plain refusal page gives the identifier when it
// shows it as a value rather than as the tail of a sentence: the one thing on that
// page a person selects and reads to somebody else has to be labelled to be found.
const referenceLabelKey = "fault.request_reference"

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

// statusTitle is the refusal's heading in the request's language. The catalogue the
// request is answered in owns the heading wherever it ships one — which is how a
// deployment rewords a verdict without a release — and what it says when that catalogue
// has nothing is this layer's own word for the verdict (faultTitles), the protocol's name
// for it only where this layer has no words of its own. A shell that ships no catalogue
// at all is answered in these source-language words, never in net/http's name for the
// verdict: "Internal Server Error" is a log line's words put in front of a person, and a
// heading invented for one verdict and not another would name a cause nobody withheld.
func statusTitle(loc *Locale, status int, detail string, sub Subject) string {
	if key, _, _, _ := plainWords(sub, status, detail); key != "" {
		return named(loc, key+titleSuffix, faultTitles[key])
	}
	english := http.StatusText(status)
	key, lookup := faultKey(detail, status)
	if !lookup {
		return english
	}
	if titled, ok := faultTitles[key]; ok {
		english = titled
	}
	return named(loc, key+titleSuffix, english)
}

// faultTitleKeys is every heading key this page can ask for: each key faultKey can
// return, with the suffix on the end. The catalogue test reads it rather than a list,
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

// formUnreadableSentence is what the body decoder says about a body that is not a
// form, in the language it is authored in, and formUnreadableKey is the entry this
// shell ships that sentence under. FormUnreadable puts both on the one verdict.
const (
	formUnreadableSentence = "this form could not be read"
	formUnreadableKey      = "fault.form_unreadable"
)

// FormUnreadable is the refusal of a body that was not a form. It is exported because
// the verdict is not one door's: the ask form, a module's own page and a generated
// screen each read a url-encoded body and each refused it with the same English
// literal. A page's refusal is a person's refusal, so it names the copy the shell
// ships for it (problem.Problem's Key) and is read in the language the person asked
// in; the source language keeps the decoder's own line, which is what the key's
// English entry is (sentence). The one form reader that cannot reach this — a body
// decoded below the presentation layer, in kit/rest — states the same sentence and
// no key, and is answered in the source language.
func FormUnreadable() error {
	refused := problem.New(http.StatusUnprocessableEntity, formUnreadableSentence)
	refused.Key = formUnreadableKey
	return refused
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
// A refusal with no code at all is keyed by its verdict, and only for the five
// verdicts whose sentence this layer writes because nobody who could translate it
// wrote one: the 404 of an address nobody mounted, the 405 of an address that does
// not take the verb it was asked with, the 413 of a body over the bound the
// operation itself declared, the 500 of a handler that broke, and the 503 of a
// guard whose own decision could not be made. Every one of those sentences is the
// framework's about its own machinery, written for the page this package renders it on.
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
// The 413 belongs here for the same reason `fault.form_unreadable` exists at all, and it
// is keyed by its verdict rather than carried on the refusal because nobody owns it: the
// bound is the operation's, the guard that reads it is the framework's, and the door a
// person is standing at when they meet it is a page's. "request body is too large" is the
// framework's sentence about bytes it threw away, which is nobody's language and
// certainly not the reader's; what the person can act on is that what they sent was too
// big for this address. Like the unreadable form, it is one verdict at every door that
// reads a body, so one entry words it for all of them.
//
// A withheld reason is the sixth case, and it is keyed the same way for every verdict. A
// refusal whose Detail a person is not shown — problem.Diagnostic, or a bare status nobody
// wrote a sentence for — leaves the page with a verdict and no words about the request. The
// paragraphs above refuse the 400, the 401, the 403, the 409 and the 422 precisely because a
// module's sentence about the caller's own request is the writer's copy;
// when that sentence is withheld there is nothing left to translate, and a page that answers
// a 401 with the 500's "Something went wrong while handling this." blames our end for the
// reader's own verdict. So the withheld reason is keyed by the verdict at those statuses too,
// and only there: the moment a Detail reaches the page, the writer's sentence is shown and
// this branch is not taken. The same argument that made the 413 and the 503 this package's
// lines — nobody else owns the sentence the framework says about itself — makes these.
//
// The 503 joins the 500 on the same ground as the other four: the sentences are
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
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusRequestEntityTooLarge,
		http.StatusInternalServerError, http.StatusServiceUnavailable:
		return "fault." + strconv.Itoa(status), true
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests:
		// Keyed only over a withheld reason; see the paragraph above. Every one of these
		// is a verdict a person is put in front of — sign-in, a denial, a stale write, an
		// unreadable form, a rate limit — and each has a sentence here because the writer
		// of a withheld Detail chose not to write one for the reader.
		if strings.TrimSpace(detail) == "" {
			return "fault." + strconv.Itoa(status), true
		}
	}
	return "", false
}

// deniedPermissionKey is the catalogue key of the sentence for a missing grant, with
// the permission as its one argument. Its English half lives in messages/en.json like
// every other line this package writes: it is the one sentence that says what the
// short catalogue sentence drops, and it is copy rather than code.
const deniedPermissionKey = "fault.AUTH_DENIED.permission"

// missingGrant keeps the permission a denial names. The guard writes it into its detail
// and a short translated sentence drops it: an editor who signed in met "Não pode fazer
// isto." and could not say what to ask for, and the administrator helping her could not
// tell what to give. A catalogue with the full sentence answers with it; one that carries
// only the short sentence keeps its language and gains the permission, so no shell answers
// a denial in two languages.
func missingGrant(line, language string, loc *Locale, prefixed bool, code, detail string) (string, string) {
	if !prefixed || code != httpx.CodeDenied {
		return line, language
	}
	permission, ok := strings.CutPrefix(strings.TrimPrefix(detail, code+": "), "this operation requires ")
	if !ok || permission == "" {
		return line, language
	}
	words := sprintf(own(deniedPermissionKey), permission)
	switch {
	case loc != nil && (loc.Language == sourceLanguage ||
		loc.Text(deniedPermissionKey, words, permission) != words):
		return code + ": " + loc.Text(deniedPermissionKey, words, permission), loc.Language
	case language != "":
		return line + " (" + permission + ")", language // the shell's own short sentence, in its own language
	default:
		return code + ": " + words, language
	}
}

// speak is one line whose English travels with the verdict rather than in
// messages/en.json — the rows plainWords writes for a verdict no guard worded — through
// the shell's catalogue, and whether the catalogue actually spoke it. own holds the
// English of every other line this package writes; these two sources are the whole of
// what a refusal page says, and a page the catalogue has no copy for is English and must
// not declare Portuguese.
func speak(loc *Locale, key, english string, args ...any) (string, bool) {
	if loc == nil {
		return sprintf(english, args...), false
	}
	out := loc.Text(key, english, args...)
	return out, out != sprintf(english, args...)
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
// put in the detail is the reason the server broke, and that is a log line.
func RefusalWords(loc *Locale, status int, detail string) (title, sentence, tone string, ok bool) {
	key, english, args, _ := plainWords(Subject{}, status, detail)
	if key == "" && status >= http.StatusInternalServerError {
		key, english = saveFailedKey, saveFailed
	}
	if key == "" {
		return "", "", "", false
	}
	text, _ := speak(loc, key, english, args...)
	return named(loc, key+titleSuffix, faultTitles[key]), text, toneOf(key), true
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
	said := guardSaid(loc.Language)
	// The guard's own line stands beside the shipped copy only where the guard wrote one
	// for this page. A refusal with no detail to show — a diagnostic, or a 500 whose
	// reason is in the log — has already said that its sentence is not the person's, and
	// echoing an empty detail after the catalogue's line would be a dash over nothing.
	if detail == "" {
		return shipped, loc.Language
	}
	if code, rest, named := strings.Cut(detail, ": "); named {
		if said {
			return code + ": " + shipped + " " + sourceMark + " " + rest, loc.Language
		}
		return code + ": " + shipped, loc.Language
	}
	if said {
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

// guardSaid is whether the language this page is read in is the language the guards
// author their sentences in — the one language that reads a refusal in both sentences
// (sentence). It names the language rather than matching its first letters, because a
// tag that begins with "en" need not be English: the pseudo-locale
// kit/locale/providers/pseudo reports is `en-XA`, a measuring tag and no reader's
// language, and a prefix match read it as English. The page that measurement rendered
// then carried a guard's English beside the catalogue's sentence on a document
// negotiated from a Portuguese catalogue — and a regional spelling of English needs no
// match here anyway, because a request's own tag is rewritten to the tenant's before
// it reaches this page (TenantPreferences).
func guardSaid(language string) bool {
	return language == sourceLanguage
}

// sourceMark is what stands between the shipped copy and the guard's own line on a
// source-language refusal: an em dash, because the two halves are written by two
// owners and read as two sayings of the same verdict.
const sourceMark = "\u2014"
