import { expect, test } from '@playwright/test';
import { mailbox, specimenAddress } from './steps/inbox';
import { endpoints, expectHome, followLinkInMessage, register, signIn, specimenPassword } from './steps/journey';

test('the verification link lets its recipient finish signing up', async ({ page }) => {
  const address = specimenAddress('link');
  const password = specimenPassword();
  const since = Date.now();

  await page.goto(endpoints.signInPage);
  const registered = await register(page, { email: address, password, displayName: 'Link Recipient' });
  expect(registered.status).toBe(202);

  const mail = await mailbox().latestTo(address, { since });
  const link = mail.links.find((one) => new URL(one).pathname === '/auth/verify-email');
  expect(link, 'the mail must contain a verification address').toBeTruthy();

  // Open the address the recipient actually gets, including its host and port.
  // A page may verify immediately or ask for an explicit confirmation.
  const opened = page.waitForResponse((response) => response.url() === link);
  await followLinkInMessage(page, link!);
  expect((await opened).status(), 'the emailed link must serve a usable page').toBeLessThan(400);
  const confirm = page.getByRole('button', { name: /confirm|verify/i });
  if (await confirm.count()) {
    await confirm.click();
  }

  await signIn(page, address, password);
  await expectHome(page);
});
