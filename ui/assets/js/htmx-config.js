// htmx-config.js teaches htmx the two things this application does that its
// defaults do not.
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

  document.body.addEventListener("htmx:afterRequest", function (event) {
    const id = event.detail.xhr && event.detail.xhr.getResponseHeader("X-Request-ID");
    if (id && event.detail.failed) {
      console.warn("platformkit: request " + id + " failed with " + event.detail.xhr.status);
    }
  });

  // A 401 means the session went away while the page was open. Sending the
  // caller to the sign-in page is the only useful thing left to do.
  document.body.addEventListener("htmx:responseError", function (event) {
    if (event.detail.xhr.status === 401 || event.detail.xhr.status === 403) {
      const problem = event.detail.xhr.getResponseHeader("Content-Type") || "";
      if (problem.indexOf("problem+json") >= 0 && event.detail.xhr.status === 401) {
        window.location.assign("/admin/login?next=" + encodeURIComponent(window.location.pathname));
      }
    }
  });
})();
