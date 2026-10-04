// command.js makes one submission of a form happen once, whichever end of the
// network forgot about it. A form opts in with hx-ext="command": the controller
// mints one key per submission, keeps the bytes it sent, and retries the
// identical write under that key when the transport failed — never after a
// refusal, which the server has already answered. The server side is
// kit/httpx/idempotency.go; a repeat that arrives while the first is running is
// its refusal, not this file's, and a conflict is another tab's and never erased.
//
// The retry listeners are capture-phase on document, so htmx-config.js's "outcome
// is unknown" notice appears only when no attempt is left to make: no new copy,
// no second notice, no second outcome region.
(function () {
  if (!window.htmx) return;
  const PREFIX = "pk:command:1:";
  const SAFE = ["get", "head", "options", "trace"];
  // Four retries, backing off, and then the person is told: an unbounded loop is
  // a client that never stops asking.
  const BACKOFF = [500, 1000, 2000, 5000];
  const memory = new Map();
  const running = new Set();
  let started = false, expected = null;

  // The record lives in sessionStorage, because the promise that survives a
  // reload is the promise this file exists for. When storage is unavailable —
  // private mode, quota — the Map still holds it for this submission: the
  // double-click promise needs no storage, only the reload one does.
  function read(id) {
    if (!memory.has(id)) {
      try {
        const raw = sessionStorage.getItem(PREFIX + id);
        if (raw) memory.set(id, JSON.parse(raw));
      } catch (_) { /* Storage is optional; the key is not. */ }
    }
    return memory.get(id);
  }
  function write(id, rec) {
    memory.set(id, rec);
    try { sessionStorage.setItem(PREFIX + id, JSON.stringify(rec)); } catch (_) {}
  }
  function drop(id) {
    memory.delete(id);
    try { sessionStorage.removeItem(PREFIX + id); } catch (_) {}
  }

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

  // One form, one submission. The principal is in the identity so a draft left by
  // an anonymous visitor is not adopted by the account they sign in to; the
  // tenant is already implied by the session this is scoped to.
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
    let rec = read(id);
    if (!rec) rec = { v: 1, key: token(), verb: verb, path: path, body: detail.parameters };
    // The identical write, not a re-serialisation of whatever has been typed
    // since: the server compares what it got with what it hashed.
    if (rec.tries > 0) detail.parameters = rec.body || {};
    rec.at = Date.now();
    write(id, rec);
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
    const rec = read(id);
    if (!rec) return;
    const tries = (rec.tries || 0) + 1;
    if (tries > BACKOFF.length) return; // out of attempts: let the notice through
    rec.tries = tries;
    write(id, rec);
    event.stopPropagation();
    expected = id;
    setTimeout(function () { htmx.ajax(rec.verb, rec.path, { source: form }); }, BACKOFF[tries - 1]);
  }

  // The record is cleared on a settled outcome and by nothing else. An outcome
  // with no status settled nothing; neither did a refusal the kernel's own gate
  // answered (Idempotency-Refusal), which is the one 4xx that says "this key is
  // spent" rather than "the server has drawn your form again".
  function settled(event) {
    const detail = event.detail;
    const form = commanded(detail.requestConfig?.elt || detail.elt);
    if (!form) return;
    const cfg = detail.requestConfig || {};
    const id = identity(form, cfg.path || form.getAttribute("hx-post") || location.pathname);
    running.delete(id);
    const xhr = detail.xhr;
    if (!xhr || !xhr.status) return;
    if (xhr.getResponseHeader("Idempotency-Refusal") || xhr.status >= 500) return;
    drop(id);
  }

  htmx.defineExtension("command", {
    init: function () {
      if (started) return;
      started = true;
      document.addEventListener("htmx:configRequest", configure, true);
      for (const name of ["htmx:sendError", "htmx:timeout"]) document.addEventListener(name, transportFailed, true);
      document.addEventListener("htmx:afterRequest", settled);
    },
  });
})();
