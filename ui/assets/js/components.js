// Enhance the source-rendered checkbox contract, including HTMX fragments.
(function () {
  const ready = new WeakSet();

  function updateCheckbox(root) {
    const input = root.querySelector('input');
    const box = root.querySelector('[data-checkbox-box]');
    if (!input || !box) return;
    const state = input.indeterminate ? 'indeterminate' : input.checked ? 'checked' : 'unchecked';
    root.dataset.state = box.dataset.state = state;
    root.querySelector('[data-checkbox-checkmark]').toggleAttribute('hidden', !input.checked || input.indeterminate);
    root.querySelector('[data-checkbox-bar]').toggleAttribute('hidden', !input.indeterminate);
  }

  function init() {
    for (const root of document.querySelectorAll('[data-controller="checkbox"]')) {
      const input = root.querySelector('input');
      if (!input || ready.has(input)) continue;
      ready.add(input);
      input.indeterminate = root.dataset.checkboxIndeterminateValue === 'true';
      updateCheckbox(root);
    }
  }

  document.addEventListener('change', event => {
    const root = event.target.closest('[data-controller="checkbox"]');
    if (root) updateCheckbox(root);
  });
  document.addEventListener('reset', event => {
    queueMicrotask(() => {
      if (event.defaultPrevented) return;
      for (const root of event.target.querySelectorAll('[data-controller="checkbox"]')) {
        const input = root.querySelector('input');
        if (input) input.indeterminate = root.dataset.checkboxIndeterminateValue === 'true';
        updateCheckbox(root);
      }
    });
  });
  document.addEventListener('htmx:afterSwap', init);
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
  else init();
})();
