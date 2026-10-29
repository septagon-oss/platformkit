import { expect, test } from '@playwright/test';
import { mailbox, specimenAddress } from './steps/inbox';
import { endpoints, origin, register, specimenPassword } from './steps/journey';

test('the verification email links to the instance that sent it', async ({ page }) => {
  const address = specimenAddress('origin');
  const since = Date.now();

  await page.goto(endpoints.signInPage);
  const answer = await register(page, {
    email: address,
    password: specimenPassword(),
    displayName: 'Link Recipient',
  });
  expect(answer.status).toBe(202);

  const mail = await mailbox().latestTo(address, { since });
  const link = mail.links.find((one) => new URL(one).pathname === '/auth/verify-email');
  expect(link, 'the verification email has a link').toBeTruthy();
  expect(new URL(link!).origin).toBe(origin());
});

test('the verification address serves a confirmation page on this instance', async ({ page }) => {
  const address = specimenAddress('confirmation');
  const since = Date.now();

  await page.goto(endpoints.signInPage);
  const answer = await register(page, {
    email: address,
    password: specimenPassword(),
    displayName: 'Link Recipient',
  });
  expect(answer.status).toBe(202);

  const mail = await mailbox().latestTo(address, { since });
  const link = mail.links.find((one) => new URL(one).pathname === '/auth/verify-email');
  expect(link, 'the verification email has a link').toBeTruthy();
  const confirmation = new URL(link!);
  confirmation.host = new URL(origin()).host;
  const response = await page.goto(confirmation.toString());
  expect(response?.status(), 'the verification page is served on this instance').toBeLessThan(400);
});
