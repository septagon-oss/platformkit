import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { expect, test } from '@playwright/test';

const snapshot = JSON.parse(execFileSync('go', ['run', './tools/designexport'], {
  cwd: resolve(__dirname, '..'), encoding: 'utf8', maxBuffer: 8 * 1024 * 1024,
}));

test('map tiles stay on the application origin before locations are rendered', async ({ page }) => {
  await page.goto('/app/admin/login');
  await page.getByLabel('Email').fill(process.env.PLATFORMKIT_E2E_EMAIL!);
  await page.getByLabel('Password').fill(process.env.PLATFORMKIT_E2E_PASSWORD!);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).toHaveURL(/\/app$/);

  const example = 'pk-ui.component.map-view/map-en';
  const props = snapshot.examples.find((entry: { id: string }) => entry.id === example).props;
  expect(props.points.length).toBeGreaterThan(0);
  const preview = (input: typeof props) => '/app/admin/_gallery/preview?' + new URLSearchParams({
    example, theme: 'light', props: JSON.stringify(input),
  });

  expect((await page.goto(preview(props)))?.status()).toBe(200);
  await expect(page.locator('[data-map-config]')).toHaveAttribute('data-map-config', new RegExp(props.points[0].id));

  for (const urlTemplate of [
    '//other.example/tiles/{z}/{x}/{y}.png',
    'https://other.example/tiles/{z}/{x}/{y}.png',
  ]) {
    const altered = structuredClone(props);
    altered.tiles.urlTemplate = urlTemplate;
    const refused = await page.request.get(preview(altered));
    expect(refused.status(), urlTemplate).toBeGreaterThanOrEqual(400);
    expect(await refused.text(), urlTemplate).not.toContain('data-map-config');
  }
});
