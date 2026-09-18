import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';
import type { Bounds, Vec3 } from './types';
import { relativePosition, selectionFromDrag } from './selection';
import { middleMouseBinding, isFlyToggle, flyLook, framingDistance } from './navigation';
import { AxisGizmo, chooseAxisSign, type Axis, type AxisSign } from './axis-gizmo';
import { WorldTiles } from './world-tiles';
import { MapTiles } from './map-tiles';
import { mapWindow } from './map-plan';
import { playerScreenPosition } from './player-projection';
import type { WorldInfo, Settings, Player } from './types';

export class Viewport {
  private renderer: THREE.WebGLRenderer;
  private scene = new THREE.Scene();
  private perspective = new THREE.PerspectiveCamera(48, 1, 0.1, 100000);
  private ortho = new THREE.OrthographicCamera(-50, 50, 50, -50, 0.1, 100000);
  private camera: THREE.PerspectiveCamera | THREE.OrthographicCamera = this.perspective;
  private controls: OrbitControls;
  private selection?: THREE.LineSegments;
  private grid = new THREE.GridHelper(4096, 256, 0x373f4c, 0x262c35);
  private ray = new THREE.Raycaster();
  private origin: Vec3 = [0, 0, 0];
  private bounds: Bounds = { minX: -32, maxX: 32, minY: 0, maxY: 255, minZ: -32, maxZ: 32 };
  private top = false;
  private selecting = false;
  private start?: [number, number];
  private locked = false;
  private dragBounds?: Bounds;
  private dragPointer?: number;
  private keys = new Set<string>();
  private last = performance.now();
  private gizmo: AxisGizmo;
  private worldTiles: WorldTiles;
  private mapTiles: MapTiles;
  private needsRender = true;
  private worldID = '';
  private tileTime = 0;
  private markers: HTMLElement;
  private players: { player: Player; button: HTMLButtonElement }[] = [];
  private playerSignature = '';
  private dimension = '';
  private fly = false;
  private flyDistance = 1;
  onSelection?: (bounds: Bounds) => void;
  onSelectionStart?: () => void;
  onCoordinates?: (position: Vec3) => void;
  onError?: (message: string) => void;
  onViewChange?: (top: boolean, orthographic: boolean) => void;
  onFlyChange?: (active: boolean) => void;
  onWorldProgress?: (loaded: number, total: number) => void;

  constructor(
    private host: HTMLElement,
    gizmoHost: HTMLElement,
    markerHost: HTMLElement,
  ) {
    this.markers = markerHost;
    this.worldTiles = new WorldTiles(
      this.scene,
      (loaded, total) => {
        this.needsRender = true;
        if (!this.top) this.onWorldProgress?.(loaded, total);
      },
      (message) => this.onError?.(message),
      () => this.renderer,
    );
    this.renderer = new THREE.WebGLRenderer({
      antialias: true,
      alpha: true,
      powerPreference: 'high-performance',
    });
    this.renderer.setPixelRatio(Math.min(devicePixelRatio, 2));
    this.renderer.outputColorSpace = THREE.SRGBColorSpace;
    this.renderer.toneMapping = THREE.ACESFilmicToneMapping;
    this.renderer.toneMappingExposure = 1.25;
    host.append(this.renderer.domElement);
    this.mapTiles = new MapTiles(
      host,
      (loaded, total) => this.onWorldProgress?.(loaded, total),
      (message) => this.onError?.(message),
    );
    this.renderer.domElement.tabIndex = 0;
    this.renderer.domElement.setAttribute(
      'aria-label',
      'Interactive Minecraft world viewport. Middle mouse to orbit, Shift and middle mouse to pan, wheel to zoom. Numpad 1, 3, 7 for views; 5 for projection; decimal to focus selection.',
    );
    this.scene.add(new THREE.HemisphereLight(0xdff8ff, 0x929477, 2.5));
    const sun = new THREE.DirectionalLight(0xfff3db, 2.4);
    sun.position.set(-90, 180, 100);
    this.scene.add(sun);
    this.scene.add(this.grid);
    this.perspective.position.set(90, 85, 110);
    this.controls = this.makeControls();
    this.gizmo = new AxisGizmo(gizmoHost, (axis, sign) => this.selectAxis(axis, sign));
    new ResizeObserver(() => this.resize()).observe(host);
    this.resize();
    host.addEventListener('contextmenu', (e) => e.preventDefault());
    host.addEventListener(
      'pointerdown',
      (e) => {
        this.controls.mouseButtons.MIDDLE = middleMouseBinding(e, this.top);
        if (this.fly) {
          this.renderer.domElement.focus();
          e.stopPropagation();
          e.preventDefault();
          return;
        }
        if (this.top && e.button === 0) {
          this.pointerDown(e);
          e.stopPropagation();
          e.preventDefault();
        }
      },
      true,
    );
    host.addEventListener('pointerdown', (e) => this.pointerDown(e));
    host.addEventListener('pointermove', (e) => this.pointerMove(e));
    host.addEventListener('pointerup', (e) => this.pointerUp(e));
    host.addEventListener('pointercancel', () => this.cancelSelection());
    host.addEventListener('keydown', (e) => this.keyDown(e));
    host.addEventListener('keyup', (e) => this.keys.delete(e.code));
    host.addEventListener('focusout', () => this.setFly(false));
    window.addEventListener('blur', () => this.setFly(false));
    document.addEventListener('pointerlockchange', () => {
      if (this.fly && document.pointerLockElement !== this.renderer.domElement) this.setFly(false);
    });
    document.addEventListener('mousemove', (e) => {
      if (
        !this.fly ||
        (document.pointerLockElement !== this.renderer.domElement &&
          e.target !== this.renderer.domElement)
      )
        return;
      const rotation = new THREE.Euler().setFromQuaternion(this.camera.quaternion, 'YXZ');
      const look = flyLook(rotation.x, rotation.y, e.movementX, e.movementY);
      this.camera.quaternion.setFromEuler(new THREE.Euler(look.pitch, look.yaw, 0, 'YXZ'));
      this.needsRender = true;
      this.controls.target
        .copy(this.camera.position)
        .addScaledVector(this.camera.getWorldDirection(new THREE.Vector3()), this.flyDistance);
    });
    this.renderer.domElement.addEventListener('webglcontextlost', (e) => {
      e.preventDefault();
      this.onError?.(
        'Графический контекст потерян. Выделение и экспорт доступны. Перезагрузите окно для восстановления просмотра.',
      );
    });
    this.renderer.setAnimationLoop(() => this.animate());
  }
  private makeControls() {
    const controls = new OrbitControls(this.camera, this.renderer.domElement);
    controls.enableDamping = false;
    controls.maxDistance = 10000;
    controls.minDistance = 1;
    controls.enableRotate = !this.top;
    controls.screenSpacePanning = true;
    controls.mouseButtons.LEFT = null;
    controls.mouseButtons.MIDDLE = this.top ? THREE.MOUSE.PAN : THREE.MOUSE.ROTATE;
    controls.mouseButtons.RIGHT = THREE.MOUSE.PAN;
    controls.addEventListener('change', () => {
      this.needsRender = true;
    });
    controls.minZoom = 0.025;
    return controls;
  }
  private resize() {
    const w = this.host.clientWidth,
      h = this.host.clientHeight;
    if (!w || !h) return;
    this.renderer.setSize(w, h);
    this.perspective.aspect = w / h;
    this.perspective.updateProjectionMatrix();
    this.ortho.left = (-60 * w) / h;
    this.ortho.right = (60 * w) / h;
    this.ortho.top = 60;
    this.ortho.bottom = -60;
    this.ortho.updateProjectionMatrix();
    this.needsRender = true;
  }
  private animate() {
    const now = performance.now(),
      dt = Math.min((now - this.last) / 1000, 0.05);
    this.last = now;
    if (this.fly && this.keys.size) {
      const dir = new THREE.Vector3();
      this.camera.getWorldDirection(dir);
      if (!this.fly || this.top) dir.y = 0;
      if (dir.length() < 0.001) dir.set(0, 0, -1);
      dir.normalize();
      const right = new THREE.Vector3().crossVectors(dir, new THREE.Vector3(0, 1, 0)).normalize();
      const move = new THREE.Vector3();
      if (this.keys.has('KeyW')) move.add(dir);
      if (this.keys.has('KeyS')) move.sub(dir);
      if (this.keys.has('KeyD')) move.add(right);
      if (this.keys.has('KeyA')) move.sub(right);
      if (this.keys.has('KeyE')) move.y += 1;
      if (this.keys.has('KeyQ')) move.y -= 1;
      move.multiplyScalar(
        dt * (this.keys.has('ShiftLeft') || this.keys.has('ShiftRight') ? 100 : 25),
      );
      this.camera.position.add(move);
      this.controls.target.add(move);
      this.needsRender = true;
    }
    if (!this.fly) this.controls.update();
    this.mapTiles.setActive(this.top);
    this.worldTiles.pause(this.locked || this.top);
    this.renderer.domElement.style.opacity = this.top ? '0' : '1';
    if (!this.top && this.needsRender) {
      this.renderer.render(this.scene, this.camera);
      this.needsRender = false;
    }
    this.gizmo.update(this.camera.quaternion);
    if (this.top) {
      const rect = mapWindow(this.ortho, this.origin),
        w = this.host.clientWidth,
        h = this.host.clientHeight;
      if (now - this.tileTime > 120) {
        this.tileTime = now;
        this.mapTiles.update(rect, w, h);
      }
      this.mapTiles.draw(rect, this.bounds, w, h);
    } else if (now - this.tileTime > 350) {
      this.tileTime = now;
      this.worldTiles.update(this.bounds);
    }
    this.updateMarkers();
  }
  private setFly(fly: boolean) {
    this.keys.clear();
    if (this.fly === fly) return;
    if (fly) {
      this.cancelSelection();
      if (this.camera === this.ortho) {
        const position = this.camera.position.clone(),
          quaternion = this.camera.quaternion.clone();
        this.top = false;
        this.switchCamera(false);
        this.camera.position.copy(position);
        this.camera.quaternion.copy(quaternion);
        this.onViewChange?.(false, false);
      }
      this.flyDistance = Math.max(1, this.camera.position.distanceTo(this.controls.target));
      // OrbitControls initializes its target at the origin; preserve the current view when replacing it.
      const target = this.controls.target.clone(),
        position = this.camera.position.clone(),
        rotation = this.camera.quaternion.clone();
      this.controls.dispose();
      this.controls = this.makeControls();
      this.controls.target.copy(target);
      this.camera.position.copy(position);
      this.camera.quaternion.copy(rotation);
      this.fly = true;
      this.controls.enabled = false;
      this.host.classList.add('fly-mode');
      const captureUnavailable = () => {
        if (this.fly)
          this.onError?.(
            'Захват мыши недоступен: обзор в полёте работает в пределах окна просмотра.',
          );
      };
      try {
        const request = this.renderer.domElement.requestPointerLock();
        if (request) void request.catch(captureUnavailable);
      } catch {
        captureUnavailable();
      }
    } else {
      this.fly = false;
      this.controls.enabled = true;
      this.host.classList.remove('fly-mode');
      if (document.pointerLockElement === this.renderer.domElement) document.exitPointerLock();
      this.camera.up.set(0, 1, 0);
      this.controls.update();
    }
    this.onFlyChange?.(this.fly);
  }
  setLocked(locked: boolean) {
    this.locked = locked;
    this.worldTiles.pause(locked || this.top);
    if (locked) this.cancelSelection();
  }
  private cancelSelection() {
    if (this.start && this.dragBounds) this.setBounds(this.dragBounds);
    this.start = undefined;
    this.dragBounds = undefined;
    this.controls.enabled = !this.fly;
    if (this.dragPointer !== undefined && this.host.hasPointerCapture(this.dragPointer))
      this.host.releasePointerCapture(this.dragPointer);
    this.dragPointer = undefined;
  }
  setSelectionMode(active: boolean) {
    this.selecting = active;
    this.host.classList.toggle('select-mode', active && this.top);
  }
  private keyDown(e: KeyboardEvent) {
    if (isFlyToggle(e)) {
      e.preventDefault();
      this.setFly(!this.fly);
      return;
    }
    if (this.fly && e.code === 'Escape') {
      e.preventDefault();
      this.setFly(false);
      return;
    }
    if (this.fly) {
      if (
        ['KeyW', 'KeyA', 'KeyS', 'KeyD', 'KeyQ', 'KeyE', 'ShiftLeft', 'ShiftRight'].includes(e.code)
      ) {
        e.preventDefault();
        this.keys.add(e.code);
      }
      return;
    }
    if (e.code === 'Numpad1' || e.code === 'Numpad3' || e.code === 'Numpad7') {
      e.preventDefault();
      this.axisView(e.code, e.ctrlKey);
      return;
    }
    if (e.code === 'Numpad5') {
      e.preventDefault();
      this.toggleProjection();
      return;
    }
    if (e.code === 'NumpadDecimal' || e.code === 'KeyF') {
      e.preventDefault();
      this.focusSelection();
      return;
    }
  }
  private switchCamera(orthographic: boolean) {
    const source = this.camera,
      target = this.controls.target.clone(),
      distance = Math.max(1, source.position.distanceTo(target));
    this.controls.dispose();
    this.camera = orthographic ? this.ortho : this.perspective;
    if (this.camera !== source) {
      this.camera.position.copy(source.position);
      this.camera.up.copy(source.up);
      if (orthographic) {
        this.ortho.zoom =
          120 / (2 * distance * Math.tan(THREE.MathUtils.degToRad(this.perspective.fov / 2)));
        this.ortho.updateProjectionMatrix();
      } else {
        const extent = 120 / this.ortho.zoom;
        const direction = source.position.clone().sub(target).normalize();
        this.perspective.position
          .copy(target)
          .addScaledVector(
            direction,
            extent / (2 * Math.tan(THREE.MathUtils.degToRad(this.perspective.fov / 2))),
          );
      }
    }
    this.controls = this.makeControls();
    this.controls.target.copy(target);
    this.controls.update();
  }
  private axisView(code: string, opposite: boolean) {
    this.setFly(false);
    const target = this.controls.target.clone(),
      distance = Math.max(20, this.camera.position.distanceTo(target));
    this.top = code === 'Numpad7' && !opposite;
    this.switchCamera(true);
    const sign = opposite ? -1 : 1;
    const direction =
      code === 'Numpad1'
        ? new THREE.Vector3(0, 0, sign)
        : code === 'Numpad3'
          ? new THREE.Vector3(sign, 0, 0)
          : new THREE.Vector3(0, sign, 0);
    this.ortho.up.copy(
      code === 'Numpad7' ? new THREE.Vector3(0, 0, -sign) : new THREE.Vector3(0, 1, 0),
    );
    this.ortho.position.copy(target).addScaledVector(direction, distance);
    this.controls.dispose();
    this.controls = this.makeControls();
    this.controls.target.copy(target);
    this.controls.update();
    this.setSelectionMode(this.selecting);
    this.onViewChange?.(this.top, true);
  }
  private selectAxis(axis: Axis, sign: AxisSign) {
    const chosen = chooseAxisSign(
      axis,
      sign,
      this.camera.position.clone().sub(this.controls.target),
    );
    this.axisView({ x: 'Numpad3', y: 'Numpad7', z: 'Numpad1' }[axis], chosen < 0);
    this.renderer.domElement.focus();
  }
  private toggleProjection() {
    this.top = false;
    this.switchCamera(this.camera !== this.ortho);
    this.setSelectionMode(this.selecting);
    this.onViewChange?.(false, this.camera === this.ortho);
  }
  setTop(top: boolean) {
    this.setFly(false);
    if (this.top === top && (top || this.camera === this.perspective)) return;
    const target = this.controls.target.clone();
    this.controls.dispose();
    this.top = top;
    if (top) {
      const distance = this.perspective.position.distanceTo(target);
      this.ortho.position.copy(target).add(new THREE.Vector3(0, 2000, 0));
      this.ortho.up.set(0, 0, -1);
      this.ortho.zoom = Math.max(0.05, 120 / Math.max(distance, 10));
      this.ortho.updateProjectionMatrix();
      this.camera = this.ortho;
    } else {
      this.camera = this.perspective;
      this.perspective.up.set(0, 1, 0);
      this.perspective.position.copy(target).add(new THREE.Vector3(70, 65, 90));
    }
    this.controls = this.makeControls();
    this.controls.target.copy(target);
    this.controls.update();
    this.setSelectionMode(this.selecting);
  }
  private worldPoint(e: PointerEvent): Vec3 | null {
    const rect = this.renderer.domElement.getBoundingClientRect();
    this.ray.setFromCamera(
      new THREE.Vector2(
        ((e.clientX - rect.left) / rect.width) * 2 - 1,
        (-(e.clientY - rect.top) / rect.height) * 2 + 1,
      ),
      this.camera,
    );
    const out = new THREE.Vector3();
    if (!this.ray.ray.intersectPlane(new THREE.Plane(new THREE.Vector3(0, 1, 0), 0), out))
      return null;
    return [out.x + this.origin[0], out.y + this.origin[1], out.z + this.origin[2]];
  }
  private pointerDown(e: PointerEvent) {
    this.renderer.domElement.focus();
    if (this.fly || this.locked || !this.top || e.button !== 0) return;
    const point = this.worldPoint(e);
    if (!point) return;
    this.onSelectionStart?.();
    this.dragBounds = { ...this.bounds };
    this.dragPointer = e.pointerId;
    this.controls.enabled = false;
    this.start = [point[0], point[2]];
    this.host.setPointerCapture(e.pointerId);
  }
  private pointerMove(e: PointerEvent) {
    const point = this.worldPoint(e);
    if (!point) return;
    this.onCoordinates?.(point);
    if (this.start)
      this.setBounds(selectionFromDrag(this.start, [point[0], point[2]], this.bounds));
  }
  private pointerUp(e: PointerEvent) {
    if (this.locked || !this.start) return;
    this.pointerMove(e);
    this.start = undefined;
    this.dragBounds = undefined;
    this.dragPointer = undefined;
    this.controls.enabled = true;
    if (this.host.hasPointerCapture(e.pointerId)) this.host.releasePointerCapture(e.pointerId);
    this.onSelection?.({ ...this.bounds });
  }
  setBounds(bounds: Bounds) {
    this.needsRender = true;
    this.bounds = { ...bounds };
    if (this.selection) {
      this.scene.remove(this.selection);
      this.selection.geometry.dispose();
      (this.selection.material as THREE.Material).dispose();
    }
    const a = relativePosition([bounds.minX, bounds.minY, bounds.minZ], this.origin),
      b = relativePosition([bounds.maxX + 1, bounds.maxY + 1, bounds.maxZ + 1], this.origin);
    const box = new THREE.BoxGeometry(b[0] - a[0], b[1] - a[1], b[2] - a[2]);
    const edges = new THREE.EdgesGeometry(box);
    box.dispose();
    this.selection = new THREE.LineSegments(
      edges,
      new THREE.LineBasicMaterial({
        color: 0x7ba9e2,
        transparent: true,
        opacity: 0.75,
        depthTest: false,
      }),
    );
    this.selection.position.set((a[0] + b[0]) / 2, (a[1] + b[1]) / 2, (a[2] + b[2]) / 2);
    this.selection.renderOrder = 20;
    this.scene.add(this.selection);
  }
  configureWorld(world: WorldInfo, settings: Settings) {
    const id = world.path + '\n' + settings.dimension;
    this.dimension = settings.dimension;
    if (id !== this.worldID) {
      this.worldID = id;
      const player = world.players.find((p) => p.dimension === settings.dimension);
      const center = player?.position || world.spawn;
      this.origin = [Math.floor(center[0] / 32) * 32, 0, Math.floor(center[2] / 32) * 32];
      this.grid.position.set(0, world.minY - 0.1, 0);
      this.teleport(center);
      this.setBounds(this.bounds);
    }
    if (world.path === 'demo' || settings.resources.length) {
      this.worldTiles.configure(
        {
          world: world.path,
          dimension: settings.dimension,
          resources: settings.resources,
          hollowLeaves: settings.hollowLeaves,
        },
        this.origin,
      );
      this.mapTiles.configure({
        world: world.path,
        dimension: settings.dimension,
        resources: settings.resources,
      });
    } else {
      this.worldTiles.clear();
      this.mapTiles.clear();
    }
    const signature = JSON.stringify(world.players);
    if (signature !== this.playerSignature) {
      this.playerSignature = signature;
      this.markers.replaceChildren();
      this.players = [];
      for (const player of world.players) {
        const button = document.createElement('button');
        button.className = 'player-marker';
        button.type = 'button';
        button.title = `${player.name || 'Игрок'} · ${player.position.map(Math.floor).join(', ')}`;
        button.setAttribute('aria-label', `Перейти к игроку ${player.name || 'Игрок'}`);
        const face = document.createElement('img');
        face.alt = '';
        face.draggable = false;
        if (player.face?.startsWith('data:image/png;base64,')) face.src = player.face;
        else {
          face.hidden = true;
          button.classList.add('steve-marker');
        }
        button.append(face);
        button.addEventListener('click', () => this.teleport(player.position));
        this.markers.append(button);
        this.players.push({ player, button });
      }
    }
  }
  private updateMarkers() {
    this.markers.hidden = !this.top;
    if (!this.top) return;
    for (const { player, button } of this.players) {
      const point =
        player.dimension === this.dimension
          ? playerScreenPosition(
              player.position,
              this.origin,
              this.camera,
              this.host.clientWidth,
              this.host.clientHeight,
            )
          : null;
      button.hidden = !point;
      if (point) {
        button.style.left = `${point.x}px`;
        button.style.top = `${point.y}px`;
      }
    }
  }
  clear() {
    this.needsRender = true;
    this.worldTiles.clear();
    this.mapTiles.clear();
    this.worldID = '';
    this.playerSignature = '';
    this.markers.replaceChildren();
    this.players = [];
    if (this.selection) {
      this.scene.remove(this.selection);
      this.selection.geometry.dispose();
      (this.selection.material as THREE.Material).dispose();
      this.selection = undefined;
    }
  }
  focusSelection() {
    this.setFly(false);
    const b = this.bounds,
      min = new THREE.Vector3(...relativePosition([b.minX, b.minY, b.minZ], this.origin)),
      max = new THREE.Vector3(
        ...relativePosition([b.maxX + 1, b.maxY + 1, b.maxZ + 1], this.origin),
      );
    const size = max.clone().sub(min);
    this.frameBounds(min.add(max).multiplyScalar(0.5), size, Math.max(size.x, size.y, size.z, 10));
  }
  private frameBounds(center: THREE.Vector3, size: THREE.Vector3, extent: number) {
    const direction = this.camera.position.clone().sub(this.controls.target).normalize();
    this.controls.target.copy(center);
    if (this.top) {
      this.ortho.position.copy(center).add(new THREE.Vector3(0, Math.max(2000, extent * 2), 0));
      this.ortho.zoom = 95 / Math.max(size.z, size.x / this.perspective.aspect, 10);
      this.ortho.updateProjectionMatrix();
    } else {
      const distance =
        this.camera === this.perspective
          ? framingDistance(size.length() / 2, this.perspective.fov, this.perspective.aspect)
          : extent * 1.8;
      this.camera.position
        .copy(center)
        .addScaledVector(
          direction.lengthSq() ? direction : new THREE.Vector3(1, 0.8, 1).normalize(),
          distance,
        );
      if (this.camera === this.ortho) {
        this.ortho.zoom = 95 / Math.max(extent, extent / this.perspective.aspect);
        this.ortho.updateProjectionMatrix();
      }
    }
    this.controls.update();
  }
  teleport(position: Vec3) {
    const target = new THREE.Vector3(...relativePosition(position, this.origin));
    this.controls.target.copy(target);
    this.camera.position
      .copy(target)
      .add(this.top ? new THREE.Vector3(0, 2000, 0) : new THREE.Vector3(12, 9, 16));
    if (this.top) {
      this.ortho.zoom = 2;
      this.ortho.updateProjectionMatrix();
    }
    this.controls.update();
  }
}
