// Application-owned SVG symbols; no runtime icon package is needed.
const icons: Record<string, string> = {
  cube: '<path d="m12 3 9 5v8l-9 5-9-5V8l9-5Z"/><path d="m3 8 9 5 9-5M12 13v8m-5-16 9 5"/>',
  folder: '<path d="M3 7V5h7l2 2h9v12H3V7Z"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  arrow: '<path d="M7 17 17 7M7 7h10v10"/>',
  download: '<path d="M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5"/>',
  layers: '<path d="m12 3 10 5-10 5L2 8l10-5Zm-9 9 9 5 9-5m-18 5 9 5 9-5"/>',
  target:
    '<rect x="5" y="5" width="14" height="14" rx="3"/><path d="M12 2v5m0 10v5M2 12h5m10 0h5"/>',
  user: '<circle cx="12" cy="7" r="4"/><path d="M4 22v-3a8 8 0 0 1 16 0v3"/>',
  chevron: '<path d="m9 5 7 7-7 7"/>',
  help: '<circle cx="12" cy="12" r="9"/><path d="M9 8a3 3 0 0 1 6 0c0 3-3 2-3 5m0 3v1"/>',
  refresh: '<path d="M20 7v5h-5M4 17v-5h5"/><path d="M19 8A8 8 0 0 0 5 6m0 10a8 8 0 0 0 14 2"/>',
  map: '<path d="m3 5 6-2 6 2 6-2v16l-6 2-6-2-6 2V5Zm6-2v16m6-14v16"/>',
  cross: '<path d="m6 6 12 12M6 18 18 6"/>',
  check: '<path d="m5 12 5 5L20 7"/>',
};
export const icon = (name: string) =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.65" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${icons[name] || icons.cube}</svg>`;
