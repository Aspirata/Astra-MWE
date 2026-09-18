import { icon } from './icons';

// Keep static markup separate from session state and event handlers.
export function editorLayout() {
  return /* HTML */ ` <main class="workspace">
      <aside class="sidebar left-panel" aria-label="Импорт">
        <div class="panel-heading">
          ${icon('folder')}
          <h1>Импорт</h1>
        </div>
        <section class="panel-section">
          <div class="section-heading"><h2>Мир</h2></div>
          <button id="choose-world" class="world-card">
            <span class="world-art">${icon('cube')}</span
            ><span
              ><strong id="world-name">Открыть мир…</strong
              ><small id="world-version">Minecraft Java · 1.13+</small></span
            >${icon('folder')}
          </button>
          <div class="path-row">
            <input
              id="world-path"
              aria-label="Путь к папке мира"
              placeholder="Путь к папке мира…"
            /><button
              id="open-world"
              class="icon-button"
              title="Открыть папку мира"
              aria-label="Открыть папку мира"
            >
              ${icon('arrow')}
            </button>
          </div>
          <div class="discovery-heading">
            <label class="field-label" for="discovered-worlds">Установленные миры</label
            ><button
              id="refresh-discovery"
              class="icon-button"
              title="Обновить список миров и версий"
              aria-label="Обновить список миров и версий"
            >
              ${icon('refresh')}
            </button>
          </div>
          <select id="discovered-worlds" aria-label="Найденные миры Minecraft">
            <option value="">Поиск миров…</option>
          </select>
          <p id="discovery-status" class="microcopy" role="status">
            Поиск в установленных лаунчерах…
          </p>
          <label class="field-label" for="dimension">Измерение</label
          ><select id="dimension" disabled>
            <option>Сначала откройте мир</option>
          </select>
        </section>
        <section class="panel-section resources-section">
          <div class="section-heading">
            <h2>Minecraft и ресурсы</h2>
            <span class="count" id="resource-count">0</span>
          </div>
          <label class="field-label" for="discovered-versions">Базовая версия Minecraft</label
          ><select id="discovered-versions" aria-label="Установленная версия Minecraft">
            <option value="">Поиск версий…</option>
          </select>
          <div class="path-row">
            <input
              id="base-jar"
              aria-label="Путь к клиентскому JAR Minecraft"
              placeholder="Путь к клиентскому JAR…"
            /><button
              id="choose-base"
              class="icon-button"
              title="Выбрать клиентский JAR"
              aria-label="Выбрать клиентский JAR"
            >
              ${icon('folder')}
            </button>
          </div>
          <label class="field-label">Моды и ресурспаки</label>
          <p class="section-note">Ниже в списке — выше приоритет.</p>
          <div id="resources" class="resource-list">
            <div class="empty-resource">
              ${icon('layers')}<span
                >Дополнительные ресурсы<small>Добавьте моды или ресурспаки.</small></span
              >
            </div>
          </div>
          <div class="resource-add">
            <button id="add-resources" class="secondary">${icon('plus')} JAR / ZIP</button
            ><button
              id="add-folder"
              class="icon-button secondary"
              aria-label="Добавить папку ресурсов"
              title="Добавить папку ресурсов"
            >
              ${icon('folder')}
            </button>
          </div>
          <button id="show-warnings" class="text-button warnings-button">Замечания · 0</button>
        </section>
        <section class="panel-section players-section">
          <div class="section-heading">
            <h2>Игроки</h2>
            <span class="count" id="player-count">0</span>
          </div>
          <div id="players" class="player-list">
            <p class="section-note">Позиции игроков появятся после открытия мира.</p>
          </div>
          <p class="microcopy">Позиции из сохранения.</p>
        </section>

        <section class="panel-section settings-section">
          <div class="app-actions">
            <button id="help" title="Управление и экспорт (F1)">${icon('help')} Справка</button>
          </div>
        </section>
      </aside>
      <section class="viewport-shell">
        <div class="viewport-toolbar">
          <div class="view-switch" role="group" aria-label="Режим просмотра">
            <button id="view-3d" class="active" aria-pressed="true">${icon('cube')} 3D</button
            ><button id="view-top" aria-pressed="false">${icon('map')} Сверху</button>
          </div>
          <div class="viewport-tools">
            <span id="world-loading" role="status"></span
            ><button
              id="frame"
              class="icon-button"
              title="Показать выделение (Num . / F)"
              aria-label="Показать выделение"
            >
              ${icon('target')}
            </button>
          </div>
        </div>
        <div id="viewport" class="viewport"></div>
        <div id="player-markers" class="player-markers" hidden></div>
        <div id="empty-state" class="viewport-empty">
          <span>Astra MWE</span><small>0.1.0 · Minecraft World Exporter</small
          ><button id="empty-open" class="secondary">${icon('folder')} Открыть мир…</button>
        </div>
        <div id="scene-label" class="scene-label" hidden>
          <span class="status-dot"></span><strong id="scene-name"></strong
          ><span id="scene-dimension"></span>
        </div>
        <div class="viewport-bottom">
          <div class="navigation-hint" id="navigation-hint">
            <kbd>СКМ</kbd> орбита <span>·</span> <kbd>Shift + СКМ</kbd> сдвиг <span>·</span>
            <kbd>Колесо</kbd> масштаб
          </div>
        </div>
        <div id="axis-gizmo"></div>
        <div class="viewport-coordinates">
          <span id="coordinates">X — &nbsp; Y — &nbsp; Z —</span>
        </div>
      </section>
      <aside class="sidebar right-panel" aria-label="Экспорт">
        <div class="panel-heading">
          ${icon('download')}
          <h1>Экспорт</h1>
        </div>
        <section class="panel-section">
          <div class="section-heading"><h2>Область экспорта</h2></div>
          <button
            id="select-area"
            class="secondary select-button"
            title="Выделить область сверху"
            aria-pressed="false"
          >
            ${icon('target')} Выделить в просмотре
          </button>
          <div class="selection-heading">
            ${icon('target')}<span>Границы области<small>Координаты включительно</small></span
            ><button
              id="reset-selection"
              class="icon-button"
              title="Область вокруг игрока или спавна"
              aria-label="Сбросить выделение"
            >
              ${icon('refresh')}
            </button>
          </div>
          <div class="bounds-header"><span>Ось</span><span>Мин.</span><span>Макс.</span></div>
          ${['X', 'Z', 'Y'].map((axis) => `<div class="bounds-row"><label class="axis axis-${axis.toLowerCase()}">${axis}</label><input id="min${axis}" type="number" step="1" value="${axis === 'Y' ? 0 : -32}" aria-label="${axis} минимум"/><span>→</span><input id="max${axis}" type="number" step="1" value="${axis === 'Y' ? 255 : 32}" aria-label="${axis} максимум"/></div>`).join('')}
          <div class="selection-size">
            <span>Размер области</span><strong id="selection-size">65 × 65 × 256</strong>
          </div>
          <div class="divider"></div>
          <label class="switch-row"
            ><span>Автоматическая высота<small>По рельефу и верхнему блоку</small></span
            ><input id="auto-min" type="checkbox" checked role="switch" /></label
          ><label class="inline-field"
            >Запас вниз
            <span
              ><input id="padding" type="number" value="4" min="4" max="384" step="1" />
              блоков</span
            ></label
          >
          <p class="microcopy">Без учёта листвы, растений и воды.</p>
        </section>
        <section class="panel-section options-section">
          <div class="section-heading"><h2>Геометрия и материалы</h2></div>
          <label class="switch-row"
            ><span>Полая листва<small>Удалять внутренние грани</small></span
            ><input id="hollow-leaves" type="checkbox" role="switch" /></label
          ><label class="switch-row"
            ><span>Экспорт игроков<small>Только в файле; в просмотре видны всегда</small></span
            ><input id="include-players" type="checkbox" checked role="switch" /></label
          ><label class="switch-row"
            ><span
              >Оптимизировать меш<small>Объединять одинаковые грани без потери вида</small></span
            ><input id="optimize-mesh" type="checkbox" role="switch"
          /></label>
          <p id="optimization-result" class="microcopy" role="status"></p>
        </section>
        <section class="panel-section export-section">
          <div class="section-heading"><h2>Файл и Blender</h2></div>
          <div class="export-format">
            <div class="file-badge">GLB</div>
            <span>Формат glTF Binary<small>Геометрия, материалы, текстуры</small></span
            ><span class="format-check">${icon('check')}</span>
          </div>
          <label class="field-label" for="export-path">Файл результата</label>
          <div class="path-row">
            <input
              id="export-path"
              value=""
              placeholder="Путь к файлу .glb…"
              aria-label="Путь к GLB"
            /><button
              id="choose-export"
              class="icon-button"
              title="Выбрать файл GLB"
              aria-label="Выбрать файл GLB"
            >
              ${icon('folder')}
            </button>
          </div>
          <button id="export" class="primary export-button" disabled>
            ${icon('download')} Экспортировать <span>.glb</span>
          </button>
        </section>
        <section class="panel-section activity-section" aria-label="Состояние работы" role="status">
          <div class="status-main">
            <span id="activity-dot" class="status-dot"></span><span id="phase">Готово</span
            ><span id="progress-label"></span
            ><button id="cancel" class="cancel-button" hidden>Отмена</button>
          </div>
          <div class="scene-stats">
            <span><strong id="stat-blocks">—</strong> блоков</span
            ><span><strong id="stat-triangles">—</strong> треугольников</span
            ><span><strong id="stat-materials">—</strong> материалов</span>
          </div>
          <div class="progress-track"><div id="progress"></div></div>
        </section>
      </aside>
    </main>
    <div id="toast" role="status" hidden></div>
    <dialog id="path-dialog">
      <form method="dialog">
        <div class="section-heading">
          <h2 id="path-title">Указать путь</h2>
          <button value="cancel" class="icon-button" aria-label="Закрыть">${icon('cross')}</button>
        </div>
        <p id="path-description" class="section-note"></p>
        <textarea
          id="dialog-path"
          rows="4"
          spellcheck="false"
          placeholder="C:\\Users\\you\\…"
        ></textarea>
        <div class="dialog-actions">
          <button value="cancel" class="secondary">Отмена</button
          ><button value="confirm" class="primary">Выбрать</button>
        </div>
      </form>
    </dialog>
    <dialog id="info-dialog">
      <div class="section-heading">
        <h2 id="info-title"></h2>
        <button id="close-info" class="icon-button" aria-label="Закрыть">${icon('cross')}</button>
      </div>
      <div id="info-body"></div>
    </dialog>`;
}
