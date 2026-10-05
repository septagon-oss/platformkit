import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { expect, test } from '@playwright/test';
import { mailbox, specimenAddress } from './steps/inbox';

test('the inbox finds a recipient beyond its first page of messages', async () => {
  const inbox = mailbox();
  const address = specimenAddress('older-recipient');
  const noise = specimenAddress('newer-recipient');
  await promisify(execFile)('python3', ['-c', `
import smtplib, sys
from email.message import EmailMessage
with smtplib.SMTP('127.0.0.1', int(sys.argv[1]), timeout=10) as smtp:
    for recipient in [sys.argv[2]] + [sys.argv[3]] * 55:
        message = EmailMessage()
        message['From'] = 'fixture@e2e.test'
        message['To'] = recipient
        message['Subject'] = 'Inbox pagination'
        message.set_content('A message whose recipient remains visible after newer mail arrives.')
        smtp.send_message(message)
`, process.env.PLATFORMKIT_MAIL_PORT ?? '1025', address, noise], { timeout: 20_000 });

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
