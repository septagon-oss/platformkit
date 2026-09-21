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
	"net/http"
	"strconv"
	"strings"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
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
// A refusal with no code at all is keyed by its verdict, and only for the three
// verdicts whose sentence this layer writes because nobody else wrote it: the 404
// of an address nobody mounted, the 405 of an address that does not take the verb
// it was asked with, and the 500 of a handler that broke. Every one of those three
// sentences is kit/httpx's own, written for the page this package renders it on.
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
func faultKey(detail string, status int) (key string, lookup bool) {
	if code, _, named := strings.Cut(detail, ": "); named {
		key, shipped := faultKeys[code]
		return key, shipped
	}
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusInternalServerError:
		return "fault." + strconv.Itoa(status), true
	}
	return "", false
}

// refusalLocale is the language this refusal is answered in, or nil for a shell that ships
// no catalog at all.
//
// The request's own Accept-Language is the only preference available, and that is a
// consequence rather than a shortcut. Shell.Locale reads a URL, an account or a tenant
// preference, and each of those needs something this response is the refusal to have: a
// resolved tenant, an open transaction, a session that was accepted, or a handler to run
// at all. A guard answers before any of them exist, so the language of a refusal can only
// be the one the caller brought — which is also exactly what makes Vary: Accept-Language
// the true thing to say about the response.
func refusalLocale(m Messages, r *http.Request) *Locale {
	if m == nil {
		return nil
	}
	loc := SelectLocale(m, r.Header.Get("Accept-Language"))
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
		loc := refusalLocale(s.Messages, r)
		// The frame is given the negotiated language for the same reason the sentence is:
		// a shell whose chrome has labels of its own renders them here as it does on every
		// page Serve mounts, and not in English because the request was refused.
		req.Locale = loc
		v := fault(status, p.Detail, loc, requestID(p.Instance), s.Back, s.BackLabel)

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
func fault(status int, detail string, loc *Locale, reference, back, backLabel string) View {
	line := detail
	if strings.TrimSpace(line) == "" {
		// A 500 carries no detail on purpose: the reason is in the log, not for the
		// browser. The sentence has to be true and useful without it.
		line = "Something went wrong while handling this."
	}
	language := ""
	if loc != nil {
		if key, lookup := faultKey(detail, status); lookup {
			if text := loc.Text(key, line); text != line {
				line, language = text, loc.Language
				if code, _, named := strings.Cut(detail, ": "); named {
					line = code + ": " + text
				}
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
	v := Fault(status, line, back, backLabel)
	if language != "" {
		v.Language = language
	}
	return v
}

// requestID is the instance URN read back into the bare identifier, which is what a
// human reads aloud to a human reading a log.
func requestID(instance string) string {
	if instance == "" {
		return ""
	}
	return strings.TrimPrefix(instance, "urn:request:")
}
