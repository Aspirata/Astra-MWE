import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readTile } from './tile-protocol.ts';

function glb(offset = 0) {
  const doc = {
    bufferViews: [{ byteOffset: offset, byteLength: 12 }],
    accessors: [{ bufferView: 0, componentType: 5126, count: 1, type: 'VEC3' }],
    meshes: [],
    materials: [],
  };
  let json = JSON.stringify(doc);
  while (json.length % 4) json += ' ';
  const bytes = new ArrayBuffer(28 + json.length + 12),
    v = new DataView(bytes);
  v.setUint32(0, 0x46546c67, true);
  v.setUint32(4, 2, true);
  v.setUint32(8, bytes.byteLength, true);
  v.setUint32(12, json.length, true);
  v.setUint32(16, 0x4e4f534a, true);
  new Uint8Array(bytes, 20, json.length).set(new TextEncoder().encode(json));
  v.setUint32(20 + json.length, 12, true);
  v.setUint32(24 + json.length, 0x004e4942, true);
  return bytes;
}
test('tile protocol retains binary ownership without expanding vertex arrays', () => {
  const b = glb(),
    r = readTile(b);
  assert.equal(r.buffer, b);
  assert.equal(r.document.accessors[0].count, 1);
});
test('tile protocol rejects invalid lengths and out of bounds attributes', () => {
  assert.throws(() => readTile(glb(4)));
  const b = glb();
  new DataView(b).setUint32(8, 1, true);
  assert.throws(() => readTile(b));
});
