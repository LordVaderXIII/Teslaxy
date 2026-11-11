import { test, expect } from '@playwright/test';
import { mkdirSync, existsSync, writeFileSync, readFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

// Real App + video.js + decoded MP4; only the local library/API data is a fixture.
// No private footage or GPS. Generate the fixture inside .git, never in the branch.
const artifacts = fileURLToPath(new URL('../../.git/', import.meta.url));
const video = `${artifacts}fixture/footage/RecentClips/2026-11-11_12-00-00-front.mp4`;
mkdirSync(`${artifacts}fixture/footage/RecentClips`, { recursive: true });
mkdirSync(`${artifacts}proof`, { recursive: true });
if (!existsSync(video)) {
  execFileSync('ffmpeg', ['-v', 'error', '-f', 'lavfi', '-i', 'testsrc2=size=960x600:rate=10',
    '-t', '60', '-c:v', 'libx264', '-preset', 'ultrafast', '-pix_fmt', 'yuv420p', video], { timeout: 60000 });
}
const timestamp = '2026-11-11T12:00:00Z';
const cameras = ['Front', 'Back', 'Left Pillar', 'Right Pillar', 'Left Repeater', 'Right Repeater'];
const files = cameras.map(camera => ({ camera, timestamp, file_path: `/fixture/${camera.replaceAll(' ', '-')}.mp4` }));
const samples = Array.from({ length: 600 }, (_, i) => ({
  frame_seq_no: i, vehicle_speed_mps: i < 100 ? 0 : 13.89,
  gear_state: 1, steering_wheel_angle: 12, accelerator_pedal_position: 35,
  brake_applied: false, autopilot_state: 1,
}));
const event = { id: 1, timestamp, event: 'Recent', city: 'HUD layout fixture', reason: '',
  source_dir: '/fixture/RecentClips', video_file_count: 6, preview_camera: 'Front',
  preview_path: files[0].file_path, preview_timestamp: timestamp, preview_seek_seconds: 30 };
const detail = { ...event, ID: 1, telemetry: {
  full_data_json: JSON.stringify(samples),
  file_timing_json: JSON.stringify([{ file_path: files[0].file_path, first_sample_index: 0,
    sample_count: samples.length, sample_times: samples.map((_, i) => i / 10), frame_duration: 0.1 }]),
} };
const baseline = process.env.HUD_BASELINE === '1';
const phase = baseline ? 'before' : 'after';
const cases = [
  { width: 1920, height: 1080, mode: 'six' },
  { width: 1920, height: 1080, mode: 'focus' },
  { width: 2560, height: 1440, mode: 'six' },
  { width: 2560, height: 1440, mode: 'focus' },
  { width: 390, height: 844, mode: 'single' },
];

for (const { width, height, mode } of cases) {
  test(`${width}x${height} ${mode}: telemetry leaves video dominant`, async ({ page }) => {
    await page.setViewportSize({ width, height });
    await page.clock.setFixedTime(new Date(timestamp));
    await page.route('**/api/**', async route => {
      const path = new URL(route.request().url()).pathname;
      if (path.startsWith('/api/video')) {
        // Match a real seekable HTTP video endpoint, including byte ranges.
        const body = readFileSync(video);
        const range = /^bytes=(\d+)-(\d*)$/.exec(route.request().headers().range || '');
        const start = range ? Number(range[1]) : 0;
        const end = range?.[2] ? Math.min(Number(range[2]), body.length - 1) : body.length - 1;
        return route.fulfill({ status: range ? 206 : 200, contentType: 'video/mp4',
          headers: { 'Accept-Ranges': 'bytes', ...(range ? { 'Content-Range': `bytes ${start}-${end}/${body.length}` } : {}) },
          body: body.subarray(start, end + 1) });
      }
      const json = path === '/api/library' ? { events: [event] }
        : path === '/api/library/files' ? { files }
        : path === '/api/clips/1' ? detail
        : path === '/api/transcode/status' ? { encoder: 'fixture', hw_accel: false }
        : path === '/api/version' ? { latestVersion: '0.5.3' } : {};
      await route.fulfill({ json });
    });
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.goto(process.env.HUD_BASE_URL || 'http://127.0.0.1:5173');
    if (mode === 'single') await page.getByRole('button', { name: 'VIEWER', exact: true }).click();
    const viewer = page.getByRole('main', { name: 'Footage viewer' });
    if (mode === 'focus') await viewer.getByRole('button', { name: 'FOCUS', exact: true }).click();
    const front = viewer.getByRole('button', { name: 'Focus Front camera', exact: true });
    await expect(front.locator('video')).toBeVisible();
    await expect.poll(() => front.locator('video').evaluate(v => v.readyState)).toBeGreaterThanOrEqual(2);
    const timeline = viewer.getByRole('slider', { name: 'Playback timeline' });
    await expect(timeline).toHaveAttribute('aria-valuemax', '60');
    // Seek through product controls. No injected React state or fake video clock.
    await viewer.getByRole('button', { name: 'Skip forward 15 seconds' }).click();
    await expect(timeline).toHaveAttribute('aria-valuenow', '15');
    await expect.poll(() => front.locator('video').evaluate(v => ({ time: v.currentTime, seeking: v.seeking, ready: v.readyState, error: v.error?.message })), { timeout: 15000 }).toMatchObject({ time: 15, seeking: false });
    await viewer.getByRole('button', { name: 'Skip forward 15 seconds' }).click();
    await expect(timeline).toHaveAttribute('aria-valuenow', '30');
    await expect.poll(() => front.locator('video').evaluate(v => ({ time: v.currentTime, seeking: v.seeking, ready: v.readyState })), { timeout: 15000 }).toMatchObject({ time: 30, seeking: false });
    await expect.poll(() => front.locator('video').evaluate(v => v.readyState)).toBeGreaterThanOrEqual(2);
    await expect(viewer.locator('.desk-hud-speed')).toHaveText('50');
    await expect(viewer.locator('.desk-hud')).toHaveAttribute('data-accelerator', '0.35');
    await expect(viewer.locator('.desk-hud-sync')).toHaveCount(0);
    await expect(viewer.getByRole('button', { name: 'Play', exact: true })).toBeVisible();
    await expect.poll(() => viewer.locator('video').evaluateAll(videos => videos.every(v => v.currentTime === 30 && !v.seeking && v.readyState >= 2)), { timeout: 15000 }).toBe(true);
    await page.mouse.move(0, 0);
    await page.screenshot({ path: `${artifacts}proof/${phase}-${width}x${height}-${mode}.png` });
    const measurement = await page.evaluate(() => {
      const hud = document.querySelector('.desk-hud').getBoundingClientRect();
      const stage = document.querySelector('.desk-stage').getBoundingClientRect();
      const front = document.querySelector('[aria-label="Focus Front camera"]').getBoundingClientRect();
      const box = r => ({ x: r.x, y: r.y, width: r.width, height: r.height, area: r.width * r.height });
      const intersection = (a, b) => Math.max(0, Math.min(a.right, b.right) - Math.max(a.left, b.left)) *
        Math.max(0, Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top));
      return { hud: box(hud), front: box(front), stage: box(stage),
        frontPercent: hud.width * hud.height / (front.width * front.height) * 100,
        stagePercent: hud.width * hud.height / (stage.width * stage.height) * 100,
        frontCoveredPercent: intersection(hud, front) / (front.width * front.height) * 100,
        stageCoveredPercent: intersection(hud, stage) / (stage.width * stage.height) * 100,
        withinFront: hud.left >= front.left && hud.right <= front.right && hud.top >= front.top && hud.bottom <= front.bottom,
        withinStage: hud.left >= stage.left && hud.right <= stage.right && hud.top >= stage.top && hud.bottom <= stage.bottom,
        noPageOverflow: document.documentElement.scrollWidth <= innerWidth,
      };
    });
    writeFileSync(`${artifacts}${phase}-${width}x${height}-${mode}.json`, JSON.stringify(measurement, null, 2));
    console.log(`${phase} ${width}x${height} ${mode}: ${JSON.stringify(measurement)}`);
    expect(errors).toEqual([]);
    if (!baseline) {
      expect(measurement.withinStage).toBe(true);
      expect(measurement.withinFront).toBe(true);
      expect(measurement.noPageOverflow).toBe(true);
      expect(measurement.hud.height).toBeLessThanOrEqual(64);
      expect(measurement.hud.width).toBeLessThanOrEqual(300);
      expect(measurement.frontPercent).toBeLessThan(10);
      expect(measurement.stagePercent).toBeLessThan(mode === 'single' ? 10 : 5);
      await expect(viewer.locator('.desk-hud-ap')).toBeVisible();
      await expect(viewer.locator('.desk-hud-blink').first()).toBeVisible();
    }
  });
}
