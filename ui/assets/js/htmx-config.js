// htmx-config.js retains rendered validation, request diagnostics and the
// current form when authentication or transport fails. Writes are never replayed.
//
// First: a 422 is a response worth showing. htmx treats every 4xx as an error
// and swaps nothing, which is right for a 500 and wrong for the one status this
// API uses to say "this field is not valid" — the server has already rendered
// the form again with the errors in it, and throwing that away would leave the
// caller looking at a form that did not change.
//
// Second: every request that changes something carries the request id back, so
// a failure a person reports is one grep away from the log line behind it.
(function () {
  if (!window.htmx) return;
  // Pure classification of the existing problem contract. Status alone cannot
  // distinguish a missing session from a permission or account-change refusal.
  function failureKind(status, contentType, body) {
    if (status === 403 && /^application\/problem\+json(?:\s*;|$)/i.test(contentType || "")) {
      try {
        const detail = JSON.parse(body)?.detail;
        if (typeof detail === "string") {
          if (detail.startsWith("AUTH_ANONYMOUS:")) return "anonymous";
          if (detail.startsWith("AUTH_DENIED:")) return "denied";
          if (detail.startsWith("AUTH_PRINCIPAL_CHANGED:")) return "changed";
        }
      } catch (_) { /* An unreadable response establishes no safe outcome. */ }
    }
    return "uncertain";
  }

  // This boundary owns DOM effects only. Notices are rendered by page.Document;
  // inputs, selected files and focus remain in their original form.
  const owners = new WeakMap();
  function showFailure(event) {
    const xhr = event.detail.xhr;
    const kind = failureKind(xhr?.status, xhr?.getResponseHeader("Content-Type"), xhr?.responseText);
    const notice = document.getElementById("pk-auth-" + kind);
    if (!notice) return;
    for (const previous of document.querySelectorAll("[data-request-notice]")) previous.hidden = true;
    const sender = event.detail.requestConfig?.elt ?? event.detail.elt;
    const form = sender?.closest("form");
    if (form?.isConnected) form.after(notice);
    else document.body.prepend(notice);
    owners.set(notice, sender);
    notice.hidden = false;
    notice.scrollIntoView({ behavior: "instant", block: "nearest" });
  }

  htmx.config.responseHandling = [
    { code: "204", swap: false },
    { code: "422", swap: true },
    { code: "[23]..", swap: true },
    { code: "[45]..", swap: false, error: true },
  ];
  htmx.config.defaultSwapStyle = "outerHTML";
  // The session cookie is SameSite=Lax, so a same-origin fetch carries it; the
  // kernel's CSRF check reads Sec-Fetch-Site, which the browser sets itself.
  htmx.config.selfRequestsOnly = true;

  document.body.addEventListener("htmx:configRequest", function (event) {
    const principal = document.documentElement.getAttribute("data-principal");
    if (principal && !["get", "head", "options", "trace"].includes(event.detail.verb.toLowerCase())) {
      event.detail.headers["X-Expected-Principal"] = principal;
    }
  });

  document.body.addEventListener("htmx:afterRequest", function (event) {
    const id = event.detail.xhr && event.detail.xhr.getResponseHeader("X-Request-ID");
    if (id && event.detail.failed) {
      console.warn("platformkit: request " + id + " failed with " + event.detail.xhr.status);
    }
    if (event.detail.successful) {
      // HTMX redispatches from an ancestor after swapping out the original form.
      const sender = event.detail.requestConfig?.elt ?? event.detail.elt;
      for (const notice of document.querySelectorAll("[data-request-notice]")) {
        if (owners.get(notice) === sender) notice.hidden = true;
      }
    }
  });

  for (const name of ["htmx:responseError", "htmx:sendError", "htmx:timeout"]) {
    document.body.addEventListener(name, showFailure);
  }
})();
