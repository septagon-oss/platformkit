// theme.js is the light/dark switch.
//
// The stylesheet defines both themes: :root carries the light tokens,
// [data-theme="dark"] carries the dark ones, and a prefers-color-scheme query
// applies dark to a document that has not said otherwise. So this script's
// whole job is the "otherwise" — one attribute, written when a person asks for
// it and remembered, and never before.
//
// That last clause is the whole of the fix. This file used to call apply() on
// every load, which wrote whatever the document happened to be showing into
// localStorage: a person who had never touched the toggle came back to a
// remembered choice they never made, and an installation shipping a dark
// palette of its own was overridden by a stored "light" nobody chose. A default
// is not a choice, and only a choice is stored.
//
// A stored choice is applied before first paint by an inline snippet the shell
// emits, because a script that runs after the stylesheet would flash the wrong
// theme. What is left here is the click, and keeping the toggle's aria-pressed
// honest about what the page is currently showing.
(function () {
  const KEY = "platformkit-theme";

  // showing is what the page looks like right now: the attribute when there is
  // one, and the operating system's answer when there is not.
  function showing() {
    return document.documentElement.getAttribute("data-theme") ||
      (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
  }

  function reflect(theme) {
    for (const button of document.querySelectorAll("[data-theme-toggle]")) {
      button.setAttribute("aria-pressed", theme === "dark" ? "true" : "false");
    }
  }

  // choose is the click: the attribute, the storage and the button, in that
  // order. It is the only thing in this file that writes anything.
  function choose(theme) {
    document.documentElement.setAttribute("data-theme", theme);
    try {
      window.localStorage.setItem(KEY, theme);
    } catch (e) {
      // Private browsing refuses the write. The theme still applies for this
      // page; it just will not survive the next one.
    }
    reflect(theme);
  }

  document.addEventListener("click", function (event) {
    const button = event.target.closest("[data-theme-toggle]");
    if (!button) return;
    event.preventDefault();
    choose(showing() === "dark" ? "light" : "dark");
  });

  // On load: say what is showing, store nothing. A document with no attribute
  // is following the operating system and the installation's own palette, and
  // it goes on doing so until somebody clicks.
  reflect(showing());
  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", function (event) {
    if (!document.documentElement.hasAttribute("data-theme")) {
      reflect(event.matches ? "dark" : "light");
    }
  });
})();
