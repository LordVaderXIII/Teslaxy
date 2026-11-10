import { readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { chromium } from '@playwright/test';

const publicDir = new URL('../public/', import.meta.url);
const svg = await readFile(new URL('favicon.svg', publicDir), 'utf8');
const browser = await chromium.launch({ headless: true });
try {
  const page = await browser.newPage({ deviceScaleFactor: 1 });
  for (const [size, filename] of [[32, 'favicon-32x32.png'], [180, 'apple-touch-icon.png'], [192, 'icon-192.png'], [512, 'icon-512.png']]) {
    await page.setViewportSize({ width: size, height: size });
    await page.setContent(`<body style="margin:0;background:#070806"><img width="${size}" height="${size}" src="data:image/svg+xml;base64,${Buffer.from(svg).toString('base64')}"></body>`);
    await page.locator('img').evaluate(img => img.decode());
    await page.screenshot({ path: fileURLToPath(new URL(filename, publicDir)), omitBackground: false });
  }
  // ICO directory containing the same 32px PNG (no separate logo source).
  const png = await readFile(new URL('favicon-32x32.png', publicDir));
  const header = Buffer.alloc(22);
  header.writeUInt16LE(1, 2);
  header.writeUInt16LE(1, 4);
  header[6] = 32;
  header[7] = 32;
  header.writeUInt16LE(1, 10);
  header.writeUInt16LE(32, 12);
  header.writeUInt32LE(png.length, 14);
  header.writeUInt32LE(header.length, 18);
  await writeFile(new URL('favicon.ico', publicDir), Buffer.concat([header, png]));
  console.log('Generated opaque icons from favicon.svg');
} finally {
  await browser.close();
}
