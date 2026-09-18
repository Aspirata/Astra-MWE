import { readTile, type DecodedTile } from './tile-protocol';
const scope = self as unknown as {
  onmessage: (e: MessageEvent<ArrayBuffer>) => void;
  postMessage: (value: unknown, transfer: Transferable[]) => void;
};
scope.onmessage = async (e) => {
  const images: ImageBitmap[] = [];
  try {
    const decoded = readTile(e.data),
      imageKeys: string[] = [];
    // Decode one image at a time per worker, bounding transient image memory.
    for (const image of decoded.document.images || []) {
      const view = decoded.document.bufferViews[image.bufferView];
      if (!view) throw Error('Текстура участка отсутствует');
      const bytes = new Uint8Array(
        decoded.buffer,
        decoded.offset + (view.byteOffset || 0),
        view.byteLength,
      );
      const hash = await crypto.subtle.digest('SHA-256', bytes);
      imageKeys.push(
        [...new Uint8Array(hash)].map((n) => n.toString(16).padStart(2, '0')).join(''),
      );
      images.push(
        await createImageBitmap(new Blob([bytes], { type: image.mimeType }), {
          premultiplyAlpha: 'none',
          colorSpaceConversion: 'none',
        }),
      );
    }
    const result: DecodedTile = { ...decoded, images, imageKeys };
    scope.postMessage(result, [decoded.buffer, ...images]);
  } catch (error) {
    for (const image of images) image.close();
    scope.postMessage({ error: String(error) }, []);
  }
};
