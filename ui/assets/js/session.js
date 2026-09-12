// Authentication forms use the existing JSON API; auth alone owns credentials,
// cookies and password tokens. Products compose their own fields and copy.
// data-login-form remains supported. data-auth-form selects register, forgot,
// reset, register-password, verify-email or resend-verification. Feedback uses
// data-auth-error and data-auth-message inside the form. register requests an
// emailed password setup; register-password submits credentials for a separate
// email-verification lifecycle. The composed auth API owns the signup policy.
(function () {
  const signin = document.documentElement.getAttribute("data-signin");
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

  function announce(node, message) {
    if (!node) return;
    node.textContent = message;
    node.hidden = false;
    // Focus completed feedback, including a previously hidden acknowledgment;
    // revealing an already populated live region alone may not announce it.
    if (["alert", "status"].includes(node.getAttribute("role"))) {
      node.setAttribute("tabindex", "-1");
      node.focus();
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
            const next = local(form.getAttribute("data-next"));
            window.location.assign(next?.href || (["reset", "verify-email"].includes(kind) && local(signin)?.href) || "/");
          } else {
            announce(message, success || "If this address can receive an account email, a link will be sent. Check your inbox.");
          }
          return;
        }
        const problem = await response.json().catch(() => ({}));
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
