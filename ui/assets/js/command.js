// command.js makes one submission of a form happen once, whichever end of the
// network forgot about it. A form opts in with hx-ext="command": the controller
// mints one key per submission, keeps the bytes it sent, and retries the identical
// write under that key when the transport failed — never after a refusal, which the
// server has already answered. The server side is kit/httpx/idempotency.go; a
// repeat while the first is running is its refusal, not this file's, and a conflict
// is another tab's and never erased.
//
// One record per form, kept while this tab has a submission outstanding. Inside one
// page the controller knows its own request went unanswered, so the key names one
// command and its retry is safe. A reload is the same case in a different document:
// the submission that left is still outstanding, and a person pressing that button
// again is asking for that write, not for a second one. So a pending record goes to
// sessionStorage — one tab's storage for one tab's intent — and is adopted only when
// it names an attempt somebody still owes.
//
// Bytes say which command a press is, here as at the server, which compares the body it
// hashed with the body it is handed: the same bytes keep the key and inherit the answer
// that key already holds, and other bytes are the next command, with a key of its own.
// A form whose command depends on what the world holds therefore says what it counted,
// so a world that moved since the page was drawn gives the next press other bytes.
//
// Listeners are capture-phase, so that notice waits until no attempt is left to make.
(function () {
  if (!window.htmx) return;
  const SAFE = ["get", "head", "options", "trace"];
  // Four retries, backing off, and then the person is told: an unbounded loop is
  // a client that never stops asking.
  const BACKOFF = [500, 1000, 2000, 5000];
  // The form's own outcome region, which the page renders inside the form and which
  // no other form's result may use: see modules/admin's commandOutcome.
  const OUTCOME = '[role="status"],[role="alert"]';
  const records = new Map(), timers = new Map(), running = new Set(), expected = new Set();
  let started = false;

  function token() {
    if (crypto.randomUUID) return crypto.randomUUID();
    return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, function (c) {
      const r = crypto.getRandomValues(new Uint8Array(1))[0] % 16;
      return (c === "x" ? r : (r & 3) | 8).toString(16);
    });
  }

  // The bytes one press puts on the wire, as one string, over the same fields the
  // server hashes.
  function bytes(parameters) {
    const parts = [];
    for (const key of Object.keys(parameters || {}).sort()) {
      const value = parameters[key];
      parts.push(key + "=" + (value === null || typeof value !== "object" ? value : JSON.stringify(value)));
    }
    return parts.join("&");
  }

  function commanded(elt) {
    for (let n = elt; n && n.nodeType === 1; n = n.parentElement) {
      if (/(^|\s)command(\s|$)/.test(n.getAttribute("hx-ext") || "")) return n.closest("form") || n;
    }
    return null;
  }

  // One form, one submission. The principal is in the identity so a draft left by an
  // anonymous visitor is not adopted by the account they sign in to.
  function identity(form, path) {
    return path + "|" + (form.id || form.getAttribute("name") || "") + "|" +
      (document.documentElement.getAttribute("data-principal") || "");
  }

  const slot = function (id) { return "platformkit-command:" + id; };
  function store(id, rec) {
    records.set(id, rec);
    try { sessionStorage.setItem(slot(id), JSON.stringify(rec)); } catch (_) { /* no cross-reload memory */ }
  }
  // A stored record names an attempt somebody owes, or it is nothing: a key with no
  // attempt behind it was answered, and asking it again is a new command.
  function adopt(id) {
    let rec = null;
    try { rec = JSON.parse(sessionStorage.getItem(slot(id))); } catch (_) { return null; }
    if (!rec || typeof rec.key !== "string" || !(rec.tries > 0)) return null;
    return rec;
  }
  // Retire: this key means nothing now, and nothing waits on this document.
  function retire(id) {
    records.delete(id);
    expected.delete(id);
    if (timers.has(id)) { clearTimeout(timers.get(id)); timers.delete(id); }
    try { sessionStorage.removeItem(slot(id)); } catch (_) { /* gone either way */ }
  }
  // A press is a person, and the person supersedes the timer: what was scheduled for
  // this form has just become the next thing they did.
  function unschedule(id) {
    if (timers.has(id)) { clearTimeout(timers.get(id)); timers.delete(id); }
  }

  function configure(event) {
    const detail = event.detail;
    const verb = String(detail.verb || "get").toLowerCase();
    if (SAFE.indexOf(verb) >= 0) return;
    const form = commanded(detail.elt);
    if (!form) return;
    const path = detail.path || form.getAttribute("hx-post") || form.getAttribute("action") || location.pathname;
    const id = identity(form, path);
    // One request per submission, from this page: the submit button's own double
    // click is the second request nobody asked for. A retry of ours is expected — of
    // the form that armed it, and of no other.
    if (running.has(id) && !expected.has(id)) { event.preventDefault(); return; }
    expected.delete(id);
    unschedule(id);
    let rec = records.get(id) || adopt(id);
    // A record belongs to the bytes that left with it. A form saying something else
    // now is a new command, not the outstanding one, and the spent key would only
    // replay the old answer: retire the record, as a spent key retires it at the server.
    if (rec && bytes(rec.body) !== bytes(detail.parameters)) { retire(id); rec = null; }
    if (!rec) rec = { key: token(), verb: verb, path: path, body: detail.parameters };
    // The identical write, not a re-serialisation of whatever has been typed
    // since: the server compares what it got with what it hashed.
    if (rec.tries > 0) detail.parameters = rec.body || {};
    store(id, rec); // in flight from this line: a reload must not forget it
    detail.headers["Idempotency-Key"] = rec.key;
    running.add(id);
  }

  // A transport failure is the only thing retried, because it is the only answer
  // that says nothing about whether the command ran. A status is an answer.
  function transportFailed(event) {
    const detail = event.detail;
    const form = commanded(detail.elt || detail.requestConfig?.elt);
    if (!form) return;
    const path = (detail.requestConfig && detail.requestConfig.path) || detail.path ||
      form.getAttribute("hx-post") || form.getAttribute("action") || location.pathname;
    const id = identity(form, path);
    running.delete(id);
    const rec = records.get(id) || adopt(id);
    if (!rec) return;
    const tries = (rec.tries || 0) + 1;
    if (tries > BACKOFF.length) return; // out of attempts: let the notice through
    rec.tries = tries;
    store(id, rec);
    event.stopPropagation();
    expected.add(id);
    timers.set(id, setTimeout(function () {
      timers.delete(id);
      htmx.ajax(rec.verb, rec.path, { source: form });
    }, BACKOFF[tries - 1]));
  }

  // The record is cleared once the key means nothing. A 5xx settled nothing, and so
  // did the refusal saying an answer is still owed: another request with this key is
  // running it, and the same key asks for its result later. Every other answer retires
  // it — a spent key especially — so the next press is a new command. A key the server
  // has already run answers from its record (Idempotency-Replay), redirect included,
  // which lands the person on the list the command redrew: the same place a delivered
  // answer would have, one GET later. Nothing is asked a second time under a second key
  // — the person pressed once, and once is the promise.
  function settled(event) {
    const detail = event.detail;
    const form = commanded(detail.requestConfig?.elt || detail.elt);
    if (!form) return;
    const id = identity(form, (detail.requestConfig || {}).path || form.getAttribute("hx-post") || location.pathname);
    running.delete(id);
    const xhr = detail.xhr;
    if (!xhr || !xhr.status || xhr.status >= 500) return;
    if (xhr.getResponseHeader("Idempotency-Refusal") === "IDEMPOTENCY_IN_PROGRESS") return;
    retire(id);
  }

  // A keyed command's refusal is a statement about the key, not markup about this
  // form. Swapping it in would replace the form the person is looking at, and what
  // they typed in it, to show a sentence about a token they never saw — so the form
  // stays and the sentence goes to the region the page set aside for this form's
  // result. With no region rendered there is nothing to say it in, and the form is
  // still the thing to leave alone.
  function reportRefusal(event) {
    const detail = event.detail;
    if (!detail.xhr || typeof detail.xhr.getResponseHeader !== "function") return;
    const code = detail.xhr.getResponseHeader("Idempotency-Refusal");
    if (!code) return;
    const form = commanded(detail.requestConfig?.elt || detail.target);
    if (!form) return;
    detail.shouldSwap = false;
    const outcome = form.querySelector(OUTCOME), message = outcome?.querySelector("[data-alert-message]");
    if (!message) return;
    const text = String(detail.xhr.responseText || "").trim();
    message.replaceChildren(document.createTextNode(text.startsWith(code + ":") ? text.slice(code.length + 1).trim() : text));
    outcome.hidden = false;
  }

  htmx.defineExtension("command", {
    init: function () {
      if (started) return;
      started = true;
      document.addEventListener("htmx:configRequest", configure, true);
      for (const name of ["htmx:sendError", "htmx:timeout"]) document.addEventListener(name, transportFailed, true);
      document.addEventListener("htmx:afterRequest", settled);
      document.addEventListener("htmx:beforeSwap", reportRefusal, true);
      // A document restored from the back/forward cache kept this script and its
      // records while its page was redrawn elsewhere: a reload's case by another door.
      window.addEventListener("pageshow", function (event) {
        if (!event.persisted) return;
        for (const id of Array.from(records.keys())) retire(id);
        running.clear();
      });
    },
  });
})();
