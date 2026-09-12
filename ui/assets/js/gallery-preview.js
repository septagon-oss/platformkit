// A closed confirmation needs a trigger to demonstrate it. Keep this scaffolding
// in the isolated preview; the exported Go component and its slots stay exact.
document.querySelectorAll('[data-confirm-message]').forEach(message => {
  const dialog = message.closest('dialog');
  if (!dialog) return;
  const trigger = document.createElement('button');
  trigger.type = 'button';
  trigger.textContent = 'Open confirmation';
  trigger.className = dialog.querySelector('[data-confirm-cancel]').className;
  trigger.dataset.confirm = 'Confirm this sample action? No data will be changed.';
  dialog.before(trigger);
});

document.querySelectorAll('[data-gallery-skiplink]').forEach(preview => {
  const link = preview.querySelector('a[href^="#"]');
  const target = preview.querySelector('[data-gallery-skip-target]');
  if (!link || !target) return;
  target.id = link.getAttribute('href').slice(1);
  preview.querySelector('nav a').setAttribute('href', link.getAttribute('href'));
  preview.querySelector('[data-gallery-focus-skip]').addEventListener('click', () => link.focus());
});

const sidebarHint = document.querySelector('[data-gallery-sidebar-hint]');
if (sidebarHint) {
  const sidebar = document.querySelector('[data-component="sidebar"]');
  const update = () => { sidebarHint.hidden = !sidebar || getComputedStyle(sidebar).display !== 'none'; };
  window.addEventListener('resize', update);
  update();
}
