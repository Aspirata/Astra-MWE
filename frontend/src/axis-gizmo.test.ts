import { test } from 'node:test';
import assert from 'node:assert/strict';
import { PerspectiveCamera, Quaternion, Vector3 } from 'three';
import { projectAxes, axisDirection, chooseAxisSign } from './axis-gizmo.ts';

test('axis compass projects world axes into camera space with correct depth', () => {
  const axes = projectAxes(new Quaternion());
  assert.equal(axes.length, 6);
  const x = axes.find((a) => a.axis === 'x' && a.sign === 1)!;
  const y = axes.find((a) => a.axis === 'y' && a.sign === 1)!;
  assert.deepEqual([x.x, x.y, x.depth], [1, 0, 0]);
  assert.equal(y.y, -1);
  for (const axis of ['x', 'y', 'z'] as const) {
    const a = axes.find((a) => a.axis === axis && a.sign === 1)!;
    const b = axes.find((a) => a.axis === axis && a.sign === -1)!;
    assert.ok(Math.abs(a.x + b.x) + Math.abs(a.y + b.y) + Math.abs(a.depth + b.depth) < 1e-10);
  }
});

test('all six camera directions project the facing endpoint to the front center', () => {
  for (const axis of ['x', 'y', 'z'] as const)
    for (const sign of [-1, 1] as const) {
      const camera = new PerspectiveCamera();
      camera.position.copy(axisDirection(axis, sign)).multiplyScalar(10);
      camera.up.copy(axis === 'y' ? new Vector3(0, 0, -sign) : new Vector3(0, 1, 0));
      camera.lookAt(0, 0, 0);
      const point = projectAxes(camera.quaternion).find((a) => a.axis === axis && a.sign === sign)!;
      assert.ok(Math.hypot(point.x, point.y) < 1e-7);
      assert.ok(point.depth > 0.999999);
      assert.equal(chooseAxisSign(axis, sign, camera.position), -sign);
      assert.equal(chooseAxisSign(axis, -sign as -1 | 1, camera.position), -sign);
    }
});

test('oblique views keep the clicked axis sign and depth is sorted back to front', () => {
  const camera = new PerspectiveCamera();
  camera.position.set(4, 3, 5);
  camera.lookAt(0, 0, 0);
  const points = projectAxes(camera.quaternion);
  assert.ok(points.every((p, i) => !i || p.depth >= points[i - 1].depth));
  assert.equal(chooseAxisSign('x', 1, camera.position), 1);
  assert.equal(chooseAxisSign('y', -1, camera.position), -1);
});
