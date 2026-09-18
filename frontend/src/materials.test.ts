import test from 'node:test';
import assert from 'node:assert/strict';
import { MeshStandardMaterial } from 'three';
import { prepareWorldMaterial } from './materials.ts';

test('fractional alpha uses hashed depth coverage without discarding alpha', () => {
  const m = new MeshStandardMaterial({ transparent: true, opacity: 0.4, depthWrite: false });
  prepareWorldMaterial(m);
  assert.equal(m.transparent, false);
  assert.equal(m.alphaHash, true);
  assert.equal(m.depthWrite, true);
  assert.equal(m.opacity, 0.4);
  assert.equal(m.alphaTest, 0);
});
test('cutout and opaque materials retain their coverage', () => {
  for (const cutoff of [0, 0.5]) {
    const m = new MeshStandardMaterial({ alphaTest: cutoff });
    prepareWorldMaterial(m);
    assert.equal(m.alphaTest, cutoff);
    assert.equal(m.alphaHash, false);
  }
});
