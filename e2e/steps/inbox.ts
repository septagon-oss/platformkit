import { expect } from '@playwright/test';

// Reading the mail, from the browser's side of the stack.
//
// A verification link is the credential a sign-up journey has to survive, and
// until now no spec here could read one: e2e/email-verification-forms.spec.ts
// fabricates the forms a mail would have opened and says so at its own line 5.
// This file talks to the sink the development stack actually runs — Mailpit,
// started by `make up`, addressed by PLATFORMKIT_E2E_MAIL_URL, which
// scripts/e2e.sh exports — over its REST API, so the assertion is about a message
// the application sent, not about one a test wrote.
//
// Two rules, both learned from what the flat specs it replaces got wrong:
//
//   - Nothing reads process.env at import time. `mailbox()` reads it when it is
//     called, so a spec that imports this file for one step does not fail at
//     collection because the inbox is switched off.
//   - Nothing here hard-codes a port. The app is PLATFORMKIT_E2E_URL and the
//     inbox is PLATFORMKIT_E2E_MAIL_URL; a fixed 8025 is a collision with the
//     next worker's stack, which is why compose parameterises both.

/** One message, as the step library needs it: who it was for and what it says. */
export interface Mail {
  id: string;
  to: string[];
  subject: string;
  /** Mailpit's own UTC timestamp, ISO-8601. */
  created: string;
  text: string;
  /** Absolute http(s) URLs in the body, in the order the reader meets them. */
  links: string[];
}

export interface Mailbox {
  /** The newest message for `address` posted after `since`, waiting up to
   *  `timeoutMs`. The failure names the address, what the inbox held in total and
   *  how much of it was for somebody else — which is the difference between "no
   *  mail was ever sent" and "the mail went somewhere else". */
  latestTo(address: string, opts?: { since?: number; timeoutMs?: number }): Promise<Mail>;
  /** How many messages the inbox holds for `address`. The assertion form: a
   *  refused sign-up that sends nothing has to be able to prove it sent nothing. */
  countTo(address: string): Promise<number>;
  /** The whole inbox. Only in a `beforeAll` of a spec that owns its own Mailpit:
   *  the suite runs one worker against one shared inbox, so clearing between two
   *  specs would eat the other one's mail. */
  clear(): Promise<void>;  /** Where this mailbox is, for a failure message. */
  readonly url: string;
}

interface ListResponse {
  total: number;
  messages: {
    ID: string;
    To: { Address: string }[] | null;
    Subject: string;
    Created: string;
  }[];
}

interface MessageResponse {
  Subject: string;
  Text: string;
}

/** The absolute URL of the inbox, refusing to invent one: a step that guessed a
 *  port would pass against the machine that happens to run 8025 and fail on the
 *  next one, which is the failure the parameterised ports exist to prevent. */
export function mailbox(url = process.env.PLATFORMKIT_E2E_MAIL_URL): Mailbox {
  if (!url) {
    throw new Error(
      'no inbox is configured: set PLATFORMKIT_E2E_MAIL_URL (scripts/e2e.sh exports it; `make up` starts the sink)',
    );
  }
  const base = url.replace(/\/$/, '');
  return {
    url: base,
    latestTo: (address, opts) => latestTo(base, address, opts),
    countTo: (address) => countTo(base, address),
    clear: () => clear(base),
  };
}

async function get<T>(base: string, path: string): Promise<T> {
  const response = await fetch(base + path);
  if (!response.ok) {
    throw new Error(`the inbox at ${base}${path} answered ${response.status}; is Mailpit up?`);
  }
  return (await response.json()) as T;
}

function lower(list: { Address: string }[] | null): string[] {
  return (list ?? []).map((one) => one.Address.toLowerCase());
}

/** Mailpit answers `/api/v1/messages` one page at a time and defaults the page to 50
 *  messages. Asking once and reading the answer as the whole inbox is what made a
 *  51st message invisible: `countTo` reported that nothing was sent for a recipient
 *  whose mail the sink held, and `latestTo` gave up on a verification link that was
 *  sitting in the box. So every read here walks the pages until it has seen `total`
 *  of them, and a page that comes back short ends the walk. `total` is what bounds
 *  the loop: Mailpit keeps the newest messages it retains, so the number it reports
 *  is the number there is to fetch. */
const pageSize = 50;

async function pages(base: string): Promise<ListResponse> {
  const seen: ListResponse['messages'] = [];
  const byId = new Set<string>();
  let total = 0;
  for (let start = 0; ; start += pageSize) {
    const listed = await get<ListResponse>(
      base,
      `/api/v1/messages?start=${start}&limit=${pageSize}`,
    );
    total = listed.total;
    for (const one of listed.messages) {
      // New mail arrives between two pages and shifts what is left of the older
      // one, so a message can appear on two of them. Counting it twice would be a
      // count the fixture cannot trust, which is the failure this file is for.
      if (!byId.has(one.ID)) {
        byId.add(one.ID);
        seen.push(one);
      }
    }
    if (listed.messages.length < pageSize || seen.length >= total) break;
  }
  return { total, messages: seen };
}

async function latestTo(
  base: string,
  address: string,
  opts: { since?: number; timeoutMs?: number } = {},
): Promise<Mail> {
  const wanted = address.toLowerCase();
  const since = opts.since ?? 0;
  const deadline = Date.now() + (opts.timeoutMs ?? 15_000);
  let last = 'the inbox answered no list at all';
  while (Date.now() < deadline) {
    const listed = await pages(base);
    const mine = listed.messages.filter((one) => lower(one.To).includes(wanted));
    const fresh = mine.filter((one) => Date.parse(one.Created) >= since);
    if (fresh.length > 0) {
      const newest = fresh.reduce((a, b) => (Date.parse(a.Created) >= Date.parse(b.Created) ? a : b));
      const full = await get<MessageResponse>(base, `/api/v1/message/${encodeURIComponent(newest.ID)}`);
      return {
        id: newest.ID,
        to: lower(newest.To),
        subject: newest.Subject,
        created: newest.Created,
        text: full.Text,
        links: [...full.Text.matchAll(/https?:\/\/[^\s>"']+/g)].map((m) => m[0]),
      };
    }
    last = mine.length > 0
      ? `${mine.length} message(s) for ${wanted} exist but none is newer than ${new Date(since).toISOString()}`
      : `the inbox holds ${listed.total} message(s) and none is addressed to ${wanted}`;
    await new Promise((wait) => setTimeout(wait, 500));
  }
  throw new Error(`no message for ${wanted} within ${opts.timeoutMs ?? 15_000}ms: ${last}`);
}

async function countTo(base: string, address: string): Promise<number> {
  // Counted by filtering the list here rather than by asking Mailpit's search for
  // `recipient:<address>`: its query language tokenises, and measured against a real
  // inbox it answered a search for an address nobody had ever used with the one
  // message the inbox happened to hold. A count a fixture cannot trust is worse than
  // no count, because it turns "no mail was sent" into a passing assertion.
  const wanted = address.toLowerCase();
  const listed = await pages(base);
  return listed.messages.filter((one) => lower(one.To).includes(wanted)).length;
}

async function clear(base: string): Promise<void> {
  const response = await fetch(base + '/api/v1/messages', { method: 'DELETE' });
  if (!response.ok) {
    throw new Error(`the inbox at ${base} refused to be cleared: ${response.status}`);
  }
}

/** A unique address per run. Specs share one application, one database and one
 *  inbox, and one worker does not make an email address unique. */
export function specimenAddress(label: string): string {
  return `${label}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}@e2e.test`;
}

/** The assertion a sign-up journey should open with: the inbox is reachable, and
 *  it says so in a fraction of a second rather than after a spec timeout. */
export async function expectInboxReady(inbox: Mailbox): Promise<void> {
  expect(await inbox.countTo(specimenAddress('nobody')), 'a fresh address has no mail').toBe(0);
}
