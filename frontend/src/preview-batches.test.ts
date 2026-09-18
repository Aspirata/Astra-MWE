import { test } from 'node:test';
import assert from 'node:assert/strict';
import * as THREE from 'three';
import { PreviewBatches } from './preview-batches.ts';
import type { DecodedTile } from './tile-protocol.ts';

function tile(): DecodedTile {
  const buffer = new ArrayBuffer(48 + 24);
  new Float32Array(buffer, 0, 12).set([0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0]);
  new Uint32Array(buffer, 48, 6).set([0, 1, 2, 0, 2, 3]);
  return {
    buffer,
    offset: 0,
    images: [],
    imageKeys: [],
    document: {
      bufferViews: [
        { byteOffset: 0, byteLength: 48 },
        { byteOffset: 48, byteLength: 24 },
      ],
      accessors: [
        {
          bufferView: 0,
          componentType: 5126,
          count: 4,
          type: 'VEC3',
          min: [0, 0, 0],
          max: [1, 1, 0],
        },
        { bufferView: 1, componentType: 5125, count: 6, type: 'SCALAR' },
      ],
      materials: [{ name: 'stone', pbrMetallicRoughness: {} }],
      meshes: [
        { name: 'World', primitives: [{ attributes: { POSITION: 0 }, indices: 1, material: 0 }] },
      ],
    },
  };
}
test('identical materials across tiles share one draw batch and release their buffers', async () => {
  const scene = new THREE.Scene(),
    b = new PreviewBatches(scene, () => {}),
    signal = new AbortController().signal;
  const a = await b.add(tile(), [0, 0, 0], signal),
    c = await b.add(tile(), [32, 0, 0], signal);
  assert.equal(scene.children.length, 1);
  const mesh = scene.children[0] as THREE.BatchedMesh;
  assert.equal(mesh.instanceCount, 2);
  const matrix = new THREE.Matrix4();
  mesh.getMatrixAt(1, matrix);
  assert.equal(matrix.elements[12], 32);
  b.remove(a);
  assert.equal(mesh.instanceCount, 1);
  assert.ok(b.bytes > 0);
  b.remove(c);
  assert.equal(scene.children.length, 0);
  assert.equal(b.bytes, 0);
});
test('cancelled and stale tile handles cannot leave or delete current geometry', async () => {
  const scene = new THREE.Scene(),
    b = new PreviewBatches(scene, () => {}),
    controller = new AbortController();
  controller.abort();
  await assert.rejects(b.add(tile(), [0, 0, 0], controller.signal));
  assert.equal(scene.children.length, 0);
  const a = await b.add(tile(), [0, 0, 0], new AbortController().signal);
  b.clear();
  const c = await b.add(tile(), [0, 0, 0], new AbortController().signal);
  b.remove(a);
  assert.equal(scene.children.length, 1);
  b.remove(c);
  assert.equal(b.bytes, 0);
});

test('distinct block textures share a draw batch with separate layers and alpha modes', async () => {
  const scene = new THREE.Scene(),
    uploads: number[] = [],
    closed: string[] = [];
  const renderer = {
    initTexture() {},
    copyTextureToTexture(
      _source: THREE.Texture,
      _target: THREE.Texture,
      _region: unknown,
      position: THREE.Vector3,
    ) {
      uploads.push(position.z);
    },
  } as unknown as THREE.WebGLRenderer;
  const b = new PreviewBatches(
      scene,
      () => {},
      () => renderer,
    ),
    signal = new AbortController().signal;
  function textured(key: string, alphaMode = 'OPAQUE') {
    const t = tile(),
      buffer = new ArrayBuffer(104);
    new Uint8Array(buffer).set(new Uint8Array(t.buffer));
    t.buffer = buffer;
    new Float32Array(t.buffer, 72, 8).set([0, 0, 1, 0, 1, 1, 0, 1]);
    t.document.bufferViews.push({ byteOffset: 72, byteLength: 32 });
    t.document.accessors.push({ bufferView: 2, componentType: 5126, count: 4, type: 'VEC2' });
    t.document.meshes[0].primitives[0].attributes.TEXCOORD_0 = 2;
    t.document.materials[0].pbrMetallicRoughness.baseColorTexture = { index: 0 };
    t.document.materials[0].alphaMode = alphaMode;
    t.document.textures = [{ source: 0 }];
    t.imageKeys = [key];
    t.images = [
      {
        width: 16,
        height: 16,
        close() {
          closed.push(key);
        },
      } as ImageBitmap,
    ];
    return t;
  }
  const a = await b.add(textured('stone'), [0, 0, 0], signal),
    c = await b.add(textured('dirt'), [1, 0, 0], signal),
    d = await b.add(textured('stone'), [2, 0, 0], signal);
  assert.equal(scene.children.length, 1);
  assert.deepEqual(uploads, [0, 1]);
  const mesh = scene.children[0] as THREE.BatchedMesh,
    layer = mesh.geometry.getAttribute('astraLayer');
  assert.deepEqual(
    Array.from({ length: 12 }, (_, i) => layer.getX(i)),
    [0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 0, 0],
  );
  const glass = await b.add(textured('glass', 'BLEND'), [3, 0, 0], signal);
  assert.equal(scene.children.length, 2);
  assert.equal((scene.children[1] as THREE.BatchedMesh).material.alphaHash, true);
  assert.deepEqual(closed, ['stone', 'dirt', 'stone', 'glass']);
  b.remove(a);
  b.remove(c);
  assert.equal(scene.children.length, 2);
  b.remove(d);
  b.remove(glass);
  assert.equal(b.bytes, 0);
});
