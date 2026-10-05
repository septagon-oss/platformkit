// command.js makes one submission of a form happen once, whichever end of the
// network forgot about it. A form opts in with hx-ext="command": the controller
// mints one key per submission, keeps the bytes it sent, and retries the
// identical write under that key when the transport failed — never after a
// refusal, which the server has already answered. The server side is
// kit/httpx/idempotency.go; a repeat while the first is running is its refusal, not
// this file's, and a conflict is another tab's and never erased. The outcome region
// is htmx-config.js's notice, which the page renders: this file writes no markup.
//
// One record per form, for as long as this document is the page in front of the
// person. Inside one page the controller knows the answer to its own request never
// arrived, so the key names one command and its retry is safe. After a reload it
// knows nothing: the person may be retrying that lost submission or asking for
// something new, the two are identical bytes, and guessing wrong either replays the
// old answer over the new command or runs the lost one twice. So the record dies
// with the document, and the redrawn page says whether to press again.
//
// Listeners are capture-phase, so that notice waits until no attempt is left to make.
(function () {
  if (!window.htmx) return;
  const SAFE = ["get", "head", "options", "trace"];
  // Four retries, backing off, and then the person is told: an unbounded loop is
  // a client that never stops asking.
  const BACKOFF = [500, 1000, 2000, 5000];
  const records = new Map();
  const running = new Set();
  let started = false, expected = null;

  function token() {
    if (crypto.randomUUID) return crypto.randomUUID();
    return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, function (c) {
      const r = crypto.getRandomValues(new Uint8Array(1))[0] % 16;
      return (c === "x" ? r : (r & 3) | 8).toString(16);
    });
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

  function configure(event) {
    const detail = event.detail;
    const verb = String(detail.verb || "get").toLowerCase();
    if (SAFE.indexOf(verb) >= 0) return;
    const form = commanded(detail.elt);
    if (!form) return;
    const path = detail.path || form.getAttribute("hx-post") || form.getAttribute("action") || location.pathname;
    const id = identity(form, path);
    // One request per submission, from this page: the submit button's own double
    // click is the second request nobody asked for. A retry of ours is expected.
    if (running.has(id) && expected !== id) { event.preventDefault(); return; }
    expected = null;
    let rec = records.get(id);
    if (!rec) rec = { key: token(), verb: verb, path: path, body: detail.parameters };
    // The identical write, not a re-serialisation of whatever has been typed
    // since: the server compares what it got with what it hashed.
    if (rec.tries > 0) detail.parameters = rec.body || {};
    records.set(id, rec);
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
    const rec = records.get(id);
    if (!rec) return;
    const tries = (rec.tries || 0) + 1;
    if (tries > BACKOFF.length) return; // out of attempts: let the notice through
    rec.tries = tries;
    records.set(id, rec);
    event.stopPropagation();
    expected = id;
    setTimeout(function () { htmx.ajax(rec.verb, rec.path, { source: form }); }, BACKOFF[tries - 1]);
  }

  // The record is cleared once the key means nothing. A 5xx settled nothing, and so
  // did the refusal saying an answer is still owed: another request with this key is
  // running it, and the same key asks for its result later. Every other answer retires
  // it — a spent key especially — so the next press is a new command.
  function settled(event) {
    const detail = event.detail;
    const form = commanded(detail.requestConfig?.elt || detail.elt);
    if (!form) return;
    const cfg = detail.requestConfig || {};
    const id = identity(form, cfg.path || form.getAttribute("hx-post") || location.pathname);
    running.delete(id);
    const xhr = detail.xhr;
    if (!xhr || !xhr.status) return;
    if (xhr.status >= 500) return;
    if (xhr.getResponseHeader("Idempotency-Refusal") === "IDEMPOTENCY_IN_PROGRESS") return;
    records.delete(id);
  }

  htmx.defineExtension("command", {
    init: function () {
      if (started) return;
      started = true;
      document.addEventListener("htmx:configRequest", configure, true);
      for (const name of ["htmx:sendError", "htmx:timeout"]) document.addEventListener(name, transportFailed, true);
      document.addEventListener("htmx:afterRequest", settled);
      // A document restored from the back/forward cache kept this script and its
      // records while its page was redrawn elsewhere: a reload's case by another door.
      window.addEventListener("pageshow", function (event) {
        if (event.persisted) { records.clear(); running.clear(); }
      });
    },
  });
})();
