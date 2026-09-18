import { mapTiles, type MapTile, type MapRect } from './map-plan';
import type { Bounds } from './types';

interface MapConfig {
  world: string;
  dimension: string;
  resources: string[];
}
interface Entry {
  tile: MapTile;
  image: ImageBitmap | null;
  used: number;
  retryAt?: number;
}
/** Owns surface PNGs independently of the 3D scene and export selection.
 * Coarse cached tiles remain visible until sharper replacements are ready.
 */
export class MapTiles {
  readonly canvas = document.createElement('canvas');
  private context: CanvasRenderingContext2D;
  private config?: MapConfig;
  private signature = '';
  private generation = 0;
  private cache = new Map<string, Entry>();
  private desired: MapTile[] = [];
  private active = false;
  private requests = new Map<string, AbortController>();
  private readonly concurrency = Math.min(
    4,
    Math.max(1, Math.floor((navigator.hardwareConcurrency || 4) / 2)),
  );
  private readonly budget = 64 * 1024 * 1024;
  private cacheBytes = 0;
  private tick = 0;
  private drawSignature = '';
  private errors = new Set<string>();
  private revision = 0;
  constructor(
    host: HTMLElement,
    private progress: (loaded: number, total: number) => void,
    private error: (message: string) => void,
  ) {
    this.canvas.className = 'surface-map';
    this.canvas.style.cssText =
      'position:absolute;inset:0;width:100%;height:100%;pointer-events:none;display:none';
    this.canvas.setAttribute('role', 'img');
    this.canvas.setAttribute('aria-label', 'Карта Minecraft сверху');
    this.context = this.canvas.getContext('2d', { alpha: false })!;
    host.append(this.canvas);
  }
  configure(config: MapConfig) {
    const signature = JSON.stringify(config);
    if (signature === this.signature) return;
    this.clear();
    this.config = config;
    this.signature = signature;
  }
  setActive(active: boolean) {
    if (this.active === active) return;
    this.active = active;
    this.canvas.style.display = active ? 'block' : 'none';
    if (!active) for (const controller of this.requests.values()) controller.abort();
    else {
      this.revision++;
      this.report();
      void this.pump();
    }
  }
  clear() {
    this.generation++;
    for (const controller of this.requests.values()) controller.abort();
    for (const entry of this.cache.values()) entry.image?.close();
    this.cache.clear();
    this.cacheBytes = 0;
    this.desired = [];
    this.config = undefined;
    this.signature = '';
    this.errors.clear();
    this.revision++;
    this.report();
  }
  update(rect: MapRect, width: number, height: number) {
    if (!this.config || !this.active) return;
    this.desired = mapTiles(rect, width, height, Math.min(devicePixelRatio || 1, 2));
    const wanted = new Set(this.desired.map((t) => t.key));
    for (const [key, controller] of this.requests) if (!wanted.has(key)) controller.abort();
    for (const t of this.desired) {
      const entry = this.cache.get(t.key);
      if (entry?.retryAt && entry.retryAt < Date.now()) this.cache.delete(t.key);
      else if (entry) entry.used = ++this.tick;
    }
    this.evict(wanted);
    this.report();
    void this.pump();
  }
  private report() {
    if (this.active)
      this.progress(this.desired.filter((t) => this.cache.has(t.key)).length, this.desired.length);
  }
  private evict(wanted = new Set(this.desired.map((t) => t.key))) {
    while (this.cacheBytes > this.budget || this.cache.size > 512) {
      let oldest: Entry | undefined;
      for (const e of this.cache.values())
        if (!wanted.has(e.tile.key) && (!oldest || e.used < oldest.used)) oldest = e;
      if (!oldest) break;
      this.cacheBytes -= (oldest.image?.width || 0) * (oldest.image?.height || 0) * 4;
      oldest.image?.close();
      this.cache.delete(oldest.tile.key);
    }
  }
  private pump() {
    if (!this.active || !this.config) return;
    while (this.requests.size < this.concurrency) {
      const tile = this.desired.find((t) => !this.cache.has(t.key) && !this.requests.has(t.key));
      if (!tile) break;
      void this.load(tile);
    }
  }
  private async load(tile: MapTile) {
    if (!this.config) return;
    const config = this.config,
      generation = this.generation,
      controller = new AbortController();
    this.requests.set(tile.key, controller);
    let image: ImageBitmap | undefined;
    try {
      const response = await fetch('/api/map-tile', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          ...config,
          x: tile.x,
          z: tile.z,
          step: tile.step,
          detail: tile.detail,
        }),
        signal: controller.signal,
      });
      if (!response.ok) {
        const data = await response.json();
        throw Error(data.error || 'Не удалось загрузить карту');
      }
      if (response.status !== 204) image = await createImageBitmap(await response.blob());
      if (controller.signal.aborted || generation !== this.generation) {
        image?.close();
        return;
      }
      this.cache.set(tile.key, { tile, image: image || null, used: ++this.tick });
      this.cacheBytes += (image?.width || 0) * (image?.height || 0) * 4;
      this.evict();
      this.revision++;
    } catch (error) {
      image?.close();
      if (!controller.signal.aborted && generation === this.generation) {
        const message = String(error);
        if (!this.errors.has(message)) {
          this.errors.add(message);
          this.error(`Карта: ${message}`);
        }
        this.cache.set(tile.key, {
          tile,
          image: null,
          used: ++this.tick,
          retryAt: Date.now() + 10000,
        });
      }
    } finally {
      if (this.requests.get(tile.key) === controller) this.requests.delete(tile.key);
      this.report();
      void this.pump();
    }
  }
  draw(rect: MapRect, bounds: Bounds, width: number, height: number) {
    if (!this.active || !width || !height) return;
    const ratio = Math.min(devicePixelRatio, 2),
      signature = JSON.stringify([rect, bounds, width, height, ratio, this.revision]);
    if (signature === this.drawSignature) return;
    this.drawSignature = signature;
    const ctx = this.context;
    if (
      this.canvas.width !== Math.round(width * ratio) ||
      this.canvas.height !== Math.round(height * ratio)
    ) {
      this.canvas.width = Math.round(width * ratio);
      this.canvas.height = Math.round(height * ratio);
    }
    ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
    ctx.fillStyle = '#171a20';
    ctx.fillRect(0, 0, width, height);
    ctx.imageSmoothingEnabled = false;
    const sx = width / (rect.maxX - rect.minX),
      sz = height / (rect.maxZ - rect.minZ);
    // Retain the previous resolution as a fallback until the new tiles arrive.
    const entries = [...this.cache.values()].sort(
      (a, b) => b.tile.step / b.tile.detail - a.tile.step / a.tile.detail,
    );
    for (const { tile, image } of entries) {
      if (!image) continue;
      const size = 64 * tile.step,
        x = tile.x * size,
        z = tile.z * size;
      if (x > rect.maxX || z > rect.maxZ || x + size < rect.minX || z + size < rect.minZ) continue;
      const left = (x - rect.minX) * sx,
        top = (z - rect.minZ) * sz;
      ctx.drawImage(image, left, top, size * sx + 0.2, size * sz + 0.2);
    }
    ctx.strokeStyle = '#91b9ee';
    ctx.lineWidth = 1;
    ctx.fillStyle = '#77a8e511';
    const x = (bounds.minX - rect.minX) * sx,
      z = (bounds.minZ - rect.minZ) * sz,
      w = (bounds.maxX - bounds.minX + 1) * sx,
      h = (bounds.maxZ - bounds.minZ + 1) * sz;
    ctx.fillRect(x, z, w, h);
    ctx.strokeRect(x, z, w, h);
  }
}
