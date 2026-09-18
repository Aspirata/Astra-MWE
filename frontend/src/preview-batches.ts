import * as THREE from 'three';
import type { DecodedTile, TileDocument } from './tile-protocol';
import { prepareWorldMaterial } from './materials.ts';
import type { Vec3 } from './types';

interface TextureEntry {
  texture: THREE.Texture;
  // Material pools share images; tile handles own geometry, not textures.
  refs: number;
  bytes: number;
}
interface Page {
  mesh: THREE.BatchedMesh;
  vertices: number;
  indices: number;
  live: number;
  dirty: boolean;
  stride: number;
}
interface Pool {
  material: THREE.MeshStandardMaterial;
  textures: string[];
  pages: Page[];
  arrayGroup?: string;
  array?: THREE.DataArrayTexture;
  layers?: Map<string, number>;
  capacity?: number;
}
export interface TileHandle {
  parts: { page: Page; geometry: number }[];
  epoch: number;
}
const MAX_PAGE_VERTICES = 65_536;
const MAX_PAGE_INDICES = 393_216;
const UPLOAD_SLICE_MS = 4;
const names: Record<string, [string, number]> = {
  POSITION: ['position', 3],
  NORMAL: ['normal', 3],
  TEXCOORD_0: ['uv', 2],
  COLOR_0: ['color', 4],
};

// Share identical materials/textures across tiles and draw their geometries in
// batches. CPU vertex arrays belong to the batches, never to a second tile copy.
export class PreviewBatches {
  private pools = new Map<string, Pool>();
  private textures = new Map<string, TextureEntry>();
  private epoch = 0;
  private arrayID = 0;
  private renderer?: () => THREE.WebGLRenderer;
  private scene: THREE.Scene;
  private changed: () => void;
  constructor(scene: THREE.Scene, changed: () => void, renderer?: () => THREE.WebGLRenderer) {
    this.scene = scene;
    this.changed = changed;
    this.renderer = renderer;
  }
  get bytes() {
    // Scheduling estimate, not total process/GPU memory: mipmaps and driver
    // overhead are excluded. In-flight requests may briefly exceed the budget.
    let bytes = 0;
    for (const p of this.pools.values()) {
      for (const page of p.pages) bytes += page.vertices * page.stride + page.indices * 4;
      if (p.array) bytes += p.array.image.width * p.array.image.height * p.array.image.depth * 4;
    }
    for (const t of this.textures.values()) bytes += t.bytes;
    return bytes;
  }
  private addArrayImage(pool: Pool, decoded: DecodedTile, imageIndex: number) {
    const key = decoded.imageKeys[imageIndex];
    if (pool.layers!.has(key)) return;
    const layer = pool.layers!.size,
      source = new THREE.Texture(decoded.images[imageIndex]);
    source.flipY = false;
    source.colorSpace = THREE.SRGBColorSpace;
    try {
      this.renderer!().copyTextureToTexture(
        source,
        pool.array!,
        null,
        new THREE.Vector3(0, 0, layer),
      );
      pool.layers!.set(key, layer);
    } finally {
      source.dispose();
    }
  }
  private texture(
    decoded: DecodedTile,
    index: number | undefined,
    used: Set<ImageBitmap>,
  ): [THREE.Texture | null, string | null] {
    if (index === undefined) return [null, null];
    const imageIndex = decoded.document.textures?.[index]?.source;
    if (imageIndex === undefined || !decoded.images[imageIndex])
      throw Error('Отсутствует текстура участка');
    const key = decoded.imageKeys[imageIndex];
    let entry = this.textures.get(key);
    if (!entry) {
      const image = decoded.images[imageIndex],
        texture = new THREE.Texture(image);
      texture.flipY = false;
      texture.colorSpace = THREE.SRGBColorSpace;
      texture.wrapS = texture.wrapT = THREE.RepeatWrapping;
      entry = { texture, refs: 0, bytes: image.width * image.height * 4 };
      this.textures.set(key, entry);
      used.add(image);
    }
    entry.refs++;
    return [entry.texture, key];
  }
  private pool(
    decoded: DecodedTile,
    materialIndex: number,
    attributes: Record<string, number>,
    used: Set<ImageBitmap>,
  ): Pool {
    const doc = decoded.document,
      m = doc.materials[materialIndex];
    if (!m) throw Error('Отсутствует материал участка');
    const pbr = m.pbrMetallicRoughness,
      imageKey = (index: number | undefined) =>
        index === undefined ? null : decoded.imageKeys[doc.textures![index].source];
    let key = JSON.stringify([
      m.alphaMode,
      m.alphaCutoff,
      m.doubleSided,
      pbr.baseColorFactor,
      m.emissiveFactor,
      imageKey(pbr.baseColorTexture?.index),
      imageKey(m.emissiveTexture?.index),
      Object.keys(attributes).sort(),
    ]);
    const existing = this.pools.get(key);
    if (existing) return existing;
    let array: THREE.DataArrayTexture | undefined,
      arrayGroup: string | undefined,
      capacity: number | undefined;
    const imageIndex =
      pbr.baseColorTexture === undefined
        ? undefined
        : doc.textures![pbr.baseColorTexture.index].source;
    // Independent texture-array layers retain repeat UVs, nearest texels and
    // sRGB/alpha. This is only GPU storage; exported PNGs/materials stay separate.
    if (
      this.renderer &&
      imageIndex !== undefined &&
      !m.emissiveTexture &&
      attributes.TEXCOORD_0 !== undefined
    ) {
      const image = decoded.images[imageIndex];
      capacity = Math.max(
        1,
        Math.min(128, Math.floor((8 * 1024 * 1024) / (image.width * image.height * 4))),
      );
      arrayGroup = JSON.stringify([
        m.alphaMode,
        m.alphaCutoff,
        m.doubleSided,
        pbr.baseColorFactor,
        m.emissiveFactor,
        Object.keys(attributes).sort(),
        image.width,
        image.height,
      ]);
      for (const pool of this.pools.values())
        if (
          pool.arrayGroup === arrayGroup &&
          (pool.layers!.has(decoded.imageKeys[imageIndex]) || pool.layers!.size < pool.capacity!)
        ) {
          this.addArrayImage(pool, decoded, imageIndex);
          return pool;
        }
      array = new THREE.DataArrayTexture(null, image.width, image.height, capacity);
      array.colorSpace = THREE.SRGBColorSpace;
      array.wrapS = array.wrapT = THREE.RepeatWrapping;
      array.generateMipmaps = true;
      array.minFilter = THREE.NearestMipmapNearestFilter;
      array.needsUpdate = true;
      this.renderer().initTexture(array);
      key = `array:${this.arrayID++}`;
    }
    const [map, mapKey]: [THREE.Texture | null, string | null] = array
        ? [array, null]
        : this.texture(decoded, pbr.baseColorTexture?.index, used),
      [emissiveMap, emissiveKey] = this.texture(decoded, m.emissiveTexture?.index, used);
    const factor = pbr.baseColorFactor || [1, 1, 1, 1];
    const material = new THREE.MeshStandardMaterial({
      name: m.name,
      color: new THREE.Color().setRGB(factor[0], factor[1], factor[2]),
      opacity: factor[3],
      map,
      emissiveMap,
      emissive: new THREE.Color().fromArray(m.emissiveFactor || [0, 0, 0]),
      metalness: 0,
      roughness: 1,
      vertexColors: attributes.COLOR_0 !== undefined,
      side: m.doubleSided ? THREE.DoubleSide : THREE.FrontSide,
      transparent: m.alphaMode === 'BLEND',
      alphaTest: m.alphaMode === 'MASK' ? (m.alphaCutoff ?? 0.5) : 0,
    });
    prepareWorldMaterial(material);
    if (array) {
      // The texture is initialized once; only newly encountered layers are copied.
      material.onBeforeCompile = (shader) => {
        shader.vertexShader =
          'attribute float astraLayer; varying float vAstraLayer;\n' + shader.vertexShader;
        shader.vertexShader = shader.vertexShader.replace(
          '#include <uv_vertex>',
          '#include <uv_vertex>\nvAstraLayer = astraLayer;',
        );
        shader.fragmentShader = 'varying float vAstraLayer;\n' + shader.fragmentShader;
        shader.fragmentShader = shader.fragmentShader.replace(
          '#include <map_pars_fragment>',
          'uniform highp sampler2DArray map;',
        );
        shader.fragmentShader = shader.fragmentShader.replace(
          '#include <map_fragment>',
          'diffuseColor *= texture(map, vec3(vMapUv, vAstraLayer));',
        );
      };
      material.customProgramCacheKey = () => 'astra-texture-array-v1';
    }
    const pool: Pool = {
      material,
      textures: [mapKey, emissiveKey].filter((k): k is string => k !== null),
      pages: [],
      array,
      arrayGroup,
      capacity,
      layers: array ? new Map() : undefined,
    };
    this.pools.set(key, pool);
    if (array) this.addArrayImage(pool, decoded, imageIndex!);
    return pool;
  }
  private geometry(decoded: DecodedTile, p: TileDocument['meshes'][number]['primitives'][number]) {
    // Views borrow the transferred GLB until BatchedMesh copies the attributes.
    const geometry = new THREE.BufferGeometry(),
      doc = decoded.document;
    for (const [name, index] of Object.entries(p.attributes)) {
      const mapping = names[name];
      if (!mapping) continue;
      const a = doc.accessors[index],
        view = doc.bufferViews[a.bufferView];
      geometry.setAttribute(
        mapping[0],
        new THREE.BufferAttribute(
          new Float32Array(
            decoded.buffer,
            decoded.offset + (view.byteOffset || 0) + (a.byteOffset || 0),
            a.count * mapping[1],
          ),
          mapping[1],
        ),
      );
    }
    const a = doc.accessors[p.indices],
      view = doc.bufferViews[a.bufferView];
    geometry.setIndex(
      new THREE.BufferAttribute(
        new Uint32Array(
          decoded.buffer,
          decoded.offset + (view.byteOffset || 0) + (a.byteOffset || 0),
          a.count,
        ),
        1,
      ),
    );
    const position = doc.accessors[p.attributes.POSITION];
    if (position.min && position.max) {
      geometry.boundingBox = new THREE.Box3(
        new THREE.Vector3().fromArray(position.min),
        new THREE.Vector3().fromArray(position.max),
      );
      geometry.boundingSphere = geometry.boundingBox.getBoundingSphere(new THREE.Sphere());
    }
    return geometry;
  }
  private page(pool: Pool, geometry: THREE.BufferGeometry): Page {
    const vertices = geometry.getAttribute('position').count,
      indices = geometry.index!.count;
    let stride = 0;
    for (const a of Object.values(geometry.attributes)) stride += a.itemSize * 4;
    for (const page of pool.pages) {
      if (page.live >= page.mesh.maxInstanceCount) continue;
      if (page.dirty) {
        page.mesh.optimize();
        page.dirty = false;
      }
      if (page.mesh.unusedVertexCount >= vertices && page.mesh.unusedIndexCount >= indices)
        return page;
      // Grow modestly; use another page above this bound instead of recopying a
      // whole material's terrain whenever a distant tile appears.
      const v = Math.max(page.vertices + vertices, Math.ceil(page.vertices * 1.5)),
        i = Math.max(page.indices + indices, Math.ceil(page.indices * 1.5));
      if (v <= MAX_PAGE_VERTICES && i <= MAX_PAGE_INDICES) {
        page.mesh.setGeometrySize(v, i);
        page.vertices = v;
        page.indices = i;
        return page;
      }
    }
    const v = Math.max(256, Math.ceil(vertices * 1.2)),
      i = Math.max(384, Math.ceil(indices * 1.2)),
      mesh = new THREE.BatchedMesh(256, v, i, pool.material);
    mesh.sortObjects = false;
    mesh.perObjectFrustumCulled = true;
    mesh.frustumCulled = false;
    const page: Page = { mesh, vertices: v, indices: i, live: 0, dirty: false, stride };
    pool.pages.push(page);
    this.scene.add(mesh);
    return page;
  }
  async add(decoded: DecodedTile, translation: Vec3, signal: AbortSignal): Promise<TileHandle> {
    const handle: TileHandle = { parts: [], epoch: this.epoch },
      used = new Set<ImageBitmap>(),
      matrix = new THREE.Matrix4().makeTranslation(...translation);
    let slice = performance.now();
    try {
      for (const mesh of decoded.document.meshes)
        for (const primitive of mesh.primitives) {
          signal.throwIfAborted();
          const pool = this.pool(decoded, primitive.material, primitive.attributes, used),
            geometry = this.geometry(decoded, primitive);
          if (pool.array) {
            const material = decoded.document.materials[primitive.material],
              imageIndex =
                decoded.document.textures![material.pbrMetallicRoughness.baseColorTexture!.index]
                  .source,
              layer = pool.layers!.get(decoded.imageKeys[imageIndex])!;
            geometry.setAttribute(
              'astraLayer',
              new THREE.BufferAttribute(
                new Float32Array(geometry.getAttribute('position').count).fill(layer),
                1,
              ),
            );
          }
          try {
            const page = this.page(pool, geometry),
              id = page.mesh.addGeometry(geometry),
              instance = page.mesh.addInstance(id);
            page.mesh.setMatrixAt(instance, matrix);
            page.live++;
            handle.parts.push({ page, geometry: id });
          } finally {
            geometry.dispose();
          }
          if (performance.now() - slice > UPLOAD_SLICE_MS) {
            this.changed();
            await new Promise<void>((resolve) => setTimeout(resolve, 0));
            slice = performance.now();
          }
        }
      this.changed();
      return handle;
    } catch (error) {
      this.remove(handle);
      throw error;
    } finally {
      // Regular textures retain their bitmap until the last material releases
      // it. Array layers have already been uploaded, so their bitmaps can close.
      for (const image of decoded.images) if (!used.has(image)) image.close();
    }
  }
  remove(handle: TileHandle) {
    // An aborted old load must never delete geometry from a replacement scene.
    if (handle.epoch !== this.epoch) return;
    for (const part of handle.parts) {
      part.page.mesh.deleteGeometry(part.geometry);
      part.page.live--;
      part.page.dirty = true;
    }
    handle.parts = [];
    for (const [key, pool] of this.pools) {
      pool.pages = pool.pages.filter((page) => {
        if (page.live) return true;
        this.scene.remove(page.mesh);
        page.mesh.dispose();
        return false;
      });
      if (pool.pages.length) continue;
      pool.material.dispose();
      pool.array?.dispose();
      for (const key of pool.textures) {
        const entry = this.textures.get(key)!;
        if (--entry.refs === 0) {
          entry.texture.dispose();
          (entry.texture.image as ImageBitmap).close();
          this.textures.delete(key);
        }
      }
      this.pools.delete(key);
    }
    this.changed();
  }
  clear() {
    this.epoch++;
    for (const pool of this.pools.values()) {
      for (const page of pool.pages) {
        this.scene.remove(page.mesh);
        page.mesh.dispose();
      }
      pool.material.dispose();
      pool.array?.dispose();
    }
    for (const t of this.textures.values()) {
      t.texture.dispose();
      (t.texture.image as ImageBitmap).close();
    }
    this.pools.clear();
    this.textures.clear();
    this.changed();
  }
}
