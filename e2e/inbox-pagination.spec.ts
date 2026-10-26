import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { expect, test } from '@playwright/test';
import { mailbox, specimenAddress } from './steps/inbox';

// The catcher this run resolved, named by scripts/e2e.sh rather than written down here:
// the run points the application at that host and reads the same one back, and a job
// whose catcher answers by container name rather than on the loopback address would
// otherwise have this journey inject its messages into a server nobody dialed while it
// reads an inbox the run never reached.
test('the inbox finds a recipient beyond its first page of messages', async () => {
  const inbox = mailbox();
  const address = specimenAddress('older-recipient');
  const noise = specimenAddress('newer-recipient');
  await promisify(execFile)('python3', ['-c', `
import smtplib, sys
from email.message import EmailMessage
with smtplib.SMTP(sys.argv[1], int(sys.argv[2]), timeout=10) as smtp:
    for recipient in [sys.argv[3]] + [sys.argv[4]] * 55:
        message = EmailMessage()
        message['From'] = 'fixture@e2e.test'
        message['To'] = recipient
        message['Subject'] = 'Inbox pagination'
        message.set_content('A message whose recipient remains visible after newer mail arrives.')
        smtp.send_message(message)
`, process.env.PLATFORMKIT_E2E_MAIL_HOST ?? '127.0.0.1', process.env.PLATFORMKIT_MAIL_PORT ?? '1025',
    address, noise], { timeout: 20_000 });

  // Prove the real sink holds the message independently of the helper under test.
  await expect.poll(async () => {
    const response = await fetch(`${inbox.url}/api/v1/messages?start=50&limit=50`);
    expect(response.ok).toBe(true);
    const listed = await response.json();
    return listed.messages.some((message: { To: { Address: string }[] }) =>
      message.To.some((to) => to.Address === address));
  }).toBe(true);

  expect.soft(await inbox.countTo(address), 'all retained messages for the recipient are counted').toBe(1);
  const message = await inbox.latestTo(address, { timeoutMs: 1_000 });
  expect(message.to).toContain(address);
});
