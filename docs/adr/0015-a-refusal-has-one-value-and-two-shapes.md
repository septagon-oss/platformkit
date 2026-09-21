# 0015: A refusal is one value, and the client that asked decides its shape

Status: accepted. The kernel half (`kit/httpx`), the presentation half
(`ui/page`) and one adopted application (`apps/platformkit`) are implemented and
tested against each other. The demonstration applications in
`platformkit-catalog` still hand a browser the problem document; for each of
them that is one line of composition, in a release that can carry it.

## Problem

A person navigated to a URL in a disposable demonstration installation and the browser
showed them this:

```json
{"type":"about:blank","title":"Forbidden","status":403,
 "detail":"csrf: this request carries a session cookie and came from another site",
 "instance":"urn:request:ba11d123-…"}
```

Every word of it is correct. All of it is useless to them: there is no way onward, the
reference cannot be quoted from a JSON blob in a window frame, and nothing on the page
says whether this is their fault, the application's, or a transient accident.

The kernel already produced a proper page for one class of refusal. `ui/page` turns a
handler's own 4xx into `Fault` — a document with the shell's chrome, the reason, and one
link back. What it never covered is a refusal that happens *before* a handler exists: the
cross-site guard, the panic recoverer, and every guard that runs ahead of routing. Those
answered with the RFC 9457 document, because `kit/httpx` writes problem JSON and knows
nothing about chrome.

So the same verdict had two shapes, and which one a client received depended on which line
of code had noticed the problem. Nothing in the code said so. The API surface did not
predict it, and a reviewer could not see it.

## Decision

**One value, two shapes, chosen by the client.**

1. The verdict is always a `*problem.Problem`: status, curated detail, and the request
   identifier as RFC 9457's `instance`. This does not change, and no new failure type is
   introduced.
2. `kit/httpx` gains exactly one function — `(*API).fail` — that answers a refusal the
   kernel makes for itself. Every such refusal goes through it. A second writer of problem
   bodies in that package is a second answer to the question this ADR asks. That includes
   the two the router decides before any middleware or handler is involved: an address
   nothing is mounted at, and an address mounted but not for that verb. Left alone, the
   browser got net/http's plain-text `404 page not found` — the same defect, arriving
   through a different door, and far more often.
3. `httpx.Options` gains one field, `Fault`. The kernel asks the client, not the route: a
   request that named `text/html` is offered the document; `Accept: */*` is deliberately
   *not* a request for a page, because that is what curl, probes, monitors and SDKs send
   and answering them with markup is a quiet outage. An htmx request asks with
   `text/html,*/*` and is still a controller rather than a window — see `wantsDocument`.
4. The document is supplied by the presentation layer: `page.FaultHandler(shell)` renders
   it with that shell's chrome, stylesheet and way back. `kit/app` cannot compose this —
   a failure page is chrome, chrome is `ui`, and kit may not import ui — so the
   *application* passes it, and the application that owns the look of its pages owns the
   look of its refusals.
5. The status survives the rendering. A page that says "Forbidden" while answering 200
   would teach monitoring that the incident is healthy.
6. A renderer may decline, which falls back to problem JSON. A shell that cannot render a
   given request must not invent a half-drawn page.
7. `Fault` is nil by default, which is byte-for-byte the previous behaviour. This is why
   the change can land in a released kernel.

## The alternatives we rejected

**A template layer.** A per-status error-page registry, routes like `/errors/403`, or a
hook API an application installs. Each is a second place where "what happens when a request
fails" gets decided, and this change exists to remove that duplication, not to add a tidy
version of it. A registry invites a page per status — 24 statuses of prose nobody maintains
— and it still would not reach the guards that refuse before routing.

**Configure the look with a `FaultPageProps` value.** It would carry brand, title, detail,
back href and stylesheet: the fields `page.Shell` already carries. Two owners of the same
chrome drift on the first theme change. Passing the shell is the same act with one owner.

**HTML as the default, JSON as the exception.** Inverted, this breaks every client that asked
for nothing in particular — monitors, SDKs, this repository's own suite — which is the
quietest possible way to find out who was relying on a value.

**A middleware that rewrites the body afterwards.** It sees a body it cannot attribute to the
verdict that produced it, and cannot keep the status, the content type and the rendered
reason telling one story.

## Consequences

- A mistyped address and a stale bookmark are now the common case this handles: a person
  who typed the wrong thing is told so, in a page with a way back, and a monitor asking the
  same address still receives a parseable document with `"status":404`.
- Guards and handlers stop disagreeing about what a browser sees. The defect is invisible
  in an API test suite because API tests read JSON, and it is invisible in a screenshot of
  a working page. It is caught here by asking one refusal of several kinds of client.
- The 500 page carries the request reference and nothing about what broke. The detail of a
  500 is empty by construction; the reference is what connects a screenshot to a log line.
  The first cut put the reference only on the branch where a detail existed, which left the
  one status a person cannot resolve themselves with an apology and nothing to quote.
- `detail` is data, never markup. A refusal page renders it escaped: it can contain a
  path, an SQL fragment, or a string somebody else supplied.
- The reference is shown as a bare identifier, not the `urn:request:…` form, so it can be
  read aloud; the JSON keeps the URN.
- Two entry points of the reference application now share one composition function rather
  than each spelling out its options. That is how the test exercises the wiring instead of
  a copy of it, and it is the drift that would otherwise have meant one entry point serving
  pages and the other serving JSON.
- Applications that do not set `Fault` keep getting JSON to browsers. That is a legitimate
  choice for a headless API, and it is now an explicit one rather than an accident.
- Nothing about the machine-readable contract moved: same `application/problem+json` body,
  same `instance`, for every client that did not ask to be shown something.

## What this does not do

It is not an error-handling framework. There are no error templates to register, no
`/errors/404` routes, no per-status registry, and no new failure type. It does not decide
*which* refusal a route returns — that stays with the module that owns the rule. It does
not render a refusal for a request that never named a host with a site, because that
happens before a shell is known; such a request gets the problem document, and saying
otherwise would be the same mistake in a different place.

