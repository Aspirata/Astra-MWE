import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createAutoBuildScheduler } from './auto-build.ts';

test('selection changes debounce for 600 ms and build only the latest bounds', (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  let selection = 1;
  const built: number[] = [];
  const scheduler = createAutoBuildScheduler(
    () => true,
    () => built.push(selection),
  );
  scheduler.schedule();
  t.mock.timers.tick(400);
  selection = 2;
  scheduler.schedule();
  t.mock.timers.tick(599);
  assert.deepEqual(built, []);
  t.mock.timers.tick(1);
  assert.deepEqual(built, [2]);
  t.mock.timers.tick(1000);
  assert.deepEqual(built, [2]);
});

test('unready inputs prevent a queued build, and explicit actions cancel it', (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  let ready = true,
    calls = 0;
  const scheduler = createAutoBuildScheduler(
    () => ready,
    () => calls++,
  );
  scheduler.schedule();
  ready = false;
  t.mock.timers.tick(600);
  assert.equal(calls, 0);
  ready = true;
  scheduler.schedule();
  scheduler.cancel();
  t.mock.timers.tick(600);
  assert.equal(calls, 0);
  scheduler.schedule();
  t.mock.timers.tick(600);
  assert.equal(calls, 1);
});

test('a change made during a request builds once when the request finishes', (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  let ready = false,
    calls = 0;
  const scheduler = createAutoBuildScheduler(
    () => ready,
    () => calls++,
  );
  scheduler.schedule();
  t.mock.timers.tick(600);
  assert.equal(calls, 0);
  ready = true;
  scheduler.resume();
  assert.equal(calls, 1);
  scheduler.resume();
  t.mock.timers.tick(1000);
  assert.equal(calls, 1);
});

test('resuming during debounce does not build early; cancellation drops deferred work', (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  let ready = true,
    calls = 0;
  const scheduler = createAutoBuildScheduler(
    () => ready,
    () => calls++,
  );
  scheduler.schedule();
  t.mock.timers.tick(300);
  scheduler.resume();
  assert.equal(calls, 0);
  t.mock.timers.tick(300);
  assert.equal(calls, 1);
  ready = false;
  scheduler.schedule();
  t.mock.timers.tick(600);
  scheduler.cancel();
  ready = true;
  scheduler.resume();
  assert.equal(calls, 1);
});
