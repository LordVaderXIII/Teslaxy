import assert from 'node:assert/strict';
import { chromium, expect } from '@playwright/test';
import { mkdir } from 'node:fs/promises';

// Run only against the operator-started, auth-off local fixture.
const base = 'http://127.0.0.1:18080';
const browser = await chromium.launch({ headless: true });
const proof = new URL('../../.git/proof/', import.meta.url).pathname;
await mkdir(proof, { recursive: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1080 }, recordVideo: { dir: proof, size: { width: 1440, height: 1080 } } });
const page = await context.newPage();
const settingsWrites = [];
page.on('request', request => {
  if (request.url().endsWith('/api/retention/settings') && request.method() === 'PUT') settingsWrites.push(request);
});
const pause = () => page.waitForTimeout(1000);
try {
  await page.goto(base);
  const library = page.getByRole('complementary', { name: 'Event library' });
  const viewer = page.getByRole('main', { name: 'Footage viewer' });
  const listProtect = library.getByRole('button', { name: /^Protect event / }).first();
  await expect(listProtect).toBeVisible({ timeout: 10000 });
  await pause();
  await listProtect.click();
  await expect(library.getByText('PROTECTED', { exact: true })).toHaveCount(1);
  await pause();
  await library.getByRole('button', { name: /^Unprotect event / }).click();
  await expect(library.getByText('PROTECTED', { exact: true })).toHaveCount(0);
  await pause();
  await viewer.getByRole('button', { name: 'Protect selected event', exact: true }).click();
  await expect(viewer.getByText('PROTECTED', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Show filters' }).click();
  await page.getByRole('checkbox', { name: 'Protected only', exact: true }).check();
  await expect(library.getByRole('button', { name: /^Unprotect event / })).toHaveCount(1);
  await expect(library.getByRole('button', { name: /^Protect event / })).toHaveCount(0);
  await page.getByRole('button', { name: 'Hide filters' }).click();
  await pause();
  await page.screenshot({ path: `${proof}protected-filter.png` });
  await page.getByRole('button', { name: 'Refresh library' }).click();
  await expect(library.getByText('PROTECTED', { exact: true })).toHaveCount(1);
  await viewer.getByRole('button', { name: 'Unprotect selected event', exact: true }).click();
  await expect(viewer.getByText('PROTECTED', { exact: true })).toHaveCount(0);
  await expect(library.getByText('No events shown.', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Show filters' }).click();
  await page.getByRole('checkbox', { name: 'Protected only', exact: true }).uncheck();
  await page.getByRole('button', { name: 'Hide filters' }).click();
  await pause();
  if (process.env.PROTECT_ONLY) {
    console.log('retention-ui-proof: PASS (list/player protect/unprotect, badge, filter, refresh)');
  } else {
    await page.getByRole('button', { name: 'Retention & storage', exact: true }).click();
    const settings = page.getByRole('dialog', { name: 'Retention & storage' });
    await expect(settings.getByRole('checkbox', { name: 'Enable retention', exact: true })).not.toBeChecked();
    await expect(settings.getByRole('checkbox', { name: 'Dry-run only', exact: true })).toBeChecked();
    assert.equal(settingsWrites.length, 0, 'opening settings must not write or enable retention');
    await settings.getByRole('checkbox', { name: 'Enable retention', exact: true }).check();
    await settings.getByRole('checkbox', { name: 'Dry-run only', exact: true }).uncheck();
    assert.equal(settingsWrites.length, 0, 'editing toggles must not save automatically');
    await settings.getByRole('checkbox', { name: 'Enable retention', exact: true }).uncheck();
    await settings.getByRole('checkbox', { name: 'Dry-run only', exact: true }).check();
    await settings.getByLabel('Max age (days)', { exact: true }).fill('45');
    await settings.getByLabel('Max size (GB)', { exact: true }).fill('200.5');
    const putPromise = page.waitForRequest(r => r.url().endsWith('/api/retention/settings') && r.method() === 'PUT');
    await settings.getByRole('button', { name: 'Save settings', exact: true }).click();
    const put = await putPromise;
    assert.deepEqual(Object.keys(put.postDataJSON()).sort(), ['dry_run', 'enabled', 'interval_hours', 'max_age_days', 'max_size_gb', 'min_file_age_minutes'].sort());
    assert.equal(put.headers()['x-requested-with'], 'XMLHttpRequest');
    await expect(settings.getByRole('status')).toHaveText('Settings saved.');
    await expect(settings.getByText(/Last dry-run:.*would delete/)).toBeVisible();
    await expect(settings.getByText(/Volume used/)).toBeVisible();
    assert.equal(settingsWrites.length, 1);
    await pause();
    await page.screenshot({ path: `${proof}retention-storage.png` });
    const saved = await (await context.request.get(`${base}/api/retention/settings`)).json();
    assert.equal(saved.enabled, false);
    assert.equal(saved.dry_run, true);
    assert.equal(saved.max_age_days, 45);
    assert.equal(saved.max_size_gb, 200.5);
    await settings.getByRole('button', { name: 'Close retention settings' }).click();
    await expect(page.getByRole('button', { name: 'Retention & storage', exact: true })).toBeFocused();
    await page.screenshot({ path: `${proof}footage-desk.png` });
    // Check native dialog keyboard dismissal and small-screen reflow too.
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole('button', { name: 'Retention & storage', exact: true }).click();
    await expect(settings.getByLabel('Max age (days)', { exact: true })).toHaveValue('45');
    await page.screenshot({ path: `${proof}retention-mobile.png` });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true);
    await page.keyboard.press('Escape');
    await expect(settings).not.toBeVisible();
    console.log('retention-ui-proof: PASS (real fixture; list/player protect/unprotect; persisted badge/filter; settings PUT+CSRF; disabled/dry-run preserved; history/storage; mobile; focus/Escape)');
  }
} finally {
  await context.close();
  await browser.close();
}
