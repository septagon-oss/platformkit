import { expect, test } from '@playwright/test';

// The fifth review's pin: one ask, one notice to each holder of role management —
// never one per role — in the one composition the merge of origin/main left.
//
// What is already covered, and what is not. The delivery's own journey
// (`e2e/access-request.spec.ts`) and the two Go cases in `apps/platformkit` each
// make *one* administrator the recipient, so the recipient list holds one person
// holding one role. `modules/user/contracts/user.go` writes a stronger promise for
// `Holders` — "the people in this tenant holding at least one of roles, **one id
// each**" — and the reason it is phrased that way is that "who holds this role" is
// an overlap query over a text array (`roles && $1::text[]`), which answers one row
// per person only because that is how the predicate is written. A person holding
// TWO administering roles is the shape that would turn the sentence into a mail
// merge: the same predicate written as a join, or the loop moved into the role list,
// returns that person twice, and every ask in the tenant then bells them twice, and
// `notified` counts a notice that was never asked for. No case in the tree puts one
// person behind two administering roles, in any round.
//
// The second thing this file asks is the thing the merge of origin/main could have
// silently broken and no single case asks: are the kernel's two workspace doors —
// `GET /api/v1/app/resources`, which main's mobile contract mounts, and
// `POST /api/v1/app/access-requests`, which this branch mounts — answered by *one*
// composition, to *one* refused person? Each door has its own test elsewhere
// (`TestThePublishedCatalogContractComesFromTheCompositionItself` for the document,
// `TestTheJSONAskDoorTellsTheSameTrailAndGrantsNothing` for the ask); neither asks
// them of the same booted application as the same person, which is what a merge
// either keeps or loses.
//
// Nothing here waits on a defect, and no assertion reads an English sentence to
// find its way in: the start is the 403 a member is given, the ask's answer is the
// 202 the door returns, and the notices are counted by the asker's row key (their
// id), which is what the notice body carries.

const admin = {
  email: process.env.PLATFORMKIT_E2E_EMAIL ?? 'admin@e2e.test',
  password: process.env.PLATFORMKIT_E2E_PASSWORD ?? '',
};

const run = Date.now();
// The person who holds two roles that each administer the tenant. Two roles are
// the whole point: one notice per *person* is the claim, and one role cannot ask
// it.
const holder = {
  email: `r5-holder-${run}@e2e.test`,
  password: 'a-review-fixture-chooses-this-1',
  name: 'Holds Two Administering Roles',
};
// The person the kernel refuses, who asks.
const asker = {
  email: `r5-asker-${run}@e2e.test`,
  password: 'a-review-fixture-chooses-this-2',
  name: 'Refused Asker',
};

const grantsManageA = `r5_grants_a_${run}`;
const grantsManageB = `r5_grants_b_${run}`;
// A role of this run's own that grants no task grant: the journey must start with a
// refusal whatever another spec did to a shared role.
const grantsNoTasks = `r5_no_tasks_${run}`;

const refusedScreen = '/app/task/tasks';
const jsonAskDoor = '/api/v1/app/access-requests';
const catalogDoor = '/api/v1/app/resources';
const noticeDoor = '/api/v1/notification/notifications';

type Notice = { title?: string; body?: string; link?: string };

test('one ask tells each holder of role management once, and both kernel doors answer one composition', async ({
  page,
  browser,
}) => {
  test.setTimeout(120_000);

  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(admin.email);
  await page.getByLabel('Password').fill(admin.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/app$/);

  // Two roles, both of which administer the tenant, and one that does not read
  // tasks. All three are this run's own, so nothing another spec ticked or unticked
  // can move this journey's start.
  for (const role of [grantsManageA, grantsManageB]) {
    const r = await page.request.put(`/api/v1/auth/roles/${role}`, { data: { permissions: ['role:manage'] } });
    expect(r.status(), await r.text()).toBe(200);
  }
  const nothing = await page.request.put(`/api/v1/auth/roles/${grantsNoTasks}`, {
    data: { permissions: ['audit:read'] },
  });
  expect(nothing.status(), await nothing.text()).toBe(200);

  // The two people, invited with their roles and given a password through the
  // person's own command — the only door that lets a new person choose one in a
  // browser is the invitation link, which goes to a mailbox no browser can read.
  const invitedHolder = await page.request.post('/api/v1/user/invitations', {
    data: { email: holder.email, displayName: holder.name, roles: [grantsManageA, grantsManageB] },
  });
  expect(invitedHolder.status(), await invitedHolder.text()).toBe(201);
  const holderID = (await invitedHolder.json()).id as string;
  const activatedHolder = await page.request.post(`/api/v1/user/users/${holderID}/set-password`, {
    data: { password: holder.password },
  });
  expect(activatedHolder.status(), await activatedHolder.text()).toBe(200);

  const invitedAsker = await page.request.post('/api/v1/user/invitations', {
    data: { email: asker.email, displayName: asker.name, roles: [grantsNoTasks] },
  });
  expect(invitedAsker.status(), await invitedAsker.text()).toBe(201);
  const askerID = (await invitedAsker.json()).id as string;
  const activatedAsker = await page.request.post(`/api/v1/user/users/${askerID}/set-password`, {
    data: { password: asker.password },
  });
  expect(activatedAsker.status(), await activatedAsker.text()).toBe(200);

  // The refused person, in a browser of their own.
  const askerContext = await browser.newContext();
  const person = await askerContext.newPage();
  await person.goto('/app/admin/login');
  await person.getByLabel('Email').fill(asker.email);
  await person.getByLabel('Password').fill(asker.password);
  await person.getByRole('button', { name: 'Sign in' }).click();
  await expect(person).toHaveURL(/\/app$/);

  const refusal = await person.goto(refusedScreen);
  expect(refusal?.status(), 'the member was not refused, so this journey has no start').toBe(403);

  // The way on is followed, not admired: 404 at that address is the dead end the
  // brief exists to close, and only the answer at it says which of the two this is.
  const way = (await person.locator('a[href="/app"]').first().getAttribute('href')) ?? '';
  expect(way, 'the refusal offers no way on').not.toBe('');
  expect((await person.goto(way))?.status(), `the refusal's way on ${way} answers nothing`).toBe(200);

  // Both kernel doors, one composition, this one refused person.
  const catalog = await person.request.get(catalogDoor);
  expect(catalog.status(), await catalog.text()).toBe(200);
  const document = await catalog.json();
  expect(typeof document.catalogVersion, 'the catalog door answered without a version').toBe('number');
  expect(Array.isArray(document.resources), 'the catalog door answered without a resource list').toBe(true);

  const asked = await person.request.post(jsonAskDoor, {
    data: { permission: 'task:read', path: refusedScreen },
  });
  expect(asked.status(), await asked.text()).toBe(202);

  // An ask is not a grant: the address that refused answers exactly as it did.
  expect((await person.goto(refusedScreen))?.status(), 'an ask granted something').toBe(403);

  // The person who holds BOTH administering roles, in their own browser. One ask
  // may write them one notice, whichever way the overlap query is written.
  const holderContext = await browser.newContext();
  const keeper = await holderContext.newPage();
  await keeper.goto('/app/admin/login');
  await keeper.getByLabel('Email').fill(holder.email);
  await keeper.getByLabel('Password').fill(holder.password);
  await keeper.getByRole('button', { name: 'Sign in' }).click();
  await expect(keeper).toHaveURL(/\/app$/);

  // Counted by the asker's row key, which is what the notice body carries: a count
  // of titles would also catch a notice from another spec on this shared fixture.
  const mine = async (client: typeof keeper.request): Promise<Notice[]> => {
    const bell = await client.get(noticeDoor);
    expect(bell.status()).toBe(200);
    const body = await bell.json();
    return ((body.items ?? []) as Notice[]).filter((n) => (n.body ?? '').includes(askerID));
  };

  await expect
    .poll(async () => (await mine(keeper.request)).length, { timeout: 10_000 })
    .toBe(1);
  const told = await mine(keeper.request);
  expect(told[0].body, 'the notice does not name the grant it asked for').toContain('task:read');
  expect(told[0].link, 'the notice carries no deep link to the person\'s own screen')
    .toBe(`/app/user/users/${askerID}`);

  // And the tenant's other holder of role management — its administrator, whose
  // wildcard covers the same grant — was told once too, by the same ask.
  expect((await mine(page.request)).length, 'the ask wrote the administrator more than one notice').toBe(1);

  await askerContext.close();
  await holderContext.close();
});
