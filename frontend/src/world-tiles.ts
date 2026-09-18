import * as THREE from 'three';
import { selectionTiles } from './map-plan';
import { PreviewBatches, type TileHandle } from './preview-batches';
import type { DecodedTile } from './tile-protocol';
import type { Vec3, Bounds } from './types';

interface ViewConfig {
  world: string;
  dimension: string;
  resources: string[];
  hollowLeaves: boolean;
}
const concurrency = Math.max(1, Math.min(4, Math.floor((navigator.hardwareConcurrency || 4) / 2)));
const previewBytes = 256 * 1024 * 1024;

/** Owns the bounded 3D tile set, requests and idle decoder workers.
 * Export geometry is built separately: dropping a preview tile never changes a GLB.
 */
export class WorldTiles {
  private config?: ViewConfig;
  private signature = '';
  private generation = 0;
  private origin: Vec3 = [0, 0, 0];
  private tiles = new Map<string, TileHandle | null>();
  private desired: ReturnType<typeof selectionTiles> = [];
  private wanted = new Set<string>();
  private requests = new Map<string, AbortController>();
  private decoders: Worker[] = [];
  private paused = false;
  private errors = new Set<string>();
  private lastProgress = '';
  private batches: PreviewBatches;
  constructor(
    scene: THREE.Scene,
    private onProgress: (loaded: number, total: number) => void,
    private onError: (message: string) => void,
    renderer?: () => THREE.WebGLRenderer,
  ) {
    this.batches = new PreviewBatches(
      scene,
      () => {
        this.lastProgress = '';
        this.progress();
      },
      renderer,
    );
  }
  configure(config: ViewConfig, origin: Vec3) {
    const signature = JSON.stringify(config);
    if (signature === this.signature) return;
    this.clear();
    this.config = config;
    this.signature = signature;
    this.origin = origin;
  }
  pause(paused: boolean) {
    if (this.paused === paused) return;
    this.paused = paused;
    if (paused) {
      for (const request of this.requests.values()) request.abort();
    } else {
      this.lastProgress = '';
      this.progress();
      this.pump();
    }
  }
  clear() {
    // Fetch may have finished before cancellation. Decode completions must also
    // check their generation before adding resources to the current scene.
    this.generation++;
    for (const request of this.requests.values()) request.abort();
    for (const worker of this.decoders) worker.terminate();
    this.decoders = [];
    this.tiles.clear();
    this.desired = [];
    this.wanted.clear();
    this.config = undefined;
    this.signature = '';
    this.errors.clear();
    this.batches.clear();
    this.onProgress(0, 0);
  }
  update(bounds: Bounds) {
    if (!this.config) return;
    this.desired = selectionTiles(bounds);
    const wanted = (this.wanted = new Set(this.desired.map((t) => t.key)));
    for (const [key, request] of this.requests) if (!wanted.has(key)) request.abort();
    for (const [key, tile] of this.tiles)
      if (!wanted.has(key)) {
        this.tiles.delete(key);
        if (tile) this.batches.remove(tile);
      }
    this.progress();
    this.pump();
  }
  private progress() {
    // Called during upload slices: avoid allocating a filtered list each time.
    let loaded = 0;
    for (const key of this.wanted) if (this.tiles.has(key)) loaded++;
    const total = this.batches.bytes >= previewBytes ? loaded : this.desired.length,
      key = `${loaded}/${total}`;
    if (key !== this.lastProgress) {
      this.lastProgress = key;
      this.onProgress(loaded, total);
    }
  }
  private pump() {
    if (this.paused || !this.config || this.batches.bytes >= previewBytes) return;
    while (this.requests.size < concurrency) {
      const next = this.desired.find((t) => !this.tiles.has(t.key) && !this.requests.has(t.key));
      if (!next) break;
      const controller = new AbortController();
      this.requests.set(next.key, controller);
      void this.load(next, controller);
    }
  }
  private decode(buffer: ArrayBuffer, signal: AbortSignal): Promise<DecodedTile> {
    // One decode per worker. Transfer detaches the sender's buffer; a successful
    // result transfers it back together with ownership of the decoded images.
    const worker =
      this.decoders.pop() ||
      new Worker(new URL('./tile-decode.worker.ts', import.meta.url), { type: 'module' });
    return new Promise((resolve, reject) => {
      let done = false;
      const finish = (reuse: boolean) => {
        done = true;
        signal.removeEventListener('abort', abort);
        worker.onmessage = null;
        worker.onerror = null;
        if (reuse && !this.paused) this.decoders.push(worker);
        else worker.terminate();
      };
      const abort = () => {
        if (done) return;
        finish(false);
        reject(new DOMException('Aborted', 'AbortError'));
      };
      worker.onerror = (e) => {
        finish(false);
        reject(Error(e.message || 'Не удалось разобрать участок мира'));
      };
      worker.onmessage = (e: MessageEvent<DecodedTile & { error?: string }>) => {
        finish(true);
        if (e.data.error) reject(Error(e.data.error));
        else resolve(e.data);
      };
      signal.addEventListener('abort', abort, { once: true });
      if (signal.aborted) {
        abort();
        return;
      }
      worker.postMessage(buffer, [buffer]);
    });
  }
  private async load(next: ReturnType<typeof selectionTiles>[number], controller: AbortController) {
    const generation = this.generation,
      config = this.config;
    let tile: TileHandle | undefined;
    try {
      const response = await fetch('/api/view-tile', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...config, x: next.x, z: next.z }),
        signal: controller.signal,
      });
      if (!response.ok) {
        const error = await response.json();
        throw Error(error.error || 'Не удалось прочитать участок мира.');
      }
      if (response.status !== 204) {
        const origin = JSON.parse(response.headers.get('X-Astra-Origin') || 'null') as Vec3 | null;
        if (!origin || origin.length !== 3 || !origin.every(Number.isFinite))
          throw Error('Некорректные координаты участка мира.');
        const decoded = await this.decode(await response.arrayBuffer(), controller.signal);
        if (controller.signal.aborted || generation !== this.generation) {
          for (const image of decoded.images) image.close();
          return;
        }
        tile = await this.batches.add(
          decoded,
          origin.map((n, i) => n - this.origin[i]) as Vec3,
          controller.signal,
        );
      }
      if (
        controller.signal.aborted ||
        generation !== this.generation ||
        !this.wanted.has(next.key)
      ) {
        if (tile) this.batches.remove(tile);
        return;
      }
      this.tiles.set(next.key, tile || null);
    } catch (error) {
      if (tile) this.batches.remove(tile);
      if (!controller.signal.aborted && generation === this.generation) {
        this.tiles.set(next.key, null);
        const message = String(error);
        if (!this.errors.has(message)) {
          this.errors.add(message);
          this.onError(`Просмотр мира: ${message}`);
        }
      }
    } finally {
      if (this.requests.get(next.key) === controller) this.requests.delete(next.key);
      this.progress();
      this.pump();
    }
  }
}
