import { expect, test } from '@playwright/test';
import { openHome, workspaceEntries } from './steps/public';

// Decision 0020: the anonymous public frame enters the workspace at its root,
// the address apps/platformkit/modules.go pins, and offers nothing deeper into
// it. A page with no entry at all is the same failure, so assert one exists.
test('the anonymous public frame links only the workspace root', async ({ page }) => {
  await openHome(page);
  const entries = await workspaceEntries(page);
  expect(entries.length).toBeGreaterThan(0);
  expect(entries).toEqual(['/app']);
});
