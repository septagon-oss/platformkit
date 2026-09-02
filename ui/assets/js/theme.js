// theme.js is the light/dark switch.
//
// The stylesheet defines both themes: :root carries light, [data-theme="dark"]
// carries dark, and a prefers-color-scheme query applies dark to a document
// that has not said otherwise. So this script's whole job is the "otherwise" —
// one attribute, remembered.
//
// The attribute is written before first paint by an inline snippet the shell
// emits (see the layout), because a script that runs after the stylesheet would
// flash the wrong theme. This file only handles the click.
(function () {
  const KEY = "platformkit-theme";

  function current() {
    return document.documentElement.getAttribute("data-theme") ||
      (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
  }

  function apply(theme) {
    document.documentElement.setAttribute("data-theme", theme);
    try {
      window.localStorage.setItem(KEY, theme);
    } catch (e) {
      // Private browsing refuses the write. The theme still applies for this
      // page; it just will not survive the next one.
    }
    for (const button of document.querySelectorAll("[data-theme-toggle]")) {
      button.setAttribute("aria-pressed", theme === "dark" ? "true" : "false");
    }
  }

  document.addEventListener("click", function (event) {
    const button = event.target.closest("[data-theme-toggle]");
    if (!button) return;
    event.preventDefault();
    apply(current() === "dark" ? "light" : "dark");
  });

  apply(current());
})();
