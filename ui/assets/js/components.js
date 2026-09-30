// Enhance source-rendered contracts, including fragments inserted by HTMX.
(function () {
  const ready = new WeakSet();
  const triggers = new WeakMap();
  const openings = new WeakMap();
  const modalRequests = new WeakMap();
  const detailRequests = new WeakMap();
  const latestDetails = new WeakMap();
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

  function restoreDetailFocus(panel) {
    const trigger = triggers.get(panel);
    const fallback = document.getElementById(panel.dataset.detailReturnFocus);
    const target = trigger?.isConnected ? trigger : fallback;
    if (target && !target.closest('[hidden], [inert]') && target.getClientRects().length) target.focus();
  }
  function openDetail(panel, trigger) {
    if (panel.matches('dialog')) show(panel, trigger);
    else {
      triggers.set(panel, trigger);
      panel.hidden = false;
      if (!openings.has(panel)) openings.set(panel, {});
      panel.querySelector('h2[tabindex]')?.focus();
    }
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

  const selectionResults = new Map();
  function selectionRoot(node) {
    return node.closest('[data-component="data-list"]') || node.closest('[data-component="table"]');
  }
  function selectableRows(root) {
    return [...root.querySelectorAll('[data-pk-select-row]')].filter(input => selectionRoot(input) === root && !input.disabled);
  }
  function updateSelection(root) {
    const rows = selectableRows(root);
    const count = rows.filter(input => input.checked).length;
    for (const all of root.querySelectorAll('[data-pk-select="all"]:not([data-pk-select-row])')) {
      if (selectionRoot(all) !== root) continue;
      all.disabled = rows.length === 0;
      all.checked = rows.length > 0 && count === rows.length;
      all.indeterminate = count > 0 && count < rows.length;
      all.closest('[data-component="selection-control"]').hidden = false;
    }
    const output = root.querySelector(':scope > [data-selection-count]');
    const label = root.querySelector(':scope > [data-selection-labels]')?.children[count];
    if (output && label && output.textContent !== label.textContent) output.textContent = label.textContent;
    const clear = root.querySelector(':scope > [data-selection-clear]');
    if (clear) clear.hidden = false;
  }
  function initSelections() {
    const present = new Set();
    for (const root of document.querySelectorAll('[data-component="data-list"], [data-component="table"]')) {
      if (root.matches('[data-component="table"]') && root.closest('[data-component="data-list"]')) continue;
      if (root.id) present.add(root.id);
      if (!root.querySelector('[data-pk-select]')) continue;
      if (root.id) {
        const key = root.dataset.resultKey || '';
        if (selectionResults.has(root.id) && selectionResults.get(root.id) !== key) {
          for (const input of root.querySelectorAll('[data-pk-select-row]')) {
            if (selectionRoot(input) === root) input.checked = input.defaultChecked = false;
          }
        }
        selectionResults.set(root.id, key);
      }
      updateSelection(root);
    }
    for (const id of selectionResults.keys()) if (!present.has(id)) selectionResults.delete(id);
  }

  function quantityValue(root) {
    const input = root.querySelector('[data-quantity-value]');
    if (!input || !/^\d+$/.test(input.value)) return null;
    const value = BigInt(input.value), min = BigInt(input.dataset.min), max = BigInt(input.dataset.max), step = BigInt(input.dataset.step);
    return value >= min && value <= max && (value - min) % step === 0n ? { input, value, min, max, step } : null;
  }
  function updateQuantity(root) {
    const current = quantityValue(root);
    for (const wrapper of root.querySelectorAll('[data-quantity-enhancement]')) wrapper.hidden = false;
    for (const button of root.querySelectorAll('[data-quantity-direction]')) {
      const next = current && current.value + BigInt(button.dataset.quantityDirection) * current.step;
      button.disabled = !current || current.input.disabled || next < current.min || next > current.max;
    }
    const input = root.querySelector('[data-quantity-value]');
    if (input) { input.setAttribute('aria-invalid', String(!current)); input.setCustomValidity(current ? '' : input.dataset.quantityInvalid); }
  }
  function showPhoto(gallery, index) {
    const template = gallery.querySelector(`template[data-photo-template="${index}"]`);
    const viewer = gallery.querySelector('[data-photo-viewer]');
    if (!template || !viewer) return false;
    viewer.querySelector('[data-photo-content]').replaceChildren(template.content.cloneNode(true));
    viewer.querySelector('[data-photo-position]').textContent = template.dataset.photoPosition;
    viewer.dataset.photoSelected = String(index);
    const indices = [...gallery.querySelectorAll('[data-photo-template]')].map(el => Number(el.dataset.photoTemplate));
    viewer.querySelector('[data-photo-direction="-1"]').disabled = index === indices[0];
    viewer.querySelector('[data-photo-direction="1"]').disabled = index === indices.at(-1);
    return true;
  }
  function movePhoto(viewer, direction) {
    const gallery = document.getElementById(viewer.dataset.photoViewer);
    const indices = [...gallery.querySelectorAll('[data-photo-template]')].map(el => Number(el.dataset.photoTemplate));
    const index = indices.indexOf(Number(viewer.dataset.photoSelected));
    const next = direction === 'first' ? indices[0] : direction === 'last' ? indices.at(-1) : indices[index + Number(direction)];
    if (next !== undefined) showPhoto(gallery, next);
  }

  function init() {
    for (const close of document.querySelectorAll('[data-alert-close]')) close.hidden = false;
    initSelections();
    for (const root of document.querySelectorAll('[data-component="quantity-input"]')) updateQuantity(root);
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
        restoreDetailFocus(dialog);
      });
      if (dialog.dataset.htmxModalOpenValue === 'true') {
        let trigger;
        if (dialog.dataset.photoViewer) {
          const gallery = document.getElementById(dialog.dataset.photoViewer);
          showPhoto(gallery, Number(dialog.dataset.photoSelected));
          trigger = gallery.querySelector(`[data-photo-index="${dialog.dataset.photoSelected}"]`);
        }
        show(dialog, trigger);
      }
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
    const quantity = event.target.closest('[data-quantity-direction]');
    if (quantity && !quantity.disabled) {
      const root = quantity.closest('[data-component="quantity-input"]');
      const current = quantityValue(root);
      if (current) {
        const next = current.value + BigInt(quantity.dataset.quantityDirection) * current.step;
        if (next >= current.min && next <= current.max) {
          current.input.value = String(next);
          current.input.dispatchEvent(new Event('input', { bubbles: true }));
          current.input.dispatchEvent(new Event('change', { bubbles: true }));
        }
      }
    }
    const photo = event.target.closest('[data-photo-open]');
    if (photo && !event.ctrlKey && !event.metaKey && !event.shiftKey && !event.altKey && event.button === 0) {
      const gallery = document.getElementById(photo.dataset.photoOpen);
      if (showPhoto(gallery, Number(photo.dataset.photoIndex))) {
        event.preventDefault();
        show(gallery.querySelector('[data-photo-viewer]'), photo);
      }
    }
    const photoMove = event.target.closest('[data-photo-direction]');
    if (photoMove && !photoMove.disabled) movePhoto(photoMove.closest('[data-photo-viewer]'), photoMove.dataset.photoDirection);
    const photoClose = event.target.closest('[data-photo-close]');
    if (photoClose) { event.preventDefault(); photoClose.closest('dialog').close(); }
    const detailOpener = event.target.closest('[data-detail-open]');
    if (detailOpener && !event.ctrlKey && !event.metaKey && !event.shiftKey && !event.altKey && event.button === 0 && detailOpener.getAttribute('aria-disabled') !== 'true' && !detailOpener.disabled) {
      const panel = document.getElementById(detailOpener.dataset.detailOpen);
      if (panel?.matches('[data-detail-panel]')) {
        event.preventDefault();
        openDetail(panel, detailOpener);
      }
    }
    const detailClose = event.target.closest('[data-detail-close]');
    const detail = detailClose?.closest('[data-detail-panel]');
    if (detail) {
      event.preventDefault();
      latestDetails.delete(detail);
      openings.delete(detail);
      if (detail.matches('dialog')) detail.close();
      else { detail.hidden = true; restoreDetailFocus(detail); }
    }
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
    if (action?.dataset.action.includes('click->alert#dismiss') && !action.disabled) {
      const notice = action.closest('[data-controller="alert"]');
      if (notice) {
        const hadFocus = notice.contains(document.activeElement);
        const stops = [...document.querySelectorAll('button, input, select, textarea, a[href], [tabindex]')]
          .filter(el => !notice.contains(el) && !el.disabled && el.tabIndex >= 0 &&
            !el.closest('[inert], [hidden]') && el.getClientRects().length);
        const associated = document.getElementById(notice.dataset.alertReturnFocus);
        const next = stops.find(el => notice.compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING);
        const target = stops.includes(associated) ? associated : next || stops.at(-1);
        notice.remove();
        if (hadFocus) target?.focus();
      }
    }
    const tab = event.target.closest('[data-tabs-tab]');
    if (tab) activate(tab, false);
    const clearSelection = event.target.closest('[data-clear-selection]');
    if (clearSelection && !clearSelection.disabled) {
      const root = selectionRoot(clearSelection);
      for (const input of selectableRows(root)) input.checked = false;
      updateSelection(root);
    }
  });
  document.addEventListener('keydown', event => {
    const viewer = event.target.closest('[data-photo-viewer]');
    if (viewer && !event.target.closest('input, textarea, select, [contenteditable]')) {
      const direction = { ArrowLeft: '-1', ArrowRight: '1', Home: 'first', End: 'last' }[event.key];
      if (direction) { event.preventDefault(); movePhoto(viewer, direction); }
    }
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
    const quantity = event.target.closest('[data-component=quantity-input]');
    if (quantity) updateQuantity(quantity);
  });
  document.addEventListener('change', event => {
    if (event.target.matches('[data-pk-select]')) {
      const root = selectionRoot(event.target);
      if (!event.target.hasAttribute('data-pk-select-row')) {
        for (const input of selectableRows(root)) input.checked = event.target.checked;
      }
      updateSelection(root);
    }
    const root = event.target.closest('[data-controller="checkbox"]');
    if (root) updateCheckbox(root);
  });
  document.addEventListener('reset', event => {
    queueMicrotask(() => {
      if (event.defaultPrevented) return;
      for (const input of document.querySelectorAll('[data-quantity-value]')) if (input.form === event.target) updateQuantity(input.closest('[data-component="quantity-input"]'));
      const roots = new Set([...document.querySelectorAll('[data-pk-select-row]')]
        .filter(input => input.form === event.target).map(selectionRoot));
      for (const root of roots) updateSelection(root);
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
    const target = event.detail.target?.closest('[data-detail-panel]');
    if (target && event.detail.requestConfig?.verb?.toLowerCase() === 'get') {
      if (form?.hasAttribute('data-detail-open')) openDetail(target, form);
      const request = { panel: target, opening: openings.get(target), trigger: triggers.get(target) };
      latestDetails.set(target, request);
      detailRequests.set(event.detail.xhr, request);
    }
    if (!form?.matches('form[data-action*="htmx-modal#closeOnSuccess"]')) return;
    const dialog = form.closest('dialog[data-component="modal"]');
    // An outerHTML response removes the form before completion is dispatched.
    if (dialog) modalRequests.set(event.detail.xhr, { dialog, opening: openings.get(dialog) });
  });
  document.addEventListener('htmx:beforeSwap', event => {
    const request = detailRequests.get(event.detail?.xhr);
    if (!request) return;
    const { panel, opening } = request;
    const visible = panel.matches('dialog') ? panel.open : !panel.hidden;
    if (!panel.isConnected || !visible || latestDetails.get(panel) !== request || openings.get(panel) !== opening) {
      event.detail.shouldSwap = false;
      event.preventDefault();
      return;
    }
    // Apply a detail selection atomically with this validation. HTMX's delayed
    // swap runs after afterRequest, when a close or another selection may occur.
    // Other swap options remain caller-owned; detail swaps never queue animation.
    const source = event.detail.requestConfig?.elt;
    const swap = event.detail.swapOverride || source?.closest('[hx-swap]')?.getAttribute('hx-swap') || htmx.config.defaultSwapStyle;
    event.detail.swapOverride = swap + ' swap:0ms transition:false';
  });
  document.addEventListener('htmx:afterSwap', event => {
    const request = detailRequests.get(event.detail?.xhr);
    if (!request) return;
    const panel = document.getElementById(request.panel.id);
    if (!panel?.matches('[data-detail-panel]')) return;
    // outerHTML may replace the root; transfer this opening, not the old DOM.
    if (panel !== request.panel) {
      triggers.set(panel, request.trigger);
      openings.set(panel, request.opening);
      openDetail(panel, request.trigger);
    } else if (!panel.matches('dialog')) panel.querySelector('h2[tabindex]')?.focus();
  });
  document.addEventListener('htmx:afterRequest', event => {
    const { xhr, successful } = event.detail;
    detailRequests.delete(xhr);
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
