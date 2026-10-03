// Zone renderers. Cards are keyed DOM nodes so status animations keep running across 1 Hz updates.
import { esc, bytes, gib, rate, tokens, duration, clock, spark } from './format.js';

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
    ? `<span class="mono">ctx ${tokens(u.contextTokens)}</span><span class="mono">out ${tokens(u.outputTokens)}</span>`
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
    zone.innerHTML = '<div class="zone-head"><span class="eyebrow">Agents</span><div class="counts"></div><span class="right"></span></div><div class="cards"></div>';
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
  const cpu = tile('cpu', 'CPU', sys.cpu, now, `${Math.round(sys.cpu?.value)}<small>%</small>`,
    spark(h.cpu, { max: 100, color: 'var(--pw-accent)', id: 'cpu' }),
    `load <span class="mono">${sys.cpu?.load?.[0]?.toFixed(2) ?? 'n/a'}</span> · ${sys.cores ?? '?'} cores`);
  const mem = tile('mem', 'Memory', sys.mem, now, `${Math.round(sys.mem?.value)}<small>%</small>`,
    spark(h.mem, { max: 100, color: 'var(--muted)', id: 'mem' }),
    `<span class="mono">${gib(sys.mem?.used)}</span> of <span class="mono">${gib(sys.mem?.total, 0)}</span>${sys.mem?.swapBytes > 5e8 ? ` · swap <span class="mono">${gib(sys.mem.swapBytes)}</span>` : ''}`);
  const disk = tile('disk', 'Disk free', sys.disk, now, bytes(sys.disk?.freeBytes, 0).replace(' ', '<small>') + '</small>',
    `<div class="bar"><i style="width:${Math.round(sys.disk?.value ?? 0)}%;background:var(--muted)"></i></div>`,
    `of <span class="mono">${bytes(sys.disk?.totalBytes, 0)}</span> · <span class="mono">${Math.round(sys.disk?.value)}%</span> used`);
  const net = tile('net', 'Network', sys.net, now,
    `<span class="nr"><span class="ar" style="color:var(--work)">↓</span>${rate(sys.net?.rxBps)}</span><span class="nr tx"><span class="ar" style="color:var(--faint)">↑</span>${rate(sys.net?.txBps)}</span>`,
    spark(h.netRx, { color: 'var(--work)', id: 'rx' }) + spark(h.netTx, { color: 'var(--faint)', id: 'tx', fill: false, max: Math.max(...(h.netRx ?? [1]), ...(h.netTx ?? [1]), 1) }),
    '');
  const extra = [];
  if (sys.gpu?.value != null) extra.push(`GPU <span class="mono">${Math.round(sys.gpu.value)}%</span>`);
  if (sys.battery?.value != null) extra.push(`Battery <span class="mono">${sys.battery.percent}%</span>${sys.battery.source === 'ac' ? ' ⚡' : ''}`);
  if (sys.thermal?.value && sys.thermal.value !== 'nominal') extra.push(`<span style="color:var(--wait)">Thermal ${esc(sys.thermal.value)}</span>`);
  setHTML(zone, `<div class="zone-head"><span class="eyebrow">${esc(s.ui.privacy ? 'This computer' : sys.host ?? 'This computer')}</span><span class="right">${extra.join(' · ')}</span></div>
    <div class="tiles">${cpu}${mem}${net}${disk}</div>`);
}

// ---------------- Launcher zone ----------------
export function renderLaunch(zone, s, now, flash) {
  const proj = s.projects.find((p) => p.id === s.ui.project);
  const projName = proj ? (s.ui.privacy ? 'Project hidden' : proj.name) : 'All projects';
  const actions = (s.actions ?? []).slice(0, 6).map((a) => `<button class="action${flash?.id === a.id ? (flash.ok ? ' flash' : ' flash-bad') : ''}" data-action="${esc(a.id)}"><span class="kb">${esc((a.key ?? '').toUpperCase())}</span><span class="lab">${esc(a.label)}</span><span class="hint">${esc(a.hint ?? '')}</span></button>`).join('');
  setHTML(zone, `<div class="zone-head"><span class="eyebrow">Launch</span><span class="right">in <b>${esc(projName)}</b></span></div>
    <div class="actions">${actions}</div>`);
}

// ---------------- Frame: header and footer ----------------
export function renderHeader(el, s, now, agents, layout) {
  const proj = s.projects.find((p) => p.id === s.ui.project);
  const scope = proj ? (s.ui.privacy ? 'Project hidden' : proj.name) : 'All projects';
  const urgent = agents.filter((a) => a.status === 'waiting' || a.status === 'failed');
  let center;
  if (urgent.length) {
    // the attention pill replaces the workspace title; approvals and failures only, completions are counted, not shouted
    const a = urgent[0];
    const failed = a.status === 'failed';
    const what = failed ? `failed · ${String(a.error ?? 'error').replaceAll('_', ' ')}` : `${WAIT_TEXT[a.wait?.kind] ?? 'is waiting for you'}${a.wait?.tool ? ` · ${a.wait.tool}` : ''}`;
    center = `<div class="pill${failed ? ' failed' : ''}"><span>${failed ? '✕' : '▲'}</span><b>${esc(a.name ?? a.project ?? 'Claude Code')}</b><span>${esc(what)}</span>${urgent.length > 1 ? `<span class="more">+${urgent.length - 1} more</span>` : ''}<span class="num">${duration(now - (a.since ?? now))}</span></div>`;
  } else {
    center = `<div class="title">${esc(layout?.name ?? 'Overview')} <span class="scope">/ ${esc(scope)}</span></div>`;
  }
  const ds = s.display?.state;
  const meta = [
    s.mode === 'demo' ? '<span class="demo">Demo data</span>' : '<span>Live</span>',
    ds && !['streaming', 'unknown', 'stopped'].includes(ds) ? `<span class="warn">Panel ${esc(ds)}</span>` : '',
    `<span class="clock">${clock(now)}</span>`,
  ].filter(Boolean).join('<span>·</span>');
  setHTML(el, `<img class="wordmark" src="/brand/lockup-light.svg" alt="pitwall">${center}<div class="meta">${meta}</div>`);
}

export function renderFooter(el, s, agents) {
  const names = Object.fromEntries((s.layouts ?? []).map((l) => [l.id, l.name]));
  const tabs = (s.pageOrder ?? []).map((id, i) => `<span class="tab${s.ui.page === id && !s.ui.focus ? ' on' : ''}"><span class="n">${String(i + 1).padStart(2, '0')}</span>${esc(names[id] ?? id)}</span>`).join('');
  const flags = [s.ui.focus && 'Focus', s.ui.privacy && 'Private', s.ui.rotate && 'Rotating'].filter(Boolean).map((f) => `<span class="flag">${f}</span>`).join('');
  const waiting = agents.filter((a) => a.status === 'waiting').length;
  const failed = agents.filter((a) => a.status === 'failed').length;
  let right;
  if (waiting || failed) {
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
  setHTML(zone, `<div class="zone-head"><span class="eyebrow">AI usage · today</span><span class="right">${(s.tools?.tools ?? []).map((x) => `${esc(x.label)}: ${esc(x.detail)}`).join(' · ') || 'measured from Claude Code transcripts · cost not shown'}</span></div>
    <div class="usage-grid"><div class="bigstats">
      <div class="bigstat"><span class="k">Output tokens</span><span class="v num">${tokens(t.output)}</span><span class="s">all sessions today</span></div>
      <div class="bigstat"><span class="k">Input tokens</span><span class="v num">${tokens(t.input)}</span><span class="s">incl. cache reads</span></div>
      <div class="bigstat"><span class="k">Turns done</span><span class="v num">${t.turns ?? 0}</span><span class="s">${t.hookCompletions ?? 0} via hooks</span></div>
      <div class="bigstat"><span class="k">Sessions</span><span class="v num">${t.sessions ?? 0}</span><span class="s">active today</span></div>
    </div><div class="tbl-wrap"><table class="tbl"><thead><tr><th>Session</th><th>Model</th><th class="r">Context</th><th class="r">Input</th><th class="r">Output</th><th class="r">Turns</th></tr></thead><tbody>${rows || '<tr><td colspan="6" class="note">No live sessions with usage data.</td></tr>'}</tbody></table><div class="note" style="padding:8px 12px 0">Measured from Claude Code transcripts. Cost is not shown: no reliable local source yet.</div></div></div>`);
}

export function renderSystem(zone, s) {
  const sys = s.system ?? {};
  const cores = (sys.cpu?.perCore ?? []).map((v) => `<div class="core"><i style="height:${Math.max(2, v).toFixed(0)}%"></i></div>`).join('');
  const m = sys.mem ?? {};
  const pct = (x) => (m.total ? (100 * x) / m.total : 0);
  const procs = (sys.procs?.value ?? []).slice(0, 6).map((p) => `<div class="proc"><span class="n">${esc(p.name)}</span><span class="c num">${p.cpu.toFixed(1)}%</span><span class="m num">${bytes(p.rssBytes, 0)}</span></div>`).join('');
  setHTML(zone, `<div class="zone-head"><span class="eyebrow">System detail · ${esc(sys.cpuModel ?? '')}</span><span class="right">load <span class="mono">${(sys.cpu?.load ?? []).map((x) => x.toFixed(2)).join(' ')}</span></span></div>
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
  launcher: (el, c) => renderLaunch(el, c.state, c.now, c.flash),
  usage: (el, c) => renderUsage(el, c.state, c.agents),
  system: (el, c) => renderSystem(el, c.state),
};
