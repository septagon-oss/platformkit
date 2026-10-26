// Enhance source-rendered contracts, including fragments inserted by HTMX.
(function () {
  const ready = new WeakSet();
  const told = new WeakSet();
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
    const maxChars = Number(input.dataset.maxChars || input.maxLength);
    if (counter) counter.textContent = String([...input.value].length) + (maxChars > 0 ? ` / ${maxChars}` : '');
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
  // A disabled section keeps its content readable but offers no action. The
  // renderers put no destination or enhancement hook inside one; the photo
  // enhancement rechecks that state all the same, as [data-detail-open] does.
  function disabledWithin(node) {
    return !!node.closest('[disabled], [aria-disabled="true"]');
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

  // Times are said in the reader's own time. The server writes UTC into the datetime
  // attribute and its own words inside the element; only the reader's machine knows the
  // zone to read them in. Intl is in every browser this repository serves and speaks the
  // page's own language for free, so no library is fetched, and a browser without it keeps
  // the server's sentence rather than a blank cell.
  const MINUTE = 60000, HOUR = 3600000, DAY = 86400000, WEEK = 7 * DAY;
  // lang is the language the document already declares — the one the locale path picked
  // for this request — so the same page in pt-PT is formatted in pt-PT. A lang no runtime
  // parses is answered as none, the reader's own browser locale: the one throw these formatters
  // have, answered here rather than under a catch that would hide our own defects along with it.
  function language() {
    const host = document.documentElement.closest('[lang]');
    const lang = (host && host.lang) || undefined;
    if (lang === undefined || typeof Intl === 'undefined' || typeof Intl.DateTimeFormat !== 'function') return lang;
    try { new Intl.DateTimeFormat(lang); } catch { return undefined; }
    return lang;
  }
  function said(at, style, lang) {
    if (typeof Intl === 'undefined' || typeof Intl.DateTimeFormat !== 'function') return null;
    return new Intl.DateTimeFormat(lang, style).format(at);
  }
  // relative is the largest unit that is at least one of itself, long style, because that
  // reads as a sentence ("5 minutes ago"), is correct in every language Intl speaks, and
  // needs no English-only table of short forms. Nothing on the page knows which field is a
  // deadline, so an instant that has not happened is never counted down.
  function relative(at, now, lang) {
    if (typeof Intl === 'undefined' || typeof Intl.RelativeTimeFormat !== 'function' || at.getTime() > now) return null;
    const say = new Intl.RelativeTimeFormat(lang, { numeric: 'auto', style: 'long' });
    if (now - at >= DAY) return say.format(-Math.round((now - at) / DAY), 'day');
    if (now - at >= HOUR) return say.format(-Math.round((now - at) / HOUR), 'hour');
    return say.format(-Math.round((now - at) / MINUTE), 'minute');
  }
  function policyTime(el, at, now, lang) {
    if (typeof Intl === 'undefined' || typeof Intl.DateTimeFormat !== 'function') return;
    const kind = el.dataset.timeKind;
    const zone = el.dataset.timeZone || undefined;
    const offset = el.dataset.timeOffset;
    const displayAt = offset === undefined ? at : new Date(at.getTime() + Number(offset) * 1000);
    const format = new Intl.DateTimeFormat('en-CA', {
      timeZone: offset === undefined ? zone : 'UTC', year: 'numeric', month: '2-digit', day: '2-digit',
      hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
    });
    const parts = Object.fromEntries(format.formatToParts(displayAt).map(p => [p.type, p.value]));
    const day = `${parts.year}-${parts.month}-${parts.day}`;
    const minute = `${day} ${parts.hour}:${parts.minute}`;
    el.title = `${minute} ${zone || format.resolvedOptions().timeZone}`;
    if (kind === 'date') { el.textContent = day; return; }
    if (el.dataset.timeRelative !== 'true') { el.textContent = minute; return; }
    if (typeof Intl.RelativeTimeFormat !== 'function') return;
    const delta = at.getTime() - now, distance = Math.abs(delta);
    const [unit, divisor] = distance < MINUTE ? ['second', 1000] :
      distance < HOUR ? ['minute', MINUTE] : distance < DAY ? ['hour', HOUR] : ['day', DAY];
    const count = Math.trunc(delta / divisor);
    el.dataset.timeUnit = unit;
    el.dataset.timeCount = String(count);
    el.textContent = count === 0 ? el.dataset.timeNow :
      new Intl.RelativeTimeFormat(el.dataset.timeLanguage || lang, { numeric: 'always', style: 'long' }).format(count, unit);
  }
  function tellTime(el, now) {
    const raw = el.getAttribute('datetime');
    if (!raw) return;
    const at = new Date(raw);
    if (isNaN(at.getTime())) return; // The server's words stand for a value that is no instant.
    const lang = language();
    if (el.dataset.timePolicy === '1') { policyTime(el, at, now, lang); return; }
    // The title is the exact moment read where the person is: date, time and zone name.
    const precise = said(at, { dateStyle: 'full', timeStyle: 'long' }, lang);
    if (precise !== null) el.title = precise;
    // Under a week says when it happened in the words a person uses; a week or more, and
    // anything still to come, is read as a date.
    const words = now - at < WEEK ? relative(at, now, lang) : null;
    const text = words || said(at, { dateStyle: 'medium', timeStyle: 'short' }, lang);
    if (text !== null) el.textContent = text;
  }

  // A live region re-reads itself when the server says its state changed. The sse
  // extension owns the connection; this owns the rate: at most one request a
  // second, and always one after the last notice. htmx's own `throttle` modifier
  // is not used because it drops the last event of a burst, and the last event is
  // the state the person came back to see.
  const liveWindow = 1000;
  const liveRefreshes = new WeakMap();
  function mountLive(region) {
    if (liveRefreshes.has(region)) return;
    liveRefreshes.set(region, null);
    region.addEventListener('htmx:sseBeforeMessage', event => {
      // A frame says "read again" and carries no markup: cancelling the default
      // is what keeps a notice's body out of the document.
      event.preventDefault();
      if (liveRefreshes.get(region) !== null) return;
      // A resync says this connection is not current, so the refresh is now.
      const overdue = event.detail && event.detail.type === 'resync';
      liveRefreshes.set(region, setTimeout(() => {
        liveRefreshes.set(region, null);
        htmx.trigger(region, 'refresh');
      }, overdue ? 0 : liveWindow));
    });
  }

  function init() {
    const now = Date.now();
    for (const el of document.querySelectorAll('time[datetime]')) {
      // One pass per element: a page left open keeps the now it was read at, rather than
      // every interval rewriting a sentence the person is in the middle of reading.
      if (told.has(el)) continue;
      told.add(el);
      // No catch on purpose: `language` answers an unparseable lang as none, and nothing else
      // here throws. A guard is what let a broken formatter read as a browser without Intl.
      tellTime(el, now);
    }
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
    for (const region of document.querySelectorAll('[data-controller="live"]')) mountLive(region);
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
    if (photo && !disabledWithin(photo) && !event.ctrlKey && !event.metaKey && !event.shiftKey && !event.altKey && event.button === 0) {
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
    if (viewer && !disabledWithin(viewer) && !event.target.closest('input, textarea, select, [contenteditable]')) {
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

// Choice drafts are node-local; successful native inputs remain the submitted form.
(function () {
  const instances = new WeakMap();
  const foldExceptions = {
    "\u00b5":"\u03bc", "\u00df":"ss", "\u0149":"\u02bcn", "\u017f":"s", "\u01f0":"j\u030c", "\u0345":"\u03b9", "\u0390":"\u03b9\u0308\u0301", "\u03b0":"\u03c5\u0308\u0301",
    "\u03c2":"\u03c3", "\u03d0":"\u03b2", "\u03d1":"\u03b8", "\u03d5":"\u03c6", "\u03d6":"\u03c0", "\u03f0":"\u03ba", "\u03f1":"\u03c1", "\u03f5":"\u03b5",
    "\u0587":"\u0565\u0582", "\uab70":"\u13a0", "\uab71":"\u13a1", "\uab72":"\u13a2", "\uab73":"\u13a3", "\uab74":"\u13a4", "\uab75":"\u13a5", "\uab76":"\u13a6",
    "\uab77":"\u13a7", "\uab78":"\u13a8", "\uab79":"\u13a9", "\uab7a":"\u13aa", "\uab7b":"\u13ab", "\uab7c":"\u13ac", "\uab7d":"\u13ad", "\uab7e":"\u13ae",
    "\uab7f":"\u13af", "\uab80":"\u13b0", "\uab81":"\u13b1", "\uab82":"\u13b2", "\uab83":"\u13b3", "\uab84":"\u13b4", "\uab85":"\u13b5", "\uab86":"\u13b6",
    "\uab87":"\u13b7", "\uab88":"\u13b8", "\uab89":"\u13b9", "\uab8a":"\u13ba", "\uab8b":"\u13bb", "\uab8c":"\u13bc", "\uab8d":"\u13bd", "\uab8e":"\u13be",
    "\uab8f":"\u13bf", "\uab90":"\u13c0", "\uab91":"\u13c1", "\uab92":"\u13c2", "\uab93":"\u13c3", "\uab94":"\u13c4", "\uab95":"\u13c5", "\uab96":"\u13c6",
    "\uab97":"\u13c7", "\uab98":"\u13c8", "\uab99":"\u13c9", "\uab9a":"\u13ca", "\uab9b":"\u13cb", "\uab9c":"\u13cc", "\uab9d":"\u13cd", "\uab9e":"\u13ce",
    "\uab9f":"\u13cf", "\uaba0":"\u13d0", "\uaba1":"\u13d1", "\uaba2":"\u13d2", "\uaba3":"\u13d3", "\uaba4":"\u13d4", "\uaba5":"\u13d5", "\uaba6":"\u13d6",
    "\uaba7":"\u13d7", "\uaba8":"\u13d8", "\uaba9":"\u13d9", "\uabaa":"\u13da", "\uabab":"\u13db", "\uabac":"\u13dc", "\uabad":"\u13dd", "\uabae":"\u13de",
    "\uabaf":"\u13df", "\uabb0":"\u13e0", "\uabb1":"\u13e1", "\uabb2":"\u13e2", "\uabb3":"\u13e3", "\uabb4":"\u13e4", "\uabb5":"\u13e5", "\uabb6":"\u13e6",
    "\uabb7":"\u13e7", "\uabb8":"\u13e8", "\uabb9":"\u13e9", "\uabba":"\u13ea", "\uabbb":"\u13eb", "\uabbc":"\u13ec", "\uabbd":"\u13ed", "\uabbe":"\u13ee",
    "\uabbf":"\u13ef", "\u13f8":"\u13f0", "\u13f9":"\u13f1", "\u13fa":"\u13f2", "\u13fb":"\u13f3", "\u13fc":"\u13f4", "\u13fd":"\u13f5", "\u1c80":"\u0432",
    "\u1c81":"\u0434", "\u1c82":"\u043e", "\u1c83":"\u0441", "\u1c84":"\u0442", "\u1c85":"\u0442", "\u1c86":"\u044a", "\u1c87":"\u0463", "\u1c88":"\ua64b",
    "\u1e96":"h\u0331", "\u1e97":"t\u0308", "\u1e98":"w\u030a", "\u1e99":"y\u030a", "\u1e9a":"a\u02be", "\u1e9b":"\u1e61", "\u1f50":"\u03c5\u0313", "\u1f52":"\u03c5\u0313\u0300",
    "\u1f54":"\u03c5\u0313\u0301", "\u1f56":"\u03c5\u0313\u0342", "\u1f80":"\u1f00\u03b9", "\u1f81":"\u1f01\u03b9", "\u1f82":"\u1f02\u03b9", "\u1f83":"\u1f03\u03b9", "\u1f84":"\u1f04\u03b9", "\u1f85":"\u1f05\u03b9",
    "\u1f86":"\u1f06\u03b9", "\u1f87":"\u1f07\u03b9", "\u1f90":"\u1f20\u03b9", "\u1f91":"\u1f21\u03b9", "\u1f92":"\u1f22\u03b9", "\u1f93":"\u1f23\u03b9", "\u1f94":"\u1f24\u03b9", "\u1f95":"\u1f25\u03b9",
    "\u1f96":"\u1f26\u03b9", "\u1f97":"\u1f27\u03b9", "\u1fa0":"\u1f60\u03b9", "\u1fa1":"\u1f61\u03b9", "\u1fa2":"\u1f62\u03b9", "\u1fa3":"\u1f63\u03b9", "\u1fa4":"\u1f64\u03b9", "\u1fa5":"\u1f65\u03b9",
    "\u1fa6":"\u1f66\u03b9", "\u1fa7":"\u1f67\u03b9", "\u1fb2":"\u1f70\u03b9", "\u1fb3":"\u03b1\u03b9", "\u1fb4":"\u03ac\u03b9", "\u1fb6":"\u03b1\u0342", "\u1fb7":"\u03b1\u0342\u03b9", "\u1fbe":"\u03b9",
    "\u1fc2":"\u1f74\u03b9", "\u1fc3":"\u03b7\u03b9", "\u1fc4":"\u03ae\u03b9", "\u1fc6":"\u03b7\u0342", "\u1fc7":"\u03b7\u0342\u03b9", "\u1fd2":"\u03b9\u0308\u0300", "\u1fd3":"\u03b9\u0308\u0301", "\u1fd6":"\u03b9\u0342",
    "\u1fd7":"\u03b9\u0308\u0342", "\u1fe2":"\u03c5\u0308\u0300", "\u1fe3":"\u03c5\u0308\u0301", "\u1fe4":"\u03c1\u0313", "\u1fe6":"\u03c5\u0342", "\u1fe7":"\u03c5\u0308\u0342", "\u1ff2":"\u1f7c\u03b9", "\u1ff3":"\u03c9\u03b9",
    "\u1ff4":"\u03ce\u03b9", "\u1ff6":"\u03c9\u0342", "\u1ff7":"\u03c9\u0342\u03b9", "\ufb00":"ff", "\ufb01":"fi", "\ufb02":"fl", "\ufb03":"ffi", "\ufb04":"ffl",
    "\ufb05":"st", "\ufb06":"st", "\ufb13":"\u0574\u0576", "\ufb14":"\u0574\u0565", "\ufb15":"\u0574\u056b", "\ufb16":"\u057e\u0576", "\ufb17":"\u0574\u056d",
  };
  function choiceFold(text) {
    return [...text.toLowerCase()].map(char => foldExceptions[char] || char).join('');
  }
  function visible(state) {
    return state.inputs.filter(input => !input.disabled && !input.closest('[hidden]'));
  }
  function focusChoice(state, input, move) {
    for (const item of state.inputs) item.tabIndex = item === input ? 0 : -1;
    if (move) input?.focus();
  }
  function choiceNext(state, id) {
    const c = state.config;
    let next = state.ids.filter(value => value !== id);
    if (id !== '' && !state.ids.includes(id)) {
      next = c.cardinality === 'one' || id === c.noPreference || state.ids.includes(c.noPreference) ? [id] : [...state.ids, id];
    }
    if (id === '') next = [];
    if (state.ids.length && !next.length && c.empty.mode === 'refuse') return { error: c.empty.message };
    if (c.maximum && next.length > c.maximum.count) {
      if (c.maximum.behaviour === 'refuse') return { error: c.maximum.message };
      next = next.slice(-c.maximum.count);
    }
    return { next };
  }
  function repaintChoice(state) {
    const { root, ids } = state;
    for (const input of state.inputs) input.checked = input.value === '' ? !ids.length : ids.includes(input.value);
    if (state.order) state.order.value = JSON.stringify(ids);
    root.querySelectorAll('[data-choice-figure] [data-choice]').forEach(region => {
      if (ids.includes(region.dataset.choice)) region.setAttribute('data-selected', 'true');
      else region.removeAttribute('data-selected');
    });
    const count = root.querySelector('[data-choice-count]');
    count.textContent = `${ids.length} ${state.words.selected}`;
    const labels = ids.map(id => state.labels.get(id));
    root.querySelector('[data-choice-names]').textContent = labels.join(', ');
    const stack = root.querySelector('[data-choice-stack]');
    stack.replaceChildren();
    for (const id of ids) {
      const picture = state.pictures.get(id);
      if (picture && stack.childElementCount < 4) stack.append(picture.cloneNode(true));
    }
  }
  function toggleChoice(state, input) {
    if (!state.root.isConnected || input.disabled) return;
    const result = choiceNext(state, input.value);
    const status = state.root.querySelector('[data-choice-status]');
    if (result.error) {
      status.textContent = result.error;
      return;
    }
    const removed = state.ids.filter(id => !result.next.includes(id));
    state.ids = result.next;
    repaintChoice(state);
    status.textContent = state.ids.map(id => state.labels.get(id)).join(', ') || `0 ${state.words.selected}`;
    if (removed.length && !removed.includes(input.value)) status.textContent += ` · ${state.words.removed}: ${removed.map(id => state.labels.get(id)).join(', ')}`;
  }
  function filterChoices(state) {
    const query = choiceFold(state.search.value.trim());
    let matches = 0;
    for (const input of state.inputs) {
      const item = input.closest('[data-choice-item]');
      const group = item.querySelector('[data-choice-group]')?.dataset.choiceGroup || '';
      const hit = input.value === state.config.noPreference || input.value === '' || choiceFold(`${state.labels.get(input.value)} ${group}`).includes(query);
      item.hidden = !hit;
      if (hit && input.value && input.value !== state.config.noPreference) matches++;
    }
    state.root.querySelector('[data-choice-status]').textContent = matches ? `${matches} ${state.words.matches}` : state.words.noResults;
    const options = visible(state);
    const active = options.find(input => input.tabIndex === 0) || options[0];
    focusChoice(state, active, false);
  }
  function bindChoices(state) {
    const { root } = state;
    root.addEventListener('click', event => {
      const input = event.target.closest('[data-choice-input]');
      const region = event.target.closest('[data-choice-figure] [data-choice]');
      if (input) {
        toggleChoice(state, input);
        repaintChoice(state);
      } else if (region) {
        const target = state.inputs.find(option => option.value === region.dataset.choice);
        if (target) toggleChoice(state, target);
      }
    });
    root.addEventListener('keydown', event => {
      const input = event.target.closest('[data-choice-input]');
      if (!input) return;
      const items = visible(state), at = items.indexOf(input);
      let next = at;
      if (event.key === 'ArrowRight' || event.key === 'ArrowDown') next = (at + 1) % items.length;
      else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') next = (at + items.length - 1) % items.length;
      else if (event.key === 'Home') next = 0;
      else if (event.key === 'End') next = items.length - 1;
      else if (event.key === ' ') { event.preventDefault(); toggleChoice(state, input); return; }
      else return;
      event.preventDefault();
      focusChoice(state, items[next], true);
    });
    root.addEventListener('focusin', event => {
      if (state.inputs.includes(event.target)) focusChoice(state, event.target, false);
    });
    if (state.search) {
      state.search.addEventListener('input', () => filterChoices(state));
      root.querySelector('[data-choice-search-label]').hidden = false;
    }
    root.addEventListener('error', event => {
      if (event.target.matches('img')) event.target.hidden = true;
    }, true);
  }
  function mountChoices() {
    document.querySelectorAll('[data-choice-surface]').forEach(root => {
      if (instances.has(root)) return;
      const inputs = [...root.querySelectorAll('[data-choice-input]')];
      const config = JSON.parse(root.dataset.choiceConfig);
      const ids = JSON.parse(root.dataset.choiceInitial);
      const labels = new Map(inputs.map(input => [input.value, input.closest('[data-choice-item]').querySelector('[data-choice-label]').textContent]));
      if (ids.some(id => !labels.has(id)) || new Set(ids).size !== ids.length || (config.maximum && ids.length > config.maximum.count) || (config.cardinality === 'one' && ids.length > 1) || (ids.length > 1 && ids.includes(config.noPreference))) return;
      // Retired IDs are rendered for native correction, without draft enhancement.
      if (root.dataset.choiceStale === 'true') return;
      const pictures = new Map([...root.querySelectorAll('[data-choice-picture]')].map(template => [template.dataset.choicePicture, template.content.firstElementChild]));
      const state = { root, config, inputs, ids, initial: [...ids], labels, pictures,
        order: root.querySelector('[data-choice-order]'), search: root.querySelector('[data-choice-search]'), words: JSON.parse(root.dataset.choiceWords) };
      instances.set(root, state);
      bindChoices(state);
      focusChoice(state, inputs.find(input => input.checked && !input.disabled) || visible(state)[0], false);
      repaintChoice(state);
    });
  }
  document.addEventListener('reset', event => queueMicrotask(() => {
    if (event.defaultPrevented || !event.target.isConnected) return;
    event.target.querySelectorAll('[data-choice-surface]').forEach(root => {
      const state = instances.get(root);
      if (!state) return;
      state.ids = [...state.initial];
      if (state.search) { state.search.value = ''; filterChoices(state); }
      repaintChoice(state);
      root.querySelector('[data-choice-status]').textContent = '';
      root.querySelector(`#${CSS.escape(root.id)}-error`).textContent = '';
    });
  }));
  document.addEventListener('htmx:afterSwap', mountChoices);
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mountChoices);
  else mountChoices();
})();
