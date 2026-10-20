import { expect, type Locator, type Page, type Response } from '@playwright/test';

// The version series beside a record, walked the way a person walks it.
//
// The panel is the record's own past: a native <details> element whose summary is
// the only word it needs, and whose restore control is a form like the one the
// delete control is. Everything here reaches it through that structure — the
// disclosure, the form that posts to the record's `/restore`, the hidden version
// number inside it — because the copy on the panel comes from the shell's
// catalogue and belongs to whoever shipped the language, not to this file. The one
// piece of copy matched here is the summary, and it is matched in both the
// languages this fixture bootstraps.

const panelSummary = /^(History|Histórico)$/;

/** openHistoryPanel opens the record's version series and returns it. */
export async function openHistoryPanel(page: Page): Promise<Locator> {
  const summary = page.locator('summary').filter({ hasText: panelSummary }).first();
  await expect(summary, 'the record page offers a version series').toBeVisible();
  const panel = page.locator('details').filter({ has: summary });
  if (!(await panel.evaluate((el: HTMLDetailsElement) => el.open))) await summary.click();
  await expect(panel).toHaveJSProperty('open', true);
  return panel;
}

/**
 * restoreForm is the form the panel offers for one version: the record's own
 * address plus `restore`, carrying the version it asks for and the write count the
 * page was drawn from.
 */
export function restoreForm(page: Page, version: number): Locator {
  return page.locator(
    `form[action$="/restore"]:has(input[name="version"][value="${version}"])`,
  );
}

/**
 * restoreVersion presses the version's own button, answers yes to the question the
 * page asks first, and returns the answer the record gave. A panel that asks is the
 * point: the write that comes back is a new version, not an overwrite. The answer
 * arrives after the dialog closes, so the wait starts before the press.
 */
export async function restoreVersion(page: Page, version: number): Promise<Response> {
  const form = restoreForm(page, version);
  await expect(form, `the panel offers version ${version}`).toHaveCount(1);
  const dialog = page.locator('#pk-confirm');
  const answered = page.waitForResponse(
    r => new URL(r.url()).pathname.endsWith('/restore'), { timeout: 15_000 });
  await form.getByRole('button', { type: 'submit' }).click();
  await expect(dialog, 'restoring asks before it writes').toBeVisible();
  await dialog.getByRole('button').last().click();
  return answered;
}

/**
 * storedVersions reads the series from the record's own address — the same read the
 * panel is drawn from, and the only answer that says what each version holds.
 */
export async function storedVersions(page: Page, id: string): Promise<{
  version: number; reason: string; changed: string[]; fields: { body?: string }; actor?: string;
}[]> {
  const read = await page.request.get(`/api/v1/content/contents/${id}/versions`);
  expect(read.status(), await read.text()).toBe(200);
  const body = await read.json();
  return body.items ?? body;
}
