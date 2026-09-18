import { MOUSE } from 'three';

export function framingDistance(radius: number, fovDegrees: number, aspect: number) {
  const vertical = (fovDegrees * Math.PI) / 360;
  const horizontal = Math.atan(Math.tan(vertical) * Math.max(0.01, aspect));
  return (1.1 * Math.max(1, radius)) / Math.sin(Math.min(vertical, horizontal));
}

export function middleMouseBinding(
  event: Pick<MouseEvent, 'ctrlKey' | 'shiftKey' | 'metaKey'>,
  top: boolean,
) {
  // ROTATE already becomes PAN for Shift in OrbitControls. DOLLY bypasses that modifier switch.
  if (event.ctrlKey || event.metaKey) return MOUSE.DOLLY;
  return top && !event.shiftKey ? MOUSE.PAN : MOUSE.ROTATE;
}

export function isFlyToggle(event: Pick<KeyboardEvent, 'code' | 'shiftKey' | 'repeat'>) {
  return event.code === 'Backquote' && event.shiftKey && !event.repeat;
}

export function flyLook(pitch: number, yaw: number, dx: number, dy: number) {
  const limit = Math.PI / 2 - 0.01;
  return { pitch: Math.max(-limit, Math.min(limit, pitch - dy * 0.0025)), yaw: yaw - dx * 0.0025 };
}
