import { editorLayout } from './ui/layout';
import { helpContent } from './ui/help';
import { icon } from './ui/icons';
import './style.css';
import { Viewport } from './viewport';
import { createAutoBuildScheduler } from './auto-build';
import { centeredBounds, validateBounds } from './selection';
import type { Bounds, Discovery, Player, Settings, Status, Vec3, WorldInfo } from './types';

const $ = <T extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as T;
const escape = (s: unknown) =>
  String(s ?? '').replace(
    /[&<>"']/g,
    (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!,
  );
// Reuse locale formatters: polling updates several counters every second.
const standardNumber = new Intl.NumberFormat('en-US', { maximumFractionDigits: 1 });
const compactNumber = new Intl.NumberFormat('en-US', {
  notation: 'compact',
  maximumFractionDigits: 1,
});
const number = (n: number | undefined) =>
  (n && n >= 1e6 ? compactNumber : standardNumber).format(n || 0);
const shortDimension = (s: string) =>
  ({
    'minecraft:overworld': 'Обычный мир',
    'minecraft:the_nether': 'Нижний мир',
    'minecraft:the_end': 'Край',
  })[s] || s.replace('minecraft:', '').replaceAll('_', ' ');

// Install the static shell once; updates touch only the relevant controls.
document.querySelector('#app')!.innerHTML = editorLayout();

type Resource = { path: string; enabled: boolean };
let baseJar = '',
  discovery: Discovery = { versions: [], worlds: [], warnings: [] },
  discovering = false;
let resources: Resource[] = [],
  world: WorldInfo | undefined,
  status: Status = { busy: false, phase: '', progress: 0 },
  previewRevision: string | number | undefined;
// A status poll started before a user action must not restore obsolete state.
let requestEpoch = 0;
let dirty = true,
  requesting = false,
  activePlayer: Player | undefined,
  top = false,
  selecting = false,
  pendingTeleport: Vec3 | undefined;
let viewport: Viewport | undefined,
  toastTimer: ReturnType<typeof setTimeout>,
  connectionErrorShown = false;
// Edits are debounced, while explicit open/export requests stay serialized.
const automaticBuild = createAutoBuildScheduler(
  () => {
    if (busy() || !world || !dirty) return false;
    const current = settings();
    if (world.path !== 'demo' && !current.resources.length) return false;
    try {
      validateBounds(current.bounds);
    } catch {
      return false;
    }
    return !!current.dimension && Number.isInteger(current.padding) && current.padding >= 4;
  },
  () => {
    void withRequest(build);
  },
);
function toast(message: string, error = false) {
  $('toast').textContent = message;
  $('toast').classList.toggle('error', error);
  $('toast').hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => ($('toast').hidden = true), error ? 12000 : 4500);
}
function report(error: unknown) {
  toast(error instanceof Error ? error.message : String(error), true);
}
try {
  viewport = new Viewport($('viewport'), $('axis-gizmo'), $('player-markers'));
  viewport.onWorldProgress = (loaded, total) => {
    $('world-loading').textContent =
      total && loaded < total ? `Загрузка мира ${loaded}/${total}` : '';
  };
  viewport.onSelectionStart = () => automaticBuild.cancel();
  viewport.onSelection = (b) => {
    if (busy()) {
      viewport?.setBounds(bounds());
      return;
    }
    setBounds(b);
    markDirty();
    automaticBuild.schedule();
  };
  viewport.onCoordinates = (p) =>
    ($('coordinates').textContent =
      `X ${Math.floor(p[0])}   Y ${Math.floor(p[1])}   Z ${Math.floor(p[2])}`);
  viewport.onError = (message) => toast(message, true);
  viewport.onViewChange = (isTop) => {
    top = isTop;
    syncViewUI();
  };
  viewport.onFlyChange = (active) => {
    if (active)
      $('navigation-hint').innerHTML =
        '<kbd>ПОЛЁТ</kbd> Мышь · WASD · Q/E · Shift ускорение · Esc выход';
    else syncViewUI();
  };
} catch (error) {
  toast(`3D-просмотр недоступен. Построение и экспорт продолжают работать. ${String(error)}`, true);
}
async function api<T>(path: string, body?: unknown): Promise<T> {
  const response = await fetch(`/api/${path}`, {
    method: body === undefined ? 'GET' : 'POST',
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const content = await response.text();
  let result: unknown;
  try {
    result = JSON.parse(content);
  } catch {
    throw Error(
      response.ok
        ? 'Сервис вернул некорректный ответ.'
        : 'Нет связи с локальным сервисом экспорта.',
    );
  }
  if (!response.ok)
    throw Error((result as { error?: string }).error || `Ошибка запроса (${response.status}).`);
  return result as T;
}
function busy() {
  return status.busy || requesting;
}
function controls() {
  const locked = busy();
  viewport?.setLocked(locked);
  for (const id of [
    'open-world',
    'choose-world',
    'empty-open',
    'add-resources',
    'add-folder',
    'choose-base',
    'reset-selection',
  ])
    $(id)!.toggleAttribute('disabled', locked);
  $<HTMLSelectElement>('discovered-worlds').disabled = locked || discovering;
  $<HTMLSelectElement>('discovered-versions').disabled = locked || discovering;
  $<HTMLInputElement>('base-jar').disabled = locked;
  $<HTMLButtonElement>('refresh-discovery').disabled = discovering;
  $('export').toggleAttribute('disabled', locked || !status.preview || dirty);
  $('cancel').hidden = !status.busy;
  $('dimension').toggleAttribute('disabled', locked || !world);
  $('activity-dot').classList.toggle('working', locked);
  document
    .querySelectorAll<HTMLInputElement>(
      '.bounds-row input,.options-section input,.options-section select,#auto-min,#padding',
    )
    .forEach((el) => (el.disabled = locked));
  for (const id of ['minY', 'maxY'])
    $(id).toggleAttribute('disabled', locked || $<HTMLInputElement>('auto-min').checked);
  document
    .querySelectorAll<HTMLButtonElement>('#resources button,#resources input,#players button')
    .forEach((el) => (el.disabled = locked));
}
function bounds(): Bounds {
  return Object.fromEntries(
    ['minX', 'maxX', 'minY', 'maxY', 'minZ', 'maxZ'].map((key) => [
      key,
      Number($<HTMLInputElement>(key).value),
    ]),
  ) as unknown as Bounds;
}
function setBounds(b: Bounds) {
  for (const [key, val] of Object.entries(b)) $<HTMLInputElement>(key).value = String(val);
  updateBounds();
}
function updateBounds() {
  const b = bounds();
  $('selection-size').textContent =
    `${number(b.maxX - b.minX + 1)} × ${number(b.maxZ - b.minZ + 1)} × ${number(b.maxY - b.minY + 1)}`;
  try {
    validateBounds(b);
    viewport?.setBounds(b);
  } catch {
    /* Inputs remain editable until valid. */
  }
}
function markDirty() {
  dirty = true;
  if (world) viewport?.configureWorld(world, settings());
  updateBounds();
  controls();
  automaticBuild.schedule();
}
function settings(): Settings {
  return {
    dimension: $<HTMLSelectElement>('dimension').value,
    bounds: bounds(),
    resources: [
      ...(baseJar ? [baseJar] : []),
      ...resources.filter((r) => r.enabled && r.path !== baseJar).map((r) => r.path),
    ],
    autoMin: $<HTMLInputElement>('auto-min').checked,
    padding: Number($<HTMLInputElement>('padding').value),
    hollowLeaves: $<HTMLInputElement>('hollow-leaves').checked,
    biomeColors: true,
    biomeBlend: 7,
    players: $<HTMLInputElement>('include-players').checked,
    optimizeMesh: $<HTMLInputElement>('optimize-mesh').checked,
  };
}
function renderResources() {
  $('resource-count').textContent = String(
    resources.filter((r) => r.enabled).length + (baseJar ? 1 : 0),
  );
  $('resources').innerHTML = resources.length
    ? resources
        .map(
          (r, i) =>
            `<div class="resource-item ${r.enabled ? '' : 'disabled'}"><input type="checkbox" ${r.enabled ? 'checked' : ''} data-resource="${i}" aria-label="Включить ${escape(r.path)}"/><div title="${escape(r.path)}"><strong>${escape(r.path.split(/[\\/]/).pop())}</strong><small>${`Приоритет ${i + 1}`}</small></div><div class="resource-controls"><button data-move="${i}" data-direction="-1" title="Выше" aria-label="Переместить выше" ${i === 0 ? 'disabled' : ''}>↑</button><button data-move="${i}" data-direction="1" title="Ниже" aria-label="Переместить ниже" ${i === resources.length - 1 ? 'disabled' : ''}>↓</button><button data-remove="${i}" title="Удалить ресурс" aria-label="Удалить ресурс">×</button></div></div>`,
        )
        .join('')
    : `<div class="empty-resource">${icon('layers')}<span>Дополнительные ресурсы<small>Добавьте моды или ресурспаки.</small></span></div>`;
}
function renderPlayers() {
  if (world) viewport?.configureWorld(world, settings());
  const players = world?.players || [];
  $('player-count').textContent = String(players.length);
  $('players').innerHTML = players.length
    ? players
        .map(
          (p, i) =>
            `<button class="player-card ${activePlayer?.uuid === p.uuid ? 'selected' : ''}" data-player="${i}" title="Перейти к ${escape(p.name || p.uuid)}"><span class="player-avatar">${icon('user')}</span><span><strong>${escape(p.name || p.uuid.slice(0, 13) || 'Player')}</strong><small>${escape(shortDimension(p.dimension))} · ${p.position.map(Math.floor).join(', ')}</small></span>${icon('arrow')}</button>`,
        )
        .join('')
    : '<p class="section-note">Игроки не найдены. Начальная позиция — спавн мира.</p>';
}
function applyWorld(info: WorldInfo) {
  world = info;
  activePlayer = info.players?.[0];
  previewRevision = undefined;
  status.preview = undefined;
  viewport?.clear();
  $('world-name').textContent = info.name || 'Без названия';
  $('world-version').textContent = `Java ${info.version || 'unknown'} · ${info.dataVersion}`;
  $<HTMLInputElement>('world-path').value = info.path || '';
  $('dimension').innerHTML = (info.dimensions || ['minecraft:overworld'])
    .map((d) => `<option value="${escape(d)}">${escape(shortDimension(d))}</option>`)
    .join('');
  const initialDimension = activePlayer?.dimension || info.spawnDimension || 'minecraft:overworld';
  if (info.dimensions?.includes(initialDimension))
    $<HTMLSelectElement>('dimension').value = initialDimension;
  setBounds(
    centeredBounds(
      activePlayer?.position || info.spawn || [0, 64, 0],
      info.minY ?? 0,
      info.maxY ?? 255,
    ),
  );
  pendingTeleport = activePlayer?.position || info.spawn;
  $('scene-label').hidden = false;
  $('scene-name').textContent = info.name;
  $('scene-dimension').textContent = shortDimension($<HTMLSelectElement>('dimension').value);
  $('empty-state').hidden = true;
  renderPlayers();
  markDirty();
}
async function withRequest(work: () => Promise<void>) {
  if (busy()) return;
  automaticBuild.cancel();
  requestEpoch++;
  requesting = true;
  controls();
  try {
    await work();
  } catch (error) {
    report(error);
  } finally {
    requesting = false;
    requestEpoch++;
    controls();
    automaticBuild.resume();
  }
}
async function openWorld(path: string) {
  if (!path.trim()) throw Error('Выберите папку мира Minecraft с файлом level.dat.');
  const info = await api<WorldInfo>('open', { path: path.trim() });
  applyWorld(info);
  renderDiscovery();
  if (!baseJar) {
    const launcher = discovery.worlds.find((w) => w.path === info.path)?.launcher;
    const matches = discovery.versions.filter((v) => v.version === info.version);
    const match = matches.find((v) => v.launcher === launcher) || matches[0];
    if (match) setBaseJar(match.path);
  }
  const matched = discovery.versions.find((v) => v.path === baseJar);
  if (matched && matched.version === info.version) {
    toast(`Мир открыт. Minecraft ${info.version} — построение предпросмотра.`);
    await build();
  } else toast('Мир открыт. Выберите версию Minecraft — предпросмотр построится автоматически.');
}
async function pathDialog(title: string, description: string, initial = ''): Promise<string> {
  const dialog = $<HTMLDialogElement>('path-dialog');
  $('path-title').textContent = title;
  $('path-description').textContent = description;
  $<HTMLTextAreaElement>('dialog-path').value = initial;
  dialog.returnValue = 'cancel';
  dialog.showModal();
  return new Promise((resolve) =>
    dialog.addEventListener(
      'close',
      () =>
        resolve(
          dialog.returnValue === 'confirm'
            ? $<HTMLTextAreaElement>('dialog-path').value.trim()
            : '',
        ),
      { once: true },
    ),
  );
}
async function chooseDirectory(title: string) {
  return window.go?.main.App
    ? window.go.main.App.ChooseDirectory()
    : pathDialog(title, 'Введите абсолютный путь к папке на этом компьютере.');
}
async function build() {
  automaticBuild.cancel();
  if (!world) throw Error('Сначала откройте мир.');
  const s = settings();
  validateBounds(s.bounds);
  if (!Number.isInteger(s.padding) || s.padding < 4)
    throw Error('Запас вниз должен быть целым числом не меньше 4.');
  if (!s.dimension) throw Error('Выберите измерение.');
  if (world.path !== 'demo' && !s.resources.length)
    throw Error('Сначала выберите клиентский JAR нужной версии Minecraft.');
  await api('build', s);
  dirty = true;
  status.busy = true;
  status.phase = 'Построение предпросмотра';
  controls();
}
function setView(isTop: boolean) {
  top = isTop;
  viewport?.setTop(top);
  syncViewUI();
}
function syncViewUI() {
  for (const [id, selected] of [
    ['view-3d', !top],
    ['view-top', top],
  ] as const) {
    $(id).classList.toggle('active', selected);
    $(id).setAttribute('aria-pressed', String(selected));
  }
  $('navigation-hint').innerHTML = top
    ? '<kbd>ЛКМ</kbd> выделение <span>·</span> <kbd>СКМ</kbd> сдвиг <span>·</span> <kbd>Колесо</kbd> масштаб'
    : '<kbd>СКМ</kbd> орбита <span>·</span> <kbd>Shift + СКМ</kbd> сдвиг <span>·</span> <kbd>Колесо</kbd> масштаб';
}
async function teleport(player: Player) {
  activePlayer = player;
  renderPlayers();
  const sameDimension = $<HTMLSelectElement>('dimension').value === player.dimension;
  const b = bounds(),
    inside =
      player.position[0] >= b.minX &&
      player.position[0] <= b.maxX &&
      player.position[2] >= b.minZ &&
      player.position[2] <= b.maxZ;
  if (sameDimension && inside && status.preview) {
    viewport?.teleport(player.position);
    return;
  }
  $<HTMLSelectElement>('dimension').value = player.dimension;
  $('scene-dimension').textContent = shortDimension(player.dimension);
  setBounds(centeredBounds(player.position, world?.minY ?? 0, world?.maxY ?? 255));
  pendingTeleport = player.position;
  markDirty();
  await build();
}
function info(title: string, html: string) {
  $('info-title').textContent = title;
  $('info-body').innerHTML = html;
  $<HTMLDialogElement>('info-dialog').showModal();
}
function bind(id: string, handler: () => void | Promise<void>) {
  $(id).addEventListener('click', () => {
    Promise.resolve().then(handler).catch(report);
  });
}
bind('choose-world', () =>
  withRequest(async () => {
    const path = await chooseDirectory('Открыть мир Minecraft');
    if (path) await openWorld(path);
  }),
);
bind('empty-open', () => $('choose-world').click());
bind('open-world', () => withRequest(() => openWorld($<HTMLInputElement>('world-path').value)));
bind('add-resources', () =>
  withRequest(async () => {
    const paths = window.go?.main.App
      ? await window.go.main.App.ChooseFiles()
      : (
          await pathDialog(
            'Добавить архивы ресурсов',
            'Укажите пути к JAR модов или ZIP ресурспаков, по одному на строку.',
          )
        ).split(/\r?\n/);
    for (const path of paths || [])
      if (path.trim() && !resources.some((r) => r.path === path.trim()))
        resources.push({ path: path.trim(), enabled: true });
    renderResources();
    markDirty();
  }),
);
bind('add-folder', () =>
  withRequest(async () => {
    const path = await chooseDirectory('Добавить папку ресурсов');
    if (path) {
      resources.push({ path, enabled: true });
      renderResources();
      markDirty();
    }
  }),
);
$('resources').addEventListener('click', (e) => {
  if (busy()) return;
  const el = (e.target as HTMLElement).closest<HTMLButtonElement>('button');
  if (!el) return;
  if (el.dataset.remove !== undefined) resources.splice(Number(el.dataset.remove), 1);
  else if (el.dataset.move !== undefined) {
    const i = Number(el.dataset.move),
      j = i + Number(el.dataset.direction);
    if (j < 0 || j >= resources.length) return;
    [resources[i], resources[j]] = [resources[j], resources[i]];
  }
  renderResources();
  markDirty();
});
$('resources').addEventListener('change', (e) => {
  const el = e.target as HTMLInputElement;
  if (el.dataset.resource !== undefined) {
    resources[Number(el.dataset.resource)].enabled = el.checked;
    renderResources();
    markDirty();
  }
});
$('players').addEventListener('click', (e) => {
  const el = (e.target as HTMLElement).closest<HTMLElement>('[data-player]');
  if (el) void withRequest(() => teleport(world!.players[Number(el.dataset.player)]));
});
bind('view-3d', () => setView(false));
bind('view-top', () => setView(true));
bind('frame', () => viewport?.focusSelection());
bind('select-area', () => {
  if (!top) setView(true);
  selecting = !selecting;
  viewport?.setSelectionMode(selecting);
  $('select-area').classList.toggle('active', selecting);
  $('select-area').setAttribute('aria-pressed', String(selecting));
});
bind('reset-selection', () => {
  if (world) {
    setBounds(centeredBounds(activePlayer?.position || world.spawn, world.minY, world.maxY));
    markDirty();
    automaticBuild.schedule();
  }
});
bind('choose-export', async () => {
  const path = window.go?.main.App
    ? await window.go.main.App.ChooseSaveFile()
    : await pathDialog(
        'Выбрать файл GLB',
        'Введите абсолютный путь к выходному файлу .glb.',
        $<HTMLInputElement>('export-path').value,
      );
  if (path) $<HTMLInputElement>('export-path').value = path;
});
bind('export', () =>
  withRequest(async () => {
    if (dirty) throw Error('Перед экспортом постройте текущую область.');
    let path = $<HTMLInputElement>('export-path').value.trim();
    if (!path) {
      path = window.go?.main.App
        ? await window.go.main.App.ChooseSaveFile()
        : await pathDialog('Экспортировать', 'Введите полный путь к выходному файлу .glb.');
      if (!path) return;
      $<HTMLInputElement>('export-path').value = path;
    }
    if (!/\.glb$/i.test(path)) {
      path += '.glb';
      $<HTMLInputElement>('export-path').value = path;
    }
    await api('export', { path });
    status.busy = true;
    status.phase = 'Экспорт';
    controls();
  }),
);
bind('cancel', async () => {
  await api('cancel', {});
  toast('Запрошена отмена.');
});
bind('show-warnings', () => {
  const warnings = status.preview?.warnings || [];
  info(
    'Диагностика ресурсов и построения',
    warnings.length
      ? `<ul class="issue-list">${warnings.map((w) => `<li>${escape(w)}</li>`).join('')}</ul>`
      : '<p class="section-note">Для текущего предпросмотра нет замечаний.</p>',
  );
});
bind('close-info', () => $<HTMLDialogElement>('info-dialog').close());
bind('help', () => info('Управление и экспорт', helpContent));
$('world-path').addEventListener('keydown', (e) => {
  if (e.key === 'Enter') $('open-world').click();
});
for (const id of [
  'minX',
  'maxX',
  'minY',
  'maxY',
  'minZ',
  'maxZ',
  'auto-min',
  'padding',
  'hollow-leaves',
  'include-players',
  'optimize-mesh',
])
  $(id).addEventListener('change', markDirty);
for (const id of ['minX', 'maxX', 'minZ', 'maxZ', 'minY', 'maxY', 'padding'])
  $(id).addEventListener('input', markDirty);
$('dimension').addEventListener('change', () => {
  $('scene-dimension').textContent = shortDimension($<HTMLSelectElement>('dimension').value);
  markDirty();
});

controls();
async function poll() {
  const epoch = requestEpoch;
  try {
    const next = await api<Status>('status');
    if (epoch !== requestEpoch || requesting) return;
    connectionErrorShown = false;
    const wasBusy = status.busy,
      previousPhase = status.phase;
    status = next;
    if (next.world && !world) applyWorld(next.world);
    else if (
      next.world &&
      world &&
      JSON.stringify(world.players) !== JSON.stringify(next.world.players)
    ) {
      const selected = activePlayer?.uuid;
      world = { ...world, players: next.world.players };
      activePlayer = world.players.find((p) => p.uuid === selected) || world.players[0];
      renderPlayers();
    }
    $('phase').textContent = next.error ? 'Ошибка' : next.phase || 'Готово';
    $('progress-label').textContent = next.busy
      ? `${Math.round(Math.min(1, Math.max(0, next.progress)) * 100)}%`
      : '';
    $('progress').style.width = next.busy
      ? `${Math.max(2, Math.min(1, next.progress) * 100)}%`
      : '0';
    if (next.error && next.error !== lastError) {
      report(next.error);
      lastError = next.error;
      dirty = true;
    }
    if (!next.error) lastError = '';
    if (wasBusy && !next.busy && !next.error && previousPhase.toLowerCase().includes('export'))
      toast('GLB экспортирован. Файл готов для импорта в Blender.');
    if (next.preview) {
      const p = next.preview;
      $('stat-blocks').textContent = number(p.blocks);
      $('stat-triangles').textContent = number(p.triangles);
      $('stat-materials').textContent = number(p.materials);
      $('optimization-result').textContent = p.optimized
        ? `${number(p.worldTrianglesBefore)} → ${number(p.worldTrianglesAfter)} треугольников мира (−${Math.round(100 * (1 - (p.worldTrianglesAfter || 0) / Math.max(1, p.worldTrianglesBefore || 0)))}%)`
        : '';
      $('show-warnings').textContent = `Замечания · ${p.warnings?.length || 0}`;
      $('show-warnings').classList.toggle('has-issues', !!p.warnings?.length);
      if (p.revision !== previewRevision && !next.busy) {
        previewRevision = p.revision;
        setBounds(p.bounds);
        dirty = false;
        try {
          if (pendingTeleport) {
            viewport?.teleport(pendingTeleport);
            pendingTeleport = undefined;
          }
        } catch (error) {
          report(`Не удалось показать предпросмотр: ${String(error)}`);
        }
      }
    }
    controls();
  } catch (error) {
    if (!connectionErrorShown) {
      $('phase').textContent = 'Ожидание локального сервиса';
      report(error);
      connectionErrorShown = true;
    }
  } finally {
    automaticBuild.resume();
    setTimeout(poll, status.busy ? 300 : 1000);
  }
}
let lastError = '';
void poll();
void discover();

function discoveryContext(path: string, paths: string[], minimum: number) {
  const parts = path.replaceAll('\\', '/').split('/').filter(Boolean);
  for (let length = minimum; length <= parts.length; length++) {
    const suffix = parts.slice(-length).join('/');
    if (
      !paths.some((other) => other !== path && other.replaceAll('\\', '/').endsWith('/' + suffix))
    )
      return suffix;
  }
  return parts.join('/');
}
function renderDiscovery() {
  const currentWorld = $<HTMLInputElement>('world-path').value;
  $('discovered-worlds').innerHTML =
    '<option value="">' +
    (discovery.worlds.length ? 'Выберите мир…' : 'Миры не найдены — укажите папку') +
    '</option>' +
    discovery.worlds
      .map((w) => {
        const peers = discovery.worlds
          .filter((other) => other.name === w.name && other.launcher === w.launcher)
          .map((other) => other.path);
        return `<option value="${escape(w.path)}" title="${escape(w.path)}">${escape(w.name)} · ${escape(w.launcher)} · ${escape(discoveryContext(w.path, peers, 3))}</option>`;
      })
      .join('');
  $('discovered-versions').innerHTML =
    '<option value="">' +
    (discovery.versions.length ? 'Выберите версию Minecraft…' : 'Версии не найдены — укажите JAR') +
    '</option>' +
    discovery.versions
      .map((v) => {
        const peers = discovery.versions
          .filter((other) => other.version === v.version && other.launcher === v.launcher)
          .map((other) => other.path);
        return `<option value="${escape(v.path)}" title="${escape(v.path)}">${escape(v.version || v.name)} · ${escape(v.launcher)} · ${escape(discoveryContext(v.path, peers, 2))}</option>`;
      })
      .join('');
  $<HTMLSelectElement>('discovered-worlds').value = discovery.worlds.some(
    (w) => w.path === currentWorld,
  )
    ? currentWorld
    : '';
  $<HTMLSelectElement>('discovered-versions').value = discovery.versions.some(
    (v) => v.path === baseJar,
  )
    ? baseJar
    : '';
  $('discovered-worlds').title = currentWorld;
  $('discovered-versions').title = baseJar;
  $('world-path').title = currentWorld;
  $('base-jar').title = baseJar;
}
async function discover() {
  if (discovering) return;
  discovering = true;
  controls();
  $('discovery-status').textContent = 'Поиск в установленных лаунчерах…';
  $('discovery-status').classList.remove('has-error');
  try {
    discovery = await api<Discovery>('discover');
    discovery.versions ??= [];
    discovery.worlds ??= [];
    discovery.warnings ??= [];
    renderDiscovery();
    $('discovery-status').textContent =
      `${discovery.worlds.length} миров · ${discovery.versions.length} версий${discovery.warnings.length ? ' · ' + discovery.warnings.length + ' замечаний поиска' : ''}`;
    $('discovery-status').title = discovery.warnings.join('\n');
  } catch (error) {
    $('discovery-status').textContent = 'Ошибка поиска. Обновите список или укажите пути вручную.';
    $('discovery-status').title = String(error);
    $('discovery-status').classList.add('has-error');
    renderDiscovery();
  } finally {
    discovering = false;
    controls();
  }
}
function setBaseJar(path: string) {
  baseJar = path.trim();
  $<HTMLInputElement>('base-jar').value = baseJar;
  $<HTMLSelectElement>('discovered-versions').value = baseJar;
  $('discovered-versions').title = baseJar;
  $('base-jar').title = baseJar;
  renderResources();
  markDirty();
}
bind('refresh-discovery', discover);
$('discovered-worlds').addEventListener('change', () => {
  const path = $<HTMLSelectElement>('discovered-worlds').value;
  if (path) void withRequest(() => openWorld(path));
});
$('discovered-versions').addEventListener('change', () =>
  setBaseJar($<HTMLSelectElement>('discovered-versions').value),
);
$('base-jar').addEventListener('change', () => setBaseJar($<HTMLInputElement>('base-jar').value));
bind('choose-base', () =>
  withRequest(async () => {
    const paths = window.go?.main.App
      ? await window.go.main.App.ChooseFiles()
      : [
          await pathDialog(
            'Выбрать клиентский JAR Minecraft',
            'Введите путь к клиентскому JAR нужной версии Minecraft.',
            baseJar,
          ),
        ];
    if (paths?.[0]) setBaseJar(paths[0]);
  }),
);

// Native-style dock sections retain all original controls and their event bindings.
for (const section of document.querySelectorAll<HTMLElement>('.sidebar .panel-section')) {
  const heading = section.querySelector<HTMLElement>(':scope > .section-heading');
  if (!heading) continue;
  const dock = document.createElement('details');
  dock.className = 'dock';
  dock.open = true;
  const summary = document.createElement('summary');
  summary.className = 'dock-title';
  while (heading.firstChild) summary.append(heading.firstChild);
  heading.remove();
  const content = document.createElement('div');
  content.className = 'dock-content';
  while (section.firstChild) content.append(section.firstChild);
  dock.append(summary, content);
  section.append(dock);
}
document.addEventListener('keydown', (event) => {
  if (event.ctrlKey && event.code === 'KeyO') {
    event.preventDefault();
    $('choose-world').click();
  }
  if (event.code === 'F1') {
    event.preventDefault();
    if (!$<HTMLDialogElement>('info-dialog').open) $('help').click();
  }
});
