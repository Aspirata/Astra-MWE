import type { Bounds, Vec3 } from './types.ts';
export function selectionFromDrag(
  a: [number, number],
  b: [number, number],
  height: Pick<Bounds, 'minY' | 'maxY'>,
): Bounds {
  return {
    minX: Math.floor(Math.min(a[0], b[0])),
    maxX: Math.floor(Math.max(a[0], b[0])),
    minZ: Math.floor(Math.min(a[1], b[1])),
    maxZ: Math.floor(Math.max(a[1], b[1])),
    minY: height.minY,
    maxY: height.maxY,
  };
}
export function relativePosition(position: Vec3, origin: Vec3): Vec3 {
  return position.map((n, i) => n - origin[i]) as Vec3;
}
export function centeredBounds(position: Vec3, minY: number, maxY: number): Bounds {
  return {
    minX: Math.floor(position[0]) - 32,
    maxX: Math.floor(position[0]) + 32,
    minZ: Math.floor(position[2]) - 32,
    maxZ: Math.floor(position[2]) + 32,
    minY,
    maxY,
  };
}
export function validateBounds(bounds: Bounds): void {
  if (!Object.values(bounds).every(Number.isSafeInteger))
    throw Error('Координаты выделения должны быть целыми числами.');
  const size = [
    bounds.maxX - bounds.minX + 1,
    bounds.maxY - bounds.minY + 1,
    bounds.maxZ - bounds.minZ + 1,
  ];
  if (size.some((n) => n <= 0)) throw Error('Минимум каждой оси не может превышать максимум.');
  if (
    bounds.minX < -30_000_000 ||
    bounds.maxX > 30_000_000 ||
    bounds.minZ < -30_000_000 ||
    bounds.maxZ > 30_000_000
  )
    throw Error('Координаты вне границ Minecraft.');
  if (bounds.minY < -2048 || bounds.maxY > 2047) throw Error('Неподдерживаемый диапазон высот.');
}
