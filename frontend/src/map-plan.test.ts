import { test } from 'node:test';
import assert from 'node:assert/strict';
import { OrthographicCamera } from 'three';
import { mapWindow, mapTiles, selectionTiles } from './map-plan.ts';

test('map covers actual viewport, negative coordinates and adapts resolution at zoom-out', () => {
  const camera = new OrthographicCamera(-100, 100, 50, -50, 0.1, 5000);
  camera.position.set(0, 2000, 0);
  camera.up.set(0, 0, -1);
  camera.lookAt(0, 0, 0);
  const rect = mapWindow(camera, [3000000, 0, -3000000]);
  assert.equal(rect.minX, 2999900);
  assert.equal(rect.maxZ, -2999950);
  for (const r of [rect, { minX: -1000, maxX: 1000, minZ: -500, maxZ: 500 }]) {
    const tiles = mapTiles(r, 800, 400);
    assert.ok(tiles.length <= 64);
    assert.ok(tiles.length > 0);
    const size = tiles[0].step * 64;
    for (const x of [r.minX, r.maxX])
      for (const z of [r.minZ, r.maxZ])
        assert.ok(tiles.some((t) => t.x === Math.floor(x / size) && t.z === Math.floor(z / size)));
  }
  assert.ok(mapTiles({ minX: -1000, maxX: 1000, minZ: -500, maxZ: 500 }, 800, 400)[0].step > 1);
});
test('3D coverage encloses selection plus128 blocks and stays fixed when camera moves', () => {
  const b = { minX: -1, maxX: 63, minZ: 31, maxZ: 95, minY: 10, maxY: 70 };
  const tiles = selectionTiles(b);
  assert.equal(Math.min(...tiles.map((t) => t.x)), -5);
  assert.equal(Math.max(...tiles.map((t) => t.x)), 5);
  assert.equal(Math.min(...tiles.map((t) => t.z)), -4);
  assert.equal(Math.max(...tiles.map((t) => t.z)), 6);
  assert.deepEqual(tiles, selectionTiles({ ...b, minY: -64, maxY: 319 }));
});

test('close map zoom requests real texels and includes DPR in detail keys', () => {
  const rect = { minX: 0, maxX: 64, minZ: 0, maxZ: 64 };
  const regular = mapTiles(rect, 256, 256),
    retina = mapTiles(rect, 256, 256, 2);
  assert.equal(regular[0].step, 1);
  assert.equal(regular[0].detail, 4);
  assert.equal(retina[0].detail, 8);
  assert.notEqual(regular[0].key, retina[0].key);
  assert.equal(mapTiles(rect, 10000, 10000)[0].detail, 16);
  const moderate = mapTiles({ minX: 0, maxX: 600, minZ: 0, maxZ: 600 }, 1200, 1200);
  assert.equal(moderate[0].step, 1);
  assert.equal(moderate[0].detail, 2);
});
test('large export bounds keep a bounded 3D neighborhood around their center', () => {
  const tiles = selectionTiles({
    minX: -20000000,
    maxX: 20000000,
    minZ: -20000000,
    maxZ: 20000000,
    minY: -64,
    maxY: 319,
  });
  assert.ok(tiles.length > 0 && tiles.length <= 289);
  assert.ok(tiles.every((t) => Math.abs(t.x) <= 8 && Math.abs(t.z) <= 8));
});

test('world-sized map views never create an unbounded request list', () => {
  const tiles = mapTiles(
    { minX: -30000000, maxX: 29999999, minZ: -30000000, maxZ: 29999999 },
    1200,
    800,
  );
  assert.equal(tiles.length, 256);
  assert.ok(tiles.every((t) => t.step === 16 && t.detail === 1));
});
