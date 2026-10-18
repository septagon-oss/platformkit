// Authentication forms use the existing JSON API; auth alone owns credentials,
// cookies and password tokens. Products compose their own fields and copy.
// data-login-form remains supported. data-auth-form selects register, forgot,
// reset, register-password, verify-email or resend-verification. Feedback uses
// data-auth-error and data-auth-message inside the form. register requests an
// emailed password setup; register-password submits credentials for a separate
// email-verification lifecycle. The composed auth API owns the signup policy.
(function () {
  const signin = document.documentElement.getAttribute("data-signin");
  // Whose session this page was rendered into, and "" for nobody: the same value
  // the document publishes as data-principal for its controllers.
  const principal = document.documentElement.getAttribute("data-principal") || "";
  const sessionError = document.querySelector("[data-session-error]");
  const pending = new WeakSet();

  function local(value) {
    if (!value || !value.startsWith("/") || value.startsWith("//") || /[\\\x00-\x20\x7f]/.test(value)) return null;
    try {
      const target = new URL(value, window.location.origin);
      return target.origin === window.location.origin ? target : null;
    } catch { return null; }
  }

  async function post(url, body) {
    if (!local(url)) throw new Error("Invalid local authentication endpoint");
    return fetch(url, {
      method: "POST", credentials: "same-origin", redirect: "error",
      headers: { "Content-Type": "application/json" },
      body: body ? JSON.stringify(body) : undefined,
    });
  }

  function announce(node, message, quietly = false) {
    if (!node) return;
    node.textContent = message;
    node.hidden = false;
    // Focus completed feedback, including a previously hidden acknowledgment;
    // revealing an already populated live region alone may not announce it. A
    // correction that arrives on its own, seconds after the person stopped
    // waiting for the answer, is the one piece of feedback that must not move
    // focus out from under them: the live region says it either way.
    if (!quietly && ["alert", "status"].includes(node.getAttribute("role"))) {
      node.setAttribute("tabindex", "-1");
      node.focus();
    }
  }

  // The acknowledgment a mailed-link form gives cannot say whether a mail left:
  // the routes that take an address answer the same either way on purpose. The
  // delivery record can say one thing about it — that a transport refused the
  // mail this very call caused — and it is asked over the door beside the endpoint
  // just posted to, with the id that call was already answered with
  // (modules/auth/internal/email_registration.go). Two asks, spread out, because
  // the mail leaves in a worker some time after the answer arrives. A refusal
  // replaces the acknowledgment; everything else — a `pending` answer, a refused
  // or unreadable answer, a lost request — leaves it standing, because "we did
  // not learn that it failed" is not a claim that it worked.
  //
  // The door answers about the sign-up and resend mails only, which are the flows
  // that hand one message to the transport whichever branch they take; the
  // forgotten-password route mails only the addresses it found accounts at, so a
  // refusal there is a fact about the address and the door is not told to read it
  // (modules/auth/README.md). Asking after a forgot response would be a round trip
  // that can only ever come back `pending`, so it is not asked.
  //
  // The door hangs off the module's mount, not off the last segment of the route
  // that answered: kit/httpx/surfaces.go mounts a module's public JSON at
  // /api/v1/public/<module> and its app JSON at /api/v1/<module>, and a route is
  // however many segments the module declared it with, so replacing the last
  // segment asked a path that answers 404 and the failure went unsaid.
  function deliveryDoor(response) {
    try {
      const mount = new URL(response.url).pathname.match(/^\/api\/v1\/(?:public\/|ops\/)?[^/]+/);
      return mount ? mount[0] + "/mail-delivery" : null;
    } catch { return null; }
  }

  async function correctForRefusedMail(response, message) {
    const door = deliveryDoor(response);
    const requestId = response.headers.get("x-request-id");
    if (!requestId || !local(door)) return;
    for (const wait of [1500, 5000]) {
      await new Promise(resolve => setTimeout(resolve, wait));
      try {
        const answer = await post(door, { requestId });
        if (!answer.ok) return;
        const said = await answer.json().catch(() => null);
        if (said?.state === "failed") {
          // About the mail server, not about the address: the door answers about
          // this call's own record, but the record names the address rather than
          // the person, and a mailbox can be refused for reasons that have nothing
          // to do with the account — a full box, a relay that is down, a domain
          // that moved. What the person can act on is that the mail did not leave.
          announce(message, "Mail could not be sent just now: the mail server refused it, so the link may not arrive. Try again shortly, or ask your administrator to send a link.", true);
          return;
        }
      } catch { return; }
    }
  }

  for (const form of document.querySelectorAll("[data-login-form], [data-auth-form]")) {
    const kind = form.hasAttribute("data-login-form") ? "login" : form.getAttribute("data-auth-form");
    if (!["login", "register", "forgot", "reset", "register-password", "verify-email", "resend-verification"].includes(kind)) continue;
    const error = form.querySelector("[data-login-error], [data-auth-error]") || sessionError;
    const message = form.querySelector("[data-auth-message]");
    // Copy may be localized by the composing product without changing behavior.
    const success = message?.textContent.trim();
    let token = "";
    if (kind === "reset" || kind === "verify-email") {
      const url = new URL(window.location.href);
      const tokens = url.searchParams.getAll("token");
      token = tokens.length === 1 ? tokens[0] : "";
      url.searchParams.delete("token");
      window.history.replaceState(window.history.state, "", url);
    }

    form.addEventListener("submit", async function (event) {
      event.preventDefault();
      if (pending.has(form) || !form.reportValidity()) return;
      if ((kind === "reset" || kind === "verify-email") && !token) {
        announce(error, kind === "verify-email"
          ? "This verification link is missing or invalid. Request a new link."
          : "This password link is missing or invalid. Request a new link.");
        return;
      }
      pending.add(form);
      const buttons = [...form.querySelectorAll("button[type=submit], input[type=submit]")];
      const enabled = buttons.filter(button => !button.disabled);
      enabled.forEach(button => { button.disabled = true; });
      form.setAttribute("aria-busy", "true");
      if (error) error.hidden = true;
      if (message) message.hidden = true;
      try {
        const data = new FormData(form);
        let body = { email: data.get("email") };
        if (kind === "login") body.password = data.get("password");
        if (kind === "register") body.displayName = data.get("displayName");
        if (kind === "register-password") {
          body = { ...body, displayName: data.get("displayName"), password: data.get("password"),
            confirmation: data.get("confirmation"), termsAccepted: data.has("termsAccepted") };
        }
        if (kind === "reset") body = { token, new: data.get("new") };
        if (kind === "verify-email") body = { token };
        const response = await post(form.getAttribute("action"), body);
        if (response.ok) {
          if (["login", "reset", "register-password", "verify-email"].includes(kind)) {
            token = "";
            for (const input of form.querySelectorAll("input")) {
              if (input.type === "password" || ["password", "confirmation", "new"].includes(input.name)) input.value = "";
            }
          }
          if (["login", "reset", "verify-email"].includes(kind)) {
            let next = local(form.getAttribute("data-next"));
            // A destination is written for the person the page was rendered to.
            // The sign-in form can answer with somebody else — a second tab
            // switching account is that case — and then the address under it
            // belongs to the person who just lost their session, so it goes with
            // them and the switch lands on the shell's own home instead.
            if (kind === "login" && principal) {
              const answered = await response.json().catch(() => null);
              if (answered?.userId && answered.userId !== principal) next = local(form.getAttribute("data-home"));
            }
            window.location.assign(next?.href || (["reset", "verify-email"].includes(kind) && local(signin)?.href) || "/");
          } else {
            announce(message, success || "If this address can receive an account email, a link will be sent. Check your inbox.");
            if (["register", "register-password", "resend-verification"].includes(kind)) {
              void correctForRefusedMail(response, message);
            }
          }
          return;
        }
        const problem = await response.json().catch(() => ({}));
        // The refusal is this form's business only insofar as the form has to say
        // it out loud. What the refusal names — a type, a status — may be answered
        // by a control elsewhere on the page, so the page is told in the words the
        // server used rather than in a paraphrase this file would have to keep
        // up to date: the problem document is passed through unchanged, and this
        // controller decides nothing about what any listener does with it.
        form.dispatchEvent(new CustomEvent("auth-refused", {
          bubbles: true, detail: { kind, status: response.status, problem },
        }));
        announce(error, typeof problem?.detail === "string" && problem.detail.trim() ? problem.detail : "The request could not be completed. Please try again.");
      } catch {
        // A lost response does not establish whether a write committed. Never
        // replay it; retain input so the person can choose the next action.
        announce(error, "The request outcome is unknown. Check the result before trying again.");
      } finally {
        pending.delete(form);
        enabled.forEach(button => { button.disabled = false; });
        form.removeAttribute("aria-busy");
      }
    });
  }

  let signingOut = false;
  document.addEventListener("click", async function (event) {
    const out = event.target.closest("[data-sign-out]");
    if (!out) return;
    event.preventDefault();
    if (signingOut) return;
    signingOut = true;
    const disabled = out.disabled;
    if ("disabled" in out) out.disabled = true;
    out.setAttribute("aria-busy", "true");
    if (sessionError) sessionError.hidden = true;
    try {
      const response = await post("/api/v1/auth/logout", null);
      if (!response.ok) {
        announce(sessionError, "Sign-out could not be confirmed. Try again before leaving this device.");
        return;
      }
      window.location.assign(local(signin)?.href || "/");
    } catch {
      announce(sessionError, "Sign-out could not be confirmed. Check your connection and try again.");
    } finally {
      signingOut = false;
      if ("disabled" in out) out.disabled = disabled;
      out.removeAttribute("aria-busy");
    }
  });
})();
