import { test } from 'node:test';
import assert from 'node:assert/strict';
import { OrthographicCamera } from 'three';
import { playerScreenPosition } from './player-projection.ts';

test('player icon follows top-down projection at large world coordinates and clips offscreen', () => {
  const camera = new OrthographicCamera(-50, 50, 50, -50, 0.1, 1000);
  camera.up.set(0, 0, -1);
  camera.position.set(0, 200, 0);
  camera.lookAt(0, 0, 0);
  camera.updateMatrixWorld();
  const point = playerScreenPosition(
    [300010, 64, -299990],
    [300000, 0, -300000],
    camera,
    800,
    600,
  )!;
  assert.ok(Math.abs(point.x - 480) < 1e-8 && Math.abs(point.y - 360) < 1e-8);
  assert.equal(
    playerScreenPosition([301000, 64, -300000], [300000, 0, -300000], camera, 800, 600),
    null,
  );
});
