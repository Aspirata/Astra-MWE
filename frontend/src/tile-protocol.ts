// Private preview protocol: the subset of GLB produced by our Go exporter.
// Decode off-thread, transfer ownership of the binary buffer and decoded images.
export interface TileDocument {
  accessors: {
    bufferView: number;
    byteOffset?: number;
    componentType: number;
    count: number;
    type: string;
    min?: number[];
    max?: number[];
  }[];
  bufferViews: { byteOffset?: number; byteLength: number; byteStride?: number }[];
  meshes: {
    name: string;
    primitives: { attributes: Record<string, number>; indices: number; material: number }[];
  }[];
  materials: {
    name: string;
    doubleSided?: boolean;
    alphaMode?: string;
    alphaCutoff?: number;
    pbrMetallicRoughness: { baseColorFactor?: number[]; baseColorTexture?: { index: number } };
    emissiveTexture?: { index: number };
    emissiveFactor?: number[];
  }[];
  textures?: { source: number }[];
  images?: { bufferView: number; mimeType: string; name?: string }[];
}
export interface DecodedTile {
  document: TileDocument;
  buffer: ArrayBuffer;
  offset: number;
  images: ImageBitmap[];
  imageKeys: string[];
}
export function readTile(buffer: ArrayBuffer): Omit<DecodedTile, 'images' | 'imageKeys'> {
  const view = new DataView(buffer);
  if (
    buffer.byteLength < 28 ||
    view.getUint32(0, true) !== 0x46546c67 ||
    view.getUint32(4, true) !== 2 ||
    view.getUint32(8, true) !== buffer.byteLength
  )
    throw Error('Некорректная модель участка');
  const jsonLength = view.getUint32(12, true),
    binHeader = 20 + jsonLength;
  if (
    view.getUint32(16, true) !== 0x4e4f534a ||
    binHeader + 8 > buffer.byteLength ||
    view.getUint32(binHeader + 4, true) !== 0x004e4942
  )
    throw Error('Некорректные данные участка');
  const offset = binHeader + 8,
    binLength = view.getUint32(binHeader, true);
  if (offset + binLength !== buffer.byteLength) throw Error('Неполные данные участка');
  const document = JSON.parse(
    new TextDecoder().decode(new Uint8Array(buffer, 20, jsonLength)),
  ) as TileDocument;
  for (const v of document.bufferViews) {
    const start = v.byteOffset || 0;
    if (
      !Number.isSafeInteger(start) ||
      start < 0 ||
      !Number.isSafeInteger(v.byteLength) ||
      v.byteLength < 0 ||
      start + v.byteLength > binLength ||
      v.byteStride
    )
      throw Error('Некорректный буфер участка');
  }
  for (const a of document.accessors) {
    const v = document.bufferViews[a.bufferView],
      size = { SCALAR: 1, VEC2: 2, VEC3: 3, VEC4: 4 }[a.type],
      start = a.byteOffset || 0;
    if (
      !v ||
      !size ||
      ![5125, 5126].includes(a.componentType) ||
      !Number.isSafeInteger(a.count) ||
      a.count < 0 ||
      start < 0 ||
      start % 4 ||
      ((v.byteOffset || 0) + offset) % 4 ||
      start + a.count * size * 4 > v.byteLength
    )
      throw Error('Некорректная геометрия участка');
  }
  return { document, buffer, offset };
}
