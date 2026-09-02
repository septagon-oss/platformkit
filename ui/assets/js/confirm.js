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
  const dialog = () => document.getElementById("pk-confirm");

  function ask(message, label, onYes) {
    const el = dialog();
    if (!el || typeof el.showModal !== "function") {
      if (window.confirm(message)) onYes();
      return;
    }
    el.querySelector("[data-confirm-message]").textContent = message;
    const yes = el.querySelector("[data-confirm-accept]");
    if (label) yes.textContent = label;
    let accepted = false;
    const accept = () => {
      accepted = true;
      el.close();
    };
    yes.addEventListener("click", accept, { once: true });
    el.addEventListener("close", function () {
      yes.removeEventListener("click", accept);
      if (accepted) onYes();
    }, { once: true });
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
