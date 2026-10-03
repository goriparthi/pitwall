// Formatting and escaping helpers shared by renderers.
export const esc = (s) =>
  String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);

export function bytes(n, digits = 1) {
  if (!Number.isFinite(n)) return 'n/a';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (n >= 1000 && i < u.length - 1) {
    n /= 1000;
    i++;
  }
  return `${n.toFixed(i === 0 ? 0 : n >= 100 ? 0 : digits)} ${u[i]}`;
}

// Binary units labelled GB, matching how macOS reports memory.
export function gib(n, digits = 1) {
  if (!Number.isFinite(n)) return 'n/a';
  const g = n / 1024 ** 3;
  return g >= 1 ? `${g.toFixed(g >= 100 ? 0 : digits)} GB` : `${Math.round(n / 1024 ** 2)} MB`;
}

export function rate(bps) {
  if (!Number.isFinite(bps)) return 'n/a';
  return bps < 1000 ? `${Math.round(bps)} B/s` : `${bytes(bps)}/s`;
}

export function tokens(n) {
  if (!Number.isFinite(n)) return 'n/a';
  if (n >= 1e6) return `${(n / 1e6).toFixed(n >= 1e7 ? 0 : 1)}M`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(n >= 1e5 ? 0 : 1)}k`;
  return String(Math.round(n));
}

export function duration(ms) {
  if (!Number.isFinite(ms) || ms < 0) return '';
  const s = Math.floor(ms / 1000);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const ss = String(s % 60).padStart(2, '0');
  if (h >= 24) return `${Math.floor(h / 24)}d ${h % 24}h`;
  return h ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`;
}

export function ago(ms) {
  if (!Number.isFinite(ms)) return '';
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h`;
  return `${Math.floor(s / 86400)}d`;
}

export function clock(ts) {
  return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
}

// Area sparkline as SVG; values scaled to [0,max]. Returns markup for a viewBox 0..100 x 0..40.
export function spark(values, { max, color, fill = true, id }) {
  const v = (values ?? []).filter(Number.isFinite);
  if (v.length < 2) return '';
  const top = max ?? Math.max(...v, 1);
  const pts = v.map((y, i) => [(i / (v.length - 1)) * 100, 40 - (Math.min(y, top) / top) * 36 - 2]);
  const line = pts.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(2)},${y.toFixed(2)}`).join('');
  const gid = `g-${id}`;
  return `<svg class="spark" viewBox="0 0 100 40" preserveAspectRatio="none" aria-hidden="true">
    <defs><linearGradient id="${gid}" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${color}" stop-opacity="0.32"/><stop offset="1" stop-color="${color}" stop-opacity="0"/></linearGradient></defs>
    ${fill ? `<path d="${line}L100,40L0,40Z" fill="url(#${gid})"/>` : ''}
    <path d="${line}" fill="none" stroke="${color}" stroke-width="1.6" vector-effect="non-scaling-stroke" stroke-linejoin="round"/>
  </svg>`;
}
