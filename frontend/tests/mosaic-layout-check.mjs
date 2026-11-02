/**
 * Spatial multi-camera mosaic: tile order matches where cameras point,
 * no feed mirroring, and tile click still enters full-view (focus).
 *
 * Run: node frontend/tests/mosaic-layout-check.mjs
 */
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const playerSrc = readFileSync(join(root, 'src/components/Player.tsx'), 'utf8');
const cssSrc = readFileSync(join(root, 'src/index.css'), 'utf8');

function assert(condition, message) {
  if (!condition) {
    console.error(`FAIL: ${message}`);
    process.exit(1);
  }
}

function mustInclude(src, snippet, label) {
  assert(src.includes(snippet), label);
}

// --- Spatial column order (driver view) ---
mustInclude(playerSrc, "const MOSAIC_LEFT = ['Left Pillar', 'Left Repeater']", 'left column: pillar then repeater');
mustInclude(playerSrc, "const MOSAIC_MIDDLE = ['Front', 'Back']", 'middle column: front then back');
mustInclude(playerSrc, "const MOSAIC_RIGHT = ['Right Pillar', 'Right Repeater']", 'right column: pillar then repeater');
mustInclude(playerSrc, 'data-mosaic-layout="spatial"', 'mosaic marks spatial layout');
mustInclude(playerSrc, 'data-mosaic-col="left"', 'left column marker');
mustInclude(playerSrc, 'data-mosaic-col="middle"', 'middle column marker');
mustInclude(playerSrc, 'data-mosaic-col="right"', 'right column marker');
mustInclude(playerSrc, "renderMosaicTile(camName, camName === 'Front')", 'Front is the featured (largest) tile');

// Old secondary/feature mosaic must be gone
assert(!playerSrc.includes('MOSAIC_SECONDARY'), 'legacy MOSAIC_SECONDARY removed');
assert(
  !/desk-camera-grid[\s\S]{0,200}secondaryCameras/.test(playerSrc),
  'secondaryCameras mosaic path removed'
);

// --- Full-view click path (no regressions) ---
mustInclude(playerSrc, 'const focusCamera = useCallback((camName: string) => {', 'focusCamera helper exists');
mustInclude(playerSrc, "setViewMode('focus')", 'focusCamera enters focus/full view');
mustInclude(playerSrc, 'onClick={() => focusCamera(camName)}', 'mosaic tiles call focusCamera');
mustInclude(playerSrc, "onClick={() => { setViewMode('six'); setIs3D(false); }}", 'SIX CAMERAS returns to mosaic');
mustInclude(playerSrc, 'syncPeers', 'playback syncPeers still wired');
mustInclude(playerSrc, 'shouldCorrectDrift', 'inter-camera drift correction still wired');

// --- CSS: 3-column spatial grid, equal sides, front > back ---
mustInclude(cssSrc, '.desk-camera-grid', 'camera grid rule present');
mustInclude(cssSrc, 'grid-template-columns: minmax(0, 1fr) minmax(0, 2.2fr) minmax(0, 1fr)', 'three columns; middle widest');
mustInclude(cssSrc, '.desk-cam-col-side', 'side column class');
mustInclude(cssSrc, '.desk-cam-col-mid', 'middle column class');
mustInclude(cssSrc, 'grid-template-rows: 1fr 1fr', 'side tiles equal height');
mustInclude(cssSrc, 'grid-template-rows: minmax(0, 1.65fr) minmax(0, 1fr)', 'front row taller than back');

// --- No mirror / scaleX on mosaic feeds ---
assert(!/scaleX\s*\(\s*-1\s*\)/.test(cssSrc), 'index.css must not scaleX(-1) any feed');
assert(!/scaleX\s*\(\s*-1\s*\)/.test(playerSrc), 'Player.tsx must not scaleX(-1) any feed');
mustInclude(cssSrc, 'transform: none !important', 'mosaic videos explicitly un-mirrored');
assert(
  /\.desk-camera video[\s\S]*?transform:\s*none\s*!important/.test(cssSrc),
  'desk-camera video transform none'
);

// Phone keeps mosaic hidden (chips + single focus camera)
assert(
  /@media \(max-width: 767px\)[\s\S]*?\.desk-camera-grid\s*\{\s*display:\s*none/.test(cssSrc),
  'phone still hides mosaic grid'
);

console.log('mosaic-layout-check: ok');
