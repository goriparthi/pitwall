// Client entry: SSE state stream, view selection (panel | full), keyboard shortcuts, command palette.
import { esc } from './format.js';
import { viewAgents, WIDGETS, renderHeader, renderFooter } from './render.js';

const params = new URLSearchParams(location.search);
const view = params.get('view') === 'panel' ? 'panel' : 'full';
const token = document.querySelector('meta[name="pitwall-token"]').content;
document.body.classList.add(`view-${view}`);
if (view === 'panel') {
  const rot = Number(params.get('rotate'));
  if ([90, 180, 270].includes(rot)) document.body.classList.add(`native-${rot}`);
}
// Hardware frame rate follows meaning: full rate while an agent is working or waiting, 2 fps when calm.
const panelFps = Math.round(Number(params.get('fps'))) || 0;
let tickFps = 0;
let tickTimer = null;
function setPanelFps(active) {
  if (view !== 'panel' || !panelFps) return;
  const want = active ? panelFps : Math.min(2, panelFps);
  document.body.classList.toggle('calm', !active);
  if (want === tickFps) return;
  tickFps = want;
  let px = document.getElementById('tick');
  if (!px) {
    px = document.createElement('div');
    px.id = 'tick';
    document.body.appendChild(px);
  }
  clearInterval(tickTimer);
  tickTimer = setInterval(() => {
    px.classList.toggle('b');
    if (!document.body.classList.contains('calm')) document.body.style.setProperty('--time', (performance.now() / 1000).toFixed(3));
  }, 1000 / want);
}

const $ = (id) => document.getElementById(id);
const panel = $('panel');
let state = null;
let lastMsgAt = 0;
let flash = null;
let builtLayout = null;
let slots = [];

// Effective template: focus mode uses the layout flagged focus; otherwise the selected page.
function activeLayout(s) {
  const all = s.layouts ?? [];
  return (s.ui.focus && all.find((l) => l.focus)) || all.find((l) => l.id === s.ui.page) || all[0];
}

// Pixel width of each column for widgets that size themselves (e.g. agent card count).
function columnWidths(cols) {
  const inner = 1920 - 48 - 16 * (cols.length - 1); // matches .panel padding and .grid gap
  const fixed = cols.reduce((sum, c) => sum + (c.endsWith('px') ? parseFloat(c) : 0), 0);
  const fr = cols.reduce((sum, c) => sum + (c.endsWith('fr') ? parseFloat(c) : 0), 0);
  return cols.map((c) => (c.endsWith('px') ? parseFloat(c) : ((inner - fixed) * parseFloat(c)) / (fr || 1)));
}

function buildSlots(layout) {
  const grid = $('grid');
  grid.replaceChildren();
  slots = layout.slots.map((w) => {
    const el = document.createElement('section');
    el.className = `zone zone-${w}`;
    grid.appendChild(el);
    return { widget: w, el };
  });
  grid.style.gridTemplateColumns = layout.columns.join(' ');
  builtLayout = `${layout.id}|${layout.columns.join(',')}|${layout.slots.join(',')}`;
}

async function post(path, body = {}) {
  const res = await fetch(path, { method: 'POST', headers: { 'content-type': 'application/json', 'x-pitwall-token': token }, body: JSON.stringify(body) });
  return { status: res.status, body: await res.json().catch(() => ({})) };
}

function toast(msg) {
  const t = $('toast');
  t.textContent = msg;
  t.hidden = false;
  clearTimeout(toast.t);
  toast.t = setTimeout(() => (t.hidden = true), 2600);
}

function render() {
  if (!state) return;
  const now = Date.now();
  const s = state;
  const layout = activeLayout(s);
  if (!layout) return;
  const key = `${layout.id}|${layout.columns.join(',')}|${layout.slots.join(',')}`;
  if (builtLayout !== key) buildSlots(layout);
  panel.className = `panel layout-${layout.id}`;
  const agents = viewAgents({ ...s, ui: { ...s.ui, focus: s.ui.focus || layout.focus } });
  if (!layout.standalone && agents.some((a) => a.status === 'waiting')) panel.classList.add('has-wait');
  setPanelFps(agents.some((a) => a.status === 'working' || a.status === 'waiting'));
  const widths = columnWidths(layout.columns);
  const ctx = { state: s, agents, now, flash: flash && now - flash.at < 1500 ? flash : null };
  slots.forEach(({ widget, el }, i) => {
    try {
      WIDGETS[widget]?.(el, { ...ctx, width: widths[i] });
    } catch (err) {
      // one broken widget must not blank the whole panel
      el.textContent = `Widget "${widget}" failed: ${err.message}`;
    }
  });
  renderHeader($('hdr'), s, now, agents, layout);
  renderFooter($('ftr'), s, agents, layout);
  if (view === 'full') renderControls(s);
}

// ------- live data -------
function connect() {
  const es = new EventSource('/v1/stream');
  es.onmessage = (ev) => {
    const first = !state;
    state = JSON.parse(ev.data);
    lastMsgAt = Date.now();
    render();
    if (first && view === 'full' && params.get('palette') === '1') openPalette();
  };
  es.onerror = () => {
    es.close();
    setTimeout(connect, 2000);
  };
}

setInterval(() => {
  const off = $('offline');
  const gap = Date.now() - lastMsgAt;
  if (gap > 5000) {
    off.innerHTML = `<span>● Connection unavailable</span><small>${lastMsgAt ? `last update ${Math.round(gap / 1000)}s ago` : 'waiting for first update'} · retrying</small>`;
    off.hidden = false;
  } else off.hidden = true;
}, 1000);

// ------- full view: scaling, controls, keys, palette -------
function fit() {
  if (view !== 'full') return;
  const stage = $('stage');
  panel.style.transform = `scale(${stage.clientWidth / 1920})`;
}

async function setUi(patch) {
  const r = await post('/v1/ui', patch);
  if (r.status === 200 && state) {
    state.ui = r.body.ui;
    render();
  }
}

async function runAction(id) {
  const a = state?.actions.find((x) => x.id === id);
  if (!a) return;
  const r = await post(`/v1/actions/${encodeURIComponent(id)}/run`);
  flash = { id, ok: r.status === 200, at: Date.now() };
  toast(r.status === 200 ? `${a.label}: launched` : `${a.label}: ${r.body.error ?? 'failed'}`);
  render();
}

function cycleProject(dir) {
  const ids = ['all', ...state.projects.map((p) => p.id)];
  const i = ids.indexOf(state.ui.project);
  setUi({ project: ids[(i + dir + ids.length) % ids.length] });
}

function commands() {
  if (!state) return [];
  const s = state;
  return [
    ...s.actions.map((a) => ({ label: a.label, hint: a.hint, grp: 'Action', key: a.key, run: () => runAction(a.id) })),
    ...(s.layouts ?? []).map((l) => {
      const n = (s.pageOrder ?? []).indexOf(l.id);
      return { label: `Layout: ${l.name}`, grp: 'Layout', key: n >= 0 && n < 9 ? String(n + 1) : undefined, run: () => setUi(l.focus ? { focus: true } : { page: l.id, focus: false }) };
    }),
    { label: `${s.ui.focus ? 'Exit' : 'Enter'} focus mode`, grp: 'Mode', key: 'f', run: () => setUi({ focus: !s.ui.focus }) },
    { label: `${s.ui.privacy ? 'Disable' : 'Enable'} privacy mode`, grp: 'Mode', key: 'h', run: () => setUi({ privacy: !s.ui.privacy }) },
    { label: `${s.ui.rotate ? 'Stop' : 'Start'} page rotation`, grp: 'Mode', key: 'r', run: () => setUi({ rotate: !s.ui.rotate }) },
    { label: 'All projects', grp: 'Project', run: () => setUi({ project: 'all' }) },
    ...s.projects.map((p) => ({ label: p.name, hint: p.path, grp: 'Project', run: () => setUi({ project: p.id }) })),
    ...(s.claude?.attention ?? []).map((x) => ({ label: `Dismiss: ${x.name ?? 'session'} ${x.kind}`, grp: 'Attention', run: () => post(`/v1/attention/${encodeURIComponent(x.id)}/ack`) })),
  ];
}

function openPalette() {
  const pal = $('palette');
  let sel = 0;
  pal.innerHTML = '<div class="box"><input name="command" placeholder="Run an action, switch page, project or mode…" aria-label="Command"><ul role="listbox"></ul></div>';
  pal.hidden = false;
  const input = pal.querySelector('input');
  const list = pal.querySelector('ul');
  let items = [];
  const draw = () => {
    const q = input.value.toLowerCase().trim();
    items = commands().filter((c) => !q || `${c.label} ${c.grp} ${c.hint ?? ''}`.toLowerCase().includes(q));
    sel = Math.min(sel, Math.max(0, items.length - 1));
    list.innerHTML = items.map((c, i) => `<li class="${i === sel ? 'sel' : ''}" data-i="${i}">${c.key ? `<span class="kb">${esc(c.key.toUpperCase())}</span>` : '<span class="kb">·</span>'}${esc(c.label)}<span class="grp">${esc(c.grp)}</span></li>`).join('');
  };
  const close = () => {
    pal.hidden = true;
    pal.innerHTML = '';
  };
  const pick = (i) => {
    const c = items[i];
    close();
    c?.run();
  };
  input.addEventListener('input', () => {
    sel = 0;
    draw();
  });
  input.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown') sel = Math.min(sel + 1, items.length - 1);
    else if (e.key === 'ArrowUp') sel = Math.max(sel - 1, 0);
    else if (e.key === 'Enter') return pick(sel);
    else if (e.key === 'Escape') return close();
    else return;
    e.preventDefault();
    draw();
  });
  list.addEventListener('click', (e) => {
    const li = e.target.closest('li');
    if (li) pick(Number(li.dataset.i));
  });
  pal.addEventListener('click', (e) => e.target === pal && close());
  draw();
  input.focus();
}

function onKey(e) {
  if (!state || !$('palette').hidden || e.target.closest?.('input, select, textarea')) return;
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
    e.preventDefault();
    return openPalette();
  }
  if (e.metaKey || e.ctrlKey || e.altKey) return;
  const k = e.key.toLowerCase();
  if (/^[1-9]$/.test(k)) {
    const id = (state.pageOrder ?? [])[Number(k) - 1];
    return id && setUi({ page: id, focus: false });
  }
  if (k === 'f') return setUi({ focus: !state.ui.focus });
  if (k === 'h') return setUi({ privacy: !state.ui.privacy });
  if (k === 'r') return setUi({ rotate: !state.ui.rotate });
  if (k === '[') return cycleProject(-1);
  if (k === ']') return cycleProject(1);
  if (k === 'k') return openPalette();
  const a = state.actions.find((x) => x.key === k);
  if (a) runAction(a.id);
}

function renderControls(s) {
  const c = $('controls');
  c.hidden = false;
  const btn = (label, key, on, data) => `<button class="${on ? 'on' : ''}" ${data}><span class="kb">${key}</span>${label}</button>`;
  const att = (s.claude?.attention ?? []).map((x) => `<button data-ack="${esc(x.id)}">✓ ${esc(s.ui.privacy ? 'session' : x.name ?? 'session')} · ${esc(x.kind)}</button>`).join('');
  const names = Object.fromEntries((s.layouts ?? []).map((l) => [l.id, l.name]));
  const others = (s.layouts ?? []).filter((l) => !(s.pageOrder ?? []).includes(l.id) && !l.focus);
  const html = `${(s.pageOrder ?? []).map((id, i) => btn(esc(names[id] ?? id), i + 1, s.ui.page === id && !s.ui.focus, `data-page="${esc(id)}"`)).join('')}
    ${others.length ? `<select name="layout" data-layout aria-label="More layouts"><option value="">More layouts</option>${others.map((l) => `<option value="${esc(l.id)}" ${s.ui.page === l.id ? 'selected' : ''}>${esc(l.name)}</option>`).join('')}</select>` : ''}
    ${btn('Focus', 'F', s.ui.focus, 'data-toggle="focus"')}${btn('Privacy', 'H', s.ui.privacy, 'data-toggle="privacy"')}${btn('Rotate', 'R', s.ui.rotate, 'data-toggle="rotate"')}
    <select name="project" data-project aria-label="Project"><option value="all">All projects</option>${s.projects.map((p) => `<option value="${esc(p.id)}" ${s.ui.project === p.id ? 'selected' : ''}>${esc(p.name)}</option>`).join('')}</select>
    <span class="grow"></span>
    <label>Panel brightness <input name="brightness" type="range" min="5" max="100" step="5" value="${s.ui.brightness}" data-brightness></label>
    <button data-palette><span class="kb">⌘K</span>Command palette</button>
    ${att ? `<div class="att"><span>Attention, select to acknowledge:</span>${att}</div>` : ''}`;
  if (c.__html !== html && !c.contains(document.activeElement)) {
    c.innerHTML = html;
    c.__html = html;
  }
}

if (view === 'full') {
  window.addEventListener('resize', fit);
  new ResizeObserver(fit).observe($('stage'));
  fit();
  document.addEventListener('keydown', onKey);
  $('controls').addEventListener('click', (e) => {
    const b = e.target.closest('button');
    if (!b) return;
    if (b.dataset.page) setUi({ page: b.dataset.page, focus: false });
    else if (b.dataset.toggle) setUi({ [b.dataset.toggle]: !state.ui[b.dataset.toggle] });
    else if (b.dataset.ack) post(`/v1/attention/${encodeURIComponent(b.dataset.ack)}/ack`);
    else if ('palette' in b.dataset) openPalette();
    b.blur();
  });
  $('controls').addEventListener('change', (e) => {
    if ('project' in e.target.dataset) setUi({ project: e.target.value });
    if ('layout' in e.target.dataset && e.target.value) setUi({ page: e.target.value, focus: false });
    if ('brightness' in e.target.dataset) setUi({ brightness: Number(e.target.value) });
    e.target.blur();
  });
  panel.addEventListener('click', (e) => {
    const b = e.target.closest('[data-action]');
    if (b) runAction(b.dataset.action);
  });
}

connect();
setInterval(render, 1000);
