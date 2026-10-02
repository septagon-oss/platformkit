// Passkeys are the two ceremony doors this application can offer: a signed-in
// person enrolling one, and a person at the sign-in page answering one instead of
// a password. Both legs are the auth module's own JSON routes — this file mints
// nothing, learns nothing and stores nothing.
//
// What happens here is the browser half of the standard and nothing else: the
// options the server began are handed to navigator.credentials, and whatever the
// platform returns is handed back. The credential lives in the platform, so a
// refusal here has no state to clean up and a retry costs one more tap.
//
// The controls:
//   [data-passkey-signin] a button at the sign-in door — data-begin, data-verify,
//       data-next say where the two legs are and where a session goes.
//   [data-passkey-enrol]  a form for a signed-in person — its name input is the
//       label the platform will show, data-begin and data-finish the two legs.
// Refusals land in the form's own [data-auth-error] / [data-login-error], or in
// [data-session-error], which is where every other auth refusal on the page
// already appears.
(function () {
  const sessionError = document.querySelector("[data-session-error]");

  function local(value) {
    if (!value || !value.startsWith("/") || value.startsWith("//")) return null;
    try {
      const target = new URL(value, window.location.origin);
      return target.origin === window.location.origin ? target.pathname + target.search : null;
    } catch { return null; }
  }

  // base64url is how the server writes the bytes and how the browser wants them,
  // so the two conversions are the whole of the wire format this file knows.
  function toBytes(value) {
    const padded = value.replace(/=/g, "").replace(/-/g, "+").replace(/_/g, "/");
    const raw = atob(padded + "=".repeat((4 - padded.length % 4) % 4));
    return Uint8Array.from(raw, (c) => c.charCodeAt(0));
  }

  function toText(buffer) {
    const bytes = buffer instanceof ArrayBuffer ? new Uint8Array(buffer) : buffer;
    return btoa(String.fromCharCode(...bytes)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  }

  // The options the server sent are JSON with the byte fields base64url-encoded;
  // navigator.credentials wants them as buffers. Nothing else is changed, which
  // is what keeps the relying-party id, the timeout and the allow list the
  // server's decision rather than this file's.
  function buffers(options) {
    const publicKey = { ...options };
    if (typeof publicKey.challenge === "string") publicKey.challenge = toBytes(publicKey.challenge);
    if (publicKey.user && typeof publicKey.user.id === "string") publicKey.user = { ...publicKey.user, id: toBytes(publicKey.user.id) };
    if (Array.isArray(publicKey.allowCredentials)) {
      publicKey.allowCredentials = publicKey.allowCredentials.map((c) => ({ ...c, id: toBytes(c.id) }));
    }
    return publicKey;
  }

  function answer(credential) {
    const wire = typeof credential.toJSON === "function" ? credential.toJSON() : credential;
    return { id: credential.id, rawId: wire.rawId ?? credential.id, response: wire.response, type: wire.type ?? "public-key" };
  }

  async function ask(url, body) {
    const at = local(url);
    if (!at) throw new Error("Invalid passkey endpoint");
    const response = await fetch(at, {
      method: "POST", credentials: "same-origin", redirect: "error",
      headers: { "Content-Type": "application/json" },
      body: body ? JSON.stringify(body) : undefined,
    });
    if (!response.ok) throw new Error((await response.json().catch(() => ({})))?.detail || "That passkey did not answer");
    return response.json();
  }

  function say(node, text) {
    if (!node) return;
    node.textContent = text;
    node.hidden = false;
    if (["alert", "status"].includes(node.getAttribute("role"))) {
      node.setAttribute("tabindex", "-1");
      node.focus();
    }
  }

  for (const button of document.querySelectorAll("[data-passkey-signin]")) {
    const error = document.querySelector("[data-login-error], [data-auth-error]") || sessionError;
    button.addEventListener("click", async function () {
      if (button.disabled) return;
      button.disabled = true;
      try {
        const begun = await ask(button.getAttribute("data-begin"));
        const publicKey = buffers(JSON.parse(begun.options ?? "{}"));
        const credential = await navigator.credentials.get({ publicKey });
        if (!credential) throw new Error("No passkey answered the prompt");
        await ask(button.getAttribute("data-verify"), { ceremony: begun.ceremony, response: answer(credential) });
        window.location.assign(local(button.getAttribute("data-next")) ?? "/app");
      } catch (problem) {
        button.disabled = false;
        say(error, problem.message || "That passkey did not sign you in");
      }
    });
  }

  for (const form of document.querySelectorAll("[data-passkey-enrol]")) {
    const error = form.querySelector("[data-auth-error], [data-login-error]") || sessionError;
    const message = form.querySelector("[data-auth-message]");
    form.addEventListener("submit", async function (event) {
      event.preventDefault();
      if (!form.reportValidity()) return;
      const button = form.querySelector("button[type=submit]");
      if (button) button.disabled = true;
      try {
        const begun = await ask(form.getAttribute("data-begin"));
        const publicKey = buffers(JSON.parse(begun.options ?? "{}"));
        const credential = await navigator.credentials.create({ publicKey });
        if (!credential) throw new Error("No passkey answered the prompt");
        await ask(form.getAttribute("data-finish"), {
          ceremony: begun.ceremony, response: answer(credential),
          name: new FormData(form).get("name") ?? "",
        });
        form.reset();
        say(message, "That passkey is enrolled.");
      } catch (problem) {
        say(error, problem.message || "That passkey could not be enrolled");
      } finally {
        if (button) button.disabled = false;
      }
    });
  }
})();
