import { test } from 'node:test';
import assert from 'node:assert/strict';
import { MOUSE } from 'three';
import { middleMouseBinding, isFlyToggle, flyLook, framingDistance } from './navigation.ts';

test('focus fits the whole selection in narrow and wide viewports', () => {
  for (const aspect of [0.27, 1, 2.4]) {
    const radius = Math.hypot(65, 21, 65) / 2;
    const distance = framingDistance(radius, 48, aspect);
    const angularRadius = Math.asin(radius / distance);
    const vertical = (48 * Math.PI) / 360,
      horizontal = Math.atan(Math.tan(vertical) * aspect);
    assert.ok(angularRadius < vertical && angularRadius < horizontal);
  }
  assert.ok(framingDistance(50, 48, 0.27) > framingDistance(50, 48, 1));
});

test('middle mouse uses orbit, Shift pan via OrbitControls modifier, and Ctrl dolly', () => {
  assert.equal(
    middleMouseBinding({ ctrlKey: false, shiftKey: false, metaKey: false }, false),
    MOUSE.ROTATE,
  );
  // OrbitControls turns ROTATE into PAN when Shift is down.
  assert.equal(
    middleMouseBinding({ ctrlKey: false, shiftKey: true, metaKey: false }, false),
    MOUSE.ROTATE,
  );
  assert.equal(
    middleMouseBinding({ ctrlKey: true, shiftKey: false, metaKey: false }, false),
    MOUSE.DOLLY,
  );
  assert.equal(
    middleMouseBinding({ ctrlKey: true, shiftKey: true, metaKey: false }, false),
    MOUSE.DOLLY,
  );
  assert.equal(
    middleMouseBinding({ ctrlKey: false, shiftKey: false, metaKey: false }, true),
    MOUSE.PAN,
  );
  assert.equal(
    middleMouseBinding({ ctrlKey: true, shiftKey: false, metaKey: false }, true),
    MOUSE.DOLLY,
  );
});

test('fly toggle follows physical Backquote across keyboard layouts and ignores repeats', () => {
  assert.equal(isFlyToggle({ code: 'Backquote', shiftKey: true, repeat: false }), true);
  assert.equal(isFlyToggle({ code: 'Backquote', shiftKey: false, repeat: false }), false);
  assert.equal(isFlyToggle({ code: 'Backquote', shiftKey: true, repeat: true }), false);
  assert.equal(isFlyToggle({ code: 'KeyF', shiftKey: true, repeat: false }), false);
});

test('fly mouse look turns from current orientation and clamps vertical view', () => {
  assert.deepEqual(flyLook(0.3, 0.7, 10, -20), { pitch: 0.35, yaw: 0.6749999999999999 });
  const up = flyLook(0, 0, 0, -100000),
    down = flyLook(0, 0, 0, 100000);
  assert.ok(up.pitch < Math.PI / 2 && up.pitch > 1.5);
  assert.equal(down.pitch, -up.pitch);
});
