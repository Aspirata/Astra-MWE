import {
  Material,
  MeshStandardMaterial,
  NearestFilter,
  NearestMipmapNearestFilter,
  Texture,
} from 'three';

export function prepareWorldMaterial(material: Material) {
  // Hash coverage writes depth and avoids whole-world triangle sorting.
  // Keep binary cutouts and fractional alpha distinct.
  if (material.transparent) {
    material.alphaHash = true;
    material.transparent = false;
    material.depthWrite = true;
  }
  for (const value of Object.values(material)) {
    if (value instanceof Texture) {
      value.magFilter = NearestFilter;
      value.minFilter = NearestMipmapNearestFilter;
      value.needsUpdate = true;
    }
  }
  if (material instanceof MeshStandardMaterial) material.roughness = 1;
  material.needsUpdate = true;
}
