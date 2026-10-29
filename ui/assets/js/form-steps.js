// The server owns navigation and retained answers. This module only updates
// declared button copy and focuses the page's error or current heading.
for (const form of document.querySelectorAll('form[data-form-steps]')) {
  const button = form.querySelector('[data-answer-field]');
  const field = button && form.elements.namedItem(button.dataset.answerField);
  if (field) {
    const labels = JSON.parse(button.dataset.answerLabels);
    const update = () => {
      let answer = field.type === 'checkbox' ? String(field.checked) : field.value.trim();
      if (field.type !== 'checkbox' && field.validity && !field.validity.valid) answer = '';
      if (answer && Object.keys(labels).length) {
        const parts = answer.split(',').map(value => Object.hasOwn(labels, value.trim()) ? labels[value.trim()] : undefined);
        answer = parts.every(value => value !== undefined) ? parts.join(', ') : '';
      }
      button.textContent = answer
        ? button.dataset.answerTemplate.replace('{answer}', () => answer)
        : button.dataset.answerEmpty;
    };
    field.addEventListener('input', update);
    field.addEventListener('change', update);
  }
  const target = form.querySelector('[aria-invalid="true"]') || form.querySelector('[data-step-errors]') || form.querySelector('[data-step-heading]');
  target?.focus();
}
