// Enhance source-rendered contracts, including fragments inserted by HTMX.
(function () {
  const ready = new WeakSet();
  const triggers = new WeakMap();
  const openings = new WeakMap();
  const modalRequests = new WeakMap();
  const own = (root, selector) => [...root.querySelectorAll(selector)]
    .filter(el => el.closest('[data-controller="tabs"]') === root);

  function activate(tab, focus) {
    const root = tab.closest('[data-controller="tabs"]');
    if (!root || tab.disabled) return;
    const tabs = own(root, '[data-tabs-tab]');
    for (const item of tabs) {
      const selected = item === tab;
      item.setAttribute('aria-selected', String(selected));
      item.tabIndex = selected ? 0 : -1;
      for (const name of item.dataset.tabsActiveClasses.split(' ').filter(Boolean)) item.classList.toggle(name, selected);
      for (const name of item.dataset.tabsInactiveClasses.split(' ').filter(Boolean)) item.classList.toggle(name, !selected);
    }
    for (const panel of own(root, '[data-tabs-panel]')) {
      const selected = panel.id === tab.getAttribute('aria-controls');
      panel.hidden = !selected;
      panel.setAttribute('aria-hidden', String(!selected));
      panel.dataset.state = selected ? 'active' : 'inactive';
      if (selected) panel.dispatchEvent(new CustomEvent('tabs:activate', { bubbles: true }));
    }
    root.dataset.tabsActiveTabValue = tab.dataset.tabsTab;
    if (focus) tab.focus();
  }

  function show(dialog, trigger) {
    if (!dialog || dialog.getAttribute('aria-disabled') === 'true') return;
    if (dialog.matches(':modal')) return;
    if (trigger) triggers.set(dialog, trigger);
    dialog.hidden = false;
    dialog.style.removeProperty('display');
    dialog.removeAttribute('open');
    // A deferred root takes its name from the delivered panel.
    const panel = dialog.querySelector('[data-modal-panel][role="dialog"]');
    if (panel) {
      for (const name of ['aria-label', 'aria-labelledby']) {
        dialog.removeAttribute(name);
        if (panel.hasAttribute(name)) dialog.setAttribute(name, panel.getAttribute(name));
      }
      panel.removeAttribute('role');
      panel.removeAttribute('aria-modal');
    }
    dialog.setAttribute('aria-hidden', 'false');
    dialog.dataset.state = 'open';
    dialog.showModal();
    openings.set(dialog, {});
    const first = dialog.querySelector('[autofocus]') || dialog.querySelector('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), a[href]');
    (first || dialog).focus();
  }

  function updateTextarea(input) {
    const counter = input.closest('[data-component="textarea"]')?.querySelector('[data-textarea-counter-target="display"]');
    if (counter) counter.textContent = String([...input.value].length) + (input.maxLength > 0 ? ` / ${input.maxLength}` : '');
    if (input.dataset.controller !== 'autoresize') return;
    const style = getComputedStyle(input);
    const line = parseFloat(style.lineHeight) || parseFloat(style.fontSize) * 1.5;
    const padding = parseFloat(style.paddingTop) + parseFloat(style.paddingBottom);
    const border = parseFloat(style.borderTopWidth) + parseFloat(style.borderBottomWidth);
    const min = Number(input.dataset.autoresizeMinRowsValue) * line + padding + border;
    const max = Number(input.dataset.autoresizeMaxRowsValue) * line + padding + border;
    input.style.height = 'auto';
    input.style.height = `${Math.max(min, Math.min(max, input.scrollHeight + border))}px`;
    input.style.overflowY = input.scrollHeight + border > max ? 'auto' : 'hidden';
  }

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
    for (const dialog of document.querySelectorAll('dialog[data-component="modal"]')) {
      if (ready.has(dialog)) continue;
      ready.add(dialog);
      dialog.addEventListener('cancel', event => {
        if (dialog.dataset.htmxModalCloseOnEscapeValue === 'false' || dialog.getAttribute('aria-disabled') === 'true') event.preventDefault();
      });
      dialog.addEventListener('keydown', event => {
        if (event.key !== 'Tab') return;
        const stops = [...dialog.querySelectorAll('button, input, select, textarea, a[href], [tabindex]')]
          .filter(el => !el.disabled && el.tabIndex >= 0 && el.getClientRects().length);
        const first = stops[0] || dialog;
        const last = stops.at(-1) || dialog;
        if ((event.shiftKey && document.activeElement === first) || (!event.shiftKey && document.activeElement === last) || !stops.length) {
          event.preventDefault();
          (event.shiftKey ? last : first).focus();
        }
      });
      dialog.addEventListener('close', () => {
        // Ignore a queued close event after the same dialog has reopened.
        if (dialog.open) return;
        openings.delete(dialog);
        dialog.dataset.state = 'closed';
        dialog.setAttribute('aria-hidden', 'true');
        if (dialog.dataset.htmxModalClearOnCloseValue === 'true') dialog.replaceChildren();
        const trigger = triggers.get(dialog);
        if (trigger?.isConnected) trigger.focus();
      });
      if (dialog.dataset.htmxModalOpenValue === 'true') show(dialog);
    }
    for (const root of document.querySelectorAll('[data-controller="tabs"]')) {
      if (ready.has(root)) continue;
      ready.add(root);
      const selected = own(root, '[data-tabs-tab]').find(tab => tab.getAttribute('aria-selected') === 'true');
      if (selected) activate(selected, false);
    }
    for (const root of document.querySelectorAll('[data-controller="checkbox"]')) {
      const input = root.querySelector('input');
      if (!input || ready.has(input)) continue;
      ready.add(input);
      input.indeterminate = root.dataset.checkboxIndeterminateValue === 'true';
      updateCheckbox(root);
    }
    for (const input of document.querySelectorAll('[data-textarea-input]')) updateTextarea(input);
  }

  document.addEventListener('click', event => {
    const opener = event.target.closest('[data-modal-open]');
    if (opener && !opener.disabled) {
      const dialog = document.getElementById(opener.dataset.modalOpen);
      if (dialog?.matches('dialog[data-component="modal"]')) {
        event.preventDefault();
        show(dialog, opener);
      }
    }
    const action = event.target.closest('[data-action]');
    const dialog = action?.closest('dialog[data-component="modal"]');
    if (dialog && dialog.getAttribute('aria-disabled') !== 'true') {
      if (action.dataset.action.includes('click->htmx-modal#close') ||
          (event.target === dialog && action.dataset.action.includes('click->htmx-modal#backdropClick'))) dialog.close();
    }
    if (action?.dataset.action.includes('click->alert#dismiss')) action.closest('[data-controller="alert"]')?.remove();
    const tab = event.target.closest('[data-tabs-tab]');
    if (tab) activate(tab, false);
  });
  document.addEventListener('keydown', event => {
    const tab = event.target.closest('[data-tabs-tab]');
    if (!tab) return;
    const root = tab.closest('[data-controller="tabs"]');
    const tabs = own(root, '[data-tabs-tab]').filter(item => !item.disabled);
    const vertical = tab.closest('[role="tablist"]').getAttribute('aria-orientation') === 'vertical';
    const keys = vertical ? ['ArrowUp', 'ArrowDown'] : ['ArrowLeft', 'ArrowRight'];
    const index = tabs.indexOf(tab);
    let next;
    if (event.key === 'Home') next = tabs[0];
    else if (event.key === 'End') next = tabs.at(-1);
    else if (keys.includes(event.key)) next = tabs[(index + (event.key === keys[0] ? -1 : 1) + tabs.length) % tabs.length];
    if (next) {
      event.preventDefault();
      activate(next, true);
    }
  });
  document.addEventListener('input', event => {
    if (event.target.matches('[data-textarea-input]')) updateTextarea(event.target);
  });
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
  document.addEventListener('htmx:afterSwap', event => {
    init();
  });
  document.addEventListener('htmx:afterSettle', event => {
    // HTMX restores focus while settling. Open once that work is complete,
    // and only for a swap into the root, not requests by forms inside it.
    const dialog = event.target;
    if (dialog.matches('dialog[data-component="modal"]') && dialog.dataset.action?.includes('htmx:afterSettle->htmx-modal#show')) show(dialog, document.activeElement);
  });
  document.addEventListener('htmx:beforeRequest', event => {
    const form = event.detail.requestConfig?.elt ?? event.detail.elt;
    if (!form?.matches('form[data-action*="htmx-modal#closeOnSuccess"]')) return;
    const dialog = form.closest('dialog[data-component="modal"]');
    // An outerHTML response removes the form before completion is dispatched.
    if (dialog) modalRequests.set(event.detail.xhr, { dialog, opening: openings.get(dialog) });
  });
  document.addEventListener('htmx:afterRequest', event => {
    const { xhr, successful } = event.detail;
    const request = modalRequests.get(xhr);
    modalRequests.delete(xhr);
    if (!request) return;
    const { dialog, opening } = request;
    // HTMX also marks swapped 422 validation responses as successful.
    // A late save from a previous opening must not dismiss a reopened dialog.
    if (successful && xhr.status >= 200 && xhr.status < 300 && dialog.isConnected &&
        dialog.open && openings.get(dialog) === opening) dialog.close();
  });
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
  else init();
})();
