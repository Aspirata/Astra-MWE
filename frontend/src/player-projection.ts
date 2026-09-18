import { Vector3, type Camera } from 'three';
import type { Vec3 } from './types';

export function playerScreenPosition(
  position: Vec3,
  origin: Vec3,
  camera: Camera,
  width: number,
  height: number,
) {
  const p = new Vector3(
    position[0] - origin[0],
    position[1] - origin[1],
    position[2] - origin[2],
  ).project(camera);
  if (!Number.isFinite(p.x) || p.z < -1 || p.z > 1 || Math.abs(p.x) > 1 || Math.abs(p.y) > 1)
    return null;
  return { x: ((p.x + 1) * width) / 2, y: ((1 - p.y) * height) / 2 };
}
