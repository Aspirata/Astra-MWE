import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  selectionFromDrag,
  relativePosition,
  centeredBounds,
  validateBounds,
} from './selection.ts';
test('top-down reverse drag rounds negative world positions and preserves height', () => {
  assert.deepEqual(selectionFromDrag([-4.2, 9.8], [-11.8, -3.1], { minY: -64, maxY: 220 }), {
    minX: -12,
    maxX: -5,
    minZ: -4,
    maxZ: 9,
    minY: -64,
    maxY: 220,
  });
});
test('teleport subtracts preview origin including vertical origin', () => {
  assert.deepEqual(relativePosition([103, 64, -120], [96, 48, -128]), [7, 16, 8]);
});
test('top-down selection replaces horizontal bounds when passed the current full selection', () => {
  const previous = { minX: -32, maxX: 32, minZ: -32, maxZ: 32, minY: -64, maxY: 319 };
  assert.deepEqual(selectionFromDrag([5.2, 6.8], [10.4, 12.1], previous), {
    minX: 5,
    maxX: 10,
    minZ: 6,
    maxZ: 12,
    minY: -64,
    maxY: 319,
  });
});
test('start bounds follow player with supported world height', () => {
  assert.deepEqual(centeredBounds([-10.4, 64, 17.7], -64, 319), {
    minX: -43,
    maxX: 21,
    minZ: -15,
    maxZ: 49,
    minY: -64,
    maxY: 319,
  });
});
test('invalid, fractional and out-of-world selections cannot be submitted', () => {
  assert.throws(() => validateBounds({ minX: 2, maxX: 1, minY: 0, maxY: 10, minZ: 0, maxZ: 1 }));
  assert.throws(() => validateBounds({ minX: 0.5, maxX: 1, minY: 0, maxY: 10, minZ: 0, maxZ: 1 }));
  assert.doesNotThrow(() =>
    validateBounds({ minX: 0, maxX: 4096, minY: 0, maxY: 319, minZ: 0, maxZ: 4096 }),
  );
  assert.throws(() =>
    validateBounds({ minX: 0, maxX: 30_000_001, minY: 0, maxY: 319, minZ: 0, maxZ: 4096 }),
  );
  assert.throws(() =>
    validateBounds({ minX: 0, maxX: 1, minY: -2049, maxY: 319, minZ: 0, maxZ: 1 }),
  );
});
