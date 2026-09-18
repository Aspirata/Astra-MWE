import { Quaternion, Vector3 } from 'three';

export type Axis = 'x' | 'y' | 'z';
export type AxisSign = -1 | 1;
const axes: Axis[] = ['x', 'y', 'z'];

export function axisDirection(axis: Axis, sign: AxisSign): Vector3 {
  return new Vector3(axis === 'x' ? sign : 0, axis === 'y' ? sign : 0, axis === 'z' ? sign : 0);
}

// Camera coordinates keep the compass correct in orbit, orthographic and fly views.
export function projectAxes(rotation: Quaternion) {
  const inverse = rotation.clone().invert();
  return axes
    .flatMap((axis) =>
      ([-1, 1] as const).map((sign) => {
        const p = axisDirection(axis, sign).applyQuaternion(inverse);
        return { axis, sign, x: p.x, y: p.y === 0 ? 0 : -p.y, depth: p.z };
      }),
    )
    .sort((a, b) => a.depth - b.depth);
}

export function chooseAxisSign(axis: Axis, sign: AxisSign, cameraOffset: Vector3): AxisSign {
  // Clicking the facing endpoint again exposes the opposite side, even at a pole
  // where its rear endpoint is hidden behind it.
  return cameraOffset.clone().normalize().dot(axisDirection(axis, sign)) > 0.9999
    ? sign === 1
      ? -1
      : 1
    : sign;
}

export class AxisGizmo {
  private rotation?: Quaternion;
  private buttons = new Map<string, HTMLButtonElement>();
  private lines = new Map<string, SVGLineElement>();

  constructor(host: HTMLElement, onSelect: (axis: Axis, sign: AxisSign) => void) {
    host.classList.add('axis-gizmo');
    host.setAttribute('role', 'group');
    host.setAttribute(
      'aria-label',
      'Оси вида. Нажмите ось для смены ракурса; повторное нажатие — обратная сторона.',
    );
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('viewBox', '0 0 112 112');
    svg.setAttribute('aria-hidden', 'true');
    host.append(svg);
    const names = { x: 'справа', y: 'сверху', z: 'спереди' },
      opposite = { x: 'слева', y: 'снизу', z: 'сзади' };
    for (const axis of axes)
      for (const sign of [-1, 1] as const) {
        const key = `${axis}${sign}`;
        const line = document.createElementNS('http://www.w3.org/2000/svg', 'line');
        line.setAttribute('x1', '56');
        line.setAttribute('y1', '56');
        line.classList.add(`gizmo-${axis}`);
        svg.append(line);
        this.lines.set(key, line);
        const button = document.createElement('button');
        button.type = 'button';
        button.className = `axis-endpoint gizmo-${axis}${sign < 0 ? ' negative' : ''}`;
        button.textContent = sign > 0 ? axis.toUpperCase() : '';
        button.title = `Вид ${sign > 0 ? names[axis] : opposite[axis]} (${sign > 0 ? '+' : '−'}${axis.toUpperCase()})`;
        button.setAttribute('aria-label', button.title);
        button.setAttribute('aria-pressed', 'false');
        button.addEventListener('click', () => onSelect(axis, sign));
        host.append(button);
        this.buttons.set(key, button);
      }
  }

  update(rotation: Quaternion) {
    if (this.rotation && 1 - Math.abs(this.rotation.dot(rotation)) < 1e-12) return;
    this.rotation = rotation.clone();
    for (const [index, p] of projectAxes(rotation).entries()) {
      const key = `${p.axis}${p.sign}`,
        x = 56 + p.x * 36,
        y = 56 + p.y * 36;
      const button = this.buttons.get(key)!,
        line = this.lines.get(key)!;
      button.style.left = `${x}px`;
      button.style.top = `${y}px`;
      button.style.zIndex = String(index + 1);
      button.classList.toggle('rear', p.depth < -0.01);
      button.setAttribute('aria-pressed', String(p.depth > 0.9999));
      line.setAttribute('x2', String(x));
      line.setAttribute('y2', String(y));
      line.style.opacity = p.depth < -0.01 ? '.3' : '.85';
      line.parentNode!.appendChild(line);
    }
  }
}
