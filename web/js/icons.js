// Stroke icons in the brand kit's UI icon style (24 grid, 1.75 stroke, currentColor). Kit paths are reused
// where they exist (terminal, activity, attention, dashboard, connection); the rest are drawn to match.
const P = {
  terminal: 'M4 7l5 5-5 5 M12 17h8',
  activity: 'M2 12h5l3-8 4 16 3-8h5',
  attention: 'M12 3L2 21h20L12 3z M12 9v5 M12 17v1',
  app: 'M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z',
  link: 'M8 7H6a5 5 0 000 10h2 M16 7h2a5 5 0 010 10h-2 M8 12h8',
  check: 'M5 12.5l4.5 4.5L19 7.5',
  clock: 'M12 3a9 9 0 1 0 0 18a9 9 0 1 0 0-18z M12 7.5V12l3 2',
  hourglass: 'M6 3h12 M6 21h12 M7.5 3c0 4.5 9 5.5 9 9s-9 4.5-9 9 M16.5 3c0 4.5-9 5.5-9 9s9 4.5 9 9',
  gauge: 'M4 17a8 8 0 1 1 16 0 M12 17l4.5-6 M7 17h.01 M17 17h.01',
  bolt: 'M13 2.5L4.5 13.5h7l-1 8 8.5-11h-7z',
  coin: 'M12 3a9 9 0 1 0 0 18a9 9 0 1 0 0-18z M14.5 9c-.5-1-1.5-1.5-2.5-1.5-1.6 0-2.5.8-2.5 2s1 1.7 2.5 2 2.5.8 2.5 2-1 2-2.5 2c-1.1 0-2-.5-2.5-1.5 M12 6v1.5 M12 16.5V18',
  tokens: 'M4 6.5h16 M4 12h16 M4 17.5h10',
  calendar: 'M4 5h16v15H4z M4 10h16 M9 3v4 M15 3v4',
  cpu: 'M7 7h10v10H7z M10 10h4v4h-4z M9.5 3v4 M14.5 3v4 M9.5 17v4 M14.5 17v4 M3 9.5h4 M3 14.5h4 M17 9.5h4 M17 14.5h4',
  memory: 'M3 8h18v8H3z M7 8v8 M11 8v8 M15 8v8 M6 16v3 M18 16v3',
  network: 'M8 4v16 M4 8l4-4 4 4 M16 20V4 M12 16l4 4 4-4',
  disk: 'M4 6c0-1.7 3.6-3 8-3s8 1.3 8 3v12c0 1.7-3.6 3-8 3s-8-1.3-8-3z M4 6c0 1.7 3.6 3 8 3s8-1.3 8-3',
  context: 'M5 3h10l4 4v14H5z M15 3v4h4 M8.5 12h7 M8.5 16h5',
  output: 'M4 12h12 M12 7l5 5-5 5 M20 5v14',
  agents: 'M4 5h16v11H4z M8 9.5l2.5 2-2.5 2 M13 13.5h3 M9 20h6',
  layers: 'M12 3l9 5-9 5-9-5z M3 13l9 5 9-5',
};

export function icon(name, size = 18, cls = 'ic') {
  const d = P[name];
  if (!d) return '';
  return `<svg class="${cls}" width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="${d}"/></svg>`;
}
