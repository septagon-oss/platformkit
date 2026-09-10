(function () {
  const form = document.querySelector('[data-gallery-controls]');
  if (!form) return;
  const status = document.querySelector('[data-gallery-status]');
  const dirty = new Set();
  form.addEventListener('input', event => {
    if (event.target.dataset.galleryProp) dirty.add(event.target);
  });
  form.addEventListener('change', event => {
    if (event.target.dataset.galleryProp) dirty.add(event.target);
  });
  form.addEventListener('submit', event => {
    event.preventDefault();
    try {
      const props = JSON.parse(form.elements.props.value || '{}');
      for (const input of dirty) {
        props[input.dataset.galleryProp] = input.dataset.galleryKind === 'string' ? input.value : JSON.parse(input.value);
      }
      const query = new URLSearchParams();
      for (const name of ['example', 'group', 'theme', 'width']) query.set(name, form.elements[name].value);
      query.set('props', JSON.stringify(props));
      window.location.assign(form.action + '?' + query);
    } catch {
      status.textContent = 'Enter valid JSON in the changed properties, then apply the preview.';
    }
  });
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
