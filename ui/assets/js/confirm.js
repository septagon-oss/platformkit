// confirm.js replaces window.confirm with the application's own dialog.
//
// htmx raises htmx:confirm before it issues a request carrying hx-confirm, and
// a listener that calls event.detail.issueRequest() later is how the decision
// is made asynchronously. So the whole interaction is: stop the request, show
// the <dialog> the page already contains, and issue the request if the person
// says yes.
//
// It is a native <dialog>, so the focus trap, the Escape key, the backdrop and
// the inertness of everything behind it are the browser's, not ours.
(function () {
  const dialog = () => document.querySelector("[data-confirm-message]")?.closest("dialog");
  const labels = new WeakMap();

  function ask(message, label, onYes) {
    const el = dialog();
    if (!el || typeof el.showModal !== "function") {
      if (window.confirm(message)) onYes();
      return;
    }
    if (el.open) return;
    el.querySelector("[data-confirm-message]").textContent = message;
    const yes = el.querySelector("[data-confirm-accept]");
    // Cancel closes the dialog from here rather than from an onclick on the
    // button: the content security policy admits no inline handler, so an
    // attribute would have been a button that did nothing.
    const no = el.querySelector("[data-confirm-cancel]");
    if (!labels.has(yes)) labels.set(yes, yes.textContent);
    yes.textContent = label || labels.get(yes);
    let settled = false;
    function finish(accepted) {
      if (settled) return;
      settled = true;
      // Clean up synchronously: the browser queues close events, and a person
      // can reopen the dialog before the preceding close event is delivered.
      yes.removeEventListener("click", accept);
      if (no) no.removeEventListener("click", cancel);
      el.removeEventListener("cancel", escape);
      el.removeEventListener("close", closed);
      el.close();
      if (accepted) onYes();
    }
    const accept = () => finish(true);
    const cancel = () => finish(false);
    const escape = event => { event.preventDefault(); cancel(); };
    const closed = () => { if (!el.open) cancel(); };
    yes.addEventListener("click", accept);
    if (no) no.addEventListener("click", cancel);
    el.addEventListener("cancel", escape);
    el.addEventListener("close", closed);
    el.showModal();
  }

  document.body.addEventListener("htmx:confirm", function (event) {
    if (!event.detail.question) return;
    event.preventDefault();
    ask(event.detail.question, event.target.getAttribute("data-confirm-label"), function () {
      event.detail.issueRequest(true);
    });
  });

  // A plain link or form that is not htmx-driven asks the same way.
  document.addEventListener("click", function (event) {
    const trigger = event.target.closest("[data-confirm]");
    if (!trigger || trigger.hasAttribute("hx-delete") || trigger.hasAttribute("hx-post")) return;
    event.preventDefault();
    ask(trigger.getAttribute("data-confirm"), trigger.getAttribute("data-confirm-label"), function () {
      if (trigger.form) trigger.form.submit();
      else if (trigger.href) window.location.assign(trigger.href);
    });
  });
})();
