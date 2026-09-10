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
