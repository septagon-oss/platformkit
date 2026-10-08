// The kernel's own journey steps: the sign-in door, the shell every page travels in,
// the workspace the composition generates, and the response floor they arrive under.
//
// Written against the reference application, apps/platformkit. The addresses in
// `endpoints` are its own — apps/platformkit/fault.go pins `pinnedWorkspace` and
// `pinnedSignIn` and its TestPinnedAddresses asks the running server that each answers
// — so they are named as that application's and not as the kernel library's, and not as
// any client's. A client that pins this module composes these steps around its own
// addresses, its own words and its own evidence; that parameterisation is the product's
// share, and nothing in this file names a client, a sector or a price.
//
// Composed from the steps this reference application already exposes. The file's shape —
// one act per exported function, `expect` inside the step rather than after it, a comment
// naming who consumes it — is `e2e/steps/content.ts`, and every body is a move out of the
// spec that walks the same act today: `e2e/landing.spec.ts`, `e2e/site.spec.ts`,
// `e2e/admin-tasks.spec.ts` for the desk, `e2e/session-forms.spec.ts` for the way out.
// Specs compose these; a step composes a lower tier's steps and never imports a spec.
//
// Upstream: the copies consumers kept of these acts in `septagon-clients/e2e/steps/kernel.ts`,
// and the unmerged `journey.ts` of one earlier attempt at the same idea. New here, because
// nothing existing carried it: `endpoints`, so the addresses are stated once instead of in
// every spec; `signOut`, the one kernel act no step file had (it exists in a spec body at
// `e2e/session-forms.spec.ts:65-67`, and is extracted from there); and every fact the copy
// got from its own product rather than from the kernel — the run's address (`runURL()` in the
// clients' `../reporting`, here `PLATFORMKIT_E2E_URL`, the variable this harness exports at
// `scripts/e2e.sh:346` and `e2e/playwright.config.ts:17` reads as `baseURL`), the inbox
// (`PLATFORMKIT_E2E_MAILPIT_URL`), the fixture database pattern this harness writes, and the
// skip-link sentence, which is the one `ui/components/shell.go:49` draws and no product's
// translation of it. `shot` stayed upstream: it writes under `PLATFORMKIT_E2E_SHOTS`, a name
// no file here reads, and evidence this repository owns is Playwright's own
// (`e2e/playwright.config.ts:18-19`).
//
// A step takes an actor and never invents one; it asserts what the person would have seen,
// so a journey that reads as passing really was seen. See `e2e/steps/public.ts` for the
// anonymous half, which composes this file.
import { expect, type APIRequestContext, type Browser, type BrowserContext, type Locator, type Page, type Response } from '@playwright/test';

// A person is an email and a secret, held by whoever the journey invited or appointed.
// Steps take one; none of them fabricates one — `e2e/steps/content.ts`'s `makePublisher`
// is the real flow that makes a person here, through the user module's own commands.
export type Person = { email: string; password: string };

// endpoints are the addresses this reference application answers on its workspace
// surface: the workspace root (apps/platformkit/fault.go `pinnedWorkspace`), the sign-in
// door (`pinnedSignIn`, served by modules/admin/internal/mount.go), and the auth module's
// own three routes (modules/auth/internal/handler.go:38,49,60). They are pinned here and
// nowhere else, so a journey that types an address the application does not serve fails at
// the step rather than as a 404 nobody reads. A client's own surface prefix is a parameter
// of its own composition, not of this one.
export const endpoints = {
  workspace: '/app',
  signInPage: '/app/admin/login',
  signIn: '/api/v1/auth/login',
  signOut: '/api/v1/auth/logout',
} as const;

// The door's address with the guarded address carried back in `next`, which is how
// `httpx.SignIn` forms it and what `e2e/site.spec.ts:27` reads today: the reader who is
// sent away comes back to what they asked for.
function doorTo(next: string) {
  return `${endpoints.signInPage}?next=${encodeURIComponent(next)}`;
}

// A path compared as a path: a journey asserts where the browser stands, and a query or a
// fragment is the page's own business, not the address's.
function atEnd(path: string) {
  return new RegExp(`^${path.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}(\\?[^#]*)?(#.*)?$`);
}

// The run's own address, refused rather than guessed. A step that defaulted it would be a
// step willing to drive somebody else's listener, which is what `scripts/free_port.sh` and
// `check-run-owner` exist to prevent; `e2e/playwright.config.ts:17` reads the same variable
// and keeps its fallback there, where it names the port the harness's default is.
function runURL(): URL {
  const handed = process.env.PLATFORMKIT_E2E_URL ?? '';
  if (!handed) throw new Error('a journey step needs the address this run started: scripts/e2e.sh exports PLATFORMKIT_E2E_URL');
  try {
    return new URL(handed);
  } catch {
    throw new Error(`PLATFORMKIT_E2E_URL is not an address: ${handed}`);
  }
}

// The administrator this bootstrapped run owns. Read where it is used, not at import: the
// two variables are set in the environment of the Playwright run by `scripts/e2e.sh:349-350`
// and nothing else, so a step that could not work outside a run must say so when it is asked,
// not when it is loaded.
function bootstrapped(): Person {
  const email = process.env.PLATFORMKIT_E2E_EMAIL ?? '';
  const password = process.env.PLATFORMKIT_E2E_PASSWORD ?? '';
  if (!email || !password) throw new Error('the harness exported no bootstrapped administrator: run this through make e2e');
  return { email, password };
}

// disposable is the harness's permission to write. These journeys create and delete rows,
// and the only database they may do that to is the one the run created and will drop again:
// `scripts/e2e.sh:22` names it `platformkit_e2e_<epoch>_<random>_<pid>` and `:346` binds the
// run to `localhost` on a port it chose itself. Both halves are checked, so a journey cannot
// point itself at somebody's real installation. `host` and `database` are the two facts a
// client's own bootstrap changes — its tenant is addressed as `<client>.localhost` and its
// fixture database carries its own prefix — so they are asked for, and this file states only
// the reference application's.
export function disposable(ask: { host?: string; database?: RegExp } = {}): Person {
  const host = ask.host ?? 'localhost';
  const database = ask.database ?? /^platformkit_e2e_\d+_\d+_\d+$/;
  const base = runURL();
  if (base.protocol !== 'http:' || base.hostname !== host || base.username || base.password ||
      !database.test(process.env.PLATFORMKIT_E2E_FIXTURE_DATABASE ?? '')) {
    throw new Error(`a writing journey needs the disposable database scripts/e2e.sh creates (asked for http to ${host})`);
  }
  return bootstrapped();
}

// mailbox is the catcher `scripts/e2e.sh` boots beside the disposable database. A journey
// that follows a mailed link reads the link out of that inbox rather than inventing a token
// beside it, and the inbox's address is a fact about the boot, not about the journey:
// `PLATFORMKIT_E2E_MAILPIT_URL` (`scripts/e2e.sh:79-81`, exported at `:351`, started by
// `make up`). The port is never guessed here — an inbox assumed is an inbox that proves
// nothing about anybody's mail.
export function mailbox(): string {
  const handed = process.env.PLATFORMKIT_E2E_MAILPIT_URL ?? '';
  if (!handed) throw new Error('a mailed journey needs the inbox scripts/e2e.sh boots: PLATFORMKIT_E2E_MAILPIT_URL');
  const inbox = new URL(handed);
  if (inbox.protocol !== 'http:' || !inbox.hostname || !inbox.port) {
    throw new Error(`a mailed journey needs Mailpit's own http address with a host and a port, not ${handed}`);
  }
  return handed;
}

// login is the door answered as a number: the API a journey uses when the sign-in form is
// not the thing under test — a second person's session behind a page, a fixture's own
// administrator. A journey that means to prove the door takes `signIn` or `signInAs`.
export async function login(request: APIRequestContext, email: string, password: string) {
  try {
    return (await request.post(endpoints.signIn, { data: { email, password } })).status();
  } catch {
    throw new Error('the account the journey was handed could not sign in');
  }
}

// signIn is a journey's own first step, through the form a person uses and not an API call
// that leaves a cookie behind: a console nobody can sign in to is the regression this catches
// first. It walks the same eight lines `e2e/landing.spec.ts:39-43`, `e2e/site.spec.ts:28-31`
// and `e2e/admin-tasks.spec.ts:17-21` each walked for themselves, as the harness's own
// administrator (`disposable` for the journeys that write).
export async function signIn(page: Page, next: string = endpoints.workspace) {
  const person = bootstrapped();
  await page.goto(doorTo(next));
  await page.getByLabel('Email').fill(person.email);
  await page.getByLabel('Password').fill(person.password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(atEnd(next));
}

// personContext is one browser context belonging to nobody but the person who will use it,
// at the address this run drives. Both halves are load-bearing: two people in one cookie jar
// is one person wearing two hats, and a context created here inherits nothing from the
// config's `use` block, so the run's own host is stated rather than assumed. `width` and
// `reducedMotion` are the two per-context differences the journeys that hold several people
// at once each had to be able to ask for — a desk that has to hold a phone width, and a page
// whose colour transition would otherwise be measured mid-animation.
export async function personContext(browser: Browser,
  options: { width?: number; reducedMotion?: 'reduce' | 'no-preference' } = {},
): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext({
    baseURL: runURL().origin,
    ...(options.width ? { viewport: { width: options.width, height: 900 } } : {}),
  });
  const page = await context.newPage();
  if (options.reducedMotion) await page.emulateMedia({ reducedMotion: options.reducedMotion });
  return { context, page };
}

// signInAs opens a page in one person's own browser context, signed in the way a person signs
// in. The caller closes the context when the journey is done with the person — a step never
// closes a session it was handed. `e2e/landing.spec.ts:57-63` is the walk this replaces.
export async function signInAs(browser: Browser, person: Person, next: string = endpoints.workspace,
  options: { width?: number; reducedMotion?: 'reduce' | 'no-preference' } = {}): Promise<Page> {
  const { page } = await personContext(browser, options);
  await page.goto(doorTo(next));
  await page.getByLabel('Email').fill(person.email);
  await page.getByLabel('Password').fill(person.password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(atEnd(next));
  return page;
}

// openSession hands back a person's own context and page, signed in over the API. It is
// `signInAs` minus the form, for the journeys whose subject is not the door: three people on
// one desk page is four sign-ins before the first assertion, and a journey that has already
// proved the form proves it four times over. The person still signs in as themselves, through
// the same endpoint a person's session comes from, in a context nobody else shares — the
// separation the journey is about is the one kept here.
export async function openSession(browser: Browser, person: Person,
  options: { width?: number; reducedMotion?: 'reduce' | 'no-preference' } = {},
): Promise<{ context: BrowserContext; page: Page }> {
  const session = await personContext(browser, options);
  expect(await login(session.context.request, person.email, person.password)).toBe(200);
  return session;
}

// signOut is the way out, taken the way a person takes it: the shell's own button
// (`modules/admin/internal/mount.go:359-363`, rendered only for a signed-in request) rather
// than a POST a journey makes behind the page's back. The two assertions are the ones
// `e2e/session-forms.spec.ts:65-67` proves today — the person arrives at the door, and their
// session is gone — and the refusal names the half this kernel writes: when logout answers
// without ok the shell keeps the page, focuses `[data-session-error]` and says the sign-out
// could not be confirmed (`ui/assets/js/session.js:139-142`). The step reports that as the
// refusal it is rather than navigating away and calling it a sign-out.
export async function signOut(page: Page): Promise<void> {
  const control = page.locator('[data-sign-out]');
  await expect(control, 'the way out is offered to a signed-in person and to nobody else: ' +
    'this page does not hold one, so the journey asked a signed-out page to sign out').toHaveCount(1);
  await control.click();
  const arrived = await page.waitForURL(atEnd(endpoints.signInPage), { timeout: 10_000 }).then(() => true, () => false);
  if (!arrived) {
    await expect(page.locator('[data-session-error]'), 'logout did not answer ok, so the shell keeps the page ' +
      'and says the sign-out could not be confirmed').toBeVisible();
    throw new Error('sign out was refused: the person kept the page and was told, and the session was not ended');
  }
  expect((await page.request.get('/api/v1/auth/me')).status(),
    'the shell moved the person to the door but the session outlived the sign-out').toBe(403);
}

// useTheme states the theme before the document's own script runs, because the theme is
// served on the document and never switched after it has loaded: the shell's themed utilities
// carry a colour transition, and until a frame advances it the outgoing theme's foreground is
// still the computed one over the incoming theme's background — a state no reader reads and no
// assertion should measure. The key is this repository's: `ui/assets/js/theme.js:21` and the
// before-paint snippet at `ui/page/serve.go:72` both name it.
export async function useTheme(page: Page, theme: 'light' | 'dark') {
  await page.addInitScript(value => {
    try { localStorage.setItem('platformkit-theme', value); } catch { /* private mode */ }
  }, theme);
}

// SKIP_LINK is the one sentence the frame states. `ui/components/shell.go:49` draws it and the
// struct at :22-26 says in as many words that there is deliberately no label prop: a skip link
// says "Skip to content" in every application that has one. No translation of it lives in this
// repository, so a step cannot assert one — the language a document declares is checked by
// `declaresLanguage`, and whose words it is stays with whoever translated them.
export const SKIP_LINK = 'Skip to content' as const;

// StructureOptions names the two knobs `structure` offers, both of them a difference a real
// page has: which controls owe a label, and whether the skip-link words are matched exactly.
export type StructureOptions = { skip?: string; controls?: 'form' | 'page'; exact?: boolean };

// structure is what every page owes a reader regardless of what it is about: one main, one
// first-level heading, nothing to scroll sideways for, a label on every control, no repeated
// id, and a skip link that reaches the content the frame names (`ui/components/shell.go:38-58`
// writes the markup this follows).
//
// `controls` asks which controls owe that label: the generated screens put every control inside
// the form it belongs to, and asking only of those is the narrower question a console journey
// asks, while a page that draws five forms and a desk on one document owes the wider one. The
// default is the narrower, so a journey joins the wider question by naming it rather than by
// the kernel widening for everybody at once. `skip` and `exact` are for a frame that names the
// link in another language's words.
export async function structure(page: Page, asked: string | StructureOptions = {}) {
  const given: StructureOptions = typeof asked === 'string' ? { skip: asked } : asked;
  const skip = given.skip ?? SKIP_LINK;
  await expect(page.getByRole('main')).toHaveCount(1);
  await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect(await page.evaluate(() => {
    const ids = [...document.querySelectorAll('[id]')].map(element => element.id);
    return ids.filter((id, index) => ids.indexOf(id) !== index);
  })).toEqual([]);
  const owed = given.controls === 'page'
    ? 'input:not([type="hidden"]), textarea, select'
    : 'form input:not([type="hidden"]), form textarea, form select';
  for (const control of await page.locator(owed).all()) {
    expect(await control.evaluate(element => (element as HTMLInputElement).labels?.length)).toBeGreaterThan(0);
  }
  const link = given.exact
    ? page.getByRole('link', { name: skip, exact: true })
    : page.getByRole('link', { name: skip });
  await link.focus();
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(url => url.hash === '#content');
}

// declaresLanguage is the pairing a document has to hold (WCAG 3.1.1): the language the address
// named is the language the document declares, and the frame's own words are that language's
// and not the previous page's left behind. `e2e/localization.spec.ts:9-11` proves the pairing
// by hand today; the words argument is where a translated frame states its own sentence,
// because this application serves the tenant under test in pt-PT (`scripts/e2e.sh` passes
// `--language pt-PT`) while the frame's sentence above is what the kernel states.
export async function declaresLanguage(page: Page, language: string, words: string = SKIP_LINK) {
  await expect(page.locator('html')).toHaveAttribute('lang', language);
  await expect(page.getByRole('link', { name: words, exact: true })).toHaveCount(1);
}

// moment is a datetime-local value this many minutes from now, in UTC, which is what a field
// carrying no offset means to the server that reads it. The harness's browser runs in UTC, so
// the field a person sees says the same thing the assertion does.
export function moment(minutes: number) {
  return new Date(Date.now() + minutes * 60_000).toISOString().slice(0, 16);
}

// workspace is the desk's navigation, which is the composition seen from the inside:
// `modules/admin` generates one entry per resource mounted before it, so this list changing
// means the module set changed. The sidebar is desktop-only, so callers read it at a desktop
// width. `e2e/landing.spec.ts:68-69` is the read this replaces; the label is stated at
// `ui/components/sidebar.go:74` and `modules/admin/internal/mount.go:337`.
export async function workspace(page: Page): Promise<string[]> {
  return page.getByRole('navigation', { name: 'Admin navigation' }).getByRole('link').allInnerTexts()
    .then(labels => labels.map(label => label.trim()));
}

// detail is one field of a generated detail screen, addressed by the name its own term shows
// rather than by position in the list. The identity is the component's:
// `ui/components/detail_list.go:92` and `detail_panel.go:93` write `[data-detail-item]`, which
// is what `e2e/shared-retained-field-refusal.spec.ts:18` reads.
export function detail(page: Page, name: string) {
  return page.locator('[data-detail-item]')
    .filter({ has: page.locator('dt', { hasText: new RegExp(`^\\s*${name}\\s*$`) }) }).locator('dd');
}

// toggle sets a checkbox from the keyboard, wherever it lives: on a generated screen, on a
// module's own page, or inside one panel of a page that carries several. The scope is the
// document or the region holding the box; the name is the box's accessible name, as exactly
// the words or as a pattern.
//
// The shell hides the real input behind a styled box that intercepts a pointer, so a click
// never reaches it; Space on a focused checkbox is both how a person using a keyboard does it
// and the only thing that works (`e2e/checkbox-state.spec.ts:9-17`). Setting it to what it
// already is does nothing, and two boxes sharing one accessible name is refused: the name is
// the address, and the page just offered two of them.
export async function toggle(scope: Page | Locator, name: string | RegExp, checked = true): Promise<Locator> {
  const box = typeof name === 'string'
    ? scope.getByRole('checkbox', { name, exact: true })
    : scope.getByRole('checkbox', { name });
  await expect(box).toHaveCount(1);
  if (await box.isChecked() === checked) return box;
  await box.focus();
  await expect(box).toBeFocused();
  await box.press('Space');
  await expect(box).toBeChecked({ checked });
  return box;
}

// keyboardSubmit submits a form from the keyboard, and insists the submit button is reachable
// by tabbing forward from the last control and shows where the focus is. The walk is a loop
// rather than one press because a date field spends several stops on its own segments before
// it lets go, and the screens put a Cancel link between the last control and the button. The
// ring it looks for is the kernel's own: `clFocusRing` at `ui/components/classlists.go:103-106`
// is a box-shadow on `:focus-visible`, which is why the step walks rather than calling focus().
//
// `within` names the region whose form is meant, for the page that draws several; what does not
// change when a screen's markup does is that a submit button sits inside the form it submits,
// so the form is found from the button.
//
// `status` is the answer the command owes. A refusal is a document and not a redirect — the
// same page comes back with the reason above it and the answers still in the boxes
// (`ui/page/fault.go`) — so a journey that means to prove a refusal names the status it
// expects rather than watching for a navigation that will not come. Without it the step
// presses the key and leaves the wait to the caller, which is what a generated screen's own
// htmx swap needs.
export async function keyboardSubmit(page: Page, name: string,
  options: { within?: Locator; status?: number } = {}): Promise<void> {
  const scope = options.within ?? page;
  const button = scope.getByRole('button', { name, exact: true });
  await expect(button).toHaveCount(1);
  const form = button.locator('xpath=ancestor::form[1]');
  await expect(form).toHaveCount(1);
  // The walk starts from the last thing a person would fill in, because that is where they
  // are. A form with nothing to fill in has no such place, and its own button is the only
  // control it offers: start there, and the walk below has nothing to walk.
  const fields = form.locator(
    'input:not([type="hidden"]):not([disabled]):not([readonly]), textarea:not([disabled]):not([readonly]), select:not([disabled])',
  );
  if (await fields.count() > 0) await fields.last().focus();
  else await button.focus();
  for (let stop = 0; stop < 24 && !await button.evaluate(element => element === document.activeElement); stop++) {
    await page.keyboard.press('Tab');
  }
  await expect(button).toBeFocused();
  expect(await button.evaluate(element => {
    const style = getComputedStyle(element);
    return style.outlineStyle !== 'none' || style.boxShadow !== 'none';
  })).toBe(true);
  if (options.status === undefined) {
    await page.keyboard.press('Enter');
    return;
  }
  const path = new URL(await button.evaluate(element => (element as HTMLButtonElement).form!.action), page.url()).pathname;
  const answered = page.waitForResponse(r => new URL(r.url()).pathname === path && r.request().method() === 'POST');
  await page.keyboard.press('Enter');
  expect((await answered).status(), `the command answered ${options.status} and the page is the refusal`).toBe(options.status);
}

// save is a generated form's submit, with no status to wait for: the screen answers with an
// htmx swap and hands its writer to the row it wrote, which the caller reads next.
export async function save(page: Page, name = 'Save') {
  await keyboardSubmit(page, name);
}

// securityHeaders is the response floor every page of this application arrives under, asserted
// where the journey already stands rather than in a request of its own. The clauses are the
// kernel's, written at kit/httpx/headers.go: DENY (`:32`, set at :111), nosniff (:35,:113),
// the referrer policy (:33,:112), the policy's own clauses (:58 — base-uri and form-action are
// the two that make the rest hold), no-store under a session and noindex on every non-public
// surface (:97,:119-121). `desk` is the difference the kernel itself makes: a desk page carries
// one tenant's unpublished draft, so it is answered `no-store` and `no-referrer`
// (`ui/page/serve.go:159,175-179`), while an anonymous page keeps a cache's permission to be
// cached at all.
//
// No Strict-Transport-Security is asserted, and none is sent here: headers.go:113-115 withholds
// it from a deployment reached at a local name, because a browser told to use https for
// localhost is a laptop that cannot reach its own application. The gallery is asked for none of
// this either — `modules/admin/gallery_access_test.go:147` asserts SAMEORIGIN and a sandboxed
// policy there on purpose — which is why the step takes the response the journey already read
// and the caller names the surface.
export function securityHeaders(response: Response | null, surface: 'desk' | 'public') {
  expect(response, 'the page the journey was already asked to read did not answer').not.toBeNull();
  const sent = response!.headers();
  expect(sent['content-type']).toContain('text/html');
  expect(sent['x-content-type-options']).toBe('nosniff');
  expect(sent['x-frame-options']).toBe('DENY');
  expect(sent['referrer-policy'], `${surface} surfaces answer with the policy kit/httpx/headers.go writes`)
    .toBe(surface === 'desk' ? 'no-referrer' : 'strict-origin-when-cross-origin');
  const policy = sent['content-security-policy'] ?? '';
  for (const clause of ["default-src 'self'", "frame-ancestors 'none'", "base-uri 'none'",
    "form-action 'self'", "script-src 'self' 'nonce-"]) {
    expect(policy, `${response!.url()} answers without ${clause}`).toContain(clause);
  }
  if (surface === 'desk') expect(sent['cache-control']).toBe('no-store');
  expect(sent['strict-transport-security']).toBeUndefined();
}
