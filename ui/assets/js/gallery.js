(function () {
  const form = document.querySelector('[data-gallery-controls]');
  if (!form) return;
  const status = document.querySelector('[data-gallery-status]');
  const preview = document.querySelector('[data-gallery-viewport] iframe');
  const dirty = new Set();
  let timer;
  let pending;
  let revision = 0;

  function schedule(delay) {
    clearTimeout(timer);
    pending?.abort();
    revision++;
    timer = setTimeout(update, delay);
  }
  form.addEventListener('input', event => {
    if (event.isComposing) return;
    if (event.target.dataset.galleryProp) {
      dirty.add(event.target);
      event.target.removeAttribute('aria-invalid');
      event.target.removeAttribute('aria-describedby');
    }
    schedule(300);
  });
  form.addEventListener('change', event => {
    if (event.target.dataset.galleryProp) dirty.add(event.target);
    schedule(0);
  });
  form.addEventListener('compositionend', event => {
    if (event.target.dataset.galleryProp) dirty.add(event.target);
    schedule(300);
  });
  form.addEventListener('submit', event => {
    event.preventDefault();
    schedule(0);
  });

  async function update() {
    const current = revision;
    const request = new AbortController();
    pending = request;
    try {
      const props = JSON.parse(form.elements.props.value || '{}');
      for (const input of dirty) {
        try {
          props[input.dataset.galleryProp] = input.dataset.galleryKind === 'string' ? input.value : JSON.parse(input.value);
          input.removeAttribute('aria-invalid');
          input.removeAttribute('aria-describedby');
        } catch {
          input.setAttribute('aria-invalid', 'true');
          input.setAttribute('aria-describedby', status.id);
          throw new Error(`Enter valid JSON for ${input.dataset.galleryProp}. The last valid preview is still shown.`);
        }
      }
      const query = new URLSearchParams();
      for (const name of ['example', 'group', 'theme', 'width']) query.set(name, form.elements[name].value);
      query.set('props', JSON.stringify(props));
      const url = form.action + '?' + query;
      status.textContent = 'Updating preview…';
      // Reuse the authorized page render for validation and the Go properties.
      // Keep the form in place so typing, focus and scroll survive the update.
      const response = await fetch(url, { signal: request.signal, cache: 'no-store', headers: { Accept: 'text/html' } });
      if (!response.ok) throw new Error(`Preview could not be updated (${response.status}). Check the changed properties or try Apply preview again.`);
      const result = new DOMParser().parseFromString(await response.text(), 'text/html');
      if (request.signal.aborted || current !== revision) return;
      const rendered = result.querySelector('[data-gallery-viewport] iframe');
      if (!rendered) throw new Error('Preview is unavailable. Sign in again, then try Apply preview. Your edits are still here.');
      const source = rendered.getAttribute('src');
      preview.dataset.galleryWidth = form.elements.width.value;
      if (preview.getAttribute('src') !== source) preview.src = source;
      document.querySelector('[data-gallery-preview-link]').href = source;
      const code = document.querySelector('[name="go-props"]');
      if (code) code.value = result.querySelector('[name="go-props"]')?.value || '';
      form.elements.props.value = query.get('props');
      history.replaceState(history.state, '', url);
      status.textContent = 'Preview updated.';
    } catch (error) {
      if (!request.signal.aborted && current === revision) status.textContent = error.message;
    }
  }
  document.querySelector('[data-gallery-copy]')?.addEventListener('click', async () => {
    const input = document.querySelector('[name="go-props"]');
    try {
      // Clipboard API needs HTTPS; selection/copy also works on an HTTP tailnet IP.
      if (navigator.clipboard && window.isSecureContext) await navigator.clipboard.writeText(input.value);
      else {
        input.focus();
        input.select();
        if (!document.execCommand('copy')) throw new Error('Select to copy');
      }
      status.textContent = 'Go properties copied.';
    } catch {
      input.focus();
      input.select();
      status.textContent = 'Properties selected. Press Control+C or Command+C to copy.';
    }
  });
})();
