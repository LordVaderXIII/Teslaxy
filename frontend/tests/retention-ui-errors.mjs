import assert from 'node:assert/strict';
import { chromium, expect } from '@playwright/test';

const base = 'http://127.0.0.1:18080';
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1080 } });
const page = await context.newPage();
let cleanupId;
try {
  const fixtureLibrary = await (await context.request.get(`${base}/api/library`)).json();
  assert.ok(fixtureLibrary.events.some(event => event.city === 'Fixture Saved'), 'refuse to mutate a non-fixture library');
  await page.goto(base);
  const library = page.getByRole('complementary', { name: 'Event library' });
  const viewer = page.getByRole('main', { name: 'Footage viewer' });
  const control = library.getByRole('button', { name: /^Protect event / }).first();
  await expect(control).toBeVisible();
  const id = Number((await control.getAttribute('aria-label')).split(' ').at(-1));
  cleanupId = id;
  const changeProtection = async value => {
    const response = await context.request.post(`${base}/api/clips/${id}/protect`, { headers: { Origin: base, 'X-Requested-With': 'XMLHttpRequest' }, data: { protected: value } });
    assert.equal(response.ok(), true);
  };
  // A library refresh must reconcile the selected player's badge too.
  await changeProtection(true);
  await page.getByRole('button', { name: 'Refresh library' }).click();
  await expect(viewer.getByText('PROTECTED', { exact: true })).toBeVisible();
  await changeProtection(false);
  await page.getByRole('button', { name: 'Refresh library' }).click();
  await expect(viewer.getByText('PROTECTED', { exact: true })).toHaveCount(0);
  // Inject an explicit access failure. Do not replace successful fixture APIs.
  await page.route('**/api/clips/*/protect', route => route.fulfill({ status: 403, json: { error: 'Test access failure' } }));
  await viewer.getByRole('button', { name: 'Protect selected event', exact: true }).click();
  await expect(viewer.getByRole('alert')).toContainText('Protection could not be fully saved');
  await expect(viewer.getByText('PROTECTED', { exact: true })).toHaveCount(0);
  await expect(viewer.getByRole('button', { name: 'Protect selected event', exact: true })).toBeEnabled();
  await page.unroute('**/api/clips/*/protect');
  await page.getByRole('button', { name: 'Retention & storage', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Retention & storage' });
  await expect(dialog.getByLabel('Max age (days)', { exact: true })).toBeVisible();
  await page.route('**/api/retention/settings', route => route.request().method() === 'PUT' ? route.fulfill({ status: 403, json: { error: 'Test access failure' } }) : route.continue());
  await dialog.getByLabel('Max age (days)', { exact: true }).fill('46');
  await dialog.getByRole('button', { name: 'Save settings', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('403');
  await expect(dialog.getByLabel('Max age (days)', { exact: true })).toHaveValue('46');
  await expect(dialog.getByRole('button', { name: 'Save settings', exact: true })).toBeEnabled();
  await page.unroute('**/api/retention/settings');
  await page.route('**/api/retention/storage', route => route.fulfill({ status: 503, json: { error: 'Test storage failure' } }));
  await dialog.getByRole('button', { name: 'Reload retention & storage', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('Could not load storage');
  await expect(dialog.getByRole('button', { name: 'Save settings', exact: true })).toBeEnabled();
  await page.unroute('**/api/retention/storage');
  await dialog.getByRole('button', { name: 'Reload retention & storage', exact: true }).click();
  await expect(dialog.getByText(/Volume used/)).toBeVisible();
  console.log('retention-ui-errors: PASS (selected badge reconciles; injected protect/save denial; rollback; draft preserved; storage failure isolated; recovery)');
} finally {
  if (cleanupId) await context.request.post(`${base}/api/clips/${cleanupId}/protect`, { headers: { Origin: base, 'X-Requested-With': 'XMLHttpRequest' }, data: { protected: false } });
  await context.close();
  await browser.close();
}
