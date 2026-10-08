// The public frame's journey steps: the document a reader reaches with no session, the way
// into the workspace it offers, and the door that answers a guarded address by sending the
// reader to sign in.
//
// Written against the reference application, apps/platformkit: the homepage it serves at the
// root of the host, and the workspace root its frame is allowed to name — `pinnedWorkspace` in
// apps/platformkit/fault.go, whose TestPinnedAddresses asks the running server that the address
// answers. That is why the entry's address is read from `endpoints` in the desk file rather
// than typed again here, and why no client's own surface appears in either.
//
// Composed from what two specs assert today: `e2e/public-frame-links-workspace-root.spec.ts:9-13`
// (decision 0020 — every entry the anonymous frame offers resolves to the workspace root, and a
// page with no entry at all is the same failure) and `e2e/site.spec.ts:17-31` (the homepage
// answers, its one entry is followed rather than the form navigated to, and the browser ends at
// the sign-in form with the guarded address carried back). Their bodies move here unchanged;
// the claims a spec makes with those reads stay in the spec, because they are the spec's
// decision and not a step's.
//
// Upstream: `septagon-clients/e2e/steps/public.ts`. New here: `workspaceEntries` — the frame's
// entry list read once, which the two specs above each asked for in their own words. Re-typed
// here rather than copied: the copy asserts `/admin/login`, an address this application does not
// serve, so the door's address comes from `endpoints` and the `next` the guard carries back is
// asserted to name what the reader asked for.
//
// A module's own public pages are the module's steps, and would compose these.
import { expect, test as journey, type Page } from '@playwright/test';
import { endpoints } from './kernel';

// openHome is the public homepage a reader arrives at first: an address with nothing named in
// it, which the composition answers as its own landing page. It stays a step of its own rather
// than a line of whichever spec opened it, so a journey can stand on the public page before it
// enters its own pages without importing another spec to do it. The frame's floor — one main, a
// heading a reader is greeted by — is asserted here, since every public page owes it and a
// homepage that answers 200 with neither is the failure this catches.
export async function openHome(page: Page): Promise<void> {
  await journey.step('Open the public homepage', async () => {
    const response = await page.goto('/');
    expect(response?.status(), 'the public homepage did not answer').toBe(200);
    await expect(page.getByRole('main')).toHaveCount(1);
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  });
}

// workspaceEntries is the way into the workspace that an anonymous document offers, as the set
// of addresses it offers rather than as a count: decision 0020 is about which doors a stranger
// may be shown, and a frame that names the workspace root twice and a frame that names the root
// and half a module's screen are different answers. The step reads them; what the set ought to
// be is the assertion the journey makes with it.
export async function workspaceEntries(page: Page): Promise<string[]> {
  const hrefs = await page.locator(`a[href^="${endpoints.workspace}"]`)
    .evaluateAll(links => links.map(link => link.getAttribute('href')));
  return [...new Set(hrefs)] as string[];
}

// signInDoor is the guarded address answering as a door. The reader who has no session is sent
// to sign in rather than shown an empty desk or half a page, and the way a person gets there is
// the entry the public frame names — so the entry is followed, not the form navigated to, and
// the address behind it is the caller's: this step says nothing about which module owns what is
// behind the door. The `next` the guard writes is asserted to carry that address back, which is
// the half that makes a door a door and a dead end a dead end.
export async function signInDoor(page: Page, from: string = endpoints.workspace): Promise<void> {
  const entry = page.locator(`a[href="${from}"]`).first();
  await expect(entry, `the page the journey stands on offers no entry to ${from}`).toHaveCount(1);
  await entry.click();
  await expect(page).toHaveURL(url => new URL(url).pathname === endpoints.signInPage);
  expect(new URL(page.url()).searchParams.get('next'),
    'the door forgot to carry back the address the reader asked for').toBe(from);
}
