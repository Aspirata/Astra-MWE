import { Vector3, type OrthographicCamera } from 'three';
import type { Vec3, Bounds } from './types';

export interface MapRect {
  minX: number;
  maxX: number;
  minZ: number;
  maxZ: number;
}
export interface MapTile {
  x: number;
  z: number;
  step: number;
  detail: number;
  key: string;
}
export function mapWindow(camera: OrthographicCamera, origin: Vec3): MapRect {
  camera.updateMatrixWorld();
  const a = new Vector3(-1, 1, 0).unproject(camera),
    b = new Vector3(1, -1, 0).unproject(camera);
  return {
    minX: Math.min(a.x, b.x) + origin[0],
    maxX: Math.max(a.x, b.x) + origin[0],
    minZ: Math.min(a.z, b.z) + origin[2],
    maxZ: Math.max(a.z, b.z) + origin[2],
  };
}
export function mapTiles(rect: MapRect, width: number, height: number, ratio = 1): MapTile[] {
  if (
    ![...Object.values(rect), width, height, ratio].every(Number.isFinite) ||
    width <= 0 ||
    height <= 0 ||
    ratio <= 0
  )
    return [];
  const b = {
    minX: Math.max(-30000000, rect.minX),
    maxX: Math.min(29999999, rect.maxX),
    minZ: Math.max(-30000000, rect.minZ),
    maxZ: Math.min(29999999, rect.maxZ),
  };
  if (b.minX > b.maxX || b.minZ > b.maxZ) return [];
  let step = 1;
  const pixelSize = Math.max(
    (b.maxX - b.minX) / (width * ratio),
    (b.maxZ - b.minZ) / (height * ratio),
  );
  const count = (s: number) =>
    (Math.floor(b.maxX / (64 * s)) - Math.floor(b.minX / (64 * s)) + 1) *
    (Math.floor(b.maxZ / (64 * s)) - Math.floor(b.minZ / (64 * s)) + 1);
  while (step < 16 && (step < pixelSize || count(step) > 256)) step *= 2;
  let detail = 1;
  while (step === 1 && detail < 16 && detail < 1 / pixelSize) detail *= 2;
  while (detail > 1 && count(step) * 64 * 64 * detail * detail * 4 > 48 * 1024 * 1024) detail /= 2;
  const size = 64 * step,
    cx = (b.minX + b.maxX) / 2,
    cz = (b.minZ + b.maxZ) / 2,
    out: MapTile[] = [];
  let minX = Math.floor(b.minX / size),
    maxX = Math.floor(b.maxX / size),
    minZ = Math.floor(b.minZ / size),
    maxZ = Math.floor(b.maxZ / size);
  if (count(step) > 256) {
    const tx = Math.floor(cx / size),
      tz = Math.floor(cz / size);
    minX = Math.max(minX, tx - 7);
    maxX = Math.min(maxX, tx + 8);
    minZ = Math.max(minZ, tz - 7);
    maxZ = Math.min(maxZ, tz + 8);
  }
  for (let z = minZ; z <= maxZ; z++)
    for (let x = minX; x <= maxX; x++)
      out.push({ x, z, step, detail, key: `${step}:${detail}:${x},${z}` });
  return out.sort(
    (a, b) =>
      ((a.x + 0.5) * size - cx) ** 2 +
      ((a.z + 0.5) * size - cz) ** 2 -
      (((b.x + 0.5) * size - cx) ** 2 + ((b.z + 0.5) * size - cz) ** 2),
  );
}
export function selectionTiles(b: Bounds, padding = 128) {
  if (
    ![b.minX, b.maxX, b.minZ, b.maxZ].every(Number.isFinite) ||
    b.minX > b.maxX ||
    b.minZ > b.maxZ
  )
    return [];
  const centerX = (b.minX + b.maxX) / 2,
    centerZ = (b.minZ + b.maxZ) / 2;
  b = {
    ...b,
    minX: Math.max(b.minX, Math.floor(centerX - 128)),
    maxX: Math.min(b.maxX, Math.floor(centerX + 127)),
    minZ: Math.max(b.minZ, Math.floor(centerZ - 128)),
    maxZ: Math.min(b.maxZ, Math.floor(centerZ + 127)),
  };
  const minX = Math.max(-937500, Math.floor((b.minX - padding) / 32)),
    maxX = Math.min(937499, Math.floor((b.maxX + padding) / 32));
  const minZ = Math.max(-937500, Math.floor((b.minZ - padding) / 32)),
    maxZ = Math.min(937499, Math.floor((b.maxZ + padding) / 32));
  const cx = (b.minX + b.maxX) / 64,
    cz = (b.minZ + b.maxZ) / 64,
    out: { x: number; z: number; key: string }[] = [];
  for (let z = minZ; z <= maxZ; z++)
    for (let x = minX; x <= maxX; x++) out.push({ x, z, key: `${x},${z}` });
  return out.sort(
    (a, b) =>
      (a.x + 0.5 - cx) ** 2 +
      (a.z + 0.5 - cz) ** 2 -
      ((b.x + 0.5 - cx) ** 2 + (b.z + 0.5 - cz) ** 2),
  );
}
