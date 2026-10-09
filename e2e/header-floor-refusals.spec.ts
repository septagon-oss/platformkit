// The other half of the published header step: what it refuses.
//
// `e2e/step-response-headers.spec.ts` asks the step about the two real answers of this
// application — the public root and the workspace root — and that is the acceptance direction: a
// step whose floor the application does not satisfy is a step nobody can call. This file asks the
// refusal direction, which no real answer of a correct application can show, by handing the step
// the response a broken one would write. Each case varies one header off the answer the reference
// application really sends (the values below are that response, read out of the trace of the case
// above), so a pass can only come from the rule the case names.
//
// A fabricated response is not a journey's observation — it is the step's own rule asked back of
// it, the way `kit/httpx/surfaces_test.go:482,672` ask a surface's rule of the writer rather than
// wait for a deployment to get it wrong.
import { expect, test, type Response } from '@playwright/test';
import { securityHeaders } from './steps/kernel';

// A desk document as this application answers it, missing nothing.
const desk: Record<string, string> = {
  'content-type': 'text/html; charset=utf-8',
  'x-content-type-options': 'nosniff',
  'x-frame-options': 'DENY',
  'referrer-policy': 'strict-origin-when-cross-origin',
  'content-security-policy': "default-src 'self'; script-src 'self' 'nonce-abc'; style-src 'self' " +
    "'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'",
  'cache-control': 'private, no-store',
  'x-robots-tag': 'noindex, nofollow',
};

// sent — the answer, without the named header, so a refusal is a missing clause and not a typo.
const sent = (over: Record<string, string>, ...without: string[]) => Object.fromEntries(
  Object.entries({ ...desk, ...over }).filter(([name]) => !without.includes(name)));

// asked — the step, pointed at a response of the surface the case names, held as a call so the
// case says whether it expects the step to answer or to refuse.
const asked = (headers: Record<string, string>, surface: 'desk' | 'public') => () => securityHeaders({
  url: () => `http://localhost/${surface === 'desk' ? 'app' : ''}`,
  headers: () => headers,
} as unknown as Response, surface);

test('the header step accepts every answer the kernel writes on a desk page', () => {
  expect(asked(desk, 'desk')).not.toThrow();
  // The one answer a translated workspace page really gives, and the spelling a page that knows
  // its own bytes gives it (ui/page/serve.go:155-159 and :177): the directive is the floor.
  expect(asked(sent({ 'cache-control': 'Private, NO-STORE' }), 'desk')).not.toThrow();
  // A page carrying a private URL credential strengthens the referrer policy: sensitivity is the
  // page's, not the surface's (ui/page/serve.go:175-179).
  expect(asked(sent({ 'referrer-policy': 'no-referrer' }), 'desk')).not.toThrow();
  // A public page is allowed to be kept and says nothing about being found, and its policy
  // carries no embedding clause: kit/httpx/headers.go:168-174 adds object-src to the workspace's
  // answer alone.
  const face = sent({ 'cache-control': 'public, max-age=60',
    'content-security-policy': desk['content-security-policy'].replace("; object-src 'none'", '') }, 'x-robots-tag');
  expect(asked(face, 'public')).not.toThrow();
});

test('the header step refuses a desk answer a cache could keep', () => {
  expect(asked(sent({ 'cache-control': 'private, max-age=60' }), 'desk')).toThrow();
  expect(asked(sent({ 'cache-control': 'public, max-age=60' }), 'desk')).toThrow();
  expect(asked(sent({}, 'cache-control'), 'desk')).toThrow();
  // Keeping a public answer is not the failure these three name.
  expect(asked(sent({ 'cache-control': 'public, max-age=60' }), 'public')).not.toThrow();
});

test('the header step refuses a desk answer that stopped refusing the crawler', () => {
  expect(asked(sent({}, 'x-robots-tag'), 'desk')).toThrow();
  expect(asked(sent({ 'x-robots-tag': 'nofollow' }), 'desk')).toThrow();
});

test('the header step refuses a referrer policy the kernel never writes', () => {
  for (const referrer of ['unsafe-url', 'origin', 'same-origin', '']) {
    expect(asked(sent({ 'referrer-policy': referrer }), 'desk'), referrer).toThrow();
  }
  expect(asked(sent({}, 'referrer-policy'), 'desk')).toThrow();
});

test('the header step refuses a policy that dropped the clause that makes the rest hold', () => {
  for (const clause of ["form-action 'self'", "base-uri 'none'", "frame-ancestors 'none'", "object-src 'none'"]) {
    const policy = desk['content-security-policy'].replace(`; ${clause}`, '');
    expect(asked(sent({ 'content-security-policy': policy }), 'desk'), clause).toThrow();
  }
});
