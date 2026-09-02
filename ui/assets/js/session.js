// session.js is sign-in and sign-out, which are the two pages that talk to the
// JSON API rather than to a screen.
//
// The auth module owns the session cookie: one route mints it and one route
// clears it, and both take JSON. Rather than write a second pair of routes in
// the admin module that would set the same cookie a second way, the two forms
// here post to the ones that already exist. That is thirty lines instead of a
// duplicate implementation of the thing most worth having exactly once.
(function () {
  async function post(url, body) {
    return fetch(url, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: body ? JSON.stringify(body) : undefined,
    });
  }

  const form = document.querySelector("[data-login-form]");
  if (form) {
    const error = form.querySelector("[data-login-error]");
    form.addEventListener("submit", async function (event) {
      event.preventDefault();
      const button = form.querySelector("button[type=submit]");
      button.disabled = true;
      error.hidden = true;
      try {
        const data = new FormData(form);
        const response = await post(form.getAttribute("action"), {
          email: data.get("email"),
          password: data.get("password"),
        });
        if (response.ok) {
          window.location.assign(form.getAttribute("data-next") || "/admin");
          return;
        }
        const problem = await response.json().catch(() => ({}));
        error.textContent = problem.detail || "Those credentials are not right.";
        error.hidden = false;
      } catch (e) {
        error.textContent = "The server could not be reached.";
        error.hidden = false;
      } finally {
        button.disabled = false;
      }
    });
  }

  document.addEventListener("click", async function (event) {
    const out = event.target.closest("[data-sign-out]");
    if (!out) return;
    event.preventDefault();
    await post("/api/v1/auth/logout", null);
    window.location.assign("/admin/login");
  });
})();
