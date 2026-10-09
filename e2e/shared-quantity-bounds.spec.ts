import { expect, test } from '@playwright/test';

for (const language of ['en', 'pt-PT']) {
  test(`served quantity keeps exact bounds and localized validity (${language})`, async ({ page }) => {
    await page.goto('/app/admin/login');
    await page.getByLabel('Email').fill(process.env.PLATFORMKIT_E2E_EMAIL!);
    await page.getByLabel('Password').fill(process.env.PLATFORMKIT_E2E_PASSWORD!);
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await expect(page).toHaveURL(/\/app$/);

    const label = language === 'en' ? 'Quantity' : 'Quantidade';
    const previous = language === 'en' ? 'Previous' : 'Anterior';
    const next = language === 'en' ? 'Next' : 'Seguinte';
    const invalid = language === 'en' ? 'This quantity is not available.' : 'Esta quantidade não está disponível.';
    const props = {
      value: '9007199254740992', min: '9007199254740992',
      max: '9007199254740996', step: '2', validationText: invalid,
    };
    const preview = '/app/admin/_gallery/preview?' + new URLSearchParams({
      example: `pk-ui.component.quantity-input/default-${language}`,
      theme: 'dark', props: JSON.stringify(props),
    });
    expect((await page.goto(preview))?.status()).toBe(200);
    const input = page.getByRole('textbox', { name: label, exact: true });
    const decrease = page.getByRole('button', { name: previous, exact: true });
    const increase = page.getByRole('button', { name: next, exact: true });
    await expect(input).toHaveValue(props.value);
    await expect(decrease).toBeDisabled();

    await increase.focus();
    await page.keyboard.press('Enter');
    await expect(input).toHaveValue('9007199254740994');
    await page.keyboard.press('Enter');
    await expect(input).toHaveValue(props.max);
    await expect(increase).toBeDisabled();
    await expect(decrease).toBeEnabled();

    await input.fill('9007199254740995');
    await expect(decrease).toBeDisabled();
    await expect(increase).toBeDisabled();
    expect(await input.evaluate((element: HTMLInputElement) => ({
      valid: element.checkValidity(), message: element.validationMessage,
    }))).toEqual({ valid: false, message: invalid });

    await input.fill(props.min);
    await expect(decrease).toBeDisabled();
    await expect(increase).toBeEnabled();
    expect(await input.evaluate((element: HTMLInputElement) => element.checkValidity())).toBe(true);
  });
}
