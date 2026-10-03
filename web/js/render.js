// Zone renderers. Cards are keyed DOM nodes so status animations keep running across 1 Hz updates.
import { esc, bytes, gib, rate, tokens, duration, clock, spark } from './format.js';
import { icon } from './icons.js';

const STATUS_WORD = { working: 'Working', waiting: 'Waiting', failed: 'Failed', completed: 'Done', idle: 'Idle', unknown: 'Unknown' };
const WAIT_TEXT = { approval: 'needs approval', question: 'asked you a question', 'plan approval': 'has a plan to review', input: 'needs input' };

const setHTML = (el, html) => {
  if (el.__html !== html) {
    el.innerHTML = html;
    el.__html = html;
  }
};
const setText = (el, t) => {
  if (el.textContent !== t) el.textContent = t;
};

// Applies project filter and privacy masking to produce what the panel may show.
export function viewAgents(s) {
  const proj = s.projects.find((p) => p.id === s.ui.project);
  let agents = s.claude?.agents ?? [];
  if (proj) agents = agents.filter((a) => a.cwd && (a.cwd === proj.path || a.cwd.startsWith(proj.path + '/')));
  if (s.ui.focus) agents = agents.filter((a) => ['working', 'waiting', 'failed'].includes(a.status));
  if (!s.ui.privacy) return agents;
  const projIdx = new Map();
  return agents.map((a, i) => {
    if (a.project && !projIdx.has(a.project)) projIdx.set(a.project, projIdx.size);
    return {
      ...a,
      name: `Session ${i + 1}`,
      project: a.project ? `Project ${String.fromCharCode(65 + projIdx.get(a.project))}` : null,
      task: a.task ? 'Task hidden' : null,
      lastEvent: a.lastEvent ? a.lastEvent.split(' ')[0] : null,
      cwd: null,
      masked: true,
    };
  });
}

function staleness(m, now) {
  if (!m) return 'missing';
  if (m.unavailable) return 'unavailable';
  return now - m.updatedAt > m.intervalMs * 3 + 1500 ? 'stale' : 'fresh';
}

// ---------------- AI zone ----------------
function cardSkeleton(id) {
  const el = document.createElement('div');
  el.className = 'card';
  el.dataset.id = id;
  el.innerHTML = '<div class="card-top"><span class="orb"></span><span class="state"></span><span class="num el"></span></div><div class="name"></div><div class="task"></div><div class="event"></div><div class="foot"></div>';
  return el;
}

function updateCard(el, a, now) {
  const cls = `card s-${a.status}`;
  if (el.className !== cls) el.className = cls;
  const q = (sel) => el.querySelector(sel);
  setText(q('.state'), STATUS_WORD[a.status] ?? a.status);
  setText(q('.el'), a.since ? duration(now - a.since) : '');
  setText(q('.name'), a.name ?? a.project ?? 'Claude Code');
  const sub = [a.project && a.name && !a.masked ? a.project : null].filter(Boolean);
  const task = a.task ?? 'Task title not available yet';
  setText(q('.task'), sub.length ? `${sub[0]} · ${task}` : task);
  let ev = '';
  if (a.status === 'waiting') ev = `<span class="arrow">▲</span><span class="hot">${esc(WAIT_TEXT[a.wait?.kind] ?? 'is waiting for you')}${a.wait?.tool ? ` · ${esc(a.wait.tool)}` : ''}</span>`;
  else if (a.status === 'failed') ev = `<span class="arrow">✕</span><span class="bad">${esc(String(a.error ?? 'error').replaceAll('_', ' '))}</span>`;
  else if (a.status === 'completed' && a.lastTurnMs) ev = `<span class="arrow">↳</span>Finished in ${duration(a.lastTurnMs)}`;
  else if (a.statusSource === 'registry' && !a.hooks) ev = '<span class="arrow">↳</span>Status from session registry';
  else if (a.lastEvent) ev = `<span class="arrow">↳</span>${esc(a.lastEvent)}${a.subagents ? ` · ${a.subagents} subagent${a.subagents > 1 ? 's' : ''}` : ''}`;
  setHTML(q('.event'), ev);
  const u = a.usage;
  const foot = u
    ? `<span class="mono fi" title="context size">${icon('context', 18)}${tokens(u.contextTokens)}</span><span class="mono fi" title="output tokens">${icon('output', 18)}${tokens(u.outputTokens)}</span>`
    : '<span>usage unavailable</span>';
  setHTML(q('.foot'), foot);
}

function renderCards(host, agents, now, slots) {
  const visible = agents.length > slots ? agents.slice(0, slots - 1) : agents;
  const rest = agents.slice(visible.length);
  const keep = new Set(visible.map((a) => a.id));
  host.__cards ??= new Map();
  for (const [id, el] of host.__cards) {
    if (!keep.has(id)) {
      el.remove();
      host.__cards.delete(id);
    }
  }
  host.querySelectorAll('.card.more, .card.empty').forEach((e) => e.remove());
  visible.forEach((a, i) => {
    let el = host.__cards.get(a.id);
    if (!el) {
      el = cardSkeleton(a.id);
      host.__cards.set(a.id, el);
    }
    updateCard(el, a, now);
    if (host.children[i] !== el) host.insertBefore(el, host.children[i] ?? null);
  });
  if (rest.length) {
    const more = document.createElement('div');
    more.className = 'card more';
    more.innerHTML = `<div class="eyebrow">+${rest.length} more</div>${rest
      .slice(0, 4)
      .map((a) => `<div class="row"><span class="dot s-${a.status}"></span>${esc(a.name ?? a.project ?? 'session')}<span style="margin-left:auto" class="mono">${esc(STATUS_WORD[a.status])}</span></div>`)
      .join('')}`;
    host.appendChild(more);
  }
}

export function renderAI(zone, s, agents, now, width = 868) {
  if (!zone.__built) {
    zone.innerHTML = `<div class="zone-head"><span class="eyebrow">${icon('agents', 18)}Agents</span><div class="counts"></div><span class="right"></span></div><div class="cards"></div>`;
    zone.__built = true;
  }
  const counts = { waiting: 0, working: 0, failed: 0, completed: 0, idle: 0 };
  for (const a of agents) counts[a.status] = (counts[a.status] ?? 0) + 1;
  setHTML(
    zone.querySelector('.counts'),
    ['waiting', 'working', 'failed', 'completed', 'idle']
      .filter((k) => counts[k])
      .map((k) => `<span class="c"><span class="dot s-${k}"></span><b class="num">${counts[k]}</b>${STATUS_WORD[k].toLowerCase()}</span>`)
      .join('') || '<span class="c">no sessions</span>'
  );
  const t = s.claude?.today;
  setHTML(zone.querySelector('.zone-head .right'), t ? `today <span class="mono">${t.turns}</span> turns · <span class="mono">${tokens(t.output)}</span> out` : '');

  const cards = zone.querySelector('.cards');
  // as many ~270px cards as the slot holds (3 in the balanced layout, 4 in the wide ones)
  const slots = Math.max(1, Math.min(6, Math.floor((width + 16) / 286)));
  cards.style.gridTemplateColumns = `repeat(${slots}, minmax(0, 1fr))`;
  if (!agents.length) {
    renderCards(cards, [], now, slots);
    const e = document.createElement('div');
    e.className = 'card empty';
    e.innerHTML = s.ui.focus
      ? '<div class="big">Nothing active</div><div>Focus shows working, waiting and failed sessions.</div>'
      : `<div class="big">No Claude Code sessions${s.ui.project !== 'all' ? ' in this project' : ''}</div><div>Start one from the launcher or any terminal.</div>`;
    cards.appendChild(e);
  } else renderCards(cards, agents, now, slots);
}

// ---------------- Health zone ----------------
// Tile layout: label row, then value (left) beside a sparkline/bar (right), then a sub line; nothing overlaps.
function tile(key, label, m, now, value, side, sub) {
  const st = staleness(m, now);
  const tag = st === 'stale' ? '<span class="tag warn">stale</span>' : '';
  if (st === 'unavailable' || st === 'missing' || m?.value == null) {
    return `<div class="tile na"><div class="lbl">${label}</div><div class="val">${st === 'missing' ? 'collecting…' : esc(m?.unavailable ?? 'unavailable')}</div></div>`;
  }
  return `<div class="tile t-${key}"><div class="lbl">${label}${tag}</div><div class="main"><div class="val num">${value}</div><div class="side">${side}</div></div>${sub ? `<div class="sub">${sub}</div>` : ''}</div>`;
}

export function renderHealthMini(zone, s) {
  const sys = s.system ?? {};
  {
    const v = (m, f) => (m?.value == null ? 'n/a' : f(m));
    setHTML(zone, `<div class="mini">
      <div class="m"><span class="k">CPU</span><span class="v num">${v(sys.cpu, (m) => Math.round(m.value) + '%')}</span></div>
      <div class="m"><span class="k">MEM</span><span class="v num">${v(sys.mem, (m) => Math.round(m.value) + '%')}</span></div>
      <div class="m"><span class="k">NET ↓</span><span class="v num" style="font-size:24px">${v(sys.net, (m) => rate(m.rxBps))}</span></div>
      <div class="m"><span class="k">DISK</span><span class="v num" style="font-size:24px">${v(sys.disk, (m) => bytes(m.freeBytes, 0))}</span></div></div>`);
  }
}

export function renderHealth(zone, s, now) {
  const sys = s.system ?? {};
  const h = sys.history ?? {};
  const cpu = tile('cpu', `${icon('cpu', 17)}CPU`, sys.cpu, now, `${Math.round(sys.cpu?.value)}<small>%</small>`,
    spark(h.cpu, { max: 100, color: 'var(--pw-accent)', id: 'cpu' }),
    `load <span class="mono">${sys.cpu?.load?.[0]?.toFixed(2) ?? 'n/a'}</span> · ${sys.cores ?? '?'} cores`);
  const mem = tile('mem', `${icon('memory', 17)}Memory`, sys.mem, now, `${Math.round(sys.mem?.value)}<small>%</small>`,
    spark(h.mem, { max: 100, color: 'var(--muted)', id: 'mem' }),
    `<span class="mono">${gib(sys.mem?.used)}</span> of <span class="mono">${gib(sys.mem?.total, 0)}</span>${sys.mem?.swapBytes > 5e8 ? ` · swap <span class="mono">${gib(sys.mem.swapBytes)}</span>` : ''}`);
  const disk = tile('disk', `${icon('disk', 17)}Disk free`, sys.disk, now, bytes(sys.disk?.freeBytes, 0).replace(' ', '<small>') + '</small>',
    `<div class="bar"><i style="width:${Math.round(sys.disk?.value ?? 0)}%;background:var(--muted)"></i></div>`,
    `of <span class="mono">${bytes(sys.disk?.totalBytes, 0)}</span> · <span class="mono">${Math.round(sys.disk?.value)}%</span> used`);
  const net = tile('net', `${icon('network', 17)}Network`, sys.net, now,
    `<span class="nr"><span class="ar" style="color:var(--work)">↓</span>${rate(sys.net?.rxBps)}</span><span class="nr tx"><span class="ar" style="color:var(--faint)">↑</span>${rate(sys.net?.txBps)}</span>`,
    spark(h.netRx, { color: 'var(--work)', id: 'rx' }) + spark(h.netTx, { color: 'var(--faint)', id: 'tx', fill: false, max: Math.max(...(h.netRx ?? [1]), ...(h.netTx ?? [1]), 1) }),
    '');
  const extra = [];
  if (sys.gpu?.value != null) extra.push(`GPU <span class="mono">${Math.round(sys.gpu.value)}%</span>`);
  if (sys.battery?.value != null) extra.push(`Battery <span class="mono">${sys.battery.percent}%</span>${sys.battery.source === 'ac' ? ' ⚡' : ''}`);
  if (sys.thermal?.value && sys.thermal.value !== 'nominal') extra.push(`<span style="color:var(--wait)">Thermal ${esc(sys.thermal.value)}</span>`);
  setHTML(zone, `<div class="zone-head"><span class="eyebrow">${icon('cpu', 18)}${esc(s.ui.privacy ? 'This computer' : sys.host ?? 'This computer')}</span><span class="right">${extra.join(' · ')}</span></div>
    <div class="tiles">${cpu}${mem}${net}${disk}</div>`);
}

// ---------------- Launcher zone ----------------
const KIND_ICON = { terminal: 'terminal', 'open-app': 'app', 'open-url': 'link' };
export function renderLaunch(zone, s, now, flash) {
  const proj = s.projects.find((p) => p.id === s.ui.project);
  const projName = proj ? (s.ui.privacy ? 'Project hidden' : proj.name) : 'All projects';
  const actions = (s.actions ?? []).slice(0, 6).map((a) => `<button class="action${flash?.id === a.id ? (flash.ok ? ' flash' : ' flash-bad') : ''}" data-action="${esc(a.id)}"><span class="kb">${esc((a.key ?? '').toUpperCase())}</span><span class="ki">${icon(KIND_ICON[a.kind] ?? 'app', 20)}</span><span class="lab">${esc(a.label)}</span><span class="hint">${esc(a.hint ?? '')}</span></button>`).join('');
  setHTML(zone, `<div class="zone-head"><span class="eyebrow">${icon('bolt', 18)}Launch</span><span class="right">in <b>${esc(projName)}</b></span></div>
    <div class="actions">${actions}</div>`);
}

// ---------------- Limits (RedLine integration; only rendered when RedLine is installed) ----------------
const WINDOW_LABEL = { five_hour: '5h', seven_day: '7d' };

function severity(w, now) {
  if (w.used >= 90) return 'bad';
  if (w.used >= 75 || (w.pace?.hitsBeforeReset && w.resetsAt > now)) return 'warn';
  return 'ok';
}

function untilText(ms) {
  if (!(ms > 0)) return 'now';
  const m = Math.round(ms / 60000);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h}h ${String(m % 60).padStart(2, '0')}m`;
  return `${Math.floor(h / 24)}d ${h % 24}h`;
}

function whenText(ts, now) {
  const d = new Date(ts);
  const sameDay = new Date(now).toDateString() === d.toDateString();
  const t = d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  return sameDay ? t : `${d.toLocaleDateString([], { weekday: 'short' })} ${t}`;
}

// 270 degree gauge in the Dial's spirit: track, used arc, and a tick for how much of the window has passed.
function gauge(w, now, size = 96) {
  const r = 38, c = 50, sweep = 270, start = 135;
  const pt = (deg) => [c + r * Math.cos((deg * Math.PI) / 180), c + r * Math.sin((deg * Math.PI) / 180)];
  const arc = (from, to) => {
    const [x1, y1] = pt(from), [x2, y2] = pt(to);
    return `M${x1.toFixed(2)} ${y1.toFixed(2)}A${r} ${r} 0 ${to - from > 180 ? 1 : 0} 1 ${x2.toFixed(2)} ${y2.toFixed(2)}`;
  };
  const used = Math.max(0, Math.min(100, w.used));
  const passed = w.lengthMs ? Math.max(0, Math.min(1, 1 - (w.resetsAt - now) / w.lengthMs)) : null;
  const tick = passed == null ? '' : (() => {
    const a = start + sweep * passed;
    const [x1, y1] = [c + (r - 6) * Math.cos((a * Math.PI) / 180), c + (r - 6) * Math.sin((a * Math.PI) / 180)];
    const [x2, y2] = [c + (r + 6) * Math.cos((a * Math.PI) / 180), c + (r + 6) * Math.sin((a * Math.PI) / 180)];
    return `<line class="g-tick" x1="${x1.toFixed(2)}" y1="${y1.toFixed(2)}" x2="${x2.toFixed(2)}" y2="${y2.toFixed(2)}"/>`;
  })();
  return `<svg class="gauge g-${severity(w, now)}" width="${size}" height="${size}" viewBox="0 0 100 100" aria-hidden="true">
    <path class="g-track" d="${arc(start, start + sweep)}"/>${used > 0.5 ? `<path class="g-fill" d="${arc(start, start + (sweep * used) / 100)}"/>` : ''}${tick}
    <text x="50" y="56" class="g-num">${Math.round(used)}%</text></svg>`;
}

function paceLine(w, now) {
  const p = w.pace;
  if (!p) return `<span class="lr dim">${icon('hourglass', 18)}Pace not measured yet</span>`;
  if (p.hitsBeforeReset && p.exhaustsAt) return `<span class="lr warn">${icon('attention', 18)}Runs out ${whenText(p.exhaustsAt, now)}, before reset</span>`;
  return `<span class="lr ok">${icon('check', 18)}Lasts past the reset</span>`;
}

export function renderLimits(zone, s, now) {
  const L = s.limits;
  if (!L?.installed) return setHTML(zone, '');
  const age = L.asOf ? now - L.asOf : null;
  const stale = age != null && age > 15 * 60000;
  const head = `<div class="zone-head"><span class="eyebrow">${icon('gauge', 18)}Plan limits</span><span class="right">${L.available ? `${esc(L.source === 'redline status' ? 'RedLine' : L.source)} · ${age == null ? 'time unknown' : age < 60000 ? 'just now' : `${untilText(age)} ago`}` : esc(L.reason || 'no reading yet')}${stale ? ' <span class="tag warn">stale</span>' : ''}</span></div>`;
  if (!L.available || !L.windows?.length) {
    return setHTML(zone, `${head}<div class="lim-empty">${icon('gauge', 28)}<div><b>RedLine has no reading yet</b><br>Limits appear after Claude Code draws its status line once.</div></div>`);
  }
  const rows = L.windows.slice(0, 2).map((w) => {
    const left = Math.max(0, 100 - w.used);
    return `<div class="lim g-${severity(w, now)}${stale ? ' is-stale' : ''}">
      ${gauge(w, now)}
      <div class="lim-main"><span class="lim-k">${icon(w.key === 'seven_day' ? 'calendar' : 'clock', 17)}${esc(w.name)} · ${esc(WINDOW_LABEL[w.key] ?? w.key)}</span><span class="lim-left num">${Math.round(left)}%<small> left</small></span></div>
      <div class="lim-rows">
        <span class="lr">${icon('clock', 18)}Resets in ${untilText(w.resetsAt - now)}</span>
        <span class="lr">${icon('bolt', 18)}${w.pace ? `${w.pace.ratePerHour.toFixed(w.pace.ratePerHour < 10 ? 1 : 0)}% an hour` : 'Rate unknown'}</span>
        ${paceLine(w, now)}
      </div></div>`;
  }).join('');
  const tot = L.today ? `<div class="lim-tot"><span>${icon('coin', 17)}Today <b class="num">${tokens(L.today.tokens)}</b> tokens · <b class="num">$${L.today.costUsd.toFixed(2)}</b></span><span>Week <b class="num">${tokens(L.week?.tokens)}</b> · <b class="num">$${(L.week?.costUsd ?? 0).toFixed(0)}</b></span><span class="est">cost is an estimate</span></div>` : '';
  setHTML(zone, `${head}<div class="lims">${rows}</div>${tot}`);
}

// Header chip on every template when RedLine is installed: session and week used, coloured by severity.
function limitChip(s, now) {
  const L = s.limits;
  if (!L?.installed || !L.available || !L.windows?.length) return '';
  const by = Object.fromEntries(L.windows.map((w) => [w.key, w]));
  const part = (w, label) => (w ? `<span class="lc-${severity(w, now)}">${label} ${Math.round(w.used)}%</span>` : '');
  return `<span class="limchip">${icon('gauge', 18)}${part(by.five_hour, '5h')}${part(by.seven_day, 'wk')}</span>`;
}

// ---------------- Ops: read-only status commands ----------------
const OPS_WORD = { ok: 'OK', warn: 'Warn', crit: 'Crit', unknown: 'Unknown' };

function agoText(ms) {
  return ms < 60000 ? 'just now' : `${untilText(ms)} ago`;
}

// A source with no trusted reading shows grey: a failed read is never red; red means the pipeline is in trouble.
function opsState(src) {
  if (!src.hasReading || src.failing) return 'unknown';
  return src.stale ? 'unknown' : src.status;
}

const compact = (n) => (n >= 1e6 ? `${(n / 1e6).toFixed(1)}M` : n >= 1e4 ? `${Math.round(n / 1e3)}k` : n >= 1e3 ? `${(n / 1e3).toFixed(1)}k` : `${Math.round(n)}`);
const hhmm = (ms) => new Date(ms).toTimeString().slice(0, 5);
// neutrals for series the source leaves uncolored: bars recede, lines read as primary ink
const NEUTRAL = { bar: '#5B6A75', line: '#F3F5EF', area: '#83959F' };

function niceMax(v) {
  if (!(v > 0)) return 1;
  const p = 10 ** Math.floor(Math.log10(v));
  return [1, 2, 2.5, 5, 10].map((m) => m * p).find((m) => m >= v);
}

// x ticks on whole hours, about four across the range
function hourTicks(t0, t1) {
  const span = (t1 - t0) / 3600000;
  const step = [1, 2, 3, 4, 6, 12].find((h) => span / h <= 4) ?? 24;
  const out = [];
  const d = new Date(t0);
  d.setMinutes(0, 0, 0);
  for (let t = d.getTime(); t <= t1; t += 3600000) if (t >= t0 && new Date(t).getHours() % step === 0) out.push(t);
  return out;
}

// One chart as inline SVG. The panel has no input, so values are labelled in the legend instead of tooltips.
function chartSVG(c, w, h) {
  const pad = { l: 56, r: 8, t: 8, b: 32 }; // b leaves room under the 0 label for the time labels
  const pw = w - pad.l - pad.r;
  const ph = h - pad.t - pad.b;
  const series = c.series.filter((s) => s.points.length);
  if (!series.length) return `<div class="op-nochart">No data in range</div>`;
  const ts = series.flatMap((s) => s.points.map((p) => p[0]));
  const t0 = Math.min(...ts);
  const t1 = Math.max(...ts);
  const stacked = c.kind === 'stacked-area';
  let top = 0;
  const stack = new Map();
  if (stacked) {
    for (const t of new Set(ts)) {
      let sum = 0;
      for (const s of series) sum += s.points.find((p) => p[0] === t)?.[1] ?? 0;
      top = Math.max(top, sum);
    }
  } else top = Math.max(...series.flatMap((s) => s.points.map((p) => p[1])));
  const ymax = niceMax(top);
  const bucket = !stacked && series.some((s) => s.kind === 'bar') ? (t1 - t0) / Math.max(1, series.find((s) => s.kind === 'bar').points.length - 1) : 0;
  const span = t1 - t0 + bucket || 1;
  const x = (t) => pad.l + ((t - t0 + bucket / 2) / span) * pw;
  const y = (v) => pad.t + ph - (v / ymax) * ph;
  const grid = [0, 0.5, 1].map((f) => `<line class="cg" x1="${pad.l}" x2="${pad.l + pw}" y1="${y(ymax * f)}" y2="${y(ymax * f)}"/><text class="ct" x="${pad.l - 8}" y="${y(ymax * f) + 6}" text-anchor="end">${compact(ymax * f)}</text>`).join('');
  const ticks = hourTicks(t0, t1).map((t) => `<text class="ct" x="${x(t)}" y="${h - 4}" text-anchor="middle">${hhmm(t)}</text>`).join('');
  let marks = '';
  if (stacked) {
    const times = [...new Set(ts)].sort((a, b) => a - b);
    const base = new Map(times.map((t) => [t, 0]));
    for (const s of series) {
      const at = new Map(s.points);
      const lo = times.map((t) => base.get(t));
      const hi = times.map((t, i) => lo[i] + (at.get(t) ?? 0));
      if (hi.every((v, i) => v === lo[i])) continue;
      const upper = times.map((t, i) => `${x(t).toFixed(1)},${y(hi[i]).toFixed(1)}`);
      const lower = times.map((t, i) => `${x(t).toFixed(1)},${y(lo[i]).toFixed(1)}`).reverse();
      // a 2px surface stroke on each top edge keeps stacked fills apart
      marks += `<path d="M${upper.join('L')}L${lower.join('L')}Z" fill="${s.color || NEUTRAL.area}" fill-opacity=".85"/><path d="M${upper.join('L')}" fill="none" stroke="var(--card)" stroke-width="2"/>`;
      times.forEach((t, i) => base.set(t, hi[i]));
    }
  } else {
    const bars = series.filter((s) => s.kind === 'bar');
    const bw = Math.max(3, (pw / Math.max(1, bars[0]?.points.length ?? 1)) * 0.72 / Math.max(1, bars.length));
    bars.forEach((s, k) => {
      for (const [t, v] of s.points) {
        const bx = x(t) - (bw * bars.length) / 2 + k * bw + 1;
        const by = y(v);
        const bh = pad.t + ph - by;
        if (bh <= 0) continue;
        const r = Math.min(4, bh, (bw - 2) / 2);
        // rounded data end, square on the baseline
        marks += `<path d="M${bx},${by + bh}V${by + r}Q${bx},${by} ${bx + r},${by}H${bx + bw - 2 - r}Q${bx + bw - 2},${by} ${bx + bw - 2},${by + r}V${by + bh}Z" fill="${s.color || NEUTRAL.bar}"/>`;
      }
    });
    for (const s of series.filter((s) => s.kind === 'line')) {
      const pts = s.points.map(([t, v]) => `${x(t).toFixed(1)},${y(v).toFixed(1)}`);
      const col = s.color || NEUTRAL.line;
      marks += `<path d="M${pts.join('L')}" fill="none" stroke="${col}" stroke-width="2" stroke-linejoin="round"/>`;
      marks += s.points.map(([t, v]) => `<circle cx="${x(t).toFixed(1)}" cy="${y(v).toFixed(1)}" r="4" fill="${col}" stroke="var(--card)" stroke-width="2"/>`).join('');
    }
  }
  return `<svg class="op-svg" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}" role="img" aria-label="${esc(c.label)}">${grid}${marks}${ticks}</svg>`;
}

function chartLegend(c) {
  return c.series.map((s) => {
    const last = s.points.length ? s.points[s.points.length - 1][1] : null;
    const col = s.color || NEUTRAL[s.kind] || NEUTRAL.area;
    const sw = s.kind === 'line' ? `<svg width="18" height="10" aria-hidden="true"><line x1="1" y1="5" x2="17" y2="5" stroke="${col}" stroke-width="2"/><circle cx="9" cy="5" r="3" fill="${col}"/></svg>` : `<span class="op-sw" style="background:${col}"></span>`;
    return `<span class="op-li">${sw}${esc(s.name)}${last != null ? ` <b class="num">${compact(last)}</b>` : ''}</span>`;
  }).join('');
}

function opsHead(src, now, label) {
  const st = opsState(src);
  const tag = src.failing
    ? '<span class="tag">No reading</span>'
    : `<span class="tag op-${st}">${esc(OPS_WORD[st])}</span>${src.stale ? ' <span class="tag warn">stale</span>' : ''}`;
  const read = !src.hasReading
    ? src.error ? `no reading · ${esc(src.error)}` : 'waiting for the first reading'
    : src.failing ? `${esc(src.error)} · last read ${agoText(now - src.asOf)}` : `read ${agoText(now - src.asOf)}`;
  return `<div class="zone-head"><span class="eyebrow">${icon('activity', 18)}${esc(label)}</span>${tag}<span class="right"><b>${esc(src.label)}</b> · ${read}</span></div>`;
}

function opsCard(it, old, priv, swatches) {
  const st = old ? 'unknown' : it.status;
  const sw = it.series && swatches[it.series] ? `<span class="op-sw" style="background:${swatches[it.series]}"></span>` : '<span class="op-dot"></span>';
  const flag = !old && (st === 'warn' || st === 'crit') ? `<span class="tag op-${st}">${OPS_WORD[st]}</span>` : '';
  return `<div class="op op-${st}"><span class="op-k">${sw}<span class="op-kl">${esc(it.label)}</span>${flag}</span>
      <span class="op-v num">${esc(it.value)}</span><span class="op-d">${priv ? '' : esc(it.detail ?? '')}</span></div>`;
}

export function renderOps(zone, s, now, width = 600) {
  const O = s.ops;
  if (!O?.present || !O.sources?.length) return setHTML(zone, '');
  const priv = s.ui.privacy;
  const [src, ...others] = O.sources;
  const old = src.stale || src.failing;
  const head = opsHead(src, now, 'Ops');
  if (!src.hasReading) {
    return setHTML(zone, `${head}<div class="lim-empty">${icon('activity', 28)}<div><b>No reading yet</b><br>${src.error ? `The status command failed: ${esc(src.error)}.` : 'The status command runs on its interval.'}</div></div>`);
  }
  const swatches = Object.fromEntries((src.charts ?? []).flatMap((c) => c.series.filter((x) => x.color).map((x) => [x.name, x.color])));
  const notes = src.findings.filter((f) => f.status !== 'ok').slice(0, 2);
  const line = priv
    ? ''
    : notes.length
      ? notes.map((f) => `<div class="op-f op-${old ? 'unknown' : f.status}">${icon('attention', 18)}<span>${esc(f.text)}</span></div>`).join('')
      : src.summary ? `<div class="op-f">${icon('check', 18)}<span>${esc(src.summary)}</span></div>` : '';
  const more = others.map((o) => `<div class="op-f op-${opsState(o)}"><span class="op-dot"></span><span>${esc(o.label)} · ${esc(OPS_WORD[opsState(o)])}</span></div>`).join('');

  // Wide slot: the full desk, cards on the left and charts on the right, like the Grafana dashboard.
  const charts = src.charts ?? [];
  if (width >= 1400) {
    const cardsW = charts.length ? 880 : width;
    const chartW = charts.length ? Math.floor((width - cardsW - 16 * charts.length) / charts.length) : 0;
    const cards = src.items.slice(0, 8).map((it) => opsCard(it, old, priv, swatches)).join('');
    const panes = charts.map((c) => `<div class="op-chart"><div class="op-ch"><span class="op-cl">${esc(c.label)}</span>${c.unit ? `<span class="op-cu">${esc(c.unit)}</span>` : ''}</div>
        ${chartSVG(c, chartW - 24, 190)}<div class="op-lg">${chartLegend(c)}</div></div>`).join('');
    return setHTML(zone, `${head}<div class="ops ops-wide${old ? ' is-stale' : ''}" style="grid-template-columns:${cardsW}px${charts.map(() => ' minmax(0,1fr)').join('')}">
      <div class="op-tiles op-tiles-8">${cards}</div>${panes}</div><div class="op-foot${old ? ' is-stale' : ''}">${line}${more}</div>`);
  }
  // Narrow slot: the summary items, falling back to the first four.
  const pick = [...src.items.filter((i) => !i.series), ...src.items.filter((i) => i.series)].slice(0, 4);
  const tiles = pick.map((it) => opsCard(it, old, priv, swatches)).join('');
  setHTML(zone, `${head}<div class="ops${old ? ' is-stale' : ''}"><div class="op-tiles">${tiles}</div>${line}${more}</div>`);
}

// Header chip on every template when ops is configured: the worst trusted status across sources.
function opsChip(s) {
  const srcs = s.ops?.present ? s.ops.sources ?? [] : [];
  if (!srcs.length) return '';
  const rank = { unknown: 0, ok: 1, warn: 2, crit: 3 };
  const states = srcs.map(opsState);
  const worst = states.includes('unknown') && !states.some((x) => x === 'warn' || x === 'crit') ? 'unknown' : states.reduce((a, b) => (rank[b] > rank[a] ? b : a), 'ok');
  return `<span class="limchip">${icon('activity', 18)}<span class="op-c op-${worst}">Ops ${worst === 'unknown' ? '?' : OPS_WORD[worst]}</span></span>`;
}

// ---------------- Frame: header and footer ----------------
export function renderHeader(el, s, now, agents, layout) {
  const proj = s.projects.find((p) => p.id === s.ui.project);
  const scope = proj ? (s.ui.privacy ? 'Project hidden' : proj.name) : 'All projects';
  const solo = !!layout?.standalone;
  const urgent = solo ? [] : agents.filter((a) => a.status === 'waiting' || a.status === 'failed');
  let center;
  if (urgent.length) {
    // the attention pill replaces the workspace title; approvals and failures only, completions are counted, not shouted
    const a = urgent[0];
    const failed = a.status === 'failed';
    const what = failed ? `failed · ${String(a.error ?? 'error').replaceAll('_', ' ')}` : `${WAIT_TEXT[a.wait?.kind] ?? 'is waiting for you'}${a.wait?.tool ? ` · ${a.wait.tool}` : ''}`;
    center = `<div class="pill${failed ? ' failed' : ''}"><span>${failed ? '✕' : '▲'}</span><b>${esc(a.name ?? a.project ?? 'Claude Code')}</b><span>${esc(what)}</span>${urgent.length > 1 ? `<span class="more">+${urgent.length - 1} more</span>` : ''}<span class="num">${duration(now - (a.since ?? now))}</span></div>`;
  } else {
    center = `<div class="title">${esc(layout?.name ?? 'Overview')}${solo ? '' : ` <span class="scope">/ ${esc(scope)}</span>`}</div>`;
  }
  const ds = s.display?.state;
  const meta = [
    solo ? '' : opsChip(s),
    solo ? '' : limitChip(s, now),
    s.mode === 'demo' ? '<span class="demo">Demo data</span>' : '<span>Live</span>',
    ds && !['streaming', 'unknown', 'stopped'].includes(ds) ? `<span class="warn">Panel ${esc(ds)}</span>` : '',
    `<span class="clock">${clock(now)}</span>`,
  ].filter(Boolean).join('<span>·</span>');
  setHTML(el, `<img class="wordmark" src="/brand/lockup-light.svg" alt="pitwall">${center}<div class="meta">${meta}</div>`);
}

export function renderFooter(el, s, agents, layout) {
  const names = Object.fromEntries((s.layouts ?? []).map((l) => [l.id, l.name]));
  const tabs = (s.pageOrder ?? []).map((id, i) => `<span class="tab${s.ui.page === id && !s.ui.focus ? ' on' : ''}"><span class="n">${String(i + 1).padStart(2, '0')}</span>${esc(names[id] ?? id)}</span>`).join('');
  const flags = [s.ui.focus && 'Focus', s.ui.privacy && 'Private', s.ui.rotate && 'Rotating'].filter(Boolean).map((f) => `<span class="flag">${f}</span>`).join('');
  const waiting = agents.filter((a) => a.status === 'waiting').length;
  const failed = agents.filter((a) => a.status === 'failed').length;
  let right;
  if (layout?.standalone) {
    // standalone pages report on their own source, never on agents
    const src = s.ops?.sources?.[0];
    right = src?.hasReading ? `<span class="right"><span class="t">${clock(src.checkedAt)}</span>${esc(src.label)} · ${s.ui.privacy ? '' : esc(src.summary)}</span>` : '<span class="right"></span>';
  } else if (waiting || failed) {
    const parts = [waiting && `${waiting} agent${waiting > 1 ? 's' : ''} need${waiting > 1 ? '' : 's'} you`, failed && `${failed} failed`].filter(Boolean);
    right = `<span class="right ${waiting ? 'hot' : 'bad'}">${parts.join(' · ')}</span>`;
  } else {
    const f = (s.claude?.feed ?? [])[0];
    const a = f && (s.claude?.agents ?? []).find((x) => x.id === f.sessionId);
    const who = s.ui.privacy ? 'session' : a?.name ?? a?.project ?? 'session';
    right = f ? `<span class="right"><span class="t">${clock(f.at)}</span>${esc(who)} · ${esc(f.text)}</span>` : '<span class="right">All clear</span>';
  }
  setHTML(el, `${tabs}${flags}${right}`);
}

// ---------------- Secondary pages ----------------
export function renderUsage(zone, s, agents) {
  const t = s.claude?.today ?? {};
  const rows = agents.filter((a) => a.usage).slice(0, 6).map((a) => `<tr><td><span class="dot s-${a.status}" style="display:inline-block;margin-right:10px"></span>${esc(a.name ?? a.project)}</td><td>${esc((a.usage.model ?? '').replace('claude-', ''))}</td><td class="r num">${tokens(a.usage.contextTokens)}</td><td class="r num">${tokens(a.usage.inputTokens)}</td><td class="r num">${tokens(a.usage.outputTokens)}</td><td class="r num">${a.usage.turns}</td></tr>`).join('');
  setHTML(zone, `<div class="zone-head"><span class="eyebrow">${icon('activity', 18)}AI usage · today</span><span class="right">${(s.tools?.tools ?? []).map((x) => `${esc(x.label)}: ${esc(x.detail)}`).join(' · ') || 'measured from Claude Code transcripts · cost not shown'}</span></div>
    <div class="usage-grid"><div class="bigstats">
      <div class="bigstat"><span class="k">${icon('output', 17)}Output tokens</span><span class="v num">${tokens(t.output)}</span><span class="s">all sessions today</span></div>
      <div class="bigstat"><span class="k">${icon('tokens', 17)}Input tokens</span><span class="v num">${tokens(t.input)}</span><span class="s">incl. cache reads</span></div>
      <div class="bigstat"><span class="k">${icon('check', 17)}Turns done</span><span class="v num">${t.turns ?? 0}</span><span class="s">${t.hookCompletions ?? 0} via hooks</span></div>
      ${s.limits?.today ? `<div class="bigstat"><span class="k">${icon('coin', 17)}Cost today</span><span class="v num">$${s.limits.today.costUsd.toFixed(2)}</span><span class="s">estimate via RedLine · week $${(s.limits.week?.costUsd ?? 0).toFixed(0)}</span></div>`
        : `<div class="bigstat"><span class="k">${icon('agents', 17)}Sessions</span><span class="v num">${t.sessions ?? 0}</span><span class="s">active today</span></div>`}
    </div><div class="tbl-wrap"><table class="tbl"><thead><tr><th>Session</th><th>Model</th><th class="r">Context</th><th class="r">Input</th><th class="r">Output</th><th class="r">Turns</th></tr></thead><tbody>${rows || '<tr><td colspan="6" class="note">No live sessions with usage data.</td></tr>'}</tbody></table><div class="note" style="padding:8px 12px 0">${s.limits?.today ? 'Tokens measured from Claude Code transcripts. Cost is RedLine\'s local estimate.' : 'Measured from Claude Code transcripts. Cost is not shown: no reliable local source yet.'}</div></div></div>`);
}

export function renderSystem(zone, s) {
  const sys = s.system ?? {};
  const cores = (sys.cpu?.perCore ?? []).map((v) => `<div class="core"><i style="height:${Math.max(2, v).toFixed(0)}%"></i></div>`).join('');
  const m = sys.mem ?? {};
  const pct = (x) => (m.total ? (100 * x) / m.total : 0);
  const procs = (sys.procs?.value ?? []).slice(0, 6).map((p) => `<div class="proc"><span class="n">${esc(p.name)}</span><span class="c num">${p.cpu.toFixed(1)}%</span><span class="m num">${bytes(p.rssBytes, 0)}</span></div>`).join('');
  setHTML(zone, `<div class="zone-head"><span class="eyebrow">${icon('activity', 18)}System detail · ${esc(sys.cpuModel ?? '')}</span><span class="right">load <span class="mono">${(sys.cpu?.load ?? []).map((x) => x.toFixed(2)).join(' ')}</span></span></div>
    <div class="sys-grid">
      <div class="box"><div class="kv">CPU per core<b>${Math.round(sys.cpu?.value ?? 0)}%</b></div><div class="cores">${cores}</div><div class="note">${sys.cores ?? '?'} cores · 1 s sampling · temps need root</div></div>
      <div class="box"><div class="kv">Memory used<b>${bytes(m.used)}</b></div>
        <div class="bar"><i style="width:${pct(m.wired)}%;background:var(--faint)"></i><i style="width:${pct(m.compressed)}%;background:var(--line)"></i><i style="width:${Math.max(0, pct(m.used) - pct(m.wired) - pct(m.compressed))}%;background:var(--muted)"></i></div>
        <div class="kv">Wired<b>${bytes(m.wired)}</b></div><div class="kv">Compressed<b>${bytes(m.compressed)}</b></div><div class="kv">Swap<b>${bytes(m.swapBytes)}</b></div>
        <div class="kv">Network ↓ / ↑<b>${rate(sys.net?.rxBps)} / ${rate(sys.net?.txBps)}</b></div>
        <div class="kv">Disk free<b>${bytes(sys.disk?.freeBytes, 0)}</b></div>
        <div class="kv">GPU<b>${sys.gpu?.value != null ? Math.round(sys.gpu.value) + '%' : esc(sys.gpu?.unavailable ?? 'n/a')}</b></div>
        <div class="kv">Battery<b>${sys.battery?.value != null ? `${sys.battery.percent}%${sys.battery.source === 'ac' ? ' ⚡' : ''}` : esc(sys.battery?.unavailable ?? 'n/a')}</b></div>
        <div class="kv">Thermal<b>${esc(sys.thermal?.value ?? sys.thermal?.unavailable ?? 'n/a')}</b></div>
        <div class="kv">This dashboard<b>${sys.self?.value != null ? `${sys.self.value.toFixed(1)}% · ${bytes(sys.self.rssBytes, 0)}` : 'n/a'}</b></div></div>
      <div class="box"><div class="kv">Top processes<b>CPU · RSS</b></div><div class="procs" style="border:0;padding:0;background:none;gap:6px;justify-content:flex-start">${procs}</div></div>
    </div>`);
}

// Widget registry: every layout slot names one of these. render(el, ctx) gets read-only state.
export const WIDGETS = {
  ai: (el, c) => renderAI(el, c.state, c.agents, c.now, c.width),
  health: (el, c) => renderHealth(el, c.state, c.now),
  'health-mini': (el, c) => renderHealthMini(el, c.state),
  limits: (el, c) => renderLimits(el, c.state, c.now),
  ops: (el, c) => renderOps(el, c.state, c.now, c.width),
  launcher: (el, c) => renderLaunch(el, c.state, c.now, c.flash),
  usage: (el, c) => renderUsage(el, c.state, c.agents),
  system: (el, c) => renderSystem(el, c.state),
};
