import assert from 'node:assert/strict';
import { chromium } from '@playwright/test';

const browser = await chromium.launch({ headless: true });
try {
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  await page.goto('http://127.0.0.1:18080/', { waitUntil: 'networkidle' });
  assert.equal(await page.title(), 'Teslaxy');
  assert.equal(await page.locator('link[rel="apple-touch-icon"]').getAttribute('href'), '/apple-touch-icon.png');
  assert.equal(await page.locator('meta[name="apple-mobile-web-app-title"]').getAttribute('content'), 'Teslaxy');
  assert.equal(await page.locator('link[rel="manifest"]').getAttribute('href'), '/site.webmanifest');
  const manifestResponse = await page.request.get('http://127.0.0.1:18080/site.webmanifest');
  assert.equal(manifestResponse.status(), 200);
  assert.match(manifestResponse.headers()['content-type'], /^application\/manifest\+json/);
  const manifest = await manifestResponse.json();
  assert.equal(manifest.name, 'Teslaxy');
  assert.equal(manifest.background_color, '#070806');
  // Headless screenshots have no tab chrome; show the actual document title.
  await page.evaluate(() => {
    const title = document.createElement('div');
    title.textContent = `Document title: ${document.title}`;
    title.style.cssText = 'position:fixed;top:0;left:35%;z-index:2147483647;background:#070806;color:white;padding:8px;font:16px sans-serif';
    document.body.append(title);
  });
  await page.screenshot({ path: '../.git/proof/teslaxy-app.png', fullPage: true });
  await page.goto('http://127.0.0.1:18080/apple-touch-icon.png');
  const image = page.locator('img');
  assert.equal(await image.evaluate(img => img.naturalWidth), 180);
  assert.equal(await image.evaluate(img => img.naturalHeight), 180);
  await page.screenshot({ path: '../.git/proof/apple-touch-icon.png' });
  console.log('PASS Playwright: document title, iOS metadata, manifest, icon dimensions; two screenshots saved');
} finally {
  await browser.close();
}
