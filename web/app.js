// FT — frontend, phase-8.
//
// On load: call /api/auth/state, branch to setup / login / dashboard.
// Dashboard renders a tab nav (Stocks / Crypto) with real tables.

const $ = (sel, root = document) => root.querySelector(sel);

// ---------- API client ----------------------------------------------------

async function api(path, opts = {}) {
  const res = await fetch(path, {
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    ...opts,
  });
  let data = null;
  try {
    data = await res.json();
  } catch (_) {
    // non-JSON response
  }
  if (!res.ok) {
    const msg = (data && data.error) || `request failed (${res.status})`;
    const err = new Error(msg);
    err.status = res.status;
    throw err;
  }
  return data;
}

// ---------- formatters ----------------------------------------------------

const fmtUSD = new Intl.NumberFormat('en-US', {
  style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 2,
});
const fmtEUR = new Intl.NumberFormat('en-IE', {
  style: 'currency', currency: 'EUR', minimumFractionDigits: 2, maximumFractionDigits: 2,
});
const fmtNum0 = new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 });
const fmtNum1 = new Intl.NumberFormat('en-US', { minimumFractionDigits: 1, maximumFractionDigits: 1 });
const fmtNum2 = new Intl.NumberFormat('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
const fmtNum4 = new Intl.NumberFormat('en-US', { minimumFractionDigits: 0, maximumFractionDigits: 4 });
const fmtNum6 = new Intl.NumberFormat('en-US', { minimumFractionDigits: 0, maximumFractionDigits: 6 });

function dash(v, fmt = fmtNum2) {
  if (v == null || Number.isNaN(v)) return '<span class="dim">—</span>';
  return fmt.format(v);
}
function dashSigned(v, fmt, prefix = '', suffix = '') {
  if (v == null || Number.isNaN(v)) return '<span class="dim">—</span>';
  const cls = v > 0 ? 'gain' : v < 0 ? 'loss' : '';
  return `<span class="${cls}">${prefix}${fmt.format(v)}${suffix}</span>`;
}
function pct(v, decimals = 1) {
  if (v == null || Number.isNaN(v)) return '<span class="dim">—</span>';
  const f = new Intl.NumberFormat('en-US', {
    minimumFractionDigits: decimals, maximumFractionDigits: decimals,
  });
  const cls = v > 0 ? 'gain' : v < 0 ? 'loss' : '';
  return `<span class="${cls}">${v > 0 ? '+' : ''}${f.format(v)}%</span>`;
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
  })[c]);
}

// suggestedDelta compares the manual SL/TP price against the suggested % rule
// to surface an ↑ / ↓ icon (Spec 3 D11). Returns {icon, title} for the cell.
//
//   kind 'sl': manual *tighter* (closer to entry) than suggested → ↑ (more conservative)
//   kind 'tp': manual *lower target* than suggested → ↓ (less ambitious)
function suggestedDelta(entryPrice, manualPrice, suggestedPct, kind) {
  if (entryPrice == null || manualPrice == null || suggestedPct == null) {
    return { icon: '', title: '' };
  }
  const manualPct = ((manualPrice - entryPrice) / entryPrice) * 100;
  const diff = manualPct - suggestedPct; // signed
  if (Math.abs(diff) < 0.5) return { icon: '', title: 'Manual ≈ suggested' };
  if (kind === 'sl') {
    // SL is negative; "tighter" means manualPct > suggestedPct (smaller drawdown allowed).
    const tighter = manualPct > suggestedPct;
    const ic = tighter ? '↑' : '↓';
    return {
      icon: ` <span class="suggested-arrow ${tighter ? 'up' : 'down'}">${ic}</span>`,
      title: `Suggested ${suggestedPct.toFixed(1)}% vs your ${manualPct.toFixed(1)}%`,
    };
  }
  // TP is positive; "more ambitious" means manualPct > suggestedPct.
  const higher = manualPct > suggestedPct;
  const ic = higher ? '↑' : '↓';
  return {
    icon: ` <span class="suggested-arrow ${higher ? 'up' : 'down'}">${ic}</span>`,
    title: `Suggested ${suggestedPct.toFixed(1)}% vs your ${manualPct.toFixed(1)}%`,
  };
}

// proposedLevelCell — Spec 12 D5b. Renders SL/TP as "price | ±%" two-row cell.
//   manualPrice: user-entered stop/take levels (preferred when present)
//   suggestedPct: backend-computed % offset from cost basis
//   avgOpenPrice: entry, used when only the suggested pct is available
//   currentPrice: for the right-hand offset percentage (% from current price)
//   kind: 'sl' renders ↓ red, 'tp' renders ↑ green
function proposedLevelCell(manualPrice, suggestedPct, avgOpenPrice, currentPrice, kind) {
  // Resolve absolute price level.
  let price = manualPrice;
  if (price == null && suggestedPct != null && avgOpenPrice != null) {
    // Suggested pct is signed relative to cost basis (negative for SL).
    price = avgOpenPrice * (1 + suggestedPct / 100);
  }
  if (price == null) return '<span class="dim">—</span>';
  // % offset from current price (or fall back to avgOpenPrice).
  const ref = currentPrice != null ? currentPrice : avgOpenPrice;
  let pctStr = '';
  let toneClass = kind === 'sl' ? 'loss' : 'gain';
  let arrow = kind === 'sl' ? '↓' : '↑';
  if (ref != null && ref > 0) {
    const offsetPct = ((price - ref) / ref) * 100;
    pctStr = `${arrow} ${Math.abs(offsetPct).toFixed(1)}%`;
  } else {
    pctStr = '—';
    toneClass = 'dim';
  }
  return `
    <span class="proposed-level">
      <span class="pl-price">$${fmtNum2.format(price)}</span>
      <span class="pl-pct ${toneClass}">${pctStr}</span>
    </span>
  `;
}

// earningsCell + exDivCell — Spec 3 D10.
// Date strings are 'YYYY-MM-DD' from Yahoo calendarEvents.
function daysUntilISO(iso) {
  if (!iso) return null;
  const parts = iso.split('-');
  if (parts.length !== 3) return null;
  const target = Date.UTC(+parts[0], +parts[1] - 1, +parts[2]);
  const today = new Date();
  const todayUTC = Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate());
  return Math.round((target - todayUTC) / (1000 * 60 * 60 * 24));
}
function earningsCell(iso) {
  const d = daysUntilISO(iso);
  if (d == null) return '<span class="dim">—</span>';
  if (d < 0) return `<span class="dim" title="${escapeHTML(iso)}">past</span>`;
  let cls = 'dim';
  if (d <= 7) cls = 'earn-soon';
  else if (d <= 30) cls = 'earn-mid';
  return `<span class="${cls}" title="${escapeHTML(iso)}">in ${d}d</span>`;
}
// scoreCell — Spec 4 D7. Renders the latest-score badge for a holdings row.
//   never scored → "—"
//   pass + fresh  → "12/16 ✓" green
//   below thresh  → "9/16" amber
//   >90d stale    → "12/16 ⚠ 120d" dashed border
//
// Backlog polish 2026-05-17: when `clickable` is true we wrap the cell as a
// `[data-rescore-kind][data-rescore-id]` button so clicking on the score in
// the Stocks/Crypto tables opens the 8-question modal for that holding.
function scoreCell(score, clickable, kind, id) {
  const hint = clickable ? ' · click to rescore' : '';
  if (!score) {
    if (clickable) {
      return `<span class="score-badge unscored" data-rescore-kind="${escapeHTML(kind)}" data-rescore-id="${id}" title="Click to score${hint}">— score</span>`;
    }
    return '<span class="dim">—</span>';
  }
  const stale = score.staleDays > 90;
  const cls = ['score-badge'];
  cls.push(score.passes ? 'pass' : 'fail');
  if (stale) cls.push('stale');
  if (clickable) cls.push('clickable');
  const tickMark = score.passes ? ' ✓' : '';
  const staleMark = stale ? ` ⚠ ${score.staleDays}d` : '';
  const tooltip = `Scored ${score.scoredAt.slice(0,10)} · framework: ${escapeHTML(score.frameworkId)}${hint}`;
  const attrs = clickable
    ? `data-rescore-kind="${escapeHTML(kind)}" data-rescore-id="${id}"`
    : '';
  return `<span class="${cls.join(' ')}" ${attrs} title="${tooltip}">${score.totalScore}/${score.maxScore}${tickMark}${staleMark}</span>`;
}

// marketCell — Spec 5 D3. Per-row market column from {open, onBreak, nextChange, nextChangeKind, name, tzName}.
// Crypto rows pass `null` and get an em-dash since crypto trades 24/7.
function marketCell(m) {
  if (!m) return '<span class="dim">—</span>';
  const dot = m.open ? '🟢' : m.onBreak ? '🟡' : '🔴';
  const status = m.open ? 'Open' : m.onBreak ? 'Break' : 'Closed';
  const verb = m.nextChangeKind === 'close' ? 'closes' :
               m.nextChangeKind === 'break_start' ? 'breaks' :
               m.nextChangeKind === 'break_end' ? 'resumes' : 'opens';
  const when = formatLocalTimeShort(m.nextChange);
  const cls = m.open ? 'market-open' : m.onBreak ? 'market-break' : 'market-closed';
  return `<span class="${cls}" title="${escapeHTML(m.name)} (${escapeHTML(m.tzName || '')})">${dot} ${status} · ${escapeHTML(verb)} ${escapeHTML(when)}</span>`;
}

function exDivCell(iso) {
  const d = daysUntilISO(iso);
  if (d == null) return '<span class="dim">—</span>';
  if (d < 0) return `<span class="dim" title="${escapeHTML(iso)}">past</span>`;
  return `<span title="${escapeHTML(iso)}">${d <= 30 ? `in ${d}d` : iso}</span>`;
}

// ---------- screens -------------------------------------------------------

function setScreen(html) {
  $('#app').innerHTML = html;
}

function renderSetup() {
  setScreen(`
    <div class="auth-screen">
      <div class="auth-card">
        <h1>FT</h1>
        <div class="sub">First-time setup. Create your account.</div>
        <div class="error" id="err"></div>
        <form id="form-setup">
          <div class="field">
            <label for="email">email</label>
            <input id="email" name="email" type="email" autocomplete="email" required />
          </div>
          <div class="field">
            <label for="display">display name (optional)</label>
            <input id="display" name="display" type="text" autocomplete="name" />
          </div>
          <div class="field">
            <label for="password">password (8+ chars)</label>
            <input id="password" name="password" type="password" autocomplete="new-password" required minlength="8" />
          </div>
          <button type="submit" class="btn" id="submit">create account</button>
        </form>
        <div class="foot-note">
          This page only appears once. Subsequent visits show the login form.
        </div>
      </div>
    </div>
  `);
  $('#form-setup').addEventListener('submit', onSetup);
}

function renderLogin() {
  setScreen(`
    <div class="auth-screen">
      <div class="auth-card">
        <h1>FT</h1>
        <div class="sub">Sign in.</div>
        <div class="error" id="err"></div>
        <form id="form-login">
          <div class="field">
            <label for="email">email</label>
            <input id="email" name="email" type="email" autocomplete="email" required />
          </div>
          <div class="field">
            <label for="password">password</label>
            <input id="password" name="password" type="password" autocomplete="current-password" required />
          </div>
          <button type="submit" class="btn" id="submit">sign in</button>
        </form>
      </div>
    </div>
  `);
  $('#form-login').addEventListener('submit', onLogin);
}

// ---------- dashboard -----------------------------------------------------

const state = {
  user: null,
  tab: 'summary',       // 'summary' | 'stocks' | 'crypto' | 'heatmap' | 'news' | 'crypto-news'
  stocks: null,         // array of stock rows
  crypto: null,         // array of crypto rows
  summary: null,        // cached summary response
  heatmapSector: '',    // '' = all sectors
  loading: false,
  error: null,
};

const SECTORS = [
  'Technology', 'Financials', 'Healthcare', 'Consumer Discretionary',
  'Consumer Staples', 'Communication Services', 'Industrials', 'Energy',
  'Utilities', 'Materials', 'Real Estate',
];

function renderDashboard(user) {
  state.user = user;
  setScreen(`
    <div class="shell">
      <div class="topbar">
        <div class="brand">FT</div>
        <div class="market-pill" id="market-pill" title="Markets — click for all 7" tabindex="0">—</div>
        <div class="markets-dropdown" id="markets-dropdown" aria-hidden="true"></div>
        <div class="regime-pills" id="regime-pills" title="Regime — Jordi / Cowen / Effective"></div>
        <div class="right">
          <span class="dim" id="refresh-status">—</span>
          <button class="btn-ghost" id="refresh">refresh</button>
          <button class="btn-ghost" id="import">import…</button>
          <button class="btn-ghost" id="export">save master</button>
          <button class="btn-ghost" id="cmd-k-btn" title="Command palette (Cmd/Ctrl+K)">⌘K</button>
          <span>${escapeHTML(user.displayName || user.email)}</span>
          <button class="btn-ghost" id="logout">sign out</button>
        </div>
      </div>
      <div class="tabbar">
        <button class="tab ${state.tab === 'summary' ? 'active' : ''}" data-tab="summary">Summary</button>
        <button class="tab ${state.tab === 'stocks' ? 'active' : ''}" data-tab="stocks">Stocks &amp; ETFs</button>
        <button class="tab ${state.tab === 'crypto' ? 'active' : ''}" data-tab="crypto">Crypto</button>
        <button class="tab ${state.tab === 'performance' ? 'active' : ''}" data-tab="performance">Performance</button>
        <button class="tab ${state.tab === 'screener' ? 'active' : ''}" data-tab="screener">Screener</button>
        <button class="tab ${state.tab === 'sector-rotation' ? 'active' : ''}" data-tab="sector-rotation">Sector Rotation</button>
        <button class="tab ${state.tab === 'scorecards' ? 'active' : ''}" data-tab="scorecards">Scorecards</button>
        <button class="tab ${state.tab === 'watchlist' ? 'active' : ''}" data-tab="watchlist">Watchlist</button>
        <button class="tab ${state.tab === 'heatmap' ? 'active' : ''}" data-tab="heatmap">Heatmap</button>
        <button class="tab ${state.tab === 'news' ? 'active' : ''}" data-tab="news">News</button>
        <button class="tab ${state.tab === 'crypto-news' ? 'active' : ''}" data-tab="crypto-news">Crypto News</button>
        <button class="tab ${state.tab === 'settings' ? 'active' : ''}" data-tab="settings">Settings</button>
      </div>
      <div class="content" id="content"></div>
    </div>
  `);
  $('#logout').addEventListener('click', onLogout);
  $('#refresh').addEventListener('click', onRefresh);
  $('#import').addEventListener('click', openImportModal);
  $('#export').addEventListener('click', onExport);
  $('#cmd-k-btn').addEventListener('click', () => openCommandPalette());
  for (const el of document.querySelectorAll('.tab')) {
    el.addEventListener('click', () => switchTab(el.dataset.tab));
  }
  loadActiveTab();
  loadRefreshStatus();
  startMarketPill();
  startRegimePills();
}

// ---------- market status pill (top bar) — Spec 5 D4 -------------------

// marketState carries the multi-exchange snapshot: {asOf, summary, exchanges[]}.
// summary picks the headline market (earliest closing if any open, else
// earliest opening). Refreshed every 5 min; 1s ticker re-renders countdown.
let marketState = null;
let marketTicker = null;

async function startMarketPill() {
  // Spec 12 D3 — load focused-exchange pref. Drives which row of the
  // /api/marketstatus/all payload is used for the collapsed pill text.
  try {
    const r = await api('/api/preferences/focused_exchange');
    if (r && typeof r.value === 'string') state.focusedExchange = r.value;
  } catch (_) { /* default to server's primary */ }

  await refreshMarketStatus();
  setInterval(refreshMarketStatus, 5 * 60 * 1000);
  if (marketTicker) clearInterval(marketTicker);
  marketTicker = setInterval(updateMarketPillText, 1000);
  // Click pill → toggle dropdown
  const pill = $('#market-pill');
  if (pill) {
    pill.addEventListener('click', toggleMarketsDropdown);
    pill.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); toggleMarketsDropdown(); }
      if (ev.key === 'Escape') closeMarketsDropdown();
    });
  }
  // Click outside → close
  document.addEventListener('click', (ev) => {
    const dd = $('#markets-dropdown');
    const p  = $('#market-pill');
    if (dd && !dd.contains(ev.target) && p && !p.contains(ev.target)) closeMarketsDropdown();
  });
}

// Spec 12 D3 — resolve which exchange row to show in the collapsed pill.
// Returns the focused exchange when set + present in payload; otherwise
// falls back to the server-computed summary (earliest closing if any open,
// else earliest opening).
function focusedMarketRow() {
  if (!marketState) return null;
  const want = state.focusedExchange;
  if (want && Array.isArray(marketState.exchanges)) {
    // Server's payload uses `exchange` for the code (Spec 5 status.go).
    const row = marketState.exchanges.find(e => e.exchange === want);
    if (row) return row;
  }
  return null;
}

async function refreshMarketStatus() {
  try {
    marketState = await api('/api/marketstatus/all');
    updateMarketPillText();
    // If the dropdown's open, re-render its body too so values stay live.
    const dd = $('#markets-dropdown');
    if (dd && dd.classList.contains('open')) renderMarketsDropdown();
  } catch (_) { /* leave pill at last known state */ }
}

function updateMarketPillText() {
  const el = $('#market-pill');
  if (!el || !marketState) return;

  // Spec 12 D3 — prefer user's focused exchange when set + available.
  const focused = focusedMarketRow();
  if (focused) {
    const dot  = focused.open ? '🟢' : focused.onBreak ? '🟡' : '🔴';
    const verb = focused.nextChangeKind === 'close' ? 'closes' :
                 focused.nextChangeKind === 'break_start' ? 'breaks' :
                 focused.nextChangeKind === 'break_end' ? 'resumes' : 'opens';
    const remaining = formatCountdown(focused.nextChange);
    const label = focused.open ? focused.name : `${focused.name} closed`;
    el.innerHTML = `${dot} <span class="mp-label">${escapeHTML(label)}</span> · ${escapeHTML(verb)} in <span class="num mp-eta">${escapeHTML(remaining)}</span>`;
    el.classList.toggle('market-pill--open', !!focused.open);
    return;
  }

  // Fallback to server's primary.
  if (!marketState.summary) return;
  const s = marketState.summary;
  if (!s.primaryExchange) {
    el.innerHTML = '<span class="dim">—</span>';
    return;
  }
  const dot   = s.primaryOpen ? '🟢' : '🔴';
  const label = s.primaryOpen ? s.primaryLabel : `${s.primaryLabel} closed`;
  const verb  = s.primaryNextChangeKind === 'close' ? 'closes' :
                s.primaryNextChangeKind === 'break_start' ? 'breaks' :
                s.primaryNextChangeKind === 'break_end' ? 'resumes' : 'opens';
  const remaining = formatCountdown(s.primaryNextChange);
  el.innerHTML = `${dot} <span class="mp-label">${escapeHTML(label)}</span> · ${escapeHTML(verb)} in <span class="num mp-eta">${escapeHTML(remaining)}</span>`;
  el.classList.toggle('market-pill--open', !!s.primaryOpen);
}

function toggleMarketsDropdown() {
  const dd = $('#markets-dropdown');
  if (!dd) return;
  if (dd.classList.contains('open')) closeMarketsDropdown();
  else openMarketsDropdown();
}
function openMarketsDropdown() {
  const dd = $('#markets-dropdown');
  if (!dd) return;
  renderMarketsDropdown();
  dd.classList.add('open');
  dd.setAttribute('aria-hidden', 'false');
}
function closeMarketsDropdown() {
  const dd = $('#markets-dropdown');
  if (!dd) return;
  dd.classList.remove('open');
  dd.setAttribute('aria-hidden', 'true');
}

function renderMarketsDropdown() {
  const dd = $('#markets-dropdown');
  if (!dd || !marketState || !Array.isArray(marketState.exchanges)) return;
  const focused = state.focusedExchange;
  const rows = marketState.exchanges.map((e) => {
    const dot = e.open ? '🟢' : e.onBreak ? '🟡' : '🔴';
    const label = e.open ? 'Open' : e.onBreak ? 'Break' : 'Closed';
    const verb  = e.nextChangeKind === 'close' ? 'closes' :
                  e.nextChangeKind === 'break_start' ? 'breaks' :
                  e.nextChangeKind === 'break_end' ? 'resumes' : 'opens';
    const when = formatLocalTimeShort(e.nextChange);
    const dur  = formatCountdown(e.nextChange);
    const isFocused = e.exchange === focused;
    return `
      <div class="md-row md-row-clickable${e.open ? ' open' : e.onBreak ? ' break' : ''}${isFocused ? ' focused' : ''}" data-exchange="${escapeHTML(e.exchange)}" role="button" tabindex="0" title="Set as focused market for the pill">
        <span class="md-dot">${dot}</span>
        <span class="md-name">${escapeHTML(e.name)}${isFocused ? ' <span class="dim">★</span>' : ''}</span>
        <span class="md-status">${escapeHTML(label)}</span>
        <span class="md-when dim">${escapeHTML(verb)} ${escapeHTML(when)} <span class="num">(${escapeHTML(dur)})</span></span>
      </div>
    `;
  }).join('');
  dd.innerHTML = `<div class="md-head">Markets <span class="dim" style="font-weight:normal; font-size:0.75rem">— click a row to set as pill focus</span></div>${rows}`;

  // Spec 12 D3 — wire row clicks.
  dd.querySelectorAll('[data-exchange]').forEach(row => {
    const set = async () => {
      const code = row.dataset.exchange;
      state.focusedExchange = code;
      try {
        await api('/api/preferences/focused_exchange', {
          method: 'PUT',
          body: JSON.stringify({ value: code }),
        });
      } catch (e) {
        console.warn('focused_exchange persist failed', e.message);
      }
      closeMarketsDropdown();
      updateMarketPillText();
    };
    row.addEventListener('click', set);
    row.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); set(); }
    });
  });
}

// Render the next-change moment as the user's local time. ISO → "Mon 14:30".
function formatLocalTimeShort(iso) {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  const todayKey  = new Date().toDateString();
  const targetKey = d.toDateString();
  const time = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
  if (targetKey === todayKey) return time;
  const tmp = new Date(); tmp.setDate(tmp.getDate() + 1);
  if (targetKey === tmp.toDateString()) return `Tue ${time}`.replace('Tue', d.toLocaleDateString([], { weekday: 'short' }));
  return `${d.toLocaleDateString([], { weekday: 'short' })} ${time}`;
}

function formatCountdown(iso) {
  const ms = new Date(iso).getTime() - Date.now();
  if (!Number.isFinite(ms) || ms <= 0) return '0s';
  const s = Math.floor(ms / 1000);
  const days = Math.floor(s / 86400);
  const hours = Math.floor((s % 86400) / 3600);
  const mins = Math.floor((s % 3600) / 60);
  const secs = s % 60;
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${mins}m`;
  if (mins > 0) return `${mins}m ${secs}s`;
  return `${secs}s`;
}

// ---------- regime pills (Spec 9b D3 + D5) ------------------------------
//
// Three pills in the top bar after the market dropdown: Jordi / Cowen /
// Effective. Each clickable.
//   * Jordi  → 4-button quick-set modal
//   * Cowen  → choice modal: Quick set / Full Sunday capture
//   * Effective → read-only explainer + link to history
// Pills also flag stale (>14d): renders "⚠ STABLE (Jordi · 17d)".

let regimeState = null;

async function startRegimePills() {
  await refreshRegime();
  // Re-poll every 5 min so a Cowen form submission from another tab updates this one.
  setInterval(refreshRegime, 5 * 60 * 1000);
}

async function refreshRegime() {
  try {
    regimeState = await api('/api/regime');
    renderRegimePills();
  } catch (_) { /* silent — pills stay at last known state */ }
}

function renderRegimePills() {
  const el = $('#regime-pills');
  if (!el || !regimeState) return;
  const pills = [
    { side: 'jordi',     label: 'Jordi',     data: regimeState.jordi },
    { side: 'cowen',     label: 'Cowen',     data: regimeState.cowen },
    { side: 'effective', label: 'Effective', data: { regime: regimeState.effective, stale: false } },
  ];
  el.innerHTML = pills.map(p => regimePillHTML(p)).join('');
  for (const node of el.querySelectorAll('.regime-pill')) {
    node.addEventListener('click', () => onRegimePillClick(node.dataset.side));
    node.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); onRegimePillClick(node.dataset.side); }
    });
  }
}

function regimePillHTML({ side, label, data }) {
  const r = data.regime || 'unclassified';
  const tone = r === 'stable' ? 'good' : r === 'shifting' ? 'amber' : r === 'defensive' ? 'bad' : 'dim';
  const stale = data.stale ? ` ⚠ ${daysSinceLabel(data.set_at)}` : '';
  const upper = (r === 'unclassified' ? 'UNSET' : r.toUpperCase());
  return `<div class="regime-pill regime-${tone}" data-side="${side}" tabindex="0" role="button" aria-label="${escapeHTML(label + ' regime: ' + r)}">
    <span class="rp-label">${escapeHTML(label)}</span>
    <span class="rp-value">${upper}${stale}</span>
  </div>`;
}

function daysSinceLabel(iso) {
  if (!iso) return '';
  const d = Math.floor((Date.now() - new Date(iso).getTime()) / (1000 * 60 * 60 * 24));
  return `${d}d`;
}

function onRegimePillClick(side) {
  if (side === 'effective') return openEffectiveExplainer();
  if (side === 'jordi') return openManualRegimeModal('jordi');
  if (side === 'cowen') return openCowenChoiceModal();
}

// ----- Jordi quick-toggle modal (D5) -----
function openManualRegimeModal(side) {
  const cur = (regimeState && regimeState[side] && regimeState[side].regime) || 'unclassified';
  closeImportModal();
  const root = document.createElement('div');
  root.id = 'modal-root';
  const title = side === 'jordi' ? 'Set Jordi regime' : 'Quick set Cowen regime';
  const subtitle = side === 'jordi'
    ? 'Jordi regime is your judgment call on turbulence, dispersion, narrative.'
    : 'Use the full Sunday capture form for a data-backed classification — this manual override is for emergencies.';
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">${escapeHTML(title)}</div>
            <div class="desc">${escapeHTML(subtitle)}</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <div class="regime-choice">
            ${['stable','shifting','defensive','unclassified'].map(v => `
              <label class="regime-choice-btn regime-${v === 'stable' ? 'good' : v === 'shifting' ? 'amber' : v === 'defensive' ? 'bad' : 'dim'} ${v === cur ? 'selected' : ''}">
                <input type="radio" name="regime" value="${v}" ${v === cur ? 'checked' : ''} />
                <span>${v.toUpperCase()}</span>
              </label>
            `).join('')}
          </div>
          <div class="form-row">
            <label for="rm-note">Note (optional)</label>
            <input id="rm-note" name="note" type="text" placeholder="why this regime?" />
          </div>
          <div class="error" id="rm-err"></div>
        </div>
        <div class="modal-foot">
          <button class="btn-secondary" id="rm-cancel">Cancel</button>
          <button class="btn-primary" id="rm-save">Save</button>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#rm-cancel').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
  // Visually highlight the selected radio's parent button.
  document.querySelectorAll('.regime-choice-btn input').forEach(inp => {
    inp.addEventListener('change', () => {
      document.querySelectorAll('.regime-choice-btn').forEach(b => b.classList.remove('selected'));
      inp.parentElement.classList.add('selected');
    });
  });
  $('#rm-save').addEventListener('click', async () => {
    const sel = document.querySelector('input[name="regime"]:checked');
    if (!sel) { $('#rm-err').textContent = 'pick one'; return; }
    const note = $('#rm-note').value.trim();
    const path = side === 'jordi' ? '/api/regime/jordi' : '/api/regime/cowen/manual';
    try {
      await api(path, { method: 'POST', body: JSON.stringify({ regime: sel.value, note }) });
      closeImportModal();
      await refreshRegime();
    } catch (e) {
      $('#rm-err').textContent = e.message;
    }
  });
}

// ----- Cowen choice modal -----
function openCowenChoiceModal() {
  closeImportModal();
  const root = document.createElement('div');
  root.id = 'modal-root';
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">Set Cowen regime</div>
            <div class="desc">Pick how to set it.</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <div class="cowen-choice">
            <button class="btn-secondary" id="cowen-quick">Quick set (4 buttons)</button>
            <button class="btn-primary" id="cowen-full">Full Sunday capture →</button>
          </div>
          <p class="dim" style="margin-top:0.8rem;font-size:0.8rem">
            Quick set is for emergencies. The full Sunday capture form (8 fields + macro flags)
            auto-classifies the regime and keeps a row in regime history for retrospective.
          </p>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
  $('#cowen-quick').addEventListener('click', () => { closeImportModal(); openManualRegimeModal('cowen'); });
  $('#cowen-full').addEventListener('click', () => { closeImportModal(); openCowenCaptureForm(); });
}

// ----- Effective regime explainer -----
function openEffectiveExplainer() {
  closeImportModal();
  const root = document.createElement('div');
  root.id = 'modal-root';
  const eff = (regimeState && regimeState.effective) || 'unclassified';
  const mult = regimeState ? regimeState.alert_margin_multiplier : 1.0;
  const j = regimeState ? regimeState.jordi.regime : '—';
  const c = regimeState ? regimeState.cowen.regime : '—';
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">Effective regime</div>
            <div class="desc">Read-only — derived from Jordi + Cowen.</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <p>Jordi: <strong>${escapeHTML(j.toUpperCase())}</strong></p>
          <p>Cowen: <strong>${escapeHTML(c.toUpperCase())}</strong></p>
          <p>Effective: <strong>${escapeHTML(eff.toUpperCase())}</strong></p>
          <p class="dim" style="margin-top:0.8rem;font-size:0.85rem">
            Effective is the more defensive of the two. UNCLASSIFIED is treated as SHIFTING
            for alert gating, but if only one side is unclassified, the classified one wins.
            Proximity-alert margin multiplier: <strong>${mult.toFixed(2)}</strong>×.
            Watchlist entry-zone alerts: <strong>${regimeState && regimeState.watchlist_alerts_active ? 'active' : 'suppressed'}</strong>.
          </p>
        </div>
        <div class="modal-foot">
          <button class="btn-secondary" id="eff-history">View history</button>
          <button class="btn-primary" id="eff-close">Close</button>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#eff-close').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
  $('#eff-history').addEventListener('click', () => { closeImportModal(); switchTab('settings'); });
}

// ---------- Cowen weekly capture form (Spec 9b D4) ----------------------
//
// 8 numeric/radio fields + 3 macro checkboxes + optional note. Posts to
// /api/regime/cowen/auto and shows the classification + reason.
// "Show me last week's values" pre-fills from regimeState.cowen.last_inputs.

function openCowenCaptureForm() {
  closeImportModal();
  const last = regimeState && regimeState.cowen ? regimeState.cowen.last_inputs : null;
  const lastJSON = last && typeof last === 'object' ? last : null;
  const root = document.createElement('div');
  root.id = 'modal-root';
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal score-modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">Cowen weekly capture</div>
            <div class="desc">8 fields + 3 macro flags. Auto-classifies the Cowen regime.</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          ${lastJSON ? `<button class="btn-ghost" id="cw-prefill" style="margin-bottom:0.8rem">↻ Pre-fill from last submission</button>` : ''}
          <form id="cw-form">
            <div class="form-row"><label>1. BTC vs 200wk MA (%)</label>
              <input name="btc_vs_200wk_ma_pct" type="number" step="0.1" required placeholder="e.g. +28.4"/></div>
            <div class="form-row"><label>2. BTC vs 200d MA (%)</label>
              <input name="btc_vs_200d_ma_pct"  type="number" step="0.1" required placeholder="e.g. +12.3"/></div>
            <div class="form-row"><label>3. Log regression band third</label>
              <div class="radio-row">
                ${['lower','middle','upper'].map(v => `<label><input type="radio" name="log_band_third" value="${v}" required/> ${v}</label>`).join('')}
              </div></div>
            <div class="form-row"><label>4. Risk indicator (0.00–1.00)</label>
              <input name="risk_indicator" type="number" step="0.01" min="0" max="1" required placeholder="e.g. 0.62"/></div>
            <div class="form-row"><label>5. BTC dominance (%)</label>
              <input name="btc_dominance_pct" type="number" step="0.1" required placeholder="e.g. 54.2"/></div>
            <div class="form-row"><label>   ↳ 4-week trend</label>
              <div class="radio-row">
                ${['rising','flat','falling'].map(v => `<label><input type="radio" name="btc_dominance_4wk" value="${v}" required/> ${v}</label>`).join('')}
              </div></div>
            <div class="form-row"><label>6. ETH/BTC</label>
              <input name="eth_btc" type="number" step="0.0001" required placeholder="e.g. 0.0540"/></div>
            <div class="form-row"><label>   ↳ 4-week trend</label>
              <div class="radio-row">
                ${['rising','flat','falling'].map(v => `<label><input type="radio" name="eth_btc_4wk" value="${v}" required/> ${v}</label>`).join('')}
              </div></div>
            <div class="form-row"><label>7. MVRV Z-Score band</label>
              <div class="radio-row">
                ${[['undervalued','undervalued'],['neutral','neutral'],['overvalued','overvalued'],['extreme_overvalued','extreme over']].map(([v,l]) => `<label><input type="radio" name="mvrv_z_band" value="${v}" required/> ${l}</label>`).join('')}
              </div></div>
            <div class="form-row"><label>8. Cycle phase</label>
              <div class="radio-row">
                ${[[1,'1 Accum.'],[2,'2 Early Bull'],[3,'3 Late Bull'],[4,'4 Euphoria']].map(([v,l]) => `<label><input type="radio" name="cycle_phase" value="${v}" required/> ${l}</label>`).join('')}
              </div></div>
            <div class="form-row"><label>9. Macro context (check all that apply)</label>
              <div class="check-col">
                <label><input type="checkbox" name="cpi_trending_down"/> CPI trending down</label>
                <label><input type="checkbox" name="fed_not_hostile"/> Fed not hostile</label>
                <label><input type="checkbox" name="recession_risk_low"/> Recession risk low</label>
              </div></div>
            <div class="form-row"><label>10. Note (optional)</label>
              <textarea name="note" rows="2" placeholder="anything else"></textarea></div>
            <div class="error" id="cw-err"></div>
          </form>
        </div>
        <div class="modal-foot">
          <button class="btn-secondary" id="cw-cancel">Cancel</button>
          <button class="btn-primary" id="cw-save">Classify + save</button>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#cw-cancel').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
  if (lastJSON && $('#cw-prefill')) {
    $('#cw-prefill').addEventListener('click', () => cowenPrefill(lastJSON));
  }
  $('#cw-save').addEventListener('click', () => submitCowenCapture());
}

function cowenPrefill(d) {
  const form = $('#cw-form');
  const setVal = (name, val) => { const el = form.querySelector(`[name=${name}]`); if (el != null && val != null) el.value = val; };
  const setRadio = (name, val) => { const el = form.querySelector(`[name=${name}][value="${val}"]`); if (el) el.checked = true; };
  const setCheck = (name, val) => { const el = form.querySelector(`[name=${name}]`); if (el) el.checked = !!val; };
  setVal('btc_vs_200wk_ma_pct', d.btc_vs_200wk_ma_pct);
  setVal('btc_vs_200d_ma_pct',  d.btc_vs_200d_ma_pct);
  setRadio('log_band_third',    d.log_band_third);
  setVal('risk_indicator',      d.risk_indicator);
  setVal('btc_dominance_pct',   d.btc_dominance_pct);
  setRadio('btc_dominance_4wk', d.btc_dominance_4wk);
  setVal('eth_btc',             d.eth_btc);
  setRadio('eth_btc_4wk',       d.eth_btc_4wk);
  setRadio('mvrv_z_band',       d.mvrv_z_band);
  setRadio('cycle_phase',       d.cycle_phase);
  setCheck('cpi_trending_down', d.cpi_trending_down);
  setCheck('fed_not_hostile',   d.fed_not_hostile);
  setCheck('recession_risk_low', d.recession_risk_low);
}

async function submitCowenCapture() {
  const form = $('#cw-form');
  const err = $('#cw-err');
  err.textContent = '';
  const num = (k) => { const v = form.querySelector(`[name=${k}]`).value.trim(); return v === '' ? null : parseFloat(v); };
  const radio = (k) => { const el = form.querySelector(`[name=${k}]:checked`); return el ? el.value : null; };
  const check = (k) => form.querySelector(`[name=${k}]`).checked;

  const body = {
    btc_vs_200wk_ma_pct: num('btc_vs_200wk_ma_pct'),
    btc_vs_200d_ma_pct:  num('btc_vs_200d_ma_pct'),
    log_band_third:      radio('log_band_third'),
    risk_indicator:      num('risk_indicator'),
    btc_dominance_pct:   num('btc_dominance_pct'),
    btc_dominance_4wk:   radio('btc_dominance_4wk'),
    eth_btc:             num('eth_btc'),
    eth_btc_4wk:         radio('eth_btc_4wk'),
    mvrv_z_band:         radio('mvrv_z_band'),
    cycle_phase:         radio('cycle_phase') ? parseInt(radio('cycle_phase'), 10) : null,
    cpi_trending_down:   check('cpi_trending_down'),
    fed_not_hostile:     check('fed_not_hostile'),
    recession_risk_low:  check('recession_risk_low'),
    note:                form.querySelector('[name=note]').value.trim(),
  };

  // Bail early on missing required radios.
  for (const k of ['log_band_third','btc_dominance_4wk','eth_btc_4wk','mvrv_z_band']) {
    if (!body[k]) { err.textContent = 'Missing radio: ' + k; return; }
  }
  if (body.cycle_phase == null) { err.textContent = 'Missing cycle phase'; return; }
  if (body.risk_indicator == null || body.risk_indicator < 0 || body.risk_indicator > 1) {
    err.textContent = 'risk_indicator must be 0.00 – 1.00'; return;
  }

  try {
    const res = await api('/api/regime/cowen/auto', { method: 'POST', body: JSON.stringify(body) });
    closeImportModal();
    await refreshRegime();
    // Quick toast-ish modal showing the classification + reason
    const toast = document.createElement('div');
    toast.className = 'regime-toast';
    toast.innerHTML = `
      <span>✓ Cowen regime: <strong>${escapeHTML(res.regime.toUpperCase())}</strong></span>
      <span class="dim"> — ${escapeHTML(res.reason || '')}</span>
    `;
    document.body.appendChild(toast);
    setTimeout(() => toast.remove(), 6000);
  } catch (e) {
    err.textContent = e.message;
  }
}

// ---------- export -------------------------------------------------------

function onExport() {
  // Simple: navigate to the endpoint; the browser handles the download.
  window.location.href = '/api/export.xlsx';
}

// ---------- import modal -------------------------------------------------

const importState = {
  step: 'pick',           // 'pick' | 'loading' | 'preview' | 'applying' | 'applied' | 'error'
  preview: null,
  applyStocks: true,
  applyCrypto: true,
  error: null,
};

function openImportModal() {
  importState.step = 'pick';
  importState.preview = null;
  importState.applyStocks = true;
  importState.applyCrypto = true;
  importState.error = null;
  renderImportModal();
}

function closeImportModal() {
  const el = $('#modal-root');
  if (el) el.remove();
}

function renderImportModal() {
  closeImportModal();
  const root = document.createElement('div');
  root.id = 'modal-root';
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">Import master file</div>
            <div class="desc" id="modal-desc"></div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body" id="modal-body"></div>
        <div class="modal-foot" id="modal-foot"></div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => {
    if (ev.target.id === 'modal-overlay') closeImportModal();
  });
  renderImportBody();
}

function renderImportBody() {
  const body = $('#modal-body');
  const foot = $('#modal-foot');
  const desc = $('#modal-desc');
  if (!body || !foot || !desc) return;

  switch (importState.step) {
    case 'pick': {
      desc.textContent = 'Drop an .xlsx and review the diff before applying.';
      body.innerHTML = `
        <div class="dropzone" id="dropzone">
          <input type="file" class="file-input" id="file-input" accept=".xlsx,.xls" />
          <div>Drop a file here, or <button type="button" class="browse" id="browse">browse</button></div>
          <div class="hint">.xlsx · master format (stocks_etfs + crypto + meta) or legacy 5-col</div>
        </div>
        ${importState.error ? `<div class="error" style="margin-top:1rem">${escapeHTML(importState.error)}</div>` : ''}
      `;
      foot.innerHTML = '';
      const dz = $('#dropzone');
      const fi = $('#file-input');
      $('#browse').addEventListener('click', () => fi.click());
      fi.addEventListener('change', (ev) => {
        if (ev.target.files[0]) handleImportFile(ev.target.files[0]);
      });
      dz.addEventListener('dragover', (ev) => { ev.preventDefault(); dz.classList.add('drag'); });
      dz.addEventListener('dragleave', () => dz.classList.remove('drag'));
      dz.addEventListener('drop', (ev) => {
        ev.preventDefault();
        dz.classList.remove('drag');
        if (ev.dataTransfer.files[0]) handleImportFile(ev.dataTransfer.files[0]);
      });
      break;
    }
    case 'loading': {
      desc.textContent = 'Reading file…';
      body.innerHTML = `<div class="empty">parsing…</div>`;
      foot.innerHTML = '';
      break;
    }
    case 'preview': {
      const p = importState.preview;
      desc.innerHTML = `<span class="tabular">${escapeHTML(p.fileName)}</span>`;

      const sections = [
        renderDiffSection({
          title: 'Stocks & ETFs',
          tag: p.isMasterFormatStocks ? 'stocks_etfs' : 'legacy',
          rowCount: p.stockCount,
          counts: p.stockDiff.counts,
          rows: p.stockDiff.rows,
          included: importState.applyStocks,
          inputId: 'apply-stocks',
        }),
        renderDiffSection({
          title: 'Crypto',
          tag: p.hasCrypto ? 'crypto' : '—',
          rowCount: p.cryptoCount,
          counts: p.cryptoDiff.counts,
          rows: p.cryptoDiff.rows,
          included: importState.applyCrypto,
          inputId: 'apply-crypto',
        }),
      ];

      const warns = (p.warnings || []).slice(0, 5);
      const warnPanel = warns.length === 0 ? '' : `
        <div class="warn-panel">
          <div class="head">${p.warnings.length} warning${p.warnings.length === 1 ? '' : 's'}</div>
          <ul>${warns.map(w => `<li>· ${escapeHTML(w)}</li>`).join('')}
          ${p.warnings.length > 5 ? `<li class="dim">· …and ${p.warnings.length - 5} more</li>` : ''}</ul>
        </div>
      `;

      // Spec 12 D7 — enrichment banner. Lists tickers + which fields were
      // auto-filled from Yahoo during preview. Highlights only — the user
      // can edit individual cells in a follow-up, or just hit Apply to
      // accept everything (which is the daily-use flow).
      const enr = p.enriched || [];
      const enrichPanel = enr.length === 0 ? '' : `
        <div class="warn-panel" style="border-color:rgb(var(--color-accent)/0.35); background:rgb(var(--color-accent)/0.08)">
          <div class="head" style="color:rgb(var(--color-accent))">
            ✨ Auto-filled from Yahoo (${enr.length} row${enr.length === 1 ? '' : 's'})
          </div>
          <ul>
            ${enr.slice(0, 10).map(e => `<li class="tabular">· <strong>${escapeHTML(e.ticker)}</strong> <span class="dim">${escapeHTML((e.fields || []).join(', '))}</span></li>`).join('')}
            ${enr.length > 10 ? `<li class="dim">· …and ${enr.length - 10} more</li>` : ''}
          </ul>
        </div>
      `;

      const meta = (p.schemaVersion != null || p.fxSnapshotEurUsd != null) ? `
        <div class="dim" style="font-size:0.7rem; letter-spacing:0.12em; text-transform:uppercase; margin-top:0.8rem">
          ${p.schemaVersion != null ? `schema v${p.schemaVersion}` : ''}
          ${p.fxSnapshotEurUsd != null ? ` · fx eur→usd ${Number(p.fxSnapshotEurUsd).toFixed(4)}` : ''}
        </div>
      ` : '';

      body.innerHTML = sections.join('') + enrichPanel + warnPanel + meta;

      const sCb = $('#apply-stocks');
      const cCb = $('#apply-crypto');
      if (sCb) sCb.addEventListener('change', (e) => { importState.applyStocks = e.target.checked; });
      if (cCb) cCb.addEventListener('change', (e) => { importState.applyCrypto = e.target.checked; });

      foot.innerHTML = `
        <button class="btn-secondary" id="modal-cancel">Cancel</button>
        <button class="btn-primary" id="modal-apply">Apply import</button>
      `;
      $('#modal-cancel').addEventListener('click', () => {
        importState.step = 'pick';
        renderImportBody();
      });
      $('#modal-apply').addEventListener('click', applyImport);
      break;
    }
    case 'applying': {
      desc.textContent = 'Applying…';
      body.innerHTML = `<div class="empty">writing to database…</div>`;
      foot.innerHTML = '';
      break;
    }
    case 'applied': {
      desc.textContent = 'Import complete.';
      const r = importState.applyResult || { stocksApplied: 0, cryptoApplied: 0 };
      body.innerHTML = `
        <div class="applied-panel">
          <div class="head">Import applied</div>
          <p style="margin:0; font-size:0.85rem">
            ${r.stocksApplied} stock${r.stocksApplied === 1 ? '' : 's'} ·
            ${r.cryptoApplied} crypto holding${r.cryptoApplied === 1 ? '' : 's'} written.
          </p>
        </div>
      `;
      foot.innerHTML = `<button class="btn-primary" id="modal-done">Done</button>`;
      $('#modal-done').addEventListener('click', () => {
        closeImportModal();
        state.stocks = null;  // invalidate cached holdings so they refetch
        state.crypto = null;
        loadActiveTab();
      });
      break;
    }
    case 'error': {
      desc.textContent = 'Import failed.';
      body.innerHTML = `<div class="error">${escapeHTML(importState.error || 'unknown error')}</div>`;
      foot.innerHTML = `<button class="btn-secondary" id="modal-retry">Back</button>`;
      $('#modal-retry').addEventListener('click', () => {
        importState.step = 'pick';
        importState.error = null;
        renderImportBody();
      });
      break;
    }
  }
}

function renderDiffSection({ title, tag, rowCount, counts, rows, included, inputId }) {
  const empty = rowCount === 0;
  const visibleRows = (rows || []).filter(r => r.kind !== 'unchanged').slice(0, 12);
  const omitted = (counts.new + counts.updated + counts.removed) - visibleRows.length;
  const rowsHTML = empty
    ? `<div class="dim">No rows detected for this section.</div>`
    : visibleRows.length === 0
      ? `<div class="dim">No changes — every row is identical to the current state.</div>`
      : `<ul class="diff-rows">
          ${visibleRows.map(r => `
            <li>
              <span class="kind-badge ${r.kind}">${r.kind === 'unchanged' ? '—' : r.kind === 'updated' ? 'UPD' : r.kind === 'new' ? 'NEW' : 'RMV'}</span>
              <span class="label">${escapeHTML(r.label)}</span>
              ${r.sub ? `<span class="sub">· ${escapeHTML(r.sub)}</span>` : ''}
              ${r.changedFields && r.changedFields.length > 0
                ? `<span class="fields">(${r.changedFields.slice(0, 4).map(escapeHTML).join(', ')}${r.changedFields.length > 4 ? ` +${r.changedFields.length - 4}` : ''})</span>`
                : ''}
            </li>
          `).join('')}
          ${omitted > 0 ? `<li class="dim">· …and ${omitted} more</li>` : ''}
        </ul>`;

  return `
    <section class="diff-section ${empty ? 'empty' : ''}">
      <div class="diff-head">
        <label>
          <input type="checkbox" id="${inputId}" ${included && !empty ? 'checked' : ''} ${empty ? 'disabled' : ''} />
          ${escapeHTML(title)}
          <span class="sheet-tag">sheet: ${escapeHTML(tag)}</span>
        </label>
        <span class="row-count">${rowCount} row${rowCount === 1 ? '' : 's'}</span>
      </div>
      ${empty ? '' : `
        <div class="count-chips">
          <span class="count-chip gain"><span>New</span><span class="n">${counts.new}</span></span>
          <span class="count-chip warn"><span>Updated</span><span class="n">${counts.updated}</span></span>
          <span class="count-chip muted"><span>Unchanged</span><span class="n">${counts.unchanged}</span></span>
          <span class="count-chip loss"><span>Removed</span><span class="n">${counts.removed}</span></span>
        </div>
      `}
      ${rowsHTML}
    </section>
  `;
}

async function handleImportFile(file) {
  importState.step = 'loading';
  importState.error = null;
  renderImportBody();
  try {
    const fd = new FormData();
    fd.append('file', file);
    const res = await fetch('/api/import/preview', {
      method: 'POST',
      credentials: 'same-origin',
      body: fd,
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || `status ${res.status}`);
    importState.preview = data;
    importState.applyStocks = data.stockCount > 0;
    importState.applyCrypto = data.cryptoCount > 0;
    importState.step = 'preview';
  } catch (err) {
    importState.error = err.message;
    importState.step = 'error';
  }
  renderImportBody();
}

async function applyImport() {
  importState.step = 'applying';
  renderImportBody();
  try {
    const result = await api('/api/import/apply', {
      method: 'POST',
      body: JSON.stringify({
        applyStocks: importState.applyStocks,
        applyCrypto: importState.applyCrypto,
      }),
    });
    importState.applyResult = result;
    importState.step = 'applied';
  } catch (err) {
    importState.error = err.message;
    importState.step = 'error';
  }
  renderImportBody();
}

async function loadRefreshStatus() {
  try {
    const s = await api('/api/refresh-status');
    const el = $('#refresh-status');
    if (!el) return;
    if (s.lastRefreshedAt) {
      el.textContent = `refreshed ${timeAgo(s.lastRefreshedAt)}`;
      el.title = s.lastRefreshedAt;
    } else {
      el.textContent = 'never refreshed';
      el.title = '';
    }
  } catch (_) {
    // silent
  }
}

function timeAgo(iso) {
  const t = new Date(iso).getTime();
  const seconds = Math.floor((Date.now() - t) / 1000);
  if (seconds < 60) return `${seconds}s ago`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
}

async function onRefresh() {
  const btn = $('#refresh');
  const stat = $('#refresh-status');
  btn.disabled = true;
  if (stat) stat.textContent = 'refreshing…';
  try {
    const result = await api('/api/refresh', { method: 'POST' });
    // Invalidate cached data so the next tab load refetches.
    state.stocks = null;
    state.crypto = null;
    if (stat) {
      const ok = `${result.stocksUpdated}/${result.stocksAttempted} stocks, ${result.cryptoUpdated}/${result.cryptoAttempted} crypto`;
      stat.textContent = `${ok} · ${result.tookMs}ms`;
    }
    loadActiveTab();
  } catch (err) {
    if (stat) stat.textContent = `error: ${err.message}`;
  } finally {
    btn.disabled = false;
  }
}

function switchTab(tab) {
  state.tab = tab;
  // Spec 10 — clicking any top-level tab leaves the per-holding detail
  // page if one was open.
  state.holdingDetail = null;
  for (const el of document.querySelectorAll('.tab')) {
    el.classList.toggle('active', el.dataset.tab === tab);
  }
  loadActiveTab();
}

// Spec 10 — open the per-holding detail page.
function openHoldingDetail(kind, id) {
  state.holdingDetail = { kind, id };
  loadActiveTab();
}

async function loadActiveTab() {
  const content = $('#content');
  content.innerHTML = '<div class="empty">loading…</div>';
  try {
    // Spec 10 — per-holding detail page takes over the content area when
    // state.holdingDetail is set. Tab bar stays visible; clicking another
    // tab clears holdingDetail.
    if (state.holdingDetail) {
      await renderHoldingDetail(state.holdingDetail);
      return;
    }
    if (state.tab === 'summary') {
      await renderSummary();
    } else if (state.tab === 'stocks') {
      if (state.stocks == null) {
        const res = await api('/api/holdings/stocks');
        state.stocks = res.holdings || [];
      }
      renderStocks();
    } else if (state.tab === 'crypto') {
      if (state.crypto == null) {
        const res = await api('/api/holdings/crypto');
        state.crypto = res.holdings || [];
      }
      renderCrypto();
    } else if (state.tab === 'performance') {
      await renderPerformance();
    } else if (state.tab === 'screener') {
      await renderScreener();
    } else if (state.tab === 'sector-rotation') {
      await renderSectorRotation();
    } else if (state.tab === 'scorecards') {
      await renderScorecards();
    } else if (state.tab === 'watchlist') {
      await renderWatchlist();
    } else if (state.tab === 'heatmap') {
      await renderHeatmap();
    } else if (state.tab === 'news') {
      await renderNews('market');
    } else if (state.tab === 'crypto-news') {
      await renderNews('crypto');
    } else if (state.tab === 'settings') {
      await renderSettings();
    }
  } catch (err) {
    content.innerHTML = `<div class="empty">
      <div class="loss">error: ${escapeHTML(err.message)}</div>
    </div>`;
  }
}

// ---------- summary -----------------------------------------------------

async function renderSummary() {
  const content = $('#content');
  let s;
  try {
    s = await api('/api/summary');
    state.summary = s;
  } catch (err) {
    content.innerHTML = `<div class="empty"><div class="loss">summary failed: ${escapeHTML(err.message)}</div></div>`;
    return;
  }

  const k = s.kpis;

  // USD/EUR toggle (Spec 2 D4). Cookie-backed; reload re-renders with FX.
  const currency = s.currency || 'USD';
  const toggle = `
    <div class="currency-toggle" role="tablist" aria-label="Display currency">
      <button class="ct-btn ${currency === 'USD' ? 'active' : ''}" data-ccy="USD" role="tab">USD</button>
      <button class="ct-btn ${currency === 'EUR' ? 'active' : ''}" data-ccy="EUR" role="tab">EUR</button>
    </div>
  `;

  // KPI cards — render even when valued=false (em-dashes), to keep layout stable.
  // flashId + flashVal attach a data-flash hook the flashOnRender helper reads
  // to apply .flash-up / .flash-down (Spec 2 D7) when the underlying number
  // changes between renders.
  function kpiCard(label, value, sub, tone, flashId, flashVal) {
    const flashAttrs = flashId
      ? ` data-flash-id="${flashId}" data-flash-value="${Number.isFinite(flashVal) ? flashVal : ''}"`
      : '';
    return `
      <div class="kpi-card">
        <div class="kpi-label">${label}</div>
        <div class="kpi-value num ${tone || ''}"${flashAttrs}>${value}</div>
        ${sub ? `<div class="kpi-sub num">${sub}</div>` : '<div class="kpi-sub">&nbsp;</div>'}
      </div>
    `;
  }

  const valueStr  = k.valued ? formatMoney(k.totalValue) : '—';
  const investedStr = formatMoney(k.totalInvested);
  const pnlTone = k.totalPnl > 0 ? 'gain' : k.totalPnl < 0 ? 'loss' : '';
  const pnlStr  = k.valued && Number.isFinite(k.totalPnl) ? `${k.totalPnl >= 0 ? '+' : '-'}${formatMoney(Math.abs(k.totalPnl))}` : '—';
  const pnlPctStr = k.totalPnlPct != null ? `${k.totalPnlPct >= 0 ? '+' : ''}${k.totalPnlPct.toFixed(2)}%` : '—';
  const todayTone = k.todayChange > 0 ? 'gain' : k.todayChange < 0 ? 'loss' : '';
  const todayStr = k.valued ? `${k.todayChange >= 0 ? '+' : '-'}${formatMoney(Math.abs(k.todayChange))}` : '—';
  const todayPctStr = k.todayChangePct != null ? `${k.todayChangePct >= 0 ? '+' : ''}${k.todayChangePct.toFixed(2)}%` : '';

  // Spec 12 D2 — cash KPI is now editable. Click anywhere on the card to
  // open the inline edit modal. Displayed value comes from /api/summary
  // (already currency-adjusted server-side).
  const cashValue = k.cash != null ? Number(k.cash) : 0;
  const cashStr = cashValue > 0 ? formatMoney(cashValue) : '—';
  const cashSub = cashValue > 0
    ? '<span class="dim">click to edit</span>'
    : '<span class="dim">click to set</span>';
  const cashCard = `
    <div class="kpi-card kpi-cash-clickable" id="kpi-cash" role="button" tabindex="0" title="Click to edit cash balance">
      <div class="kpi-label">Cash</div>
      <div class="kpi-value num">${cashStr}</div>
      <div class="kpi-sub">${cashSub}</div>
    </div>
  `;

  const kpiRow = `
    <div class="kpi-row">
      ${kpiCard('Total Value',     valueStr,                          `invested ${investedStr}`,         '',         'kpi-total-value', k.totalValue)}
      ${kpiCard('Total P&amp;L',   pnlStr,                            pnlPctStr,                          pnlTone,    'kpi-total-pnl',   k.totalPnl)}
      ${kpiCard('Today\'s Change', todayStr,                          todayPctStr,                        todayTone,  'kpi-today',       k.todayChange)}
      ${cashCard}
    </div>
  `;

  // Three donuts side by side
  function donutCard(title, svg, legend) {
    const legendHTML = (legend || []).map(row => `
      <li>
        <span class="legend-dot" style="background:${row.color}"></span>
        <span class="legend-label">${escapeHTML(row.label)}</span>
        <span class="legend-value num">${escapeHTML(row.valueStr)}</span>
        ${row.pct != null ? `<span class="legend-pct num dim">${row.pct.toFixed(1)}%</span>` : ''}
      </li>
    `).join('');
    return `
      <div class="donut-card">
        <div class="donut-title">${title}</div>
        <div class="donut-svg">${svg}</div>
        <ul class="donut-legend">${legendHTML}</ul>
      </div>
    `;
  }

  const donutRow = `
    <div class="donut-row">
      ${donutCard('Asset class',     s.donuts.assetClass,     s.legends.assetClass)}
      ${donutCard('Crypto core / alt', s.donuts.cryptoCoreAlt, s.legends.cryptoCoreAlt)}
      ${donutCard('Stocks by sector', s.donuts.stocksBySector, s.legends.stocksBySector)}
    </div>
  `;

  // Spec 9b D7+D8: bottleneck (stocks) + phase (crypto) donuts. Each only
  // renders if at least 50% of the relevant holdings have a tag set.
  const tagDonuts = renderTagDonuts(s);

  const footer = `
    <p class="dim" style="font-size:0.78rem; margin-top:1.5rem">
      ${s.counts.stocks} stock${s.counts.stocks === 1 ? '' : 's'} ·
      ${s.counts.crypto} crypto holding${s.counts.crypto === 1 ? '' : 's'} ·
      FX EUR→USD ${Number(s.fxEURUSD).toFixed(4)} ·
      as of ${new Date(s.asOf).toLocaleString()}
    </p>
  `;

  // Spec 4 D8: stale-score nudge — counts holdings with no score or score
  // older than 90 days. Fetched lazily; if the user hasn't visited Stocks/
  // Crypto yet, we pull fresh lists in the background.
  const staleBanner = `<div id="stale-score-banner"></div>`;
  // Spec 11 D6: stale-thesis nudge — holdings with no thesis notes in 90+
  // days. Same lazy fetch pattern; silent if zero stale.
  const staleThesisBanner = `<div id="stale-thesis-banner"></div>`;

  content.innerHTML = staleBanner + staleThesisBanner + toggle + kpiRow + donutRow + tagDonuts + footer;

  // Wire toggle clicks
  for (const btn of document.querySelectorAll('.ct-btn')) {
    btn.addEventListener('click', () => {
      const ccy = btn.dataset.ccy;
      document.cookie = `display_currency=${ccy}; path=/; SameSite=Lax; max-age=2592000`;
      renderSummary();
    });
  }

  // Flash any KPI value that moved since the last render (Spec 2 D7).
  flashOnRender();

  // Stale-score nudge — defer to keep this render snappy.
  updateStaleScoreBanner();
  // Spec 11 D6 — stale-thesis nudge.
  updateStaleThesisBanner();

  // Spec 12 D2 — cash KPI editable. Click + Enter/Space (a11y) both fire.
  const cashEl = document.querySelector('#kpi-cash');
  if (cashEl) {
    const open = () => openCashBalanceModal(s);
    cashEl.addEventListener('click', open);
    cashEl.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' || ev.key === ' ') {
        ev.preventDefault();
        open();
      }
    });
  }
}

// Spec 12 D2 — minimal modal for cash balance edit. Reads current values
// from /api/summary payload (cashUsd / cashEur) to pre-fill, writes back
// via /api/preferences. On save, invalidates summary state + re-renders.
function openCashBalanceModal(summaryPayload) {
  closeImportModal();
  const k = (summaryPayload && summaryPayload.kpis) || {};
  const ccy = (summaryPayload && summaryPayload.currency) || 'USD';
  const curUsd = Number(k.cashUsd || 0);
  const curEur = Number(k.cashEur || 0);
  const root = document.createElement('div');
  root.id = 'modal-root';
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">Set cash balance</div>
            <div class="desc">Single-row, single-user. Stored separately per currency.</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <form id="cash-form">
            <div class="form-row">
              <label for="cash-ccy">Currency</label>
              <select id="cash-ccy">
                <option value="USD" ${ccy === 'USD' ? 'selected' : ''}>USD</option>
                <option value="EUR" ${ccy === 'EUR' ? 'selected' : ''}>EUR</option>
              </select>
            </div>
            <div class="form-row">
              <label for="cash-amount">Amount</label>
              <input id="cash-amount" type="number" step="0.01" min="0" value="${ccy === 'EUR' ? curEur : curUsd}" />
            </div>
            <p class="dim" style="font-size:0.78rem">Current: $${curUsd.toFixed(2)} USD · €${curEur.toFixed(2)} EUR. Cash shows as a slice on the Asset Class donut whenever non-zero.</p>
            <div class="error" id="cash-err"></div>
          </form>
        </div>
        <div class="modal-foot">
          <button class="btn-secondary" id="cash-cancel">Cancel</button>
          <button class="btn-primary" id="cash-save">Save</button>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#cash-cancel').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
  $('#cash-save').addEventListener('click', async () => {
    const errEl = $('#cash-err'); errEl.textContent = '';
    const v = parseFloat($('#cash-amount').value);
    if (!Number.isFinite(v) || v < 0) { errEl.textContent = 'amount must be ≥ 0'; return; }
    const c = $('#cash-ccy').value;
    const key = c === 'EUR' ? 'cash_balance_eur' : 'cash_balance_usd';
    try {
      await api(`/api/preferences/${key}`, {
        method: 'PUT',
        body: JSON.stringify({ value: String(v) }),
      });
      closeImportModal();
      state.summary = null;
      renderSummary();
    } catch (e) {
      errEl.textContent = e.message;
    }
  });
}

// Spec 11 D6 — surface holdings with no thesis notes in 90+ days.
async function updateStaleThesisBanner() {
  const el = $('#stale-thesis-banner');
  if (!el) return;
  let stale;
  try {
    const r = await api('/api/notes/stale');
    stale = r.stale || [];
  } catch (_) {
    return; // silent
  }
  if (stale.length === 0) {
    el.innerHTML = '';
    return;
  }
  // Top 5 worst (or never-noted) — clickable into detail page.
  const sorted = stale.slice().sort((a, b) => {
    // Never-noted (DaysSince=-1) bubble up first; then biggest gap.
    if (a.daysSince === -1 && b.daysSince !== -1) return -1;
    if (b.daysSince === -1 && a.daysSince !== -1) return 1;
    return b.daysSince - a.daysSince;
  }).slice(0, 5);
  const rows = sorted.map(s => {
    const when = s.daysSince === -1 ? 'no notes yet' : `last note ${escapeHTML(s.lastObservation)} (${s.daysSince}d ago)`;
    return `<li><a href="#" data-stale-kind="${escapeHTML(s.holdingKind)}" data-stale-id="${s.holdingId}">${escapeHTML(s.ticker)}</a> <span class="dim">— ${when}</span></li>`;
  }).join('');
  el.innerHTML = `
    <div class="stale-banner">
      ⚠ <strong>${stale.length}</strong> holding${stale.length === 1 ? '' : 's'} ha${stale.length === 1 ? 's' : 've'} no thesis updates in 90+ days.
      <ul class="stale-list">${rows}</ul>
    </div>
  `;
  // Wire ticker clicks → open holding detail.
  el.querySelectorAll('[data-stale-kind]').forEach(a => {
    a.addEventListener('click', (ev) => {
      ev.preventDefault();
      openHoldingDetail(a.dataset.staleKind, parseInt(a.dataset.staleId, 10));
    });
  });
}

// Spec 9b D7+D8 — Bottleneck (stocks) + Phase (crypto) donuts on Summary.
// Conditional render: only show when ≥50% of holdings have a tagged score.
// Below that threshold, render a placeholder nudging Fin to score more.
function renderTagDonuts(s) {
  const cov = s.tagCoverage || {};
  const cn  = s.counts || {};
  const sections = [];

  function placeholder(title, total, tagged, label) {
    const pct = total > 0 ? Math.round((tagged / total) * 100) : 0;
    return `
      <div class="donut-card placeholder">
        <div class="donut-title">${escapeHTML(title)}</div>
        <div class="donut-placeholder">
          <p>Available once ≥ 50% of ${escapeHTML(label)} have a framework tag.</p>
          <p class="dim">Currently <strong>${tagged}/${total}</strong> (${pct}%).</p>
        </div>
      </div>
    `;
  }

  function donut(title, svg, legend) {
    const legendHTML = (legend || []).map(row => `
      <li>
        <span class="legend-dot" style="background:${row.color}"></span>
        <span class="legend-label">${escapeHTML(row.label)}</span>
        <span class="legend-value num">${escapeHTML(row.valueStr)}</span>
        ${row.pct != null ? `<span class="legend-pct num dim">${row.pct.toFixed(1)}%</span>` : ''}
      </li>
    `).join('');
    return `
      <div class="donut-card">
        <div class="donut-title">${escapeHTML(title)}</div>
        <div class="donut-svg">${svg}</div>
        <ul class="donut-legend">${legendHTML}</ul>
      </div>
    `;
  }

  const bnReady = (cov.bottleneck || 0) >= 0.5;
  sections.push(bnReady
    ? donut('Bottleneck (stocks)', s.donuts.bottleneck, s.legends.bottleneck)
    : placeholder('Bottleneck (stocks)', cn.stocks || 0, cn.stocksTagged || 0, 'stocks'));

  const phReady = (cov.phase || 0) >= 0.5;
  sections.push(phReady
    ? donut('Cycle phase (crypto)', s.donuts.phase, s.legends.phase)
    : placeholder('Cycle phase (crypto)', cn.crypto || 0, cn.cryptoTagged || 0, 'crypto'));

  return `<div class="donut-row" style="margin-top:1rem">${sections.join('')}</div>`;
}

async function updateStaleScoreBanner() {
  const el = $('#stale-score-banner');
  if (!el) return;
  let stocks = state.stocks;
  let crypto = state.crypto;
  try {
    if (stocks == null) {
      const r = await api('/api/holdings/stocks');
      stocks = state.stocks = r.holdings || [];
    }
    if (crypto == null) {
      const r = await api('/api/holdings/crypto');
      crypto = state.crypto = r.holdings || [];
    }
  } catch (_) {
    return; // silent — banner is a nudge, not critical
  }
  const flagged = []; // { kind, id, ticker, name, status, days }
  for (const h of (stocks || [])) {
    if (!h.score) flagged.push({ kind: 'stock', id: h.id, ticker: h.ticker || h.name, name: h.name, status: 'unscored', days: null });
    else if (h.score.staleDays > 90) flagged.push({ kind: 'stock', id: h.id, ticker: h.ticker || h.name, name: h.name, status: 'stale', days: h.score.staleDays });
  }
  for (const h of (crypto || [])) {
    if (!h.score) flagged.push({ kind: 'crypto', id: h.id, ticker: h.symbol, name: h.name, status: 'unscored', days: null });
    else if (h.score.staleDays > 90) flagged.push({ kind: 'crypto', id: h.id, ticker: h.symbol, name: h.name, status: 'stale', days: h.score.staleDays });
  }
  if (flagged.length === 0) {
    el.innerHTML = '';
    return;
  }
  flagged.sort((a, b) => {
    // unscored before stale; among stale, oldest first.
    if (a.status !== b.status) return a.status === 'unscored' ? -1 : 1;
    return (b.days || 0) - (a.days || 0);
  });
  const unscored = flagged.filter(f => f.status === 'unscored').length;
  const stale    = flagged.filter(f => f.status === 'stale').length;
  const total = flagged.length;
  const parts = [];
  if (unscored) parts.push(`${unscored} unscored`);
  if (stale) parts.push(`${stale} stale (>90d)`);

  // Backlog polish — banner now click-to-expand with per-row jump links.
  const items = flagged.slice(0, 10).map(f => {
    const tag = f.status === 'unscored'
      ? '<span class="dim">unscored</span>'
      : `<span class="amber-text">${f.days}d stale</span>`;
    return `<li><a href="#" data-stale-score-kind="${escapeHTML(f.kind)}" data-stale-score-id="${f.id}">${escapeHTML(f.ticker)}</a> <span class="dim">— ${escapeHTML(f.name)}</span> · ${tag}</li>`;
  }).join('');

  el.innerHTML = `
    <details class="stale-banner stale-banner-collapsible">
      <summary>
        ⚠ <strong>${total}</strong> holding${total === 1 ? '' : 's'} need${total === 1 ? 's' : ''} framework scoring — ${parts.join(', ')}.
        <span class="dim">Click to expand.</span>
      </summary>
      <ul class="stale-list">${items}</ul>
      ${total > 10 ? `<p class="dim">+ ${total - 10} more…</p>` : ''}
    </details>
  `;
  // Wire ticker links → open holding detail page directly (which has the
  // rescore button now via click-to-rescore polish item).
  el.querySelectorAll('[data-stale-score-kind]').forEach(a => {
    a.addEventListener('click', (ev) => {
      ev.preventDefault();
      openHoldingDetail(a.dataset.staleScoreKind, parseInt(a.dataset.staleScoreId, 10));
    });
  });
}

// flashOnRender walks every [data-flash-id] element on the page, compares
// its data-flash-value attribute to the previously-rendered value, and adds
// a .flash-up / .flash-down class if it moved. The class self-removes on
// animationend so consecutive ticks keep flashing.
const prevFlashValues = {};
function flashOnRender() {
  for (const el of document.querySelectorAll('[data-flash-id]')) {
    const id = el.dataset.flashId;
    const raw = el.dataset.flashValue;
    if (raw === '' || raw == null) continue;
    const newV = parseFloat(raw);
    if (!Number.isFinite(newV)) continue;
    const prevV = prevFlashValues[id];
    if (prevV != null && Math.abs(newV - prevV) > 0.001) {
      const dir = newV > prevV ? 'up' : 'down';
      el.classList.remove('flash-up', 'flash-down');
      // Force reflow so the same class re-applied tick-after-tick re-fires
      // the animation. Reading offsetWidth is the canonical trick.
      void el.offsetWidth;
      el.classList.add(`flash-${dir}`);
      el.addEventListener('animationend', () => {
        el.classList.remove('flash-up', 'flash-down');
      }, { once: true });
    }
    prevFlashValues[id] = newV;
  }
}

// formatMoney mirrors the server's fmtMoney for client-side fallbacks.
function formatMoney(usd) {
  const abs = Math.abs(usd);
  if (abs >= 1_000_000) return `$${(usd / 1_000_000).toFixed(2)}M`;
  if (abs >= 10_000)    return `$${Math.round(usd).toLocaleString('en-US')}`;
  return `$${usd.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

// ---------- heatmap ------------------------------------------------------

async function renderHeatmap() {
  const content = $('#content');
  const w = Math.max(640, Math.floor(window.innerWidth - 64));
  const h = Math.floor(w * 0.55);

  // Spec 6 D2 — load mode preference (lazy; persists on change).
  if (state.heatmapMode == null) {
    try {
      const r = await api('/api/preferences/heatmap_mode');
      const v = r.value;
      state.heatmapMode = (v === 'my_holdings' || v === 'pnl') ? v : 'market_cap';
    } catch (_) { state.heatmapMode = 'market_cap'; }
  }
  const mode = state.heatmapMode;

  const legendStops = [-3, -2, -1, 0, 1, 2, 3];

  const sectorOptions = ['', ...SECTORS]
    .map((s) => `<option value="${escapeHTML(s)}" ${s === state.heatmapSector ? 'selected' : ''}>${s === '' ? 'All sectors' : escapeHTML(s)}</option>`)
    .join('');

  const modeOptions = [
    { v: 'market_cap', l: 'Market cap (S&P 500)' },
    { v: 'my_holdings', l: 'My holdings (position value)' },
    // Backlog polish 2026-05-17 — third mode: size by |P&L|.
    { v: 'pnl',         l: 'My holdings (P&L size)' },
  ].map(o => `<option value="${o.v}" ${o.v === mode ? 'selected' : ''}>${o.l}</option>`).join('');

  const caption =
      mode === 'pnl'         ? 'Your portfolio · sized by |P&L| · biggest winners/losers loudest'
    : mode === 'my_holdings' ? 'Your portfolio · sized by position value · colored by daily change'
    :                          'S&P 500 · sized by market cap · colored by daily change';

  const legendHTML = `
    <div class="heatmap-legend">
      <span>View</span>
      <select id="heatmap-mode" class="hm-select">${modeOptions}</select>
      <span style="margin-left:0.6rem">Sector</span>
      <select id="heatmap-sector" class="hm-select">${sectorOptions}</select>
      <span style="margin-left:1rem">Daily %</span>
      <span class="stops">
        ${legendStops.map(s => `
          <span class="stop">
            <span class="swatch" style="background:${tileColor(s)}"></span>
            <span class="lbl">${s > 0 ? '+' + s : s}</span>
          </span>
        `).join('')}
      </span>
      <span style="margin-left:auto" class="dim">held = amber stripe · hover for details</span>
    </div>
    <div class="heatmap-caption">${escapeHTML(caption)}</div>
  `;

  content.innerHTML = legendHTML + `<div class="heatmap-wrap" id="heatmap-svg">loading…</div>
    <div class="heatmap-note">
      ${ mode === 'pnl'         ? "Tile area = |P&L| dollars. A position up $5k draws the same as one down $5k. Color still encodes today's daily change."
       : mode === 'my_holdings' ? 'Only your active stock holdings are shown. Empty sectors hidden.'
       :                          'Live prices populated on each refresh; tiles update silently when the background scheduler runs.'}
    </div>`;

  $('#heatmap-mode').addEventListener('change', async (ev) => {
    const v = ev.target.value;
    const safe = (v === 'my_holdings' || v === 'pnl') ? v : 'market_cap';
    state.heatmapMode = safe;
    try {
      await api('/api/preferences/heatmap_mode', {
        method: 'PUT',
        body: JSON.stringify({ value: safe }),
      });
    } catch (e) {
      console.warn('heatmap_mode persist failed', e.message);
    }
    renderHeatmap();
  });

  $('#heatmap-sector').addEventListener('change', (ev) => {
    state.heatmapSector = ev.target.value;
    renderHeatmap();
  });

  try {
    const params = new URLSearchParams({ w: String(w), h: String(h), mode });
    if (state.heatmapSector) params.set('sector', state.heatmapSector);
    const res = await fetch(`/api/heatmap.svg?${params}`, { credentials: 'same-origin' });
    if (!res.ok) throw new Error(`status ${res.status}`);
    const svg = await res.text();
    $('#heatmap-svg').innerHTML = svg;
  } catch (err) {
    $('#heatmap-svg').innerHTML = `<div class="empty"><div class="loss">heatmap failed: ${escapeHTML(err.message)}</div></div>`;
  }
}

// ---------- news --------------------------------------------------------

async function renderNews(scope) {
  const content = $('#content');
  content.innerHTML = '<div class="empty">loading news…</div>';

  // Spec 9b D10 — filter mode persisted in user_preferences.news_filter_mode.
  if (state.newsFilterMode == null) {
    try {
      const r = await api('/api/preferences/news_filter_mode');
      state.newsFilterMode = r.value === 'mine' ? 'mine' : 'all';
    } catch (_) { state.newsFilterMode = 'all'; }
  }
  const filterQS = state.newsFilterMode === 'mine' ? '?filter=mine' : '';

  try {
    const [feed, macroData] = await Promise.all([
      api((scope === 'market' ? '/api/news/market' : '/api/news/crypto') + filterQS),
      // Macro cards only on the market tab (D11 spec scope).
      scope === 'market' ? api('/api/macro').catch(() => ({ upcoming: [], recent: [] })) : Promise.resolve(null),
    ]);
    renderFeed(feed, scope, macroData);
  } catch (err) {
    content.innerHTML = `<div class="empty"><div class="loss">news failed: ${escapeHTML(err.message)}</div></div>`;
  }
}

function renderFeed(feed, scope, macroData) {
  const content = $('#content');
  const articles = feed.articles || [];

  // Spec 9b D10 — toggle at the top.
  const filterMode = state.newsFilterMode || 'all';
  const filterToggle = `
    <div class="news-filter-toggle" role="tablist" aria-label="News filter">
      <button class="nf-btn ${filterMode === 'all' ? 'active' : ''}" data-filter="all" role="tab">All news</button>
      <button class="nf-btn ${filterMode === 'mine' ? 'active' : ''}" data-filter="mine" role="tab">My holdings + watchlist</button>
    </div>
  `;

  // Spec 9b D11 — macro cards (market tab only).
  let macroHTML = '';
  if (scope === 'market' && macroData) {
    macroHTML = renderMacroCards(macroData);
  }

  let banner = '';
  if (feed.source === 'unconfigured') {
    const key = scope === 'market' ? 'NEWSAPI_API_KEY' : 'CRYPTOPANIC_API_KEY';
    banner = `<div class="warn-panel" style="margin-bottom:1rem">
      <div class="head">no api key configured</div>
      <div>Add <code style="font-family:var(--font-mono)">${key}</code> to <code style="font-family:var(--font-mono)">/etc/ft/env</code> on jarvis and restart ft.service. Free tier at
      ${scope === 'market' ? 'newsapi.org' : 'cryptopanic.com'}, ~100 requests/day, plenty for hourly cache.</div>
    </div>`;
  }

  // Spec 12 D10a — F&G placement.
  //   News tab → CNN Stocks F&G via /api/feargreed/stocks
  //   Crypto News tab → Alternative.me Crypto F&G via /api/feargreed
  // Both reuse the same chip element (#fg-chip) but the loader picks the
  // right endpoint via scope.
  const fgChip = `<div id="fg-chip" class="fg-chip">fear &amp; greed: loading…</div>`;

  if (articles.length === 0) {
    content.innerHTML = filterToggle + macroHTML + banner + fgChip +
      `<div class="empty">${filterMode === 'mine' ? 'No matching articles for your holdings + watchlist.' : 'No articles to show.'}</div>`;
    wireNewsToggle(scope);
    return;
  } else {
    const list = articles.map((a, idx) => {
      const sentClass = a.sentiment === 'positive' ? 'gain' : a.sentiment === 'negative' ? 'loss' : 'dim';
      const time = a.publishedAt ? new Date(a.publishedAt).toLocaleString() : '';
      return `
        <article class="news-item" data-news-idx="${idx}">
          <div class="news-meta">
            <span class="news-source">${escapeHTML(a.source || 'unknown')}</span>
            <span class="news-time">${escapeHTML(time)}</span>
            ${a.sentiment ? `<span class="news-sent ${sentClass}">${a.sentiment}</span>` : ''}
            <button class="btn-ghost news-note-btn" data-news-note="${idx}" title="Add thesis note from this article">📝 Note this</button>
          </div>
          <a class="news-title" href="${escapeHTML(a.url)}" target="_blank" rel="noopener noreferrer">${escapeHTML(a.title)}</a>
          ${a.summary ? `<p class="news-summary">${escapeHTML(a.summary)}</p>` : ''}
        </article>
      `;
    }).join('');
    content.innerHTML = filterToggle + macroHTML + banner + fgChip + `<div class="news-list">${list}</div>`;
    // Spec 11 D5 — wire Note-this buttons.
    document.querySelectorAll('[data-news-note]').forEach(btn => {
      btn.addEventListener('click', () => {
        const a = articles[parseInt(btn.dataset.newsNote, 10)];
        if (!a) return;
        openNoteFromNewsItem(a, scope);
      });
    });
  }

  wireNewsToggle(scope);
  // Spec 12 D10a — both tabs get an F&G chip; endpoint depends on scope.
  loadFearGreed(scope);
}

// Spec 11 D5 — open Add Note modal from a news article. The user picks a
// target holding (or watchlist) via the second-stage chooser. If we can
// auto-match a ticker from the article text, default-select it.
async function openNoteFromNewsItem(article, scope) {
  // Build candidate list from current holdings + watchlist.
  let stocks = state.stocks, crypto = state.crypto, watchlist = state.watchlist;
  try {
    if (stocks == null) { const r = await api('/api/holdings/stocks'); stocks = state.stocks = r.holdings || []; }
    if (crypto == null) { const r = await api('/api/holdings/crypto'); crypto = state.crypto = r.holdings || []; }
    if (watchlist == null) { const r = await api('/api/watchlist'); watchlist = state.watchlist = r.watchlist || []; }
  } catch (_) { /* tolerate partial */ }
  // Candidate items: { label, targetKind, targetId, ticker, holdingKind }
  const candidates = [];
  for (const h of (stocks || [])) {
    if (h.ticker || h.name) candidates.push({ label: `${h.ticker || h.name} — ${h.name}`, targetKind: 'holding', targetId: h.id, ticker: (h.ticker || h.name), holdingKind: 'stock' });
  }
  for (const h of (crypto || [])) {
    candidates.push({ label: `${h.symbol} — ${h.name} (crypto)`, targetKind: 'holding', targetId: h.id, ticker: h.symbol, holdingKind: 'crypto' });
  }
  for (const w of (watchlist || [])) {
    candidates.push({ label: `${w.ticker} — ${w.companyName || w.kind} (watchlist)`, targetKind: 'watchlist', targetId: w.id, ticker: w.ticker, holdingKind: w.kind });
  }
  // Try to auto-match ticker by scanning title+summary.
  const hay = ((article.title || '') + ' ' + (article.summary || '')).toUpperCase();
  let preselect = null;
  for (const c of candidates) {
    const needle = c.ticker.toUpperCase();
    if (needle && hay.includes(needle)) { preselect = c; break; }
  }
  // If exactly one auto-match, jump straight to the modal.
  if (preselect) {
    openAddNoteModal({
      targetKind: preselect.targetKind,
      targetId: preselect.targetId,
      ticker: preselect.ticker,
      holdingKind: preselect.holdingKind,
      prefill: { observationText: article.title || '', sourceUrl: article.url || '', sourceKind: 'news' },
      onSaved: () => { /* no detail-page refresh; news tab unchanged */ },
    });
    return;
  }
  // Otherwise, open a small target-picker modal.
  closeImportModal();
  const root = document.createElement('div');
  root.id = 'modal-root';
  const opts = candidates.map((c, i) => `<option value="${i}">${escapeHTML(c.label)}</option>`).join('');
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">Add note — pick target</div>
            <div class="desc">${escapeHTML((article.title || '').slice(0, 120))}</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <div class="form-row">
            <label for="np-pick">Holding / watchlist entry</label>
            <select id="np-pick">${opts}</select>
          </div>
        </div>
        <div class="modal-foot">
          <button class="btn-secondary" id="np-cancel">Cancel</button>
          <button class="btn-primary" id="np-next">Next →</button>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#np-cancel').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
  $('#np-next').addEventListener('click', () => {
    const c = candidates[parseInt($('#np-pick').value, 10)];
    if (!c) return;
    openAddNoteModal({
      targetKind: c.targetKind,
      targetId: c.targetId,
      ticker: c.ticker,
      holdingKind: c.holdingKind,
      prefill: { observationText: article.title || '', sourceUrl: article.url || '', sourceKind: 'news' },
    });
  });
}

function wireNewsToggle(scope) {
  for (const btn of document.querySelectorAll('.nf-btn')) {
    btn.addEventListener('click', async () => {
      const v = btn.dataset.filter === 'mine' ? 'mine' : 'all';
      state.newsFilterMode = v;
      try {
        await api('/api/preferences/news_filter_mode', {
          method: 'PUT',
          body: JSON.stringify({ value: v }),
        });
      } catch (e) {
        console.warn('news_filter_mode persist failed', e.message);
      }
      renderNews(scope);
    });
  }
}

// Spec 9b D11 — Macro Economics cards.
function renderMacroCards(d) {
  const upcoming = d.upcoming || [];
  const recent = d.recent || [];
  if (upcoming.length === 0 && recent.length === 0) return '';

  const kindEmoji = (k) => ({
    cpi:  '📈',
    fomc: '🏛️',
    nfp:  '👷',
    pce:  '💵',
    gdp:  '📊',
  })[k] || '📅';

  const daysToCard = (ev) => {
    const t = new Date(ev.date + 'T12:00:00Z').getTime();
    const days = Math.round((t - Date.now()) / (1000 * 60 * 60 * 24));
    let label;
    if (days === 0) label = 'today';
    else if (days === 1) label = 'tomorrow';
    else if (days > 0) label = `in ${days}d`;
    else label = `${-days}d ago`;
    const tone = days <= 1 ? 'macro-soon' : days <= 7 ? 'macro-near' : 'macro-far';
    return `
      <a class="macro-card ${tone}" href="${escapeHTML(ev.url)}" target="_blank" rel="noopener noreferrer">
        <span class="macro-emoji">${kindEmoji(ev.kind)}</span>
        <span class="macro-body">
          <span class="macro-label">${escapeHTML(ev.label)}</span>
          <span class="macro-when">${escapeHTML(label)} · ${escapeHTML(ev.date)}</span>
        </span>
      </a>
    `;
  };

  const upcomingHTML = upcoming.length ? `
    <div class="macro-section">
      <div class="macro-section-head">Upcoming · next 14 days</div>
      <div class="macro-cards">${upcoming.map(daysToCard).join('')}</div>
    </div>
  ` : '';

  const recentHTML = recent.length ? `
    <div class="macro-section">
      <div class="macro-section-head">Recent · last 7 days</div>
      <div class="macro-cards">${recent.map(ev => `
        <a class="macro-card macro-past" href="${escapeHTML(ev.url)}" target="_blank" rel="noopener noreferrer">
          <span class="macro-emoji">${kindEmoji(ev.kind)}</span>
          <span class="macro-body">
            <span class="macro-label">${escapeHTML(ev.label)}</span>
            <span class="macro-when dim">Released ${escapeHTML(ev.date)}</span>
          </span>
        </a>
      `).join('')}</div>
    </div>
  ` : '';

  return `<div class="macro-block">${upcomingHTML}${recentHTML}</div>`;
}

async function loadFearGreed(scope) {
  const el = $('#fg-chip');
  if (!el) return;
  // Spec 12 D10a — pick endpoint by scope. crypto: alternative.me;
  // market: CNN. Both return the same {value, classification} shape.
  const endpoint = scope === 'market' ? '/api/feargreed/stocks' : '/api/feargreed';
  const label = scope === 'market' ? 'stocks fear &amp; greed' : 'crypto fear &amp; greed';
  try {
    const fg = await api(endpoint);
    if (fg.value == null) {
      el.textContent = (scope === 'market' ? 'stocks ' : 'crypto ') + 'fear & greed: unavailable';
      el.classList.add('dim');
      return;
    }
    const v = fg.value;
    const tone = v >= 75 ? 'gain' : v >= 55 ? 'gain dim' : v >= 45 ? 'dim' : v >= 25 ? 'loss dim' : 'loss';
    el.innerHTML = `${label}: <span class="${tone}" style="font-weight:600">${v}</span> · ${escapeHTML(fg.classification || '')}`;
  } catch (_) {
    el.textContent = (scope === 'market' ? 'stocks ' : 'crypto ') + 'fear & greed: error';
  }
}

// Match heatmap.TileColor in Go: lerp neutral → gain/loss across ±3%, clamped.
function tileColor(changePct) {
  const neutral = [38, 46, 60];
  const gain = [16, 200, 124];
  const loss = [245, 80, 110];
  const clamped = Math.max(-3, Math.min(3, changePct));
  const t = Math.abs(clamped) / 3;
  const target = clamped >= 0 ? gain : loss;
  const lerp = (a, b) => Math.round(a + (b - a) * t);
  return `rgb(${lerp(neutral[0], target[0])},${lerp(neutral[1], target[1])},${lerp(neutral[2], target[2])})`;
}

// ---------- stocks table --------------------------------------------------

async function renderStocks() {
  const rows = state.stocks;
  // Spec 9f D6 — fetch sector tag map (5-min cache).
  const sectorTagMap = await getSectorTagMap();
  if (rows.length === 0) {
    $('#content').innerHTML = `
      <div class="empty">
        <div>No stock holdings yet.</div>
        <div class="hint">Run <code>sudo -u ft /opt/ft/bin/ft seed</code> on the server to load demo data, or wait for xlsx import (Phase B).</div>
      </div>
    `;
    return;
  }

  // Summary chips
  let totalInvested = 0;
  let totalValue = 0;
  let totalPnl = 0;
  let countPnlable = 0;
  const alertCount = { red: 0, amber: 0, green: 0, neutral: 0 };
  for (const r of rows) {
    totalInvested += r.investedUsd || 0;
    if (r.metrics.currentValueUsd != null) {
      totalValue += r.metrics.currentValueUsd;
      countPnlable++;
    }
    if (r.metrics.pnlUsd != null) totalPnl += r.metrics.pnlUsd;
    if (r.alert && alertCount[r.alert.status] != null) alertCount[r.alert.status]++;
  }
  const totalPnlPct = totalInvested > 0 ? (totalPnl / totalInvested) * 100 : null;

  const chips = `
    <div class="summary-row">
      <div class="chip">
        <div class="label">Holdings</div>
        <div class="value">${rows.length}</div>
      </div>
      <div class="chip">
        <div class="label">Invested</div>
        <div class="value">${fmtUSD.format(totalInvested)}</div>
      </div>
      <div class="chip">
        <div class="label">Value</div>
        <div class="value">${countPnlable > 0 ? fmtUSD.format(totalValue) : '—'}</div>
      </div>
      <div class="chip">
        <div class="label">P&amp;L $</div>
        <div class="value ${totalPnl > 0 ? 'gain' : totalPnl < 0 ? 'loss' : ''}">${countPnlable > 0 ? fmtUSD.format(totalPnl) : '—'}</div>
      </div>
      <div class="chip">
        <div class="label">P&amp;L %</div>
        <div class="value ${totalPnlPct > 0 ? 'gain' : totalPnlPct < 0 ? 'loss' : ''}">${totalPnlPct != null ? `${totalPnlPct > 0 ? '+' : ''}${fmtNum2.format(totalPnlPct)}%` : '—'}</div>
      </div>
      <div class="chip">
        <div class="label">Alerts</div>
        <div class="value"><span class="alert-red">${alertCount.red}</span> <span class="alert-amber">${alertCount.amber}</span> <span class="alert-green">${alertCount.green}</span></div>
      </div>
    </div>
  `;

  // Table — Spec 12 D5 canonical column order. SL/TP now render as
  // price | %, "Stop Loss" → "Proposed SL", and 12m vol slots in between
  // distance-to-SL and earnings.
  const body = rows.map((r) => {
    const m = r.metrics;
    const a = r.alert || { status: 'neutral', triggers: [] };
    const badge = `
      <span class="alert-badge ${a.status}" title="${escapeHTML(a.triggers.join(' · ') || 'no triggers')}">
        <span class="dot"></span>${a.status}
      </span>
    `;
    const noteCell = r.note
      ? `<td class="note-cell"><span class="note-bubble" data-note="${escapeHTML(r.note)}" tabindex="0" aria-label="Show note" title="Hover for full note">💬</span></td>`
      : `<td></td>`;
    // Spec 12 D5b — combined price|% cell. Falls back to "—" when one
    // side is missing.
    const proposedSL = proposedLevelCell(r.stopLoss, r.suggestedSlPct, r.avgOpenPrice, r.currentPrice, 'sl');
    const proposedTP = proposedLevelCell(r.takeProfit, r.suggestedTpPct, r.avgOpenPrice, r.currentPrice, 'tp');
    const sparkSvg = r.sparklineSvg || '<span class="sparkline-empty">—</span>';
    const tickerCell = `<span class="ticker-hover" data-row-id="${r.id}" data-row-kind="stock" tabindex="0">${escapeHTML(r.ticker || '—')}</span>`;
    const vol12mCell = r.volatility12mPct != null
      ? `<td class="num" title="Annualized 12m realized volatility">${fmtNum1.format(r.volatility12mPct)}%</td>`
      : `<td class="num dim">—</td>`;
    return `
      <tr data-row-id="${r.id}" data-row-kind="stock">
        <td>${badge}</td>
        <td class="holding-name-cell" data-holding-detail="stock:${r.id}" title="Open detail page">
          <div>${escapeHTML(r.name)}</div>
          <div class="ticker">${tickerCell}${r.category ? ' · <span class="dim">' + escapeHTML(r.category) + '</span>' : ''}</div>
          ${sectorFlowPill(r.sectorUniverseId, sectorTagMap)}
        </td>
        <td class="num">${fmtUSD.format(r.investedUsd)}</td>
        <td class="num">${dash(r.avgOpenPrice, fmtNum2)}</td>
        <td class="num" data-flash-id="stock-${r.id}-price" data-flash-value="${r.currentPrice ?? ''}">${dash(r.currentPrice, fmtNum2)}</td>
        <td class="num" data-flash-id="stock-${r.id}-pnl" data-flash-value="${m.pnlUsd ?? ''}">${dashSigned(m.pnlUsd, fmtNum2, '$')}</td>
        <td class="num">${pct(m.pnlPct, 2)}</td>
        <td class="num">${dash(r.rsi14, fmtNum2)}</td>
        <td class="num">${proposedSL}</td>
        <td class="num">${proposedTP}</td>
        <td class="num">${pct(m.distanceToSlPct, 1)}</td>
        ${vol12mCell}
        <td>${earningsCell(r.earningsDate)}</td>
        <td>${exDivCell(r.exDividendDate)}</td>
        <td class="sparkline-cell">${sparkSvg}</td>
        <td>${scoreCell(r.score, true, 'stock', r.id)}</td>
        <td class="market-cell">${marketCell(r.market)}</td>
        ${noteCell}
        <td><button class="row-edit" data-row-id="${r.id}" data-row-kind="stock" title="Edit">✎</button></td>
      </tr>
    `;
  }).join('');

  const toolbar = `
    <div class="table-toolbar">
      <button class="btn-ghost" id="add-stock">+ Add stock</button>
    </div>
  `;

  // Spec 12 D5d — header tooltips on technical / risk metric headers.
  $('#content').innerHTML = chips + toolbar + `
    <div class="tablewrap">
      <table class="holdings">
        <thead>
          <tr>
            <th>Alert</th>
            <th>Name / Ticker</th>
            <th class="num">Invested $</th>
            <th class="num">Avg Open</th>
            <th class="num">Current</th>
            <th class="num" title="Profit/loss in absolute dollars from invested capital">P&amp;L $</th>
            <th class="num" title="Profit/loss as a percentage of invested capital">P&amp;L %</th>
            <th class="num" title="Relative Strength Index over 14 periods. >70 = overbought, <30 = oversold.">RSI(14)</th>
            <th class="num" title="Recommended stop-loss level (price | % from current). Set on eToro manually.">Proposed SL</th>
            <th class="num" title="Recommended take-profit level (price | % from current). Set on eToro manually.">Proposed TP</th>
            <th class="num" title="How close the current price is to the proposed stop-loss.">Dist to SL</th>
            <th class="num" title="Annualized realized volatility over the past 12 months.">12m Vol</th>
            <th title="Next earnings">Earn</th>
            <th title="Next ex-dividend">Ex-Div</th>
            <th title="30-day price sparkline (daily closes).">30-day</th>
            <th title="Latest Jordi framework score. Click to rescore.">Score</th>
            <th title="Exchange hours for this ticker">Market</th>
            <th>Note</th>
            <th></th>
          </tr>
        </thead>
        <tbody>${body}</tbody>
      </table>
    </div>
  `;

  $('#add-stock').addEventListener('click', () => openHoldingModal({ kind: 'stock', mode: 'add' }));
  wireRowActions('stock');
  wireTickerHover('stock');
  flashOnRender();
}

// ---------- crypto table --------------------------------------------------

async function renderCrypto() {
  const rows = state.crypto;
  if (rows.length === 0) {
    $('#content').innerHTML = `
      <div class="empty">
        <div>No crypto holdings yet.</div>
        <div class="hint">Run <code>sudo -u ft /opt/ft/bin/ft seed</code> on the server to load demo data.</div>
      </div>
    `;
    return;
  }

  let totalCostUsd = 0;
  let totalValueUsd = 0;
  let totalPnlUsd = 0;
  let countValued = 0;
  for (const r of rows) {
    if (r.costBasisUsd != null) totalCostUsd += r.costBasisUsd;
    if (r.metrics.currentValueUsd != null) { totalValueUsd += r.metrics.currentValueUsd; countValued++; }
    if (r.metrics.pnlUsd != null) totalPnlUsd += r.metrics.pnlUsd;
  }
  const totalPnlPct = totalCostUsd > 0 ? (totalPnlUsd / totalCostUsd) * 100 : null;

  const chips = `
    <div class="summary-row">
      <div class="chip">
        <div class="label">Holdings</div>
        <div class="value">${rows.length}</div>
      </div>
      <div class="chip">
        <div class="label">Cost Basis</div>
        <div class="value">${fmtUSD.format(totalCostUsd)}</div>
      </div>
      <div class="chip">
        <div class="label">Value</div>
        <div class="value">${countValued > 0 ? fmtUSD.format(totalValueUsd) : '—'}</div>
      </div>
      <div class="chip">
        <div class="label">P&amp;L $</div>
        <div class="value ${totalPnlUsd > 0 ? 'gain' : totalPnlUsd < 0 ? 'loss' : ''}">${countValued > 0 ? fmtUSD.format(totalPnlUsd) : '—'}</div>
      </div>
      <div class="chip">
        <div class="label">P&amp;L %</div>
        <div class="value ${totalPnlPct > 0 ? 'gain' : totalPnlPct < 0 ? 'loss' : ''}">${totalPnlPct != null ? `${totalPnlPct > 0 ? '+' : ''}${fmtNum2.format(totalPnlPct)}%` : '—'}</div>
      </div>
    </div>
  `;

  // Spec 12 D6 — Crypto table refinements:
  //   D6a  Current Location column (where it lives now, vs wallet=where bought)
  //   D6b  Cost → "Original Cost", Value → "Current Value"
  //   D6c  Removed Sug SL / Sug TP columns (crypto uses Cowen risk bands)
  //   D6e  Added 12m Vol % column (replaces vol_tier dropdown on display)
  //   D6f  Auto Core/Alt (BTC/ETH) — Class column kept as a read-only label
  const body = rows.map((r) => {
    const m = r.metrics;
    const noteCell = r.note
      ? `<td class="note-cell"><span class="note-bubble" data-note="${escapeHTML(r.note)}" tabindex="0" aria-label="Show note" title="Hover for full note">💬</span></td>`
      : `<td></td>`;
    const sparkSvg = r.sparklineSvg || '<span class="sparkline-empty">—</span>';
    const symbolCell = `<span class="ticker-hover" data-row-id="${r.id}" data-row-kind="crypto" tabindex="0">${escapeHTML(r.symbol)}</span>`;
    const loc = r.currentLocation ? cryptoLocationLabel(r.currentLocation) : '';
    const locCell = loc
      ? `<td><span class="loc-pill" title="Current custody location">${loc}</span></td>`
      : `<td><span class="dim">—</span></td>`;
    const vol12mCell = r.volatility12mPct != null
      ? `<td class="num" title="Annualized 12m realized volatility">${fmtNum1.format(r.volatility12mPct)}%</td>`
      : `<td class="num dim" title="vol tier: ${escapeHTML(r.volTier || 'medium')}">— <span class="dim">(${escapeHTML(r.volTier || 'medium')})</span></td>`;
    return `
      <tr data-row-id="${r.id}" data-row-kind="crypto">
        <td class="holding-name-cell" data-holding-detail="crypto:${r.id}" title="Open detail page">
          <div>${escapeHTML(r.name)} ${symbolCell}</div>
          <div class="ticker">${r.category ? escapeHTML(r.category) : '—'}${r.wallet ? ' · <span class="dim">' + escapeHTML(r.wallet) + '</span>' : ''}</div>
        </td>
        <td><span class="tag ${r.classification === 'core' ? 'core' : ''}">${escapeHTML(r.classification)}</span></td>
        ${locCell}
        <td class="num">${fmtNum6.format(m.totalQuantity)}</td>
        <td class="num" data-flash-id="crypto-${r.id}-price" data-flash-value="${r.currentPriceUsd ?? ''}">${dash(r.currentPriceUsd, fmtNum4)}</td>
        <td class="num">${dash(r.costBasisUsd, fmtNum2)}</td>
        <td class="num" data-flash-id="crypto-${r.id}-value" data-flash-value="${m.currentValueUsd ?? ''}">${dash(m.currentValueUsd, fmtNum2)}</td>
        <td class="num" data-flash-id="crypto-${r.id}-pnl" data-flash-value="${m.pnlUsd ?? ''}">${dashSigned(m.pnlUsd, fmtNum2, '$')}</td>
        <td class="num">${pct(m.pnlPct, 2)}</td>
        <td class="num">${pct(r.change7dPct, 1)}</td>
        <td class="num">${pct(r.change30dPct, 1)}</td>
        ${vol12mCell}
        <td class="sparkline-cell">${sparkSvg}</td>
        <td>${scoreCell(r.score, true, 'crypto', r.id)}</td>
        ${noteCell}
        <td><button class="row-edit" data-row-id="${r.id}" data-row-kind="crypto" title="Edit">✎</button></td>
      </tr>
    `;
  }).join('');

  const toolbar = `
    <div class="table-toolbar">
      <button class="btn-ghost" id="add-crypto">+ Add crypto</button>
    </div>
  `;

  $('#content').innerHTML = chips + toolbar + `
    <div class="tablewrap">
      <table class="holdings">
        <thead>
          <tr>
            <th>Name / Symbol</th>
            <th title="Core (BTC/ETH) vs alt — auto-assigned by symbol.">Class</th>
            <th title="Where the asset currently lives.">Location</th>
            <th class="num">Qty</th>
            <th class="num">Price $</th>
            <th class="num" title="Cost basis at acquisition.">Original Cost</th>
            <th class="num" title="Current market value of the position.">Current Value</th>
            <th class="num" title="Profit/loss in absolute dollars.">P&amp;L $</th>
            <th class="num" title="Profit/loss as a % of cost basis.">P&amp;L %</th>
            <th class="num">7d %</th>
            <th class="num">30d %</th>
            <th class="num" title="Annualized realized volatility over 12m (365d). Falls back to vol tier when insufficient history.">12m Vol</th>
            <th>30-day</th>
            <th title="Latest Cowen framework score. Click to rescore.">Score</th>
            <th>Note</th>
            <th></th>
          </tr>
        </thead>
        <tbody>${body}</tbody>
      </table>
    </div>
  `;
  $('#add-crypto').addEventListener('click', () => openHoldingModal({ kind: 'crypto', mode: 'add' }));
  wireRowActions('crypto');
  wireTickerHover('crypto');
  flashOnRender();
}

// ---------- event handlers ------------------------------------------------

async function onSetup(ev) {
  ev.preventDefault();
  const form = ev.target;
  const btn = $('#submit', form);
  const err = $('#err');
  err.textContent = '';
  btn.disabled = true;
  try {
    const data = await api('/api/auth/setup', {
      method: 'POST',
      body: JSON.stringify({
        email: form.email.value,
        password: form.password.value,
        displayName: form.display.value,
      }),
    });
    renderDashboard(data.user);
  } catch (e) {
    err.textContent = e.message;
    btn.disabled = false;
  }
}

async function onLogin(ev) {
  ev.preventDefault();
  const form = ev.target;
  const btn = $('#submit', form);
  const err = $('#err');
  err.textContent = '';
  btn.disabled = true;
  try {
    const data = await api('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({
        email: form.email.value,
        password: form.password.value,
      }),
    });
    renderDashboard(data.user);
  } catch (e) {
    err.textContent = e.message;
    btn.disabled = false;
  }
}

async function onLogout() {
  try {
    await api('/api/auth/logout', { method: 'POST' });
  } catch (_) {
    // ignore — even on error we want to go back to login
  }
  // reset state
  state.user = null;
  state.stocks = null;
  state.crypto = null;
  renderLogin();
}

// ---------- Spec 3: holdings modal + Settings tab -----------------------

// Per-row edit button → reuse the same openHoldingModal in edit mode.
function wireRowActions(kind) {
  for (const btn of document.querySelectorAll(`.row-edit[data-row-kind="${kind}"]`)) {
    btn.addEventListener('click', () => {
      const id = parseInt(btn.dataset.rowId, 10);
      const list = kind === 'stock' ? state.stocks : state.crypto;
      const holding = (list || []).find((h) => h.id === id);
      if (holding) openHoldingModal({ kind, mode: 'edit', holding });
    });
  }
  // Spec 10 — click name cell → open detail page.
  for (const cell of document.querySelectorAll('.holding-name-cell[data-holding-detail]')) {
    cell.addEventListener('click', (ev) => {
      // Don't trigger when clicking inside the cell (e.g. ticker hover).
      if (ev.target.closest('.row-edit')) return;
      const [k, idStr] = cell.dataset.holdingDetail.split(':');
      openHoldingDetail(k, parseInt(idStr, 10));
    });
  }
  // Backlog polish 2026-05-17 — click-to-rescore from holdings table.
  for (const cell of document.querySelectorAll(`[data-rescore-kind="${kind}"]`)) {
    cell.addEventListener('click', (ev) => {
      ev.stopPropagation();
      const id = parseInt(cell.dataset.rescoreId, 10);
      const list = kind === 'stock' ? state.stocks : state.crypto;
      const h = (list || []).find((x) => x.id === id);
      if (!h) return;
      openScoreScreen({
        targetKind: 'holding',
        kind,
        id,
        ticker: h.ticker || h.symbol || h.name,
        name: h.name,
      });
    });
  }
}

// ---------- Spec 3 D9: ticker hover popover -----------------------------
//
// Single floating element reused across rows. mouseenter shows; mouseleave on
// both the ticker AND the popover removes it (so the user can hover into the
// popover itself if needed). Position chosen above-row when there's room,
// otherwise below.

let _popoverEl = null;
let _popoverHideTimer = null;

function ensurePopover() {
  if (_popoverEl) return _popoverEl;
  _popoverEl = document.createElement('div');
  _popoverEl.className = 'ticker-popover';
  _popoverEl.setAttribute('aria-hidden', 'true');
  _popoverEl.addEventListener('mouseenter', () => {
    if (_popoverHideTimer) { clearTimeout(_popoverHideTimer); _popoverHideTimer = null; }
  });
  _popoverEl.addEventListener('mouseleave', schedulePopoverHide);
  document.body.appendChild(_popoverEl);
  return _popoverEl;
}
function schedulePopoverHide() {
  if (_popoverHideTimer) clearTimeout(_popoverHideTimer);
  _popoverHideTimer = setTimeout(() => {
    if (_popoverEl) {
      _popoverEl.classList.remove('show');
      _popoverEl.setAttribute('aria-hidden', 'true');
    }
  }, 120);
}

function showPopover(kind, id, anchor) {
  const list = kind === 'stock' ? state.stocks : state.crypto;
  const r = (list || []).find((x) => x.id === id);
  if (!r) return;

  // Render content. Sparkline is the row's regular SVG scaled up via CSS — no
  // separate "large" SVG round-trip needed thanks to viewBox.
  const price = kind === 'stock' ? r.currentPrice : r.currentPriceUsd;
  const change = kind === 'stock' ? (r.metrics && r.metrics.pnlPct) : r.dailyChangePct;
  const today = kind === 'stock' ? r.dailyChangePct : r.dailyChangePct;
  const alertBadge = r.alert
    ? `<span class="alert-badge ${r.alert.status}"><span class="dot"></span>${r.alert.status}</span>`
    : '';
  const big = (r.sparklineSvg || '<span class="sparkline-empty">—</span>').replace(
    'class="sparkline"',
    'class="sparkline sparkline-lg"'
  );
  const labelTicker = kind === 'stock' ? (r.ticker || '') : r.symbol;
  const direction30 = r.sparkline30dPct != null
    ? `<span class="${r.sparkline30dPct > 0 ? 'gain' : r.sparkline30dPct < 0 ? 'loss' : ''}">${r.sparkline30dPct > 0 ? '+' : ''}${fmtNum1.format(r.sparkline30dPct)}% 30d</span>`
    : '';

  const html = `
    <div class="popover-head">
      <div class="popover-name">${escapeHTML(r.name)} <span class="popover-tk">${escapeHTML(labelTicker)}</span></div>
      ${alertBadge}
    </div>
    <div class="popover-stats">
      <div>${price != null ? `<strong>${fmtNum2.format(price)}</strong>` : '—'} ${today != null ? pct(today, 2) : ''}</div>
      <div class="dim">${direction30}</div>
    </div>
    <div class="popover-spark">${big}</div>
    ${r.note ? `<div class="popover-note">${escapeHTML(r.note)}</div>` : ''}
  `;

  const pop = ensurePopover();
  pop.innerHTML = html;

  // Position: prefer above the anchor; flip below if not enough room.
  const rect = anchor.getBoundingClientRect();
  pop.style.visibility = 'hidden';
  pop.classList.add('show');
  const popH = pop.offsetHeight;
  const popW = pop.offsetWidth;
  pop.style.visibility = '';

  const margin = 6;
  const spaceAbove = rect.top;
  const wantsBelow = spaceAbove < popH + margin + 20;
  const top = wantsBelow ? (rect.bottom + margin) : (rect.top - popH - margin);

  // Horizontal: anchor centred on ticker, clamped to viewport.
  let left = rect.left + (rect.width / 2) - (popW / 2);
  left = Math.max(8, Math.min(left, window.innerWidth - popW - 8));

  pop.style.top = `${Math.max(8, top) + window.scrollY}px`;
  pop.style.left = `${left + window.scrollX}px`;
  pop.setAttribute('aria-hidden', 'false');
}

function wireTickerHover(kind) {
  for (const el of document.querySelectorAll(`.ticker-hover[data-row-kind="${kind}"]`)) {
    const id = parseInt(el.dataset.rowId, 10);
    el.addEventListener('mouseenter', () => {
      if (_popoverHideTimer) { clearTimeout(_popoverHideTimer); _popoverHideTimer = null; }
      showPopover(kind, id, el);
    });
    el.addEventListener('mouseleave', schedulePopoverHide);
    el.addEventListener('focus', () => showPopover(kind, id, el));
    el.addEventListener('blur', schedulePopoverHide);
  }
}

// Form schemas. Each field has: name (JSON key), label, type, optional opts.
// Spec 9c setup type + stage option lists, shared between stock + crypto.
const SETUP_TYPES = [
  { value: '',                   label: '(unset)' },
  { value: 'A_breakout_retest',  label: 'A — Breakout-retest' },
  { value: 'B_support_bounce',   label: 'B — Support bounce' },
  { value: 'C_continuation',     label: 'C — Continuation' },
];
const STAGES = [
  { value: 'pre_tp1',  label: 'pre-TP1' },
  { value: 'post_tp1', label: 'post-TP1' },
  { value: 'runner',   label: 'runner' },
  { value: 'stopped',  label: 'stopped' },
];

const stockFields = [
  { name: 'name',         label: 'Name',           type: 'text',     required: true },
  { name: 'ticker',       label: 'Ticker',         type: 'text' },
  { name: 'category',     label: 'Category',       type: 'text' },
  { name: 'sector',       label: 'Sector',         type: 'text' },
  // Spec 12 D7 AC #15 — listing currency auto-filled from Yahoo. Display
  // only; no P&L math depends on this.
  { name: 'currency',     label: 'Currency',       type: 'text' },
  { name: 'investedUsd',  label: 'Invested ($)',   type: 'number', required: true, step: '0.01' },
  { name: 'avgOpenPrice', label: 'Avg open price', type: 'number', step: '0.01' },
  { name: 'currentPrice', label: 'Current price',  type: 'number', step: '0.01' },
  { name: 'stopLoss',     label: 'Stop loss',      type: 'number', step: '0.01' },
  { name: 'takeProfit',   label: 'Take profit',    type: 'number', step: '0.01' },
  { name: 'beta',         label: 'Beta (manual)',  type: 'number', step: '0.01' },
  // Spec 9c — Percoco levels + setup classification.
  { name: 'support1',     label: 'Support 1',      type: 'number', step: '0.01', section: 'levels' },
  { name: 'support2',     label: 'Support 2',      type: 'number', step: '0.01', section: 'levels' },
  { name: 'resistance1',  label: 'Resistance 1 (TP1 ref)', type: 'number', step: '0.01', section: 'levels' },
  { name: 'resistance2',  label: 'Resistance 2 (TP2 ref)', type: 'number', step: '0.01', section: 'levels' },
  { name: 'setupType',    label: 'Setup type',     type: 'select-kv', options: SETUP_TYPES, section: 'levels' },
  { name: 'stage',        label: 'Stage',          type: 'select-kv', options: STAGES, section: 'levels' },
  // Spec 5 polish — manual override when ticker-suffix detection picks wrong.
  // Empty value = use suffix rule (default).
  { name: 'exchangeOverride', label: 'Exchange override', type: 'select-kv',
    options: [
      { value: '',         label: '— (auto from suffix)' },
      { value: 'US',       label: 'US (NYSE/NASDAQ)' },
      { value: 'LSE',      label: 'LSE (London)' },
      { value: 'EURONEXT', label: 'Euronext (Paris/Amsterdam/Brussels/Milan/Lisbon)' },
      { value: 'XETRA',    label: 'XETRA (Frankfurt)' },
      { value: 'TSE',      label: 'TSE (Tokyo)' },
      { value: 'HKEX',     label: 'HKEX (Hong Kong)' },
      { value: 'B3',       label: 'B3 (São Paulo)' },
    ] },
  { name: 'strategyNote', label: 'Strategy note',  type: 'textarea' },
  { name: 'note',         label: 'Note',           type: 'textarea' },
];
// Spec 12 D6a — current location options. Order matters: surface most-used
// first so the dropdown is usable without scrolling.
const CRYPTO_LOCATIONS = [
  { value: '',                label: '— (unset)' },
  { value: 'hardware_wallet', label: '🔐 Hardware wallet' },
  { value: 'ledger',          label: '🔐 Ledger' },
  { value: 'kraken',          label: '🏦 Kraken' },
  { value: 'binance',         label: '🏦 Binance' },
  { value: 'revolut',         label: '🏦 Revolut' },
  { value: 'etoro',           label: '🏦 eToro' },
  { value: 'phantom',         label: '👻 Phantom' },
  { value: 'metamask',        label: '🦊 MetaMask' },
  { value: 'other',           label: 'Other' },
];

function cryptoLocationLabel(code) {
  const found = CRYPTO_LOCATIONS.find(l => l.value === code);
  return found ? escapeHTML(found.label) : escapeHTML(code);
}

const cryptoFields = [
  { name: 'name',           label: 'Name',                 type: 'text',     required: true },
  { name: 'symbol',         label: 'Symbol',               type: 'text',     required: true },
  // Spec 12 D6f — Classification auto-assigned on save by symbol (BTC/ETH=core,
  // else alt). Kept in the form as a read-only-ish hint; user-typed value is
  // honoured server-side as an override.
  { name: 'classification', label: 'Classification',       type: 'select', options: ['core', 'alt'] },
  { name: 'volTier',        label: 'Volatility tier (manual override)', type: 'select', options: ['low','medium','high','extreme'] },
  // Spec 12 D6a — current custody location (vs `wallet` = where bought).
  { name: 'currentLocation', label: 'Current location',    type: 'select-kv', options: CRYPTO_LOCATIONS },
  { name: 'category',       label: 'Category',             type: 'text' },
  { name: 'wallet',         label: 'Wallet (where bought)',type: 'text' },
  { name: 'quantityHeld',   label: 'Quantity held',        type: 'number', step: 'any', required: true },
  { name: 'quantityStaked', label: 'Quantity staked',      type: 'number', step: 'any' },
  { name: 'avgBuyEur',      label: 'Avg buy €',            type: 'number', step: '0.0001' },
  { name: 'costBasisEur',   label: 'Cost basis €',         type: 'number', step: '0.01' },
  { name: 'currentPriceEur',label: 'Current price €',      type: 'number', step: '0.0001' },
  // Spec 9c — same Percoco fields apply to crypto.
  { name: 'support1',       label: 'Support 1',            type: 'number', step: '0.01', section: 'levels' },
  { name: 'support2',       label: 'Support 2',            type: 'number', step: '0.01', section: 'levels' },
  { name: 'resistance1',    label: 'Resistance 1 (TP1 ref)', type: 'number', step: '0.01', section: 'levels' },
  { name: 'resistance2',    label: 'Resistance 2 (TP2 ref)', type: 'number', step: '0.01', section: 'levels' },
  { name: 'setupType',      label: 'Setup type',           type: 'select-kv', options: SETUP_TYPES, section: 'levels' },
  { name: 'stage',          label: 'Stage',                type: 'select-kv', options: STAGES, section: 'levels' },
  { name: 'strategyNote',   label: 'Strategy note',        type: 'textarea' },
  { name: 'note',           label: 'Note',                 type: 'textarea' },
];

function openHoldingModal({ kind, mode, holding }) {
  const fields = kind === 'stock' ? stockFields : cryptoFields;
  const isEdit = mode === 'edit';
  const title = `${isEdit ? 'Edit' : 'Add'} ${kind === 'stock' ? 'stock' : 'crypto'} holding`;

  // Build form HTML. Spec 9c: fields marked `section:'levels'` cluster
  // visually under a "Levels & setup" heading with a thinner top border.
  // First emit non-level fields, then a separator + level fields.
  const renderField = (f) => {
    const id = `hm-${f.name}`;
    const val = isEdit && holding ? (holding[f.name] ?? '') : '';
    const req = f.required ? 'required' : '';
    let input;
    if (f.type === 'textarea') {
      input = `<textarea id="${id}" name="${f.name}" rows="2" ${req}>${escapeHTML(String(val ?? ''))}</textarea>`;
    } else if (f.type === 'select') {
      input = `<select id="${id}" name="${f.name}">` +
        f.options.map((o) => `<option value="${o}" ${o === val ? 'selected' : ''}>${o}</option>`).join('') +
        `</select>`;
    } else if (f.type === 'select-kv') {
      input = `<select id="${id}" name="${f.name}">` +
        f.options.map((o) => `<option value="${escapeHTML(o.value)}" ${o.value === val ? 'selected' : ''}>${escapeHTML(o.label)}</option>`).join('') +
        `</select>`;
    } else {
      const step = f.step ? ` step="${f.step}"` : '';
      input = `<input id="${id}" name="${f.name}" type="${f.type}" value="${escapeHTML(String(val ?? ''))}" ${req}${step} />`;
    }
    return `
      <div class="form-row">
        <label for="${id}">${escapeHTML(f.label)}${f.required ? ' *' : ''}</label>
        ${input}
      </div>
    `;
  };
  const mainFields = fields.filter(f => f.section !== 'levels').map(renderField).join('');
  const levelFields = fields.filter(f => f.section === 'levels').map(renderField).join('');
  // Spec 9c — Levels & Suggestions panel renders only in edit mode (needs
  // backend data from /levels endpoint).
  const levelsPanel = isEdit && holding ? `
    <div class="levels-panel" id="hm-levels-panel">
      <div class="levels-head">Suggestions <span class="dim" style="font-weight:normal">(updates as you change S/R)</span></div>
      <div class="levels-grid" id="hm-suggestions">loading…</div>
      <div class="position-size" id="hm-position-size"></div>
    </div>
  ` : '';
  const fieldRows = mainFields
    + (levelFields ? `<div class="form-section-head">Levels & setup (Spec 9c)</div>${levelFields}${levelsPanel}` : '');

  // Spec 12 D9 — structured reason dropdown + optional free-text. Submitted
  // as `reasonCode` (typed code) and `reason` (free-text); audit row stores
  // both. Codes shown only on edits where SL/TP/stage could change.
  const reasonCodes = [
    { code: '',                      label: '— (no code)' },
    { code: 'tech_break',            label: 'Move stop after technical break' },
    { code: 'tp1_hit',               label: 'Adjust after TP1 hit' },
    { code: 'tighten_on_profit',     label: 'Tighten as position moves to profit' },
    { code: 'loosen_vol',            label: 'Loosen due to volatility expansion' },
    { code: 'thesis_break',          label: 'Thesis broken — exit setup' },
    { code: 'earnings_approaching',  label: 'Earnings within 2 weeks' },
    { code: 'rebalance',             label: 'Portfolio rebalance' },
    { code: 'manual_other',          label: 'Other (free-text required)' },
  ];
  const reasonCodeOptions = reasonCodes
    .map(rc => `<option value="${escapeHTML(rc.code)}">${escapeHTML(rc.label)}</option>`)
    .join('');
  const reasonField = isEdit ? `
    <div class="form-row">
      <label for="hm-reason-code">Reason code</label>
      <select id="hm-reason-code" name="reasonCode">${reasonCodeOptions}</select>
    </div>
    <div class="form-row">
      <label for="hm-reason">Reason note <span class="dim">(optional unless code = Other)</span></label>
      <input id="hm-reason" name="reason" type="text" placeholder="e.g. broke trendline at $148; trailing stop to $142" />
    </div>
  ` : '';

  const errBox = `<div class="error" id="hm-err"></div>`;

  // Modal mount
  closeImportModal(); // dispose any existing modal first
  const root = document.createElement('div');
  root.id = 'modal-root';
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">${title}</div>
            ${isEdit && holding ? `<div class="desc tabular">${escapeHTML(holding.name)} ${holding.ticker || holding.symbol || ''}</div>` : ''}
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <form id="hm-form" class="holding-form">
            ${fieldRows}
            ${reasonField}
            ${errBox}
          </form>
        </div>
        <div class="modal-foot">
          ${isEdit ? `<button class="btn-danger" id="hm-delete">Delete</button>` : ''}
          <button class="btn-secondary" id="hm-cancel">Cancel</button>
          <button class="btn-primary" id="hm-save">${isEdit ? 'Save changes' : 'Add holding'}</button>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#hm-cancel').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
  $('#hm-save').addEventListener('click', () => submitHoldingForm({ kind, mode, holding }));
  if (isEdit) $('#hm-delete').addEventListener('click', () => deleteHoldingFromModal({ kind, holding }));

  // Spec 9c — wire the Levels & Suggestions panel (edit mode only).
  if (isEdit && holding) {
    setupHoldingLevelsPanel({ kind, holding });
  }

  // Spec 12 D7 — smart-autofill on the Add modal (and edits where blanks
  // exist). Debounced 300ms input on ticker/symbol/name fields. Never
  // overwrites a user-entered non-empty value; pings a small "auto-filled"
  // badge near each filled-in field.
  installAutofillForHoldingModal(kind, mode);
}

// Spec 12 D7 — autofill ticker → name/sector/currency for stocks, or
// symbol → name/category for crypto. Reverse-direction (name → ticker)
// also works because the lookup endpoint accepts freeform queries.
function installAutofillForHoldingModal(kind, mode) {
  // Edit-mode autofill is opt-in only on blanks; Add-mode fills aggressively.
  const tickerEl = document.querySelector(kind === 'stock' ? '#hm-ticker' : '#hm-symbol');
  const nameEl   = document.querySelector('#hm-name');
  if (!tickerEl && !nameEl) return;

  let timer = null;
  let lastLookup = '';
  const debounceMs = 350;

  const runLookup = async (rawQuery) => {
    const q = (rawQuery || '').trim();
    if (q.length < 2 || q === lastLookup) return;
    lastLookup = q;
    try {
      const p = await api(`/api/lookup/ticker?q=${encodeURIComponent(q)}&kind=${kind}`);
      applyAutofill(p);
    } catch (_) {
      // Silent — lookup is best-effort.
    }
  };

  const queue = (rawQuery) => {
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => runLookup(rawQuery), debounceMs);
  };

  if (tickerEl) tickerEl.addEventListener('input', () => queue(tickerEl.value));
  if (nameEl)   nameEl.addEventListener('input',   () => queue(nameEl.value));

  function applyAutofill(p) {
    if (!p) return;
    const fillIfBlank = (selector, value) => {
      if (value == null || value === '') return;
      const el = document.querySelector(selector);
      if (!el) return;
      if (mode === 'edit' && el.value.trim() !== '') return; // preserve user value
      if (mode === 'add' && el.value.trim() !== '' && el !== tickerEl && el !== nameEl) {
        // Don't overwrite something the user explicitly typed.
        return;
      }
      el.value = value;
      el.classList.add('autofilled');
      // Quick decay on the visual hint.
      setTimeout(() => el.classList.remove('autofilled'), 1500);
    };
    if (kind === 'stock') {
      fillIfBlank('#hm-ticker', p.ticker);
      fillIfBlank('#hm-name', p.name);
      fillIfBlank('#hm-sector', p.sector);
      fillIfBlank('#hm-currency', p.currency);
    } else {
      fillIfBlank('#hm-symbol', p.symbol);
      fillIfBlank('#hm-name', p.name);
      fillIfBlank('#hm-category', p.category);
      // Auto core/alt — only set if the user hasn't picked yet.
      const cls = document.querySelector('#hm-classification');
      if (cls && (cls.value === '' || cls.value === 'alt') && p.isCore) {
        cls.value = 'core';
        cls.classList.add('autofilled');
        setTimeout(() => cls.classList.remove('autofilled'), 1500);
      }
    }
  }
}

// Spec 9c — fetch /api/holdings/.../levels, render the suggestions card,
// wire live re-compute as the user types in support_1 / resistance_1 / etc.
async function setupHoldingLevelsPanel({ kind, holding }) {
  let data;
  try {
    data = await api(`/api/holdings/${kind === 'stock' ? 'stocks' : 'crypto'}/${holding.id}/levels`);
  } catch (e) {
    const el = $('#hm-suggestions');
    if (el) el.textContent = 'levels endpoint failed: ' + e.message;
    return;
  }
  // Initial render from server values.
  recomputeLevelsPanel(data);
  // Live recompute as user edits S/R inputs. Math is identical to server's:
  // SL = support_1 − N×ATR; TP1 = R1 − 0.25×ATR; etc.
  ['support1','resistance1','resistance2','stopLoss'].forEach(name => {
    const el = document.querySelector(`#hm-${name}`);
    if (!el) return;
    el.addEventListener('input', () => {
      // Build an in-memory override of data with the typed values.
      const override = JSON.parse(JSON.stringify(data));
      const num = (id) => {
        const e = document.querySelector(`#hm-${id}`);
        const v = e ? e.value.trim() : '';
        return v === '' ? null : parseFloat(v);
      };
      override.support1 = num('support1') ?? data.support1;
      override.resistance1 = num('resistance1') ?? data.resistance1;
      override.resistance2 = num('resistance2') ?? data.resistance2;
      const atr = override.atrWeekly || 0;
      const tier = override.suggestions.usingTier;
      const N = ({ low: 1.5, medium: 2.0, high: 2.5, extreme: 3.0 }[tier] || 2.0);
      // Re-derive suggestions client-side (mirror of internal/technicals).
      override.suggestions.sl = (override.support1 && atr) ? override.support1 - N * atr : 0;
      override.suggestions.tp1 = (override.resistance1 && atr) ? override.resistance1 - 0.25 * atr : 0;
      override.suggestions.tp2 = (override.resistance2 && atr) ? override.resistance2 - 0.25 * atr : 0;
      // Use user-entered entry (currentPrice if missing) for R-multiples.
      const entry = override.currentPrice || 0;
      const sl = override.suggestions.sl;
      override.suggestions.rMultipleTp1 = (entry > sl && sl > 0 && override.suggestions.tp1)
        ? (override.suggestions.tp1 - entry) / (entry - sl) : 0;
      override.suggestions.rMultipleTp2 = (entry > sl && sl > 0 && override.suggestions.tp2)
        ? (override.suggestions.tp2 - entry) / (entry - sl) : 0;
      recomputeLevelsPanel(override);
    });
  });
}

function recomputeLevelsPanel(d) {
  const el = $('#hm-suggestions');
  if (!el) return;
  const s = d.suggestions || {};
  const atr = d.atrWeekly ? `$${fmtNum2.format(d.atrWeekly)}` : '—';
  const tier = s.usingTier ? `${s.usingTier} <span class="dim">(${s.usingTierSource})</span>` : '—';
  const slStr = s.sl > 0 ? `$${fmtNum2.format(s.sl)}` : '—';
  const tp1Str = s.tp1 > 0 ? `$${fmtNum2.format(s.tp1)}` : '—';
  const tp2Str = s.tp2 > 0 ? `$${fmtNum2.format(s.tp2)}` : '—';
  const r1 = s.rMultipleTp1;
  const r2 = s.rMultipleTp2;
  const r1Class = r1 >= 1.5 ? 'gain' : r1 > 0 ? 'loss' : 'dim';
  const r2Class = r2 >= 3.0 ? 'gain' : r2 > 0 ? 'loss' : 'dim';

  // S/R candidate buttons — one-click "Use this value".
  const candHTML = (rows, target) => (rows || []).slice(0, 3).map(c =>
    `<button class="sr-candidate" data-target="${target}" data-value="${c.price.toFixed(2)}">
        $${fmtNum2.format(c.price)} <span class="dim">(${c.touches}×)</span>
     </button>`).join('');

  el.innerHTML = `
    <div class="levels-row"><span class="dim">ATR (14w):</span> ${atr}</div>
    <div class="levels-row"><span class="dim">Vol tier:</span> ${tier}</div>
    <div class="levels-row"><span class="dim">Suggested SL:</span> <strong class="num">${slStr}</strong></div>
    <div class="levels-row"><span class="dim">Proposed TP1:</span> <strong class="num">${tp1Str}</strong> · R ≈ <span class="${r1Class} num">${r1 ? r1.toFixed(2) : '—'}</span></div>
    <div class="levels-row"><span class="dim">Proposed TP2:</span> <strong class="num">${tp2Str}</strong> · R ≈ <span class="${r2Class} num">${r2 ? r2.toFixed(2) : '—'}</span></div>
    ${d.candidates && d.candidates.supports && d.candidates.supports.length ? `
      <div class="levels-row"><span class="dim">Support candidates:</span> ${candHTML(d.candidates.supports, 'support1')}</div>` : ''}
    ${d.candidates && d.candidates.resistances && d.candidates.resistances.length ? `
      <div class="levels-row"><span class="dim">Resistance candidates:</span> ${candHTML(d.candidates.resistances, 'resistance1')}</div>` : ''}
  `;
  // Wire candidate buttons → set the corresponding input + retrigger input event.
  el.querySelectorAll('.sr-candidate').forEach(btn => {
    btn.addEventListener('click', (ev) => {
      ev.preventDefault();
      const target = btn.dataset.target;
      const value = btn.dataset.value;
      const input = document.querySelector(`#hm-${target}`);
      if (input) {
        input.value = value;
        input.dispatchEvent(new Event('input', { bubbles: true }));
      }
    });
  });

  // Position Size Calculator below.
  renderPositionSizeCalc(d);
}

// Spec 9c — Position Size Calculator. Always pulls portfolio value from
// last loaded /api/summary; per-trade risk % defaults to 1% (override).
function renderPositionSizeCalc(d) {
  const el = $('#hm-position-size');
  if (!el) return;
  const portfolioValue = (state.summary && state.summary.kpis && state.summary.kpis.totalValue) || 100000;
  const entryEl = document.querySelector('#hm-currentPrice');
  const slEl = document.querySelector('#hm-stopLoss');
  // Live values: pull from the form so they update as user types.
  const entry = entryEl ? parseFloat(entryEl.value) || (d.currentPrice || 0) : (d.currentPrice || 0);
  const sl = slEl && slEl.value !== '' ? parseFloat(slEl.value) : (d.suggestions ? d.suggestions.sl : 0);
  el.innerHTML = `
    <div class="pos-size-head">Position size calculator <span class="dim">(1% per-trade risk default)</span></div>
    <div class="pos-size-grid">
      <div class="dim">Portfolio value</div><div class="num">$${fmtNum0.format(portfolioValue)}</div>
      <div class="dim">Per-trade risk %</div><div><input id="hm-risk-pct" type="number" step="0.1" min="0.1" max="5" value="1" /></div>
      <div class="dim">Entry</div><div class="num" id="ps-entry">$${entry ? fmtNum2.format(entry) : '—'}</div>
      <div class="dim">Stop loss (effective)</div><div class="num" id="ps-sl">$${sl ? fmtNum2.format(sl) : '—'}</div>
      <div class="dim">Position size</div><div id="ps-size" class="num"><strong>—</strong></div>
      <div class="dim">Max loss</div><div id="ps-loss" class="num">—</div>
    </div>
  `;
  const recompute = () => {
    const pct = parseFloat($('#hm-risk-pct').value) || 1;
    const eEl = document.querySelector('#hm-currentPrice');
    const sEl = document.querySelector('#hm-stopLoss');
    const e = eEl && eEl.value ? parseFloat(eEl.value) : entry;
    const s = sEl && sEl.value ? parseFloat(sEl.value) : sl;
    const riskUsd = portfolioValue * (pct / 100);
    if (!e || !s || e <= s) {
      $('#ps-size').innerHTML = '<span class="dim">invalid</span>';
      $('#ps-loss').innerHTML = '—';
      return;
    }
    const units = riskUsd / (e - s);
    const sizeUsd = units * e;
    const pctOfPort = (sizeUsd / portfolioValue) * 100;
    $('#ps-entry').textContent = `$${fmtNum2.format(e)}`;
    $('#ps-sl').textContent = `$${fmtNum2.format(s)}`;
    $('#ps-size').innerHTML = `<strong>${fmtNum2.format(units)}</strong> units · $${fmtNum0.format(sizeUsd)} (${pctOfPort.toFixed(1)}% of port)`;
    $('#ps-loss').innerHTML = `$${fmtNum0.format(riskUsd)} (${pct.toFixed(1)}%)`;
  };
  $('#hm-risk-pct').addEventListener('input', recompute);
  document.querySelector('#hm-currentPrice')?.addEventListener('input', recompute);
  document.querySelector('#hm-stopLoss')?.addEventListener('input', recompute);
  recompute();
}

async function submitHoldingForm({ kind, mode, holding }) {
  const form = $('#hm-form');
  const err = $('#hm-err');
  err.textContent = '';

  const fields = kind === 'stock' ? stockFields : cryptoFields;
  const body = {};
  for (const f of fields) {
    const el = form.querySelector(`[name="${f.name}"]`);
    if (!el) continue;
    let v = el.value;
    if (f.type === 'number') {
      if (v === '') v = null;
      else v = parseFloat(v);
    } else if (f.type === 'text' || f.type === 'textarea' || f.type === 'select') {
      v = v.trim();
      if (v === '' && f.name !== 'name' && f.name !== 'symbol' && f.name !== 'classification' && f.name !== 'volTier') {
        // exchangeOverride + currentLocation: empty string = "clear the
        // value" (user-intent signal); keep it so the server can blank
        // the field. Null would mean "preserve" instead.
        if (f.name !== 'exchangeOverride' && f.name !== 'currentLocation') {
          v = null;
        }
      }
    }
    body[f.name] = v;
  }
  // Required-field sanity
  if (!body.name || (kind === 'crypto' && !body.symbol)) {
    err.textContent = 'name' + (kind === 'crypto' ? ' and symbol' : '') + ' required';
    return;
  }
  // Crypto-specific: isCore defaults from classification
  if (kind === 'crypto') {
    body.isCore = body.classification === 'core';
  }
  // Update reason — Spec 12 D9 adds the typed reasonCode + keeps free-text.
  if (mode === 'edit') {
    const reasonEl = form.querySelector('[name="reason"]');
    const codeEl   = form.querySelector('[name="reasonCode"]');
    if (reasonEl && reasonEl.value.trim()) {
      body.reason = reasonEl.value.trim();
    }
    if (codeEl && codeEl.value) {
      body.reasonCode = codeEl.value;
      // Enforce "manual_other → text required" rule before submit.
      if (codeEl.value === 'manual_other' && !body.reason) {
        err.textContent = 'reason text required when code = Other';
        return;
      }
    }
  }

  const path = kind === 'stock' ? '/api/holdings/stocks' : '/api/holdings/crypto';
  const url = mode === 'add' ? path : `${path}/${holding.id}`;
  const method = mode === 'add' ? 'POST' : 'PUT';

  try {
    await api(url, { method, body: JSON.stringify(body) });
    closeImportModal();
    // Refetch the affected list + summary so the UI catches up
    if (kind === 'stock') state.stocks = null; else state.crypto = null;
    state.summary = null;
    loadActiveTab();
  } catch (e) {
    err.textContent = e.message;
  }
}

async function deleteHoldingFromModal({ kind, holding }) {
  const reasonEl = $('#hm-form').querySelector('[name="reason"]');
  const reason = reasonEl ? reasonEl.value.trim() : '';
  if (!confirm(`Soft-delete ${holding.name}? You can restore it from Settings → Deleted holdings.`)) return;
  const path = kind === 'stock' ? '/api/holdings/stocks' : '/api/holdings/crypto';
  try {
    await api(`${path}/${holding.id}`, {
      method: 'DELETE',
      body: JSON.stringify({ reason: reason || 'soft-deleted from edit modal' }),
    });
    closeImportModal();
    if (kind === 'stock') state.stocks = null; else state.crypto = null;
    state.summary = null;
    loadActiveTab();
  } catch (e) {
    alert('Delete failed: ' + e.message);
  }
}

// ---------- Settings tab (Spec 3 D6 restore UI + D13 audit log) ----------

// Spec 9c D16 — Portfolio Risk Dashboard. Bar visualisations of
// concentration / theme / total active risk / drawdown against caps,
// plus circuit-breaker state.
function renderPortfolioRiskSection(r) {
  const caps = r.caps || {};
  const concEntries = Object.entries(r.concentration || {}).sort((a, b) => b[1] - a[1]).slice(0, 10);
  const themeEntries = Object.entries(r.themeConcentration || {}).sort((a, b) => b[1] - a[1]);

  const barRow = (label, value, cap, suffix = '%') => {
    // Clamp bar at 100% even if value > cap.
    const widthPct = Math.min(100, (value / Math.max(cap, 0.01)) * 100);
    const over = value > cap;
    const tone = over ? 'risk-bar-over' : 'risk-bar-ok';
    return `
      <div class="risk-row">
        <div class="risk-label">${escapeHTML(label)}</div>
        <div class="risk-bar-track">
          <div class="risk-bar ${tone}" style="width:${widthPct.toFixed(1)}%"></div>
        </div>
        <div class="risk-value num">${value.toFixed(1)}${suffix}${over ? ' ⚠' : ''}</div>
      </div>
    `;
  };

  const concRows = concEntries.map(([t, p]) => barRow(t, p, caps.concentrationPct || 15)).join('');
  const themeRows = themeEntries.map(([t, p]) => barRow(t, p, caps.themeConcentrationPct || 30)).join('');

  const cbStatus = r.circuitBreakerActive
    ? `<span class="loss">🛑 ARMED${r.circuitBreakerUntil ? ` until ${escapeHTML(r.circuitBreakerUntil)}` : ''}</span>`
    : '<span class="gain">🟢 NORMAL</span>';

  const dd = (r.drawdownPct || 0);
  const ddCap = caps.drawdownCircuitPct || 10;

  const warns = (r.warnings || []).length
    ? `<ul class="risk-warnings">${r.warnings.map(w => `<li>⚠ ${escapeHTML(w)}</li>`).join('')}</ul>`
    : '';

  return `
    <section class="settings-block">
      <h3 class="settings-h3">Portfolio risk <span class="dim" style="font-size:0.78rem; font-weight:normal">(Spec 9c)</span></h3>
      <div class="risk-summary">
        <div class="risk-stat">
          <div class="risk-stat-label">Portfolio value</div>
          <div class="risk-stat-value num">$${fmtNum0.format(r.portfolioValue || 0)}</div>
        </div>
        <div class="risk-stat">
          <div class="risk-stat-label">Drawdown from peak</div>
          <div class="risk-stat-value num ${dd <= -ddCap ? 'loss' : dd < -3 ? 'amber-text' : ''}">${dd.toFixed(2)}%</div>
          <div class="risk-stat-sub dim">cap: -${ddCap}%</div>
        </div>
        <div class="risk-stat">
          <div class="risk-stat-label">Total active risk</div>
          <div class="risk-stat-value num">${(r.totalActiveRiskPct || 0).toFixed(2)}%</div>
          <div class="risk-stat-sub dim">cap: ${caps.totalActivePct || 8}%</div>
        </div>
        <div class="risk-stat">
          <div class="risk-stat-label">Circuit breaker</div>
          <div class="risk-stat-value">${cbStatus}</div>
        </div>
      </div>

      <h4 class="rh-side" style="margin-top:1rem">Concentration (cap ${caps.concentrationPct || 15}%)</h4>
      <div class="risk-bars">${concRows || '<div class="dim">No positions.</div>'}</div>

      <h4 class="rh-side" style="margin-top:0.8rem">Theme exposure (cap ${caps.themeConcentrationPct || 30}%)</h4>
      <div class="risk-bars">${themeRows || '<div class="dim">No themed positions.</div>'}</div>

      ${warns}

      <div style="margin-top:0.8rem">
        <button class="btn-ghost" id="risk-snapshot-btn">Run snapshot now</button>
      </div>
    </section>
  `;
}

// Spec 7 — Diagnostics & provider-health section.
// Spec 8 / Master Spec — small Settings section linking into the
// Scorecards tab with master-spec preselected. Reuses 100% of the
// Scorecards machinery for storage, render, edit, and versioning.
function renderSpecDocsSection() {
  return `
    <section class="settings-block">
      <h3 class="settings-h3">Spec docs</h3>
      <p class="dim" style="font-size:0.85rem; margin: 0 0 0.6rem 0">
        Living documentation of FT's current behaviour. Updated after every shipped change.
      </p>
      <button class="btn-ghost" id="spec-docs-open-master">📄 Open Master Spec</button>
      <button class="btn-ghost" id="spec-docs-open-philosophy" style="margin-left:0.4rem">📖 Open Philosophy doctrine</button>
      <button class="btn-ghost" id="spec-docs-open-all" style="margin-left:0.4rem">All scorecards →</button>
    </section>
  `;
}

function renderDiagnosticsSection(d) {
  const fmtAgo = (sec) => {
    if (sec == null) return '—';
    if (sec < 60) return `${sec}s ago`;
    if (sec < 3600) return `${Math.round(sec / 60)}m ago`;
    if (sec < 86400) return `${Math.round(sec / 3600)}h ago`;
    return `${Math.round(sec / 86400)}d ago`;
  };
  const fmtBytes = (n) => {
    if (n == null) return '—';
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`;
    if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MiB`;
    return `${(n / 1024 / 1024 / 1024).toFixed(2)} GiB`;
  };
  const now = new Date();
  const since = (iso) => {
    if (!iso) return null;
    const t = new Date(iso);
    return Math.max(0, Math.round((now - t) / 1000));
  };

  // Provider health rows. Color-coded by consecutive_failures + last_success age.
  const providers = (d.providers || []).slice().sort((a, b) => a.provider.localeCompare(b.provider));
  const provRows = providers.map(p => {
    const successAgo = p.lastSuccessAt ? since(p.lastSuccessAt) : null;
    let tone = 'dim';
    let pill = 'never';
    if (p.consecutiveFailures >= 3) { tone = 'loss'; pill = `❌ ${p.consecutiveFailures}× fail`; }
    else if (p.consecutiveFailures > 0) { tone = 'amber-text'; pill = `⚠ ${p.consecutiveFailures}× recent fail`; }
    else if (successAgo != null) { tone = 'gain'; pill = `✓ ok ${fmtAgo(successAgo)}`; }
    const lastErr = p.lastError ? `<div class="diag-err">${escapeHTML(p.lastError)}</div>` : '';
    return `
      <tr>
        <td><strong>${escapeHTML(p.provider)}</strong></td>
        <td><span class="${tone}">${pill}</span></td>
        <td class="num dim">${p.successCount}</td>
        <td class="num ${p.failureCount > 0 ? 'amber-text' : 'dim'}">${p.failureCount}</td>
        <td class="dim" style="font-size:0.78rem">${p.lastSuccessAt ? new Date(p.lastSuccessAt).toLocaleString() : '—'}${lastErr}</td>
      </tr>
    `;
  }).join('') || `<tr><td colspan="5" class="dim" style="text-align:center">No provider calls recorded yet. Trigger a refresh.</td></tr>`;

  // API keys table.
  const keyRows = (d.apiKeys || []).map(k => `
    <tr>
      <td><code style="font-family:var(--font-mono)">${escapeHTML(k.key)}</code></td>
      <td>${k.set ? '<span class="gain">✓ set</span>' : '<span class="dim">— missing</span>'}</td>
    </tr>
  `).join('');

  // System / backups / migrations.
  const sys = d.system || {};
  const backups = d.backups || [];
  const backupRow = backups[0]
    ? `${escapeHTML(backups[0].name)} <span class="dim">(${fmtBytes(backups[0].sizeBytes)}, ${backups[0].ageHours}h ago)</span>`
    : '<span class="dim">no backups found in /var/backups/ft</span>';
  const refreshLine = sys.lastRefreshAt
    ? `${new Date(sys.lastRefreshAt).toLocaleString()} <span class="dim">(${fmtAgo(sys.lastRefreshAgoSec)})</span>`
    : '<span class="dim">never</span>';
  const dailyLine = sys.lastDailyJobAt
    ? `${new Date(sys.lastDailyJobAt).toLocaleString()} <span class="dim">(${fmtAgo(sys.lastDailyJobAgoSec)})</span>`
    : '<span class="dim">never (runs at 04:00 UTC daily)</span>';
  const failLine = sys.lastPartialFailureAt
    ? `<span class="amber-text">⚠ ${new Date(sys.lastPartialFailureAt).toLocaleString()}</span>`
    : '<span class="dim">none</span>';

  // Frameworks.
  const fws = d.frameworks || [];
  const fwLine = fws.length === 0
    ? '<span class="loss">⚠ no frameworks loaded</span>'
    : fws.map(f => `<span class="diag-chip">${escapeHTML(f.id)} (${f.questions}Q, ${f.appliesTo})</span>`).join(' ');

  // Holidays.
  const hols = d.holidays || [];
  const holLine = hols.map(h => {
    const tone = h.count === 0 ? 'loss' : 'dim';
    return `<span class="diag-chip ${tone}">${escapeHTML(h.exchange)}: ${h.count}</span>`;
  }).join(' ');
  const missing = hols.filter(h => h.count === 0);
  const holWarn = missing.length > 0
    ? `<div class="diag-warn">⚠ ${missing.length} exchange${missing.length === 1 ? '' : 's'} have no holidays defined for ${hols[0]?.year}. Refresh JSON files in <code>internal/marketdata/holidays/</code>.</div>`
    : '';

  return `
    <section class="settings-block">
      <h3 class="settings-h3" style="display:flex; justify-content:space-between; align-items:center">
        <span>Diagnostics</span>
        <button class="btn-ghost" id="diag-refresh-btn" title="Reload diagnostics">↻</button>
      </h3>

      <div class="diag-grid">
        <div class="diag-card">
          <h4>System</h4>
          <ul class="diag-kv">
            <li><span class="dim">Last refresh:</span> ${refreshLine}</li>
            <li><span class="dim">Last daily job:</span> ${dailyLine}</li>
            <li><span class="dim">Last partial failure:</span> ${failLine}</li>
            <li><span class="dim">Latest migration:</span> ${escapeHTML(sys.latestMigration || '—')} ${sys.latestMigrationAt ? `<span class="dim">(${new Date(sys.latestMigrationAt).toLocaleDateString()})</span>` : ''}</li>
            <li><span class="dim">DB size:</span> ${fmtBytes(sys.dbSizeBytes)} <span class="dim">(${escapeHTML(sys.dbPath || '')})</span></li>
            <li><span class="dim">Latest backup:</span> ${backupRow}</li>
          </ul>
        </div>

        <div class="diag-card">
          <h4>API keys</h4>
          <table class="diag-table">
            <tbody>${keyRows}</tbody>
          </table>
        </div>
      </div>

      <h4 class="diag-h4">Provider health</h4>
      <div class="tablewrap"><table class="holdings"><thead><tr>
        <th>Provider</th><th>Status</th><th class="num">OK</th><th class="num">Fail</th><th>Last success / error</th>
      </tr></thead><tbody>${provRows}</tbody></table></div>

      <h4 class="diag-h4">Frameworks</h4>
      <div class="diag-line">${fwLine}</div>

      <h4 class="diag-h4">Exchange holidays <span class="dim" style="font-size:0.78rem; font-weight:normal">(current year)</span></h4>
      <div class="diag-line">${holLine}</div>
      ${holWarn}
    </section>
  `;
}

// Spec 10 D9 — aggregated Tax Lots section.
function renderTaxLotsSection(lots) {
  if (lots.length === 0) return '';
  const rows = lots.map((l) => {
    const longTerm = l.holdingDays >= 365;
    return `
      <tr>
        <td><strong>${escapeHTML(l.ticker)}</strong> <span class="dim">${escapeHTML(l.kind)}</span></td>
        <td class="dim">${escapeHTML(new Date(l.openedAt).toLocaleDateString())}</td>
        <td class="num">${l.holdingDays}d ${longTerm ? '<span class="gain" style="font-size:0.7rem">LT</span>' : ''}</td>
        <td class="num">${fmtNum6.format(l.quantityOpen)}</td>
        <td class="num">$${fmtNum2.format(l.pricePerUnit)}</td>
        <td class="num">$${fmtNum2.format(l.currentPrice || 0)}</td>
        <td class="num ${l.unrealizedPnlUsd > 0 ? 'gain' : l.unrealizedPnlUsd < 0 ? 'loss' : ''}">${l.unrealizedPnlUsd >= 0 ? '+' : '-'}$${fmtNum2.format(Math.abs(l.unrealizedPnlUsd || 0))}</td>
      </tr>
    `;
  }).join('');
  return `
    <section class="settings-block">
      <h3 class="settings-h3" style="display:flex; justify-content:space-between; align-items:center">
        <span>Tax lots <span class="dim" style="font-size:0.78rem; font-weight:normal">(FIFO; ${lots.length} open; "LT" = held ≥365 days)</span></span>
        <button class="btn-ghost" id="txn-import-btn">Import historical CSV…</button>
      </h3>
      <div class="tablewrap"><table class="holdings"><thead><tr>
        <th>Ticker</th><th>Opened</th><th class="num">Held</th>
        <th class="num">Qty open</th><th class="num">Cost / unit</th>
        <th class="num">Current</th><th class="num">Unrealized</th>
      </tr></thead><tbody>${rows}</tbody></table></div>
    </section>
  `;
}

// Spec 10 D10 — Import historical transactions CSV.
function openTxnImportModal() {
  closeImportModal();
  const root = document.createElement('div');
  root.id = 'modal-root';
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">Import historical transactions</div>
            <div class="desc">CSV with header: ticker, holding_kind, executed_at, txn_type, quantity, price_usd, [fees_usd, venue, note]</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <p class="dim" style="font-size:0.85rem">
            Rows matched to existing holdings by <strong>ticker</strong> (case-insensitive).
            Rows with no matching holding are skipped. Re-importing the same file
            duplicates rows — use this once per data source.
          </p>
          <input id="ti-file" type="file" accept=".csv" />
          <div class="error" id="ti-err"></div>
          <div id="ti-result" style="margin-top:0.8rem; font-size:0.85rem"></div>
        </div>
        <div class="modal-foot">
          <button class="btn-secondary" id="ti-cancel">Close</button>
          <button class="btn-primary" id="ti-upload">Upload</button>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#ti-cancel').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
  $('#ti-upload').addEventListener('click', async () => {
    const err = $('#ti-err'); err.textContent = '';
    const result = $('#ti-result'); result.innerHTML = '';
    const file = $('#ti-file').files[0];
    if (!file) { err.textContent = 'pick a file first'; return; }
    const fd = new FormData();
    fd.append('file', file);
    try {
      const resp = await fetch('/api/transactions/import', { method: 'POST', credentials: 'same-origin', body: fd });
      if (!resp.ok) {
        const text = await resp.text();
        err.textContent = `HTTP ${resp.status}: ${text}`;
        return;
      }
      const d = await resp.json();
      const s = d.summary || {};
      result.innerHTML = `
        <div class="gain">✓ Imported ${s.imported} of ${s.total} rows</div>
        ${s.skipped ? `<div class="amber-text">↪ Skipped ${s.skipped} (no matching holding)</div>` : ''}
        ${s.errored ? `<div class="loss">✗ Errored ${s.errored}</div>` : ''}
        <details style="margin-top:0.5rem">
          <summary class="dim" style="cursor:pointer">Show per-row results</summary>
          <pre style="max-height:240px; overflow:auto; font-size:0.75rem">${escapeHTML(JSON.stringify(d.rows, null, 2))}</pre>
        </details>
      `;
    } catch (e) {
      err.textContent = e.message;
    }
  });
}

// Spec 9c.1 D7 — LLM Spend dashboard section. Renders monthly + daily
// progress bars, per-feature + per-model breakdown, status pills, and
// action buttons (Adjust budgets / Emergency override / Pause toggle /
// View log).
function renderLLMSpendSection(s) {
  const monthly = s.caps.effectiveMonthly || s.caps.monthlyUsd || 5;
  const daily = s.caps.dailyUsd || 0.5;
  const monthPct = monthly > 0 ? Math.min(100, (s.month / monthly) * 100) : 0;
  const dayPct = daily > 0 ? Math.min(100, (s.today / daily) * 100) : 0;
  const monthTone = monthPct >= 90 ? 'risk-bar-over' : 'risk-bar-ok';
  const dayTone = dayPct >= 90 ? 'risk-bar-over' : 'risk-bar-ok';

  const pauseStatus = s.caps.globallyPaused
    ? '<span class="loss">⏸ PAUSED</span>'
    : '<span class="gain">🟢 ACTIVE</span>';
  const hardStop = s.caps.hardStopEnabled
    ? '<span class="gain">Hard stop on</span>'
    : '<span class="amber-text">Hard stop OFF</span>';

  const overrideStr = s.override && s.override.extraUsd > 0
    ? `<div class="dim" style="font-size:0.78rem">Override: +$${fmtNum2.format(s.override.extraUsd)} until ${escapeHTML(s.override.until)} — "${escapeHTML(s.override.reason || '')}"</div>`
    : '';

  const featureRows = Object.entries(s.byFeature || {}).sort((a, b) => b[1] - a[1])
    .map(([f, v]) => `<li><span>${escapeHTML(f)}</span><span class="num">$${fmtNum2.format(v)}</span></li>`).join('');
  const modelRows = Object.entries(s.byModel || {}).sort((a, b) => b[1] - a[1])
    .map(([m, v]) => `<li><span>${escapeHTML(m)}</span><span class="num">$${fmtNum2.format(v)}</span></li>`).join('');

  // Tiny 30-day sparkline (vertical bars). Each day = max(daily total, 0).
  const daily30 = (s.daily || []).slice(0, 30).reverse(); // oldest-first
  const maxDay = daily30.reduce((m, d) => Math.max(m, d.totalCostUsd || 0), 0.01);
  const sparkBars = daily30.map((d) => {
    const h = Math.max(1, ((d.totalCostUsd || 0) / maxDay) * 30);
    return `<div class="llm-spark-bar" title="${escapeHTML(d.date)}: $${fmtNum2.format(d.totalCostUsd || 0)}" style="height:${h}px"></div>`;
  }).join('');

  return `
    <section class="settings-block">
      <h3 class="settings-h3">LLM spend <span class="dim" style="font-size:0.78rem; font-weight:normal">(Spec 9c.1 — preventive)</span></h3>

      <div class="risk-summary">
        <div class="risk-stat">
          <div class="risk-stat-label">This month</div>
          <div class="risk-stat-value num">$${fmtNum2.format(s.month)} of $${fmtNum2.format(monthly)}</div>
          <div class="risk-bar-track" style="margin-top:0.3rem">
            <div class="risk-bar ${monthTone}" style="width:${monthPct.toFixed(1)}%"></div>
          </div>
          <div class="risk-stat-sub dim">${monthPct.toFixed(0)}% · ${s.counts.month || 0} calls${s.counts.blocked ? ` · ${s.counts.blocked} blocked` : ''}</div>
        </div>
        <div class="risk-stat">
          <div class="risk-stat-label">Today</div>
          <div class="risk-stat-value num">$${fmtNum2.format(s.today)} of $${fmtNum2.format(daily)}</div>
          <div class="risk-bar-track" style="margin-top:0.3rem">
            <div class="risk-bar ${dayTone}" style="width:${dayPct.toFixed(1)}%"></div>
          </div>
          <div class="risk-stat-sub dim">${dayPct.toFixed(0)}%</div>
        </div>
        <div class="risk-stat">
          <div class="risk-stat-label">Status</div>
          <div class="risk-stat-value">${pauseStatus}</div>
          <div class="risk-stat-sub">${hardStop} · default ${escapeHTML(s.caps.defaultModel || '—')}</div>
        </div>
        <div class="risk-stat">
          <div class="risk-stat-label">Last 30 days</div>
          <div class="llm-spark">${sparkBars || '<span class="dim">no data yet</span>'}</div>
        </div>
      </div>

      ${overrideStr}

      <div class="llm-grid" style="margin-top:0.8rem">
        <div>
          <h4 class="rh-side">By feature (month)</h4>
          <ul class="llm-breakdown">${featureRows || '<li class="dim">No calls yet.</li>'}</ul>
        </div>
        <div>
          <h4 class="rh-side">By model (month)</h4>
          <ul class="llm-breakdown">${modelRows || '<li class="dim">No calls yet.</li>'}</ul>
        </div>
      </div>

      <div style="margin-top:0.8rem; display:flex; gap:0.4rem; flex-wrap:wrap">
        <button class="btn-ghost" id="llm-adjust-btn">Adjust budgets</button>
        <button class="btn-ghost" id="llm-override-btn">Emergency override</button>
        ${s.caps.globallyPaused
          ? '<button class="btn-ghost" id="llm-pause-btn">▶ Resume LLM features</button>'
          : '<button class="btn-ghost" id="llm-pause-btn">⏸ Pause LLM features</button>'}
        ${s.override && s.override.extraUsd > 0 ? '<button class="btn-ghost" id="llm-override-clear-btn">Clear override</button>' : ''}
      </div>

      <h4 class="rh-side" style="margin-top:1rem">Per-feature kill switches (Spec 9c.1 D13)</h4>
      <div class="check-col" id="llm-feature-toggles" style="font-size:0.85rem">
        <label><input type="checkbox" data-llm-feature="sunday_digest" /> Sunday digest summarisation</label>
        <label><input type="checkbox" data-llm-feature="rescoring" /> Framework re-scoring on news</label>
        <label><input type="checkbox" data-llm-feature="alert_text" /> LLM-generated alert text</label>
        <label><input type="checkbox" data-llm-feature="jarvis_query" /> Jarvis natural-language queries</label>
      </div>
    </section>
  `;
}

// Spec 9c.1 D8 — Adjust Budgets modal.
async function openLLMBudgetModal() {
  // Read current values from preferences (one round-trip each — could
  // batch later if perf matters; for now: 5 calls, ~50ms).
  const keys = ['llm_budget_monthly_usd', 'llm_budget_daily_usd', 'llm_default_model',
                'llm_alert_threshold_50_pct', 'llm_alert_threshold_75_pct',
                'llm_alert_threshold_90_pct', 'llm_alert_threshold_100_pct'];
  const values = {};
  for (const k of keys) {
    try { const r = await api('/api/preferences/' + k); values[k] = r.value; }
    catch (_) { values[k] = ''; }
  }
  closeImportModal();
  const root = document.createElement('div');
  root.id = 'modal-root';
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">Adjust LLM budgets</div>
            <div class="desc">Hard caps the system cannot exceed.</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <form id="lb-form" class="holding-form">
            <div class="form-row">
              <label for="lb-monthly">Monthly cap ($)</label>
              <input id="lb-monthly" type="number" step="0.10" min="0.10" value="${escapeHTML(values.llm_budget_monthly_usd || '5.00')}" />
            </div>
            <div class="form-row">
              <label for="lb-daily">Daily cap ($)</label>
              <input id="lb-daily" type="number" step="0.10" min="0.05" value="${escapeHTML(values.llm_budget_daily_usd || '0.50')}" />
            </div>
            <div class="form-row">
              <label for="lb-model">Default model</label>
              <select id="lb-model">
                ${['claude-haiku-4-5','claude-sonnet-4-6','claude-opus-4-7'].map(m =>
                  `<option value="${m}" ${m === (values.llm_default_model || 'claude-haiku-4-5') ? 'selected' : ''}>${m}</option>`).join('')}
              </select>
            </div>
            <div class="form-row">
              <label>Alert thresholds (Telegram)</label>
              <div class="check-col">
                <label><input type="checkbox" id="lb-t50" ${values.llm_alert_threshold_50_pct !== 'false' ? 'checked' : ''} /> 50%</label>
                <label><input type="checkbox" id="lb-t75" ${values.llm_alert_threshold_75_pct !== 'false' ? 'checked' : ''} /> 75%</label>
                <label><input type="checkbox" id="lb-t90" ${values.llm_alert_threshold_90_pct !== 'false' ? 'checked' : ''} /> 90%</label>
                <label><input type="checkbox" id="lb-t100" ${values.llm_alert_threshold_100_pct !== 'false' ? 'checked' : ''} /> 100%</label>
              </div>
            </div>
            <div class="error" id="lb-err"></div>
          </form>
        </div>
        <div class="modal-foot">
          <button class="btn-secondary" id="lb-cancel">Cancel</button>
          <button class="btn-primary" id="lb-save">Save</button>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#lb-cancel').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
  $('#lb-save').addEventListener('click', async () => {
    const err = $('#lb-err'); err.textContent = '';
    const payload = [
      ['llm_budget_monthly_usd',       $('#lb-monthly').value],
      ['llm_budget_daily_usd',         $('#lb-daily').value],
      ['llm_default_model',            $('#lb-model').value],
      ['llm_alert_threshold_50_pct',   $('#lb-t50').checked ? 'true' : 'false'],
      ['llm_alert_threshold_75_pct',   $('#lb-t75').checked ? 'true' : 'false'],
      ['llm_alert_threshold_90_pct',   $('#lb-t90').checked ? 'true' : 'false'],
      ['llm_alert_threshold_100_pct',  $('#lb-t100').checked ? 'true' : 'false'],
    ];
    try {
      for (const [k, v] of payload) {
        await api('/api/preferences/' + k, { method: 'PUT', body: JSON.stringify({ value: String(v) }) });
      }
      closeImportModal();
      renderSettings();
    } catch (e) {
      err.textContent = e.message;
    }
  });
}

// Spec 9c.1 D9 — Emergency override modal.
function openLLMOverrideModal() {
  closeImportModal();
  const root = document.createElement('div');
  root.id = 'modal-root';
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">Emergency override</div>
            <div class="desc">⚠ Temporarily raises the monthly cap. Time-bound + audited.</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <form id="lo-form" class="holding-form">
            <div class="form-row">
              <label for="lo-cap">Extra budget ($)</label>
              <input id="lo-cap" type="number" step="0.10" min="0.10" value="5.00" />
            </div>
            <div class="form-row">
              <label for="lo-hours">Active for (hours, max 168)</label>
              <input id="lo-hours" type="number" step="1" min="1" max="168" value="24" />
            </div>
            <div class="form-row">
              <label for="lo-reason">Reason (required)</label>
              <input id="lo-reason" type="text" placeholder="e.g. one-off thesis analysis on VST" required />
            </div>
            <div class="error" id="lo-err"></div>
          </form>
        </div>
        <div class="modal-foot">
          <button class="btn-secondary" id="lo-cancel">Cancel</button>
          <button class="btn-primary" id="lo-save">Activate override</button>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#lo-cancel').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
  $('#lo-save').addEventListener('click', async () => {
    const err = $('#lo-err'); err.textContent = '';
    const capUsd = parseFloat($('#lo-cap').value);
    const durationHours = parseInt($('#lo-hours').value, 10);
    const reason = $('#lo-reason').value.trim();
    if (!Number.isFinite(capUsd) || capUsd <= 0) { err.textContent = 'capUsd must be > 0'; return; }
    if (!Number.isFinite(durationHours) || durationHours <= 0 || durationHours > 168) {
      err.textContent = 'durationHours 1..168'; return;
    }
    if (reason.length < 3) { err.textContent = 'reason required (min 3 chars)'; return; }
    try {
      await api('/api/llm/override', { method: 'POST', body: JSON.stringify({ capUsd, durationHours, reason }) });
      closeImportModal();
      renderSettings();
    } catch (e) {
      err.textContent = e.message;
    }
  });
}

async function renderSettings() {
  const content = $('#content');
  content.innerHTML = '<div class="empty">loading…</div>';

  const [delStocks, delCrypto, audit, regimeHist, risk, llmSpend, stocksForLots, cryptoForLots, diag] = await Promise.all([
    api('/api/holdings/stocks/deleted').catch(() => ({ holdings: [] })),
    api('/api/holdings/crypto/deleted').catch(() => ({ holdings: [] })),
    api('/api/audit?limit=100').catch(() => ({ audit: [] })),
    api('/api/regime/history?limit=50').catch(() => ({ history: [] })),
    api('/api/risk/dashboard').catch(() => null),
    api('/api/llm/spend').catch(() => null),
    api('/api/holdings/stocks').catch(() => ({ holdings: [] })),
    api('/api/holdings/crypto').catch(() => ({ holdings: [] })),
    // Spec 7 — diagnostics payload.
    api('/api/diagnostics').catch(() => null),
  ]);

  // Spec 10 D9 — gather all open tax lots across all holdings for the
  // Tax Lots section. Sequential because we want them in one ordered
  // list — could parallelise but list is small.
  const allLots = [];
  for (const h of (stocksForLots.holdings || [])) {
    try {
      const r = await api(`/api/holdings/stocks/${h.id}/taxlots`);
      for (const lot of (r.position?.taxLots || [])) {
        allLots.push({ ...lot, ticker: h.ticker || h.name, kind: 'stock', currentPrice: r.currentPrice });
      }
    } catch (_) { /* skip individual failures */ }
  }
  for (const h of (cryptoForLots.holdings || [])) {
    try {
      const r = await api(`/api/holdings/crypto/${h.id}/taxlots`);
      for (const lot of (r.position?.taxLots || [])) {
        allLots.push({ ...lot, ticker: h.symbol, kind: 'crypto', currentPrice: r.currentPrice });
      }
    } catch (_) { /* skip */ }
  }
  allLots.sort((a, b) => new Date(a.openedAt) - new Date(b.openedAt));
  const taxLotsHTML = renderTaxLotsSection(allLots);

  // Spec 9c D16 — Portfolio Risk Dashboard. Renders first so it's the
  // most prominent thing on the Settings page (above regime history).
  const portfolioRiskHTML = risk ? renderPortfolioRiskSection(risk) : '';
  // Spec 9c.1 D7 — LLM Spend Dashboard. Renders between portfolio risk
  // and regime history.
  const llmSpendHTML = llmSpend ? renderLLMSpendSection(llmSpend) : '';

  // Regime history table — Spec 9b D13. Two columns (Jordi / Cowen).
  const hist = regimeHist.history || [];
  const jordi = hist.filter(h => h.frameworkId === 'jordi');
  const cowen = hist.filter(h => h.frameworkId === 'cowen');
  const fmtRegimeRow = (h) => {
    const r = (h.regime || 'unclassified').toUpperCase();
    const tone = h.regime === 'stable' ? 'gain' : h.regime === 'shifting' ? 'amber-text' : h.regime === 'defensive' ? 'loss' : 'dim';
    const when = new Date(h.ts).toLocaleDateString();
    const src = h.source === 'auto_cowen_form' ? 'auto' : 'manual';
    let extra = '';
    if (h.source === 'auto_cowen_form' && h.inputsJson) {
      try {
        const inp = JSON.parse(h.inputsJson);
        extra = ` <span class="dim">— phase ${inp.cycle_phase} · risk ${(inp.risk_indicator ?? 0).toFixed(2)}</span>`;
      } catch (_) { /* */ }
    }
    return `<li>${escapeHTML(when)} → <span class="${tone}">${escapeHTML(r)}</span> <span class="dim">(${src})</span>${extra}</li>`;
  };
  const regimeHistoryHTML = `
    <section class="settings-block">
      <h3 class="settings-h3">Regime history <span class="dim" style="font-size:0.78rem; font-weight:normal">(latest 50 per side)</span></h3>
      <div class="regime-history-grid">
        <div>
          <h4 class="rh-side">Jordi (stocks)</h4>
          <ul class="rh-list">${jordi.map(fmtRegimeRow).join('') || '<li class="dim">No history yet.</li>'}</ul>
        </div>
        <div>
          <h4 class="rh-side">Cowen (crypto)</h4>
          <ul class="rh-list">${cowen.map(fmtRegimeRow).join('') || '<li class="dim">No history yet.</li>'}</ul>
        </div>
      </div>
    </section>
  `;

  const delStocksRows = (delStocks.holdings || []).map((h) => `
    <tr>
      <td>${escapeHTML(h.name)}</td>
      <td class="ticker">${escapeHTML(h.ticker || '—')}</td>
      <td class="num">${fmtUSD.format(h.investedUsd || 0)}</td>
      <td class="dim">${escapeHTML(h.deletedAt || '')}</td>
      <td><button class="btn-ghost" data-restore-kind="stock" data-restore-id="${h.id}">Restore</button></td>
    </tr>
  `).join('') || `<tr><td colspan="5" class="dim" style="text-align:center; padding:0.7rem">No deleted stock holdings.</td></tr>`;

  const delCryptoRows = (delCrypto.holdings || []).map((h) => `
    <tr>
      <td>${escapeHTML(h.name)} <span class="ticker">${escapeHTML(h.symbol)}</span></td>
      <td>${escapeHTML(h.classification)}</td>
      <td class="num">${fmtNum6.format((h.quantityHeld || 0) + (h.quantityStaked || 0))}</td>
      <td class="dim">${escapeHTML(h.deletedAt || '')}</td>
      <td><button class="btn-ghost" data-restore-kind="crypto" data-restore-id="${h.id}">Restore</button></td>
    </tr>
  `).join('') || `<tr><td colspan="5" class="dim" style="text-align:center; padding:0.7rem">No deleted crypto holdings.</td></tr>`;

  const auditRows = (audit.audit || []).map((a) => {
    const tickerOrSymbol = a.ticker || a.symbol || '—';
    let changesPreview = '';
    try {
      const c = JSON.parse(a.changes || '{}');
      if (a.action === 'update') {
        const keys = Object.keys(c).slice(0, 3);
        changesPreview = keys.map((k) => `${k}`).join(', ');
        if (Object.keys(c).length > 3) changesPreview += ` +${Object.keys(c).length - 3}`;
      } else if (a.action === 'create') {
        changesPreview = 'created';
      }
    } catch (_) { /* keep empty */ }
    // Spec 12 D9 — surface typed reasonCode alongside the free-text reason.
    const reasonBlob = [a.reasonCode ? `[${a.reasonCode}]` : '', a.reason || '']
      .filter(Boolean).join(' ');
    return `
      <tr>
        <td class="num dim">${escapeHTML(new Date(a.ts).toLocaleString())}</td>
        <td>${escapeHTML(a.action)}</td>
        <td>${escapeHTML(a.holdingKind)}</td>
        <td class="ticker">${escapeHTML(tickerOrSymbol)}</td>
        <td class="dim" style="font-size:0.78rem">${escapeHTML(changesPreview)}</td>
        <td class="dim" style="font-size:0.78rem">${escapeHTML(reasonBlob)}</td>
      </tr>
    `;
  }).join('') || `<tr><td colspan="6" class="dim" style="text-align:center; padding:0.7rem">No audit entries yet. Audit log fills as you create / edit / delete / restore holdings.</td></tr>`;

  // Spec 7 — diagnostics panel. Conditionally rendered if endpoint responded.
  const diagnosticsHTML = diag ? renderDiagnosticsSection(diag) : '';

  // Spec 8 / Master Spec — link to the living-doc scorecard.
  const specDocsHTML = renderSpecDocsSection();

  content.innerHTML = `
    <h2 class="settings-h">Settings</h2>

    ${specDocsHTML}
    ${portfolioRiskHTML}
    ${llmSpendHTML}
    ${diagnosticsHTML}
    ${taxLotsHTML}
    ${regimeHistoryHTML}

    <section class="settings-block">
      <h3 class="settings-h3">Deleted stock holdings</h3>
      <div class="tablewrap"><table class="holdings"><thead><tr>
        <th>Name</th><th>Ticker</th><th class="num">Invested $</th><th>Deleted</th><th></th>
      </tr></thead><tbody>${delStocksRows}</tbody></table></div>
    </section>

    <section class="settings-block">
      <h3 class="settings-h3">Deleted crypto holdings</h3>
      <div class="tablewrap"><table class="holdings"><thead><tr>
        <th>Name / Symbol</th><th>Class</th><th class="num">Qty</th><th>Deleted</th><th></th>
      </tr></thead><tbody>${delCryptoRows}</tbody></table></div>
    </section>

    <section class="settings-block">
      <h3 class="settings-h3">Audit log <span class="dim" style="font-size:0.78rem; font-weight:normal">(latest 100)</span></h3>
      <div class="tablewrap"><table class="holdings"><thead><tr>
        <th>When</th><th>Action</th><th>Kind</th><th>Ticker / Sym</th><th>Changed</th><th>Reason</th>
      </tr></thead><tbody>${auditRows}</tbody></table></div>
    </section>
  `;

  // Wire restore buttons
  for (const btn of document.querySelectorAll('[data-restore-id]')) {
    btn.addEventListener('click', async () => {
      const kind = btn.dataset.restoreKind;
      const id = btn.dataset.restoreId;
      const path = kind === 'stock' ? '/api/holdings/stocks' : '/api/holdings/crypto';
      try {
        await api(`${path}/${id}/restore`, { method: 'POST' });
        if (kind === 'stock') state.stocks = null; else state.crypto = null;
        state.summary = null;
        renderSettings();
      } catch (e) {
        alert('Restore failed: ' + e.message);
      }
    });
  }

  // Spec 9c — "Run snapshot now" button.
  const snapBtn = document.querySelector('#risk-snapshot-btn');
  if (snapBtn) {
    snapBtn.addEventListener('click', async () => {
      snapBtn.disabled = true;
      snapBtn.textContent = 'snapshotting…';
      try {
        await api('/api/risk/snapshot', { method: 'POST' });
        renderSettings();
      } catch (e) {
        alert('Snapshot failed: ' + e.message);
        snapBtn.disabled = false;
        snapBtn.textContent = 'Run snapshot now';
      }
    });
  }

  // Spec 10 D10 — Import historical transactions from CSV.
  document.querySelector('#txn-import-btn')?.addEventListener('click', openTxnImportModal);

  // Spec 7 — Diagnostics manual refresh.
  document.querySelector('#diag-refresh-btn')?.addEventListener('click', () => renderSettings());

  // Spec 8 / Master Spec — jump into the Scorecards tab with the right doc.
  document.querySelector('#spec-docs-open-master')?.addEventListener('click', () => {
    state.selectedScorecard = 'master-spec';
    switchTab('scorecards');
  });
  document.querySelector('#spec-docs-open-philosophy')?.addEventListener('click', () => {
    state.selectedScorecard = 'philosophy';
    switchTab('scorecards');
  });
  document.querySelector('#spec-docs-open-all')?.addEventListener('click', () => {
    state.selectedScorecard = null;
    switchTab('scorecards');
  });

  // Spec 9c.1 — LLM Spend section action buttons.
  document.querySelector('#llm-adjust-btn')?.addEventListener('click', openLLMBudgetModal);
  document.querySelector('#llm-override-btn')?.addEventListener('click', openLLMOverrideModal);
  document.querySelector('#llm-pause-btn')?.addEventListener('click', async () => {
    const cur = (llmSpend && llmSpend.caps && llmSpend.caps.globallyPaused) || false;
    try {
      await api('/api/llm/pause', { method: 'POST', body: JSON.stringify({ paused: !cur }) });
      renderSettings();
    } catch (e) { alert('Pause toggle failed: ' + e.message); }
  });
  document.querySelector('#llm-override-clear-btn')?.addEventListener('click', async () => {
    if (!confirm('Clear active LLM override?')) return;
    try {
      await api('/api/llm/override/clear', { method: 'POST' });
      renderSettings();
    } catch (e) { alert('Clear failed: ' + e.message); }
  });
  // Spec 9c.1 D13 — per-feature kill switches. Read current state into
  // checkboxes, then wire change handlers that PUT to user_preferences.
  document.querySelectorAll('[data-llm-feature]').forEach(async (cb) => {
    const feature = cb.dataset.llmFeature;
    const key = `llm_feature_${feature}`;
    try {
      const r = await api('/api/preferences/' + key);
      cb.checked = r.value !== 'false'; // default on; 'false' switches off
    } catch (_) { cb.checked = true; }
    cb.addEventListener('change', async () => {
      try {
        await api('/api/preferences/' + key, {
          method: 'PUT',
          body: JSON.stringify({ value: cb.checked ? 'true' : 'false' }),
        });
      } catch (e) {
        alert(`${feature} toggle failed: ${e.message}`);
        cb.checked = !cb.checked;
      }
    });
  });
}

// ---------- boot ----------------------------------------------------------

async function boot() {
  try {
    const s = await api('/api/auth/state');
    if (s.state === 'needs_setup') {
      renderSetup();
    } else if (s.state === 'authenticated') {
      renderDashboard(s.user);
    } else {
      renderLogin();
    }
  } catch (err) {
    setScreen(`
      <div class="auth-screen">
        <div class="auth-card">
          <h1>FT</h1>
          <div class="error">Couldn't reach the server: ${escapeHTML(err.message)}</div>
        </div>
      </div>
    `);
  }
}

// ============================================================================
// Spec 4 — Watchlist + Framework Scoring
// ============================================================================

const FRAMEWORK_THRESHOLD = 12; // default; per-framework value is loaded on the score screen.

// Distance-to-entry classification for the watchlist table. Spec 9b D6:
// when `suppressed` is true, the "In range" pill is rendered with a strike
// indicator so Fin knows the regime is gating the alert.
function distanceToEntry(currentPrice, low, high, suppressed) {
  if (currentPrice == null) return { label: '—', cls: 'dim' };
  if (low == null && high == null) return { label: '—', cls: 'dim' };
  if (low != null && currentPrice < low) {
    const pct = ((low - currentPrice) / low) * 100;
    return { label: `${pct.toFixed(1)}% below`, cls: 'dist-below' };
  }
  if (high != null && currentPrice > high) {
    const pct = ((currentPrice - high) / high) * 100;
    return { label: `${pct.toFixed(1)}% above`, cls: 'dist-above' };
  }
  return suppressed
    ? { label: 'In range (suppressed)', cls: 'dist-suppressed', title: 'Regime ≠ STABLE — alert suppressed' }
    : { label: 'In range', cls: 'dist-in' };
}

// ---------- Screener tab (Spec 9b D9) -----------------------------------
//
// Pulls the existing S&P 500 sample dataset (with live overlay) from
// /api/screener and renders a filterable, sortable grid. "Add to watchlist"
// pre-fills the watchlist add modal.

const SCREENER_DEFAULT_SORT = { col: 'changePct', dir: 'desc' };

if (!state.screenerFilters) {
  state.screenerFilters = { sectors: [], mcapMin: '', mcapMax: '', changeMin: '', changeMax: '', held: '' };
  state.screenerSort = { ...SCREENER_DEFAULT_SORT };
  state.screenerRows = null;
}

async function renderScreener() {
  // Refetch when filters change; cache in state.screenerRows.
  const f = state.screenerFilters;
  const params = new URLSearchParams();
  if (f.sectors.length) params.set('sectors', f.sectors.join(','));
  if (f.mcapMin)        params.set('mcap_min',   f.mcapMin);
  if (f.mcapMax)        params.set('mcap_max',   f.mcapMax);
  if (f.changeMin)      params.set('change_min', f.changeMin);
  if (f.changeMax)      params.set('change_max', f.changeMax);
  if (f.held)           params.set('held',       f.held);

  let data;
  try {
    data = await api('/api/screener?' + params.toString());
  } catch (e) {
    $('#content').innerHTML = `<div class="empty"><div class="loss">screener failed: ${escapeHTML(e.message)}</div></div>`;
    return;
  }
  state.screenerRows = data.rows || [];

  // Sort client-side so re-sorting doesn't roundtrip.
  const { col, dir } = state.screenerSort;
  state.screenerRows.sort((a, b) => {
    const av = a[col]; const bv = b[col];
    if (av == null && bv == null) return 0;
    if (av == null) return 1;
    if (bv == null) return -1;
    if (typeof av === 'string') return dir === 'asc' ? av.localeCompare(bv) : bv.localeCompare(av);
    return dir === 'asc' ? av - bv : bv - av;
  });

  const sectorOpts = SECTORS.map(s => {
    const checked = f.sectors.includes(s) ? 'checked' : '';
    return `<label class="screener-sector-chip"><input type="checkbox" data-sector="${escapeHTML(s)}" ${checked}/> ${escapeHTML(s)}</label>`;
  }).join('');

  const heldOpts = [['','any'],['hide','hide held'],['only','only held']]
    .map(([v,l]) => `<option value="${v}" ${f.held===v?'selected':''}>${l}</option>`).join('');

  const rows = state.screenerRows.map(r => {
    const changeCls = r.changePct > 0 ? 'gain' : r.changePct < 0 ? 'loss' : '';
    const tagPills = [
      r.held ? '<span class="screener-tag held">HELD</span>' : '',
      r.onWatchlist ? '<span class="screener-tag wl">✓ WL</span>' : '',
    ].join('');
    return `
      <tr data-ticker="${escapeHTML(r.ticker)}">
        <td><strong>${escapeHTML(r.ticker)}</strong> ${tagPills}</td>
        <td>${escapeHTML(r.name)}</td>
        <td class="dim">${escapeHTML(r.sector)}</td>
        <td class="num">${r.price ? fmtNum2.format(r.price) : '<span class="dim">—</span>'}</td>
        <td class="num ${changeCls}">${r.changePct >= 0 ? '+' : ''}${fmtNum2.format(r.changePct)}%</td>
        <td class="num">${fmtNum2.format(r.marketCapB)}B</td>
        <td>${r.onWatchlist
          ? '<span class="dim">on watchlist</span>'
          : '<button class="btn-ghost" data-add="' + escapeHTML(r.ticker) + '">+ watchlist</button>'}</td>
      </tr>
    `;
  }).join('') || `<tr><td colspan="7" class="dim" style="text-align:center; padding:1rem">No matches.</td></tr>`;

  const sortIcon = (c) => state.screenerSort.col === c
    ? (state.screenerSort.dir === 'asc' ? ' ↑' : ' ↓') : '';

  $('#content').innerHTML = `
    <div class="screener-filters">
      <div class="screener-filter-row">
        <strong>Sectors</strong>
        <div class="screener-sectors">${sectorOpts}</div>
      </div>
      <div class="screener-filter-row">
        <strong>Market cap (B)</strong>
        <input type="number" id="sc-mcap-min" placeholder="min" value="${escapeHTML(f.mcapMin)}" />
        <input type="number" id="sc-mcap-max" placeholder="max" value="${escapeHTML(f.mcapMax)}" />
        <strong>Daily % range</strong>
        <input type="number" id="sc-chg-min" placeholder="min" step="0.1" value="${escapeHTML(f.changeMin)}" />
        <input type="number" id="sc-chg-max" placeholder="max" step="0.1" value="${escapeHTML(f.changeMax)}" />
        <strong>Held</strong>
        <select id="sc-held">${heldOpts}</select>
        <button class="btn-ghost" id="sc-reset">Reset</button>
      </div>
      <div class="screener-summary dim">${data.matched} of ${data.total} S&amp;P sample tickers</div>
    </div>
    <div class="tablewrap">
      <table class="holdings screener">
        <thead><tr>
          <th data-sort="ticker">Ticker${sortIcon('ticker')}</th>
          <th data-sort="name">Company${sortIcon('name')}</th>
          <th data-sort="sector">Sector${sortIcon('sector')}</th>
          <th class="num" data-sort="price">Price${sortIcon('price')}</th>
          <th class="num" data-sort="changePct">Today %${sortIcon('changePct')}</th>
          <th class="num" data-sort="marketCapB">Mkt Cap${sortIcon('marketCapB')}</th>
          <th></th>
        </tr></thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
  `;

  // Wire filter inputs (debounced on number fields).
  let debounce;
  const reload = () => { clearTimeout(debounce); debounce = setTimeout(renderScreener, 220); };
  document.querySelectorAll('.screener-sector-chip input').forEach(inp => {
    inp.addEventListener('change', () => {
      const sec = inp.dataset.sector;
      const idx = state.screenerFilters.sectors.indexOf(sec);
      if (inp.checked && idx < 0) state.screenerFilters.sectors.push(sec);
      if (!inp.checked && idx >= 0) state.screenerFilters.sectors.splice(idx, 1);
      reload();
    });
  });
  ['sc-mcap-min','sc-mcap-max','sc-chg-min','sc-chg-max'].forEach(id => {
    const el = $('#' + id);
    if (el) el.addEventListener('input', () => {
      state.screenerFilters.mcapMin = $('#sc-mcap-min').value;
      state.screenerFilters.mcapMax = $('#sc-mcap-max').value;
      state.screenerFilters.changeMin = $('#sc-chg-min').value;
      state.screenerFilters.changeMax = $('#sc-chg-max').value;
      reload();
    });
  });
  $('#sc-held').addEventListener('change', (ev) => {
    state.screenerFilters.held = ev.target.value;
    renderScreener();
  });
  $('#sc-reset').addEventListener('click', () => {
    state.screenerFilters = { sectors: [], mcapMin: '', mcapMax: '', changeMin: '', changeMax: '', held: '' };
    state.screenerSort = { ...SCREENER_DEFAULT_SORT };
    renderScreener();
  });

  // Header click → sort.
  document.querySelectorAll('th[data-sort]').forEach(th => {
    th.addEventListener('click', () => {
      const c = th.dataset.sort;
      if (state.screenerSort.col === c) {
        state.screenerSort.dir = state.screenerSort.dir === 'asc' ? 'desc' : 'asc';
      } else {
        state.screenerSort.col = c;
        state.screenerSort.dir = c === 'changePct' ? 'desc' : 'asc';
      }
      renderScreener();
    });
  });

  // "+ watchlist" buttons → open prefilled add modal.
  document.querySelectorAll('button[data-add]').forEach(btn => {
    btn.addEventListener('click', () => {
      const ticker = btn.dataset.add;
      const row = state.screenerRows.find(r => r.ticker === ticker);
      if (!row) return;
      openWatchlistModal({
        kind: 'stock',
        mode: 'add',
        entry: undefined,
        prefill: { ticker: row.ticker, companyName: row.name, sector: row.sector, currentPrice: row.price },
      });
    });
  });
}

// ---------- watchlist tab -----------------------------------------------

// Backlog polish 2026-05-17 — watchlist column sort state.
if (state.watchlistSort == null) state.watchlistSort = { key: 'addedAt', dir: 'desc' };

async function renderWatchlist() {
  const res = await api('/api/watchlist');
  state.watchlist = res.watchlist || [];
  // Spec 9f D6 — sector flow pill cache for watchlist rows.
  const sectorTagMap = await getSectorTagMap();

  if (state.watchlist.length === 0) {
    $('#content').innerHTML = `
      <div class="table-toolbar">
        <button class="btn-ghost" id="add-watchlist-stock">+ Add stock to watchlist</button>
        <button class="btn-ghost" id="add-watchlist-crypto">+ Add crypto to watchlist</button>
      </div>
      <div class="empty">
        <div>Watchlist is empty.</div>
        <div class="hint">Add tickers you're considering. Score them against the framework, then promote to a holding when ready.</div>
      </div>
    `;
    $('#add-watchlist-stock').addEventListener('click', () => openWatchlistModal({ kind: 'stock', mode: 'add' }));
    $('#add-watchlist-crypto').addEventListener('click', () => openWatchlistModal({ kind: 'crypto', mode: 'add' }));
    return;
  }

  // Backlog polish — sort rows per state.watchlistSort.
  const sortKey = state.watchlistSort.key;
  const sortDir = state.watchlistSort.dir;
  const sorted = state.watchlist.slice().sort((a, b) => {
    const cmp = compareWatchlistRows(a, b, sortKey);
    return sortDir === 'asc' ? cmp : -cmp;
  });

  const rows = sorted.map((e) => {
    const dist = distanceToEntry(e.currentPrice, e.targetEntryLow, e.targetEntryHigh, !!e.alertSuppressed);
    const target = e.targetEntryLow != null && e.targetEntryHigh != null
      ? `$${fmtNum2.format(e.targetEntryLow)}–$${fmtNum2.format(e.targetEntryHigh)}`
      : e.targetEntryLow != null ? `≥ $${fmtNum2.format(e.targetEntryLow)}`
      : e.targetEntryHigh != null ? `≤ $${fmtNum2.format(e.targetEntryHigh)}`
      : '—';
    const score = e.latestScore;
    const passed = score && score.totalScore >= (score.maxScore - 4); // default 12/16
    const scoreCell = score
      ? `<span class="score-badge ${passed ? 'pass' : 'fail'}">${score.totalScore}/${score.maxScore}${passed ? ' ✓' : ''}</span>`
      : `<span class="dim">unscored</span>`;
    const tag = score && score.tagsJson ? parseFirstTag(score.tagsJson) : null;
    const tagCell = tag ? `<span class="tag-pill">${escapeHTML(tag)}</span>` : `<span class="dim">—</span>`;
    const added = relativeAge(e.addedAt);
    const note = e.note
      ? `<span class="note-cell"><span class="note-bubble" data-note="${escapeHTML(e.note)}" tabindex="0" aria-label="Show note" title="Hover for full note">💬</span></span>`
      : '<span class="dim">—</span>';
    // Spec 12 D4a — analyst Bear/Base/Bull. Crypto rows always show "—".
    const fc = (v) => v != null ? `$${fmtNum2.format(v)}` : '<span class="dim">—</span>';
    const forecastCell = `
      <td class="num forecast-stack">
        <div class="forecast-row">
          <span class="forecast-label loss">Bear</span><span class="forecast-val">${fc(e.forecastLow)}</span>
        </div>
        <div class="forecast-row">
          <span class="forecast-label">Base</span><span class="forecast-val">${fc(e.forecastMean)}</span>
        </div>
        <div class="forecast-row">
          <span class="forecast-label gain">Bull</span><span class="forecast-val">${fc(e.forecastHigh)}</span>
        </div>
      </td>
    `;
    return `
      <tr data-wid="${e.id}">
        <td><strong>${escapeHTML(e.ticker)}</strong>${sectorFlowPill(e.sectorUniverseId, sectorTagMap)}</td>
        <td>${escapeHTML(e.companyName || '—')}</td>
        <td><span class="dim">${escapeHTML(e.sector || '—')}</span></td>
        <td class="num">${dash(e.currentPrice, fmtNum2)}</td>
        ${forecastCell}
        <td class="num">${target}</td>
        <td><span class="${dist.cls}"${dist.title ? ` title="${escapeHTML(dist.title)}"` : ''}>${dist.label}</span></td>
        <td>${scoreCell}</td>
        <td>${tagCell}</td>
        <td><span class="dim">${added}</span></td>
        <td>${note}</td>
        <td class="wl-actions">
          <button class="row-mini" data-act="score" data-wid="${e.id}" title="Score">⚖</button>
          <button class="row-mini" data-act="edit" data-wid="${e.id}" title="Edit">✎</button>
          <button class="row-mini" data-act="promote" data-wid="${e.id}" title="Promote to Holdings">▲</button>
          <button class="row-mini danger" data-act="delete" data-wid="${e.id}" title="Delete">×</button>
        </td>
      </tr>
    `;
  }).join('');

  // Sortable column header helper.
  const sortHeader = (key, label, extraClass) => {
    const active = sortKey === key;
    const arrow = active ? (sortDir === 'asc' ? ' ▲' : ' ▼') : '';
    const cls = ['wl-sortable'];
    if (active) cls.push('active');
    if (extraClass) cls.push(extraClass);
    return `<th class="${cls.join(' ')}" data-sort-key="${key}" title="Click to sort">${label}${arrow}</th>`;
  };

  $('#content').innerHTML = `
    <div class="table-toolbar">
      <button class="btn-ghost" id="add-watchlist-stock">+ Add stock</button>
      <button class="btn-ghost" id="add-watchlist-crypto">+ Add crypto</button>
    </div>
    <div class="tablewrap">
      <table class="holdings watchlist">
        <thead>
          <tr>
            ${sortHeader('ticker',      'Ticker')}
            ${sortHeader('companyName', 'Company')}
            ${sortHeader('sector',      'Sector')}
            ${sortHeader('currentPrice','Price', 'num')}
            <th class="num" title="Analyst Bear/Base/Bull price targets (Yahoo financialData). Crypto: not available.">Forecast</th>
            ${sortHeader('target',      'Target', 'num')}
            ${sortHeader('distance',    'Distance')}
            ${sortHeader('score',       'Score')}
            <th>Tag</th>
            ${sortHeader('addedAt',     'Added')}
            <th>Note</th>
            <th></th>
          </tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
  `;
  $('#add-watchlist-stock').addEventListener('click', () => openWatchlistModal({ kind: 'stock', mode: 'add' }));
  $('#add-watchlist-crypto').addEventListener('click', () => openWatchlistModal({ kind: 'crypto', mode: 'add' }));
  for (const btn of document.querySelectorAll('.row-mini')) {
    btn.addEventListener('click', () => onWatchlistAction(btn.dataset.act, parseInt(btn.dataset.wid, 10)));
  }
  // Wire column-header clicks.
  for (const th of document.querySelectorAll('.wl-sortable')) {
    th.addEventListener('click', () => {
      const k = th.dataset.sortKey;
      if (state.watchlistSort.key === k) {
        state.watchlistSort.dir = state.watchlistSort.dir === 'asc' ? 'desc' : 'asc';
      } else {
        state.watchlistSort = { key: k, dir: 'asc' };
      }
      renderWatchlist();
    });
  }
}

// compareWatchlistRows — typed comparator per column key.
function compareWatchlistRows(a, b, key) {
  const num = (v) => (v == null ? -Infinity : v);
  const str = (v) => (v == null ? '' : String(v).toLowerCase());
  switch (key) {
    case 'ticker':       return str(a.ticker).localeCompare(str(b.ticker));
    case 'companyName':  return str(a.companyName).localeCompare(str(b.companyName));
    case 'sector':       return str(a.sector).localeCompare(str(b.sector));
    case 'currentPrice': return num(a.currentPrice) - num(b.currentPrice);
    case 'target': {
      const av = a.targetEntryLow ?? a.targetEntryHigh ?? -Infinity;
      const bv = b.targetEntryLow ?? b.targetEntryHigh ?? -Infinity;
      return av - bv;
    }
    case 'distance': {
      // Distance = |currentPrice - midTarget| / midTarget. Smaller = closer.
      const distance = (e) => {
        if (e.currentPrice == null) return Infinity;
        const lo = e.targetEntryLow, hi = e.targetEntryHigh;
        if (lo == null && hi == null) return Infinity;
        const mid = (lo != null && hi != null) ? (lo + hi) / 2 : (lo ?? hi);
        return Math.abs(e.currentPrice - mid) / mid;
      };
      return distance(a) - distance(b);
    }
    case 'score': {
      const s = (e) => (e.latestScore ? e.latestScore.totalScore : -1);
      return s(a) - s(b);
    }
    case 'addedAt':
    default:
      return new Date(a.addedAt || 0) - new Date(b.addedAt || 0);
  }
}

function parseFirstTag(jsonStr) {
  try {
    const obj = JSON.parse(jsonStr);
    for (const k of Object.keys(obj)) return obj[k];
  } catch (_) { /* ignore */ }
  return null;
}
function relativeAge(iso) {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  const days = Math.floor((Date.now() - t) / (1000 * 60 * 60 * 24));
  if (days === 0) return 'today';
  if (days === 1) return '1d ago';
  if (days < 30) return `${days}d ago`;
  if (days < 365) return `${Math.floor(days / 30)}mo ago`;
  return `${Math.floor(days / 365)}y ago`;
}

async function onWatchlistAction(act, wid) {
  const entry = (state.watchlist || []).find((e) => e.id === wid);
  if (!entry) return;
  if (act === 'edit') return openWatchlistModal({ kind: entry.kind, mode: 'edit', entry });
  if (act === 'delete') {
    if (!confirm(`Remove ${entry.ticker} from watchlist?`)) return;
    try { await api(`/api/watchlist/${wid}`, { method: 'DELETE' }); state.watchlist = null; loadActiveTab(); }
    catch (e) { alert('delete failed: ' + e.message); }
    return;
  }
  if (act === 'score') return openScoreScreen(wid);
  if (act === 'promote') return openPromoteModal(entry);
}

// ---------- add / edit watchlist modal ---------------------------------

function openWatchlistModal({ kind, mode, entry, prefill }) {
  const isEdit = mode === 'edit';
  // Field-value source: edit mode → existing entry; add mode → prefill (if any).
  const t = (k) => {
    if (isEdit) return entry[k] ?? '';
    if (prefill) return prefill[k] ?? '';
    return '';
  };
  const safeTicker = isEdit ? escapeHTML(entry.ticker) : (prefill ? escapeHTML(prefill.ticker || '') : '');
  const html = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">${isEdit ? 'Edit' : 'Add'} ${kind} watchlist entry</div>
            ${isEdit ? `<div class="desc tabular">${safeTicker}</div>` : ''}
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <form id="wl-form" class="holding-form">
            ${isEdit ? '' : `
              <div class="form-row">
                <label for="wl-ticker">Ticker *</label>
                <input id="wl-ticker" name="ticker" type="text" required value="${escapeHTML(prefill && prefill.ticker ? String(prefill.ticker) : '')}" />
              </div>`}
            <div class="form-row">
              <label for="wl-company">Company name</label>
              <input id="wl-company" name="companyName" type="text" value="${escapeHTML(String(t('companyName')))}" />
            </div>
            <div class="form-row">
              <label for="wl-sector">Sector</label>
              <input id="wl-sector" name="sector" type="text" value="${escapeHTML(String(t('sector')))}" />
            </div>
            <div class="form-row">
              <label for="wl-price">Current price</label>
              <input id="wl-price" name="currentPrice" type="number" step="0.01" value="${escapeHTML(String(t('currentPrice')))}" />
            </div>
            <div class="form-row">
              <label for="wl-low">Target entry low</label>
              <input id="wl-low" name="targetEntryLow" type="number" step="0.01" value="${escapeHTML(String(t('targetEntryLow')))}" />
            </div>
            <div class="form-row">
              <label for="wl-high">Target entry high</label>
              <input id="wl-high" name="targetEntryHigh" type="number" step="0.01" value="${escapeHTML(String(t('targetEntryHigh')))}" />
            </div>
            <div class="form-row">
              <label for="wl-thesis">Thesis link</label>
              <input id="wl-thesis" name="thesisLink" type="text" value="${escapeHTML(String(t('thesisLink')))}" />
            </div>
            <div class="form-row">
              <label for="wl-note">Note</label>
              <textarea id="wl-note" name="note" rows="2">${escapeHTML(String(t('note')))}</textarea>
            </div>
            <div class="error" id="wl-err"></div>
          </form>
        </div>
        <div class="modal-foot">
          <button class="btn-secondary" id="wl-cancel">Cancel</button>
          <button class="btn-primary" id="wl-save">${isEdit ? 'Save' : 'Add'}</button>
        </div>
      </div>
    </div>
  `;
  closeImportModal();
  const root = document.createElement('div');
  root.id = 'modal-root';
  root.innerHTML = html;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#wl-cancel').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
  $('#wl-save').addEventListener('click', () => submitWatchlistForm({ kind, mode, entry }));
}

async function submitWatchlistForm({ kind, mode, entry }) {
  const form = $('#wl-form');
  const err = $('#wl-err');
  err.textContent = '';
  const num = (k) => { const v = form.querySelector(`[name=${k}]`).value.trim(); return v === '' ? null : parseFloat(v); };
  const str = (k) => { const v = form.querySelector(`[name=${k}]`).value.trim(); return v === '' ? null : v; };
  const body = {
    kind,
    companyName: str('companyName'),
    sector: str('sector'),
    currentPrice: num('currentPrice'),
    targetEntryLow: num('targetEntryLow'),
    targetEntryHigh: num('targetEntryHigh'),
    thesisLink: str('thesisLink'),
    note: str('note'),
  };
  if (body.targetEntryLow != null && body.targetEntryHigh != null && body.targetEntryLow > body.targetEntryHigh) {
    err.textContent = 'target low must be ≤ target high';
    return;
  }
  let url = '/api/watchlist';
  let method = 'POST';
  if (mode === 'edit') {
    url = `/api/watchlist/${entry.id}`;
    method = 'PUT';
  } else {
    body.ticker = form.querySelector('[name=ticker]').value.trim().toUpperCase();
    if (!body.ticker) { err.textContent = 'ticker is required'; return; }
  }
  try {
    await api(url, { method, body: JSON.stringify(body) });
    closeImportModal();
    state.watchlist = null;
    loadActiveTab();
  } catch (e) {
    err.textContent = e.message;
  }
}

// ---------- 8-question scoring screen ----------------------------------
//
// openScoreScreen accepts either:
//   - a number (legacy) → treated as a watchlist id; entry pulled from
//     state.watchlist
//   - { targetKind: 'holding', kind: 'stock'|'crypto', id, ticker, name } →
//     used for in-place rescoring of held positions (backlog polish 2026-05-17)
async function openScoreScreen(arg) {
  let targetKind, targetId, entry;
  if (typeof arg === 'object' && arg && arg.targetKind === 'holding') {
    targetKind = 'holding';
    targetId = arg.id;
    entry = {
      id: arg.id,
      kind: arg.kind, // 'stock' | 'crypto'
      ticker: arg.ticker,
      companyName: arg.name,
    };
  } else {
    // Legacy path: numeric watchlist id.
    const wid = typeof arg === 'object' ? arg.id : arg;
    entry = (state.watchlist || []).find((e) => e.id === wid);
    if (!entry) return;
    targetKind = 'watchlist';
    targetId = entry.id;
  }
  // Load the framework definition + any existing latest score in parallel.
  const fwID = entry.kind === 'stock' ? 'jordi' : 'cowen';
  let fw, prior, contradictions;
  try {
    [fw, prior, contradictions] = await Promise.all([
      api(`/api/frameworks/${fwID}`),
      api(`/api/scores?targetKind=${targetKind}&targetId=${targetId}`),
      // Spec 11 D7 — factor-invalidation flag from notes.
      api(`/api/notes/contradictions?targetKind=${targetKind}&targetId=${targetId}`).catch(() => ({ contradictions: [] })),
    ]);
  } catch (e) {
    alert('couldn\'t load framework: ' + e.message);
    return;
  }
  const priorScores = (prior.score && prior.score.scoresJson) ? JSON.parse(prior.score.scoresJson) : {};
  const priorTags = (prior.score && prior.score.tagsJson) ? JSON.parse(prior.score.tagsJson) : {};
  const strongSet = new Set(fw.scoring.strong_signals || []);
  const tagKeys = Object.keys(fw.tags || {});
  // Map factorId → latest contradicting note (only for this framework).
  const contraByFactor = {};
  for (const n of ((contradictions && contradictions.contradictions) || [])) {
    if (n.frameworkId === fw.id && n.factorId) contraByFactor[n.factorId] = n;
  }

  const questionsHtml = fw.questions.map((q) => {
    const cur = priorScores[q.id] ? priorScores[q.id].score : null;
    const note = priorScores[q.id] ? priorScores[q.id].note : '';
    const radios = [0, 1, 2].map((v) => `
      <label class="score-radio">
        <input type="radio" name="q-${q.id}" value="${v}" ${cur === v ? 'checked' : ''} />
        <span>${v}${v === 0 ? ' — no' : v === 1 ? ' — partial' : ' — yes'}</span>
      </label>
    `).join('');
    // Spec 11 D7 — contradicting-note banner.
    const contra = contraByFactor[q.id];
    const contraBlock = contra ? `
      <div class="score-q-contra">
        ⚠ <strong>Recent thesis note contradicts:</strong>
        <span>"${escapeHTML((contra.observationText || '').slice(0, 240))}"</span>
        <span class="dim">(${escapeHTML(contra.observationAt)})</span>
      </div>
    ` : '';
    return `
      <div class="score-q ${strongSet.has(q.id) ? 'strong' : ''} ${contra ? 'contradicted' : ''}" data-qid="${q.id}">
        <div class="score-q-head">
          <span class="score-q-label">${escapeHTML(q.label)}${strongSet.has(q.id) ? ' <span class="strong-pill">strong</span>' : ''}</span>
          <span class="score-q-prompt">${escapeHTML(q.prompt)}</span>
        </div>
        ${q.guidance ? `<div class="score-q-guidance">${escapeHTML(q.guidance)}</div>` : ''}
        ${contraBlock}
        <div class="score-q-radios">${radios}</div>
        <input class="score-q-note" name="qnote-${q.id}" type="text" placeholder="optional 1-line justification" value="${escapeHTML(note || '')}" />
      </div>
    `;
  }).join('');

  const tagSelectorsHtml = tagKeys.map((k) => {
    const opts = (fw.tags[k] || []).map((v) =>
      `<option value="${escapeHTML(v)}" ${priorTags[k] === v ? 'selected' : ''}>${escapeHTML(v)}</option>`
    ).join('');
    return `
      <div class="form-row">
        <label for="tag-${k}">${escapeHTML(humanizeKey(k))}</label>
        <select id="tag-${k}" name="tag-${k}">
          <option value="">—</option>${opts}
        </select>
      </div>
    `;
  }).join('');

  const root = document.createElement('div');
  root.id = 'modal-root';
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal score-modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">${escapeHTML(fw.name)}</div>
            <div class="desc tabular">${escapeHTML(entry.ticker)} · ${escapeHTML(entry.companyName || entry.kind)}</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <div class="score-summary" id="score-summary">0/${fw.questions.length * 2}</div>
          <form id="score-form">
            ${questionsHtml}
            <div class="score-tags">${tagSelectorsHtml}</div>
            <div class="form-row">
              <label for="score-reviewer-note">Reviewer note</label>
              <textarea id="score-reviewer-note" name="reviewerNote" rows="2"></textarea>
            </div>
            <div class="error" id="score-err"></div>
          </form>
        </div>
        <div class="modal-foot">
          <button class="btn-secondary" id="score-cancel">Cancel</button>
          <button class="btn-primary" id="score-save">Save score</button>
        </div>
      </div>
    </div>
  `;
  closeImportModal();
  document.body.appendChild(root);

  const summaryEl = $('#score-summary');
  const max = fw.questions.length * 2;
  const threshold = fw.scoring.pass_threshold || FRAMEWORK_THRESHOLD;
  const updateSummary = () => {
    let total = 0;
    for (const q of fw.questions) {
      const checked = document.querySelector(`input[name="q-${q.id}"]:checked`);
      if (checked) total += parseInt(checked.value, 10);
    }
    const passes = total >= threshold;
    summaryEl.textContent = `${total}/${max} ${passes ? '✓ pass' : ''}`;
    summaryEl.className = `score-summary ${passes ? 'pass' : total > 0 ? 'fail' : ''}`;
  };
  document.querySelectorAll('input[type=radio]').forEach((r) => r.addEventListener('change', updateSummary));
  updateSummary();

  $('#modal-close').addEventListener('click', closeImportModal);
  $('#score-cancel').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
  $('#score-save').addEventListener('click', () => submitScore(fw, entry, prior.score, targetKind));
}

function humanizeKey(k) {
  return k.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}

async function submitScore(fw, entry, priorScore, targetKind) {
  const err = $('#score-err');
  err.textContent = '';
  const scores = {};
  for (const q of fw.questions) {
    const checked = document.querySelector(`input[name="q-${q.id}"]:checked`);
    if (!checked) {
      err.textContent = `Missing answer for "${q.label}"`;
      return;
    }
    const note = document.querySelector(`input[name="qnote-${q.id}"]`).value.trim();
    scores[q.id] = { score: parseInt(checked.value, 10), note: note || undefined };
  }
  const tags = {};
  for (const k of Object.keys(fw.tags || {})) {
    const v = document.querySelector(`[name="tag-${k}"]`).value;
    if (v) tags[k] = v;
  }
  const reviewerNote = document.querySelector('[name="reviewerNote"]').value.trim();

  try {
    await api('/api/scores', {
      method: 'POST',
      body: JSON.stringify({
        targetKind: targetKind || 'watchlist',
        targetId: entry.id,
        frameworkId: fw.id,
        scores,
        tags: Object.keys(tags).length ? tags : undefined,
        reviewerNote: reviewerNote || undefined,
      }),
    });
    closeImportModal();
    // Invalidate caches so the score badge reflects the new value.
    state.watchlist = null;
    state.stocks = null;
    state.crypto = null;
    state.summary = null;
    loadActiveTab();
  } catch (e) {
    err.textContent = e.message;
  }
}

// ---------- promote-to-holdings modal -----------------------------------

function openPromoteModal(entry) {
  const isStock = entry.kind === 'stock';
  const fields = isStock ? `
    <div class="form-row"><label>Invested $ *</label><input name="investedUsd" type="number" step="0.01" required /></div>
    <div class="form-row"><label>Avg open price</label><input name="avgOpenPrice" type="number" step="0.01" /></div>
    <div class="form-row"><label>Stop loss</label><input name="stopLoss" type="number" step="0.01" /></div>
    <div class="form-row"><label>Take profit</label><input name="takeProfit" type="number" step="0.01" /></div>
    <div class="form-row"><label>Category</label><input name="category" type="text" /></div>
  ` : `
    <div class="form-row"><label>Quantity held *</label><input name="quantityHeld" type="number" step="any" required /></div>
    <div class="form-row"><label>Quantity staked</label><input name="quantityStaked" type="number" step="any" /></div>
    <div class="form-row"><label>Avg buy €</label><input name="avgBuyEur" type="number" step="0.0001" /></div>
    <div class="form-row"><label>Cost basis €</label><input name="costBasisEur" type="number" step="0.01" /></div>
    <div class="form-row"><label>Classification</label>
      <select name="classification"><option value="alt">alt</option><option value="core">core</option></select>
    </div>
    <div class="form-row"><label>Volatility tier</label>
      <select name="volTier">
        <option value="low">low</option><option value="medium" selected>medium</option>
        <option value="high">high</option><option value="extreme">extreme</option>
      </select>
    </div>
  `;
  const root = document.createElement('div');
  root.id = 'modal-root';
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">Promote to ${isStock ? 'holding' : 'crypto'}</div>
            <div class="desc tabular">${escapeHTML(entry.ticker)} · ${escapeHTML(entry.companyName || entry.kind)}</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <form id="promote-form" class="holding-form">${fields}
            <div class="form-row"><label>Reason (audit)</label><input name="reason" type="text" /></div>
            <div class="error" id="promote-err"></div>
          </form>
        </div>
        <div class="modal-foot">
          <button class="btn-secondary" id="promote-cancel">Cancel</button>
          <button class="btn-primary" id="promote-save">Confirm promote</button>
        </div>
      </div>
    </div>
  `;
  closeImportModal();
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#promote-cancel').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
  $('#promote-save').addEventListener('click', () => submitPromote(entry, isStock));
}

async function submitPromote(entry, isStock) {
  const form = $('#promote-form');
  const err = $('#promote-err');
  err.textContent = '';
  const num = (k) => { const el = form.querySelector(`[name=${k}]`); if (!el) return undefined; const v = el.value.trim(); return v === '' ? null : parseFloat(v); };
  const str = (k) => { const el = form.querySelector(`[name=${k}]`); if (!el) return undefined; const v = el.value.trim(); return v === '' ? null : v; };
  const body = isStock
    ? {
        investedUsd: num('investedUsd') || 0,
        avgOpenPrice: num('avgOpenPrice'),
        stopLoss: num('stopLoss'),
        takeProfit: num('takeProfit'),
        category: str('category'),
        reason: str('reason'),
      }
    : {
        quantityHeld: num('quantityHeld') || 0,
        quantityStaked: num('quantityStaked') || 0,
        avgBuyEur: num('avgBuyEur'),
        costBasisEur: num('costBasisEur'),
        classification: str('classification') || 'alt',
        volTier: str('volTier') || 'medium',
        reason: str('reason'),
      };
  if (isStock && !body.investedUsd) { err.textContent = 'invested $ required'; return; }
  if (!isStock && !body.quantityHeld) { err.textContent = 'quantity held required'; return; }
  try {
    await api(`/api/watchlist/${entry.id}/promote`, { method: 'POST', body: JSON.stringify(body) });
    closeImportModal();
    state.watchlist = null;
    if (isStock) state.stocks = null; else state.crypto = null;
    loadActiveTab();
  } catch (e) {
    err.textContent = e.message;
  }
}

// ============================================================================
// Spec 10 — Per-holding detail page
// ============================================================================
//
// Sections (top to bottom):
//   1. Header — name, ticker, sector, exchange, current price, P&L summary
//   2. Position & risk — stage, setup, ATR, vol tier, R captured, R at risk
//   3. Framework scores — Jordi/Cowen/Percoco latest + click-through to history
//   4. Transactions table — buys/sells/fees + Add Transaction button
//   5. Tax lots — FIFO breakdown
//   6. Dividends — table + Add Dividend button (stocks only)
//   7. Thesis — link + (Spec 11) notes
//   8. Audit trail — recent audit entries for this holding

async function renderHoldingDetail({ kind, id }) {
  const content = $('#content');
  content.innerHTML = '<div class="empty">loading detail…</div>';

  let holding, txns, taxlots, audit, divs, notes, thesisData;
  try {
    const path = kind === 'stock' ? 'stocks' : 'crypto';
    const [hRes, txnRes, lotRes, auditRes, divRes, notesRes, thRes] = await Promise.all([
      api(`/api/holdings/${path}`),
      api(`/api/transactions?holdingKind=${kind}&holdingId=${id}`),
      api(`/api/holdings/${path}/${id}/taxlots`),
      api(`/api/audit?limit=50`),
      kind === 'stock' ? api(`/api/dividends?holdingId=${id}`) : Promise.resolve({ dividends: [] }),
      // Spec 11 D4 — thesis notes for this holding.
      api(`/api/notes?targetKind=holding&targetId=${id}`),
      // Spec 14 — in-app thesis (returns {thesis: null} when none yet).
      api(`/api/holdings/${kind}/${id}/thesis`).catch(() => ({ thesis: null })),
    ]);
    holding = (hRes.holdings || []).find((h) => h.id === id);
    txns = txnRes.transactions || [];
    taxlots = lotRes.position || { taxLots: [] };
    audit = (auditRes.audit || []).filter((a) => a.holdingKind === kind && a.holdingId === id).slice(0, 20);
    divs = divRes.dividends || [];
    notes = notesRes.notes || [];
    thesisData = thRes;
  } catch (err) {
    content.innerHTML = `<div class="empty"><div class="loss">detail failed: ${escapeHTML(err.message)}</div></div>`;
    return;
  }
  if (!holding) {
    content.innerHTML = `<div class="empty">Holding not found.</div>`;
    return;
  }

  const tickerOrSym = holding.ticker || holding.symbol || '—';
  const currentPrice = kind === 'stock' ? holding.currentPrice : holding.currentPriceUsd;
  const positionUSD = taxlots.taxLots.reduce((s, l) => s + (l.quantityOpen * (currentPrice || 0)), 0);
  const unrealized = taxlots.taxLots.reduce((s, l) => s + (l.unrealizedPnlUsd || 0), 0);
  const totalDivs = divs.reduce((s, d) => s + (d.totalReceivedUsd || 0), 0);

  const header = `
    <div class="detail-header">
      <div class="detail-back"><button class="btn-ghost" id="dh-back">← Back to ${escapeHTML(kind === 'stock' ? 'Stocks' : 'Crypto')}</button></div>
      <h2 class="detail-name">${escapeHTML(tickerOrSym)} <span class="dim">${escapeHTML(holding.name)}</span></h2>
      <div class="detail-meta">
        ${holding.sector ? `<span>Sector: ${escapeHTML(holding.sector)}</span>` : ''}
        ${holding.market ? `<span>· Market: ${escapeHTML(holding.market.name || '')}</span>` : ''}
        <span>· Stage: <strong>${escapeHTML(holding.stage || 'pre_tp1')}</strong></span>
      </div>
      <div class="detail-kpi-row">
        <div class="detail-kpi">
          <div class="dim">Current price</div>
          <div class="num strong">$${currentPrice != null ? fmtNum2.format(currentPrice) : '—'}</div>
        </div>
        <div class="detail-kpi">
          <div class="dim">Position</div>
          <div class="num strong">${fmtNum6.format(taxlots.quantity || 0)} ${kind === 'stock' ? 'sh' : 'units'}</div>
          <div class="dim">Avg cost $${fmtNum2.format(taxlots.costBasisAvgUsd || 0)}</div>
        </div>
        <div class="detail-kpi">
          <div class="dim">Value</div>
          <div class="num strong">$${fmtNum0.format(positionUSD)}</div>
        </div>
        <div class="detail-kpi">
          <div class="dim">Unrealized P&L</div>
          <div class="num strong ${unrealized > 0 ? 'gain' : unrealized < 0 ? 'loss' : ''}">${unrealized >= 0 ? '+' : '-'}$${fmtNum0.format(Math.abs(unrealized))}</div>
        </div>
        <div class="detail-kpi">
          <div class="dim">Realized P&L</div>
          <div class="num strong ${holding.realizedPnlUsd > 0 ? 'gain' : holding.realizedPnlUsd < 0 ? 'loss' : ''}">${holding.realizedPnlUsd >= 0 ? '+' : '-'}$${fmtNum0.format(Math.abs(holding.realizedPnlUsd || 0))}</div>
        </div>
        ${kind === 'stock' ? `
        <div class="detail-kpi">
          <div class="dim">Dividends received</div>
          <div class="num strong">$${fmtNum0.format(totalDivs)}</div>
        </div>` : ''}
      </div>
    </div>
  `;

  // Position & risk section.
  const r1 = holding.resistance1, r2 = holding.resistance2, s1 = holding.support1;
  const positionRisk = `
    <section class="detail-section">
      <h3 class="rh-side">Position & risk</h3>
      <div class="detail-grid">
        <div><span class="dim">Setup type:</span> ${escapeHTML(holding.setupType || '—')}</div>
        <div><span class="dim">Vol tier:</span> ${escapeHTML(holding.volTierAuto || holding.volTier || '—')}</div>
        <div><span class="dim">ATR (14w):</span> ${holding.atrWeekly ? '$' + fmtNum2.format(holding.atrWeekly) : '—'}</div>
        <div><span class="dim">Support 1:</span> ${s1 ? '$' + fmtNum2.format(s1) : '—'}</div>
        <div><span class="dim">Resistance 1:</span> ${r1 ? '$' + fmtNum2.format(r1) : '—'}</div>
        <div><span class="dim">Resistance 2:</span> ${r2 ? '$' + fmtNum2.format(r2) : '—'}</div>
        <div><span class="dim">Stop loss:</span> ${holding.stopLoss ? '$' + fmtNum2.format(holding.stopLoss) : '—'}</div>
        <div><span class="dim">Take profit:</span> ${holding.takeProfit ? '$' + fmtNum2.format(holding.takeProfit) : '—'}</div>
        <div><span class="dim">TP1 hit:</span> ${holding.tp1HitAt ? escapeHTML(new Date(holding.tp1HitAt).toLocaleDateString()) : '—'}</div>
        <div><span class="dim">TP2 hit:</span> ${holding.tp2HitAt ? escapeHTML(new Date(holding.tp2HitAt).toLocaleDateString()) : '—'}</div>
      </div>
    </section>
  `;

  // Framework scores (latest).
  const scoreSection = `
    <section class="detail-section">
      <h3 class="rh-side">Framework scores</h3>
      <div class="detail-score-row">
        ${scoreCell(holding.score)} <span class="dim" style="font-size:0.85rem; margin-left:0.5rem">Latest score (click on Watchlist tab to re-score; see Spec 4)</span>
      </div>
    </section>
  `;

  // Transactions.
  const txnRows = txns.map((t) => `
    <tr>
      <td class="dim">${escapeHTML(new Date(t.executedAt).toLocaleDateString())}</td>
      <td>${escapeHTML(t.txnType)}</td>
      <td class="num">${fmtNum6.format(t.quantity)}</td>
      <td class="num">$${fmtNum2.format(t.priceUsd)}</td>
      <td class="num">$${fmtNum2.format(t.feesUsd)}</td>
      <td class="num">$${fmtNum2.format(t.totalUsd)}</td>
      <td class="dim">${escapeHTML(t.venue || '')}</td>
      <td>
        <button class="row-mini danger" data-supersede="${t.id}" title="Supersede (don't UPDATE — append correction)">×</button>
      </td>
    </tr>
  `).join('') || `<tr><td colspan="8" class="dim" style="text-align:center; padding:0.6rem">No transactions yet.</td></tr>`;

  const txnSection = `
    <section class="detail-section">
      <h3 class="rh-side" style="display:flex; justify-content:space-between; align-items:center">
        <span>Transactions <span class="dim" style="font-size:0.78rem; font-weight:normal">(append-only)</span></span>
        <button class="btn-ghost" id="dh-add-txn">+ Add transaction</button>
      </h3>
      <div class="tablewrap"><table class="holdings"><thead><tr>
        <th>Date</th><th>Type</th><th class="num">Qty</th><th class="num">Price</th>
        <th class="num">Fees</th><th class="num">Total</th><th>Venue</th><th></th>
      </tr></thead><tbody>${txnRows}</tbody></table></div>
    </section>
  `;

  // Tax lots (FIFO).
  const lotRows = taxlots.taxLots.map((l) => `
    <tr>
      <td class="dim">${escapeHTML(new Date(l.openedAt).toLocaleDateString())}</td>
      <td class="num">${fmtNum6.format(l.quantityOpen)}</td>
      <td class="num">${fmtNum6.format(l.quantityOrig)}</td>
      <td class="num">$${fmtNum2.format(l.pricePerUnit)}</td>
      <td class="num">${l.holdingDays}d</td>
      <td class="num ${l.unrealizedPnlUsd > 0 ? 'gain' : l.unrealizedPnlUsd < 0 ? 'loss' : ''}">${l.unrealizedPnlUsd >= 0 ? '+' : '-'}$${fmtNum2.format(Math.abs(l.unrealizedPnlUsd))}</td>
    </tr>
  `).join('') || `<tr><td colspan="6" class="dim" style="text-align:center">No open lots.</td></tr>`;

  const lotsSection = `
    <section class="detail-section">
      <h3 class="rh-side">Tax lots <span class="dim" style="font-size:0.78rem; font-weight:normal">(FIFO)</span></h3>
      <div class="tablewrap"><table class="holdings"><thead><tr>
        <th>Opened</th><th class="num">Qty open</th><th class="num">Qty orig</th>
        <th class="num">Price</th><th class="num">Held</th><th class="num">Unrealized</th>
      </tr></thead><tbody>${lotRows}</tbody></table></div>
    </section>
  `;

  // Dividends (stocks only).
  let divSection = '';
  if (kind === 'stock') {
    const divRows = divs.map((d) => `
      <tr>
        <td class="dim">${escapeHTML(d.exDate)}</td>
        <td class="num">$${fmtNum4.format(d.amountPerShareUsd)}</td>
        <td class="num">${fmtNum2.format(d.sharesHeld)}</td>
        <td class="num">$${fmtNum2.format(d.totalReceivedUsd)}</td>
        <td class="dim">${escapeHTML(d.note || '')}</td>
      </tr>
    `).join('') || `<tr><td colspan="5" class="dim" style="text-align:center">No dividends recorded.</td></tr>`;
    divSection = `
      <section class="detail-section">
        <h3 class="rh-side" style="display:flex; justify-content:space-between; align-items:center">
          <span>Dividends</span>
          <button class="btn-ghost" id="dh-add-div">+ Record dividend</button>
        </h3>
        <div class="tablewrap"><table class="holdings"><thead><tr>
          <th>Ex-date</th><th class="num">Per share</th><th class="num">Shares</th>
          <th class="num">Total received</th><th>Note</th>
        </tr></thead><tbody>${divRows}</tbody></table></div>
      </section>
    `;
  }

  // Thesis section — Spec 14 in-app body + Spec 11 D4 notes + Spec 10 external link.
  const thesisLink = holding.thesisLink || '';
  const notesHTML = renderNotesList(notes);
  const thesis = thesisData?.thesis || null;
  const thesisHTML = thesisData?.html || '';

  // Sub-block: in-app long-form thesis. "Start one" CTA when absent.
  let thesisBodyBlock;
  if (thesis) {
    const statusTone = thesis.status === 'locked' ? 'gain' : thesis.status === 'needs-review' ? 'amber-text' : 'dim';
    thesisBodyBlock = `
      <div class="thesis-body-block">
        <div class="thesis-body-head">
          <span><strong>📄 In-app thesis</strong> · v${escapeHTML(thesis.currentVersion)} · <span class="${statusTone}">${escapeHTML(thesis.status)}</span></span>
          <span class="thesis-body-actions">
            <button class="btn-ghost" id="dh-thesis-edit">✎ Edit</button>
            <button class="btn-ghost" id="dh-thesis-versions">History</button>
          </span>
        </div>
        <div class="thesis-body-rendered">${thesisHTML}</div>
      </div>
    `;
  } else {
    thesisBodyBlock = `
      <div class="thesis-body-block thesis-body-empty">
        <p class="dim"><strong>📄 No in-app thesis yet.</strong> Start one to write the long-form argument for this holding.</p>
        <button class="btn-ghost" id="dh-thesis-edit">+ Start thesis</button>
      </div>
    `;
  }

  const thesisSection = `
    <section class="detail-section">
      <h3 class="rh-side" style="display:flex; justify-content:space-between; align-items:center">
        <span>Thesis <span class="dim" style="font-size:0.78rem; font-weight:normal">(${notes.length} note${notes.length === 1 ? '' : 's'})</span></span>
        <button class="btn-ghost" id="dh-add-note">+ Add note</button>
      </h3>
      ${thesisBodyBlock}
      <div class="form-row" style="margin: 0.8rem 0 0.6rem 0">
        <label for="dh-thesis-link">External thesis link <span class="dim" style="font-size:0.75rem">(Notion / Google Doc)</span></label>
        <input id="dh-thesis-link" type="text" value="${escapeHTML(thesisLink)}" placeholder="https://notion.so/.../your-thesis" />
        <button class="btn-ghost" id="dh-thesis-save" style="margin-left:0.5rem">Save</button>
        ${thesisLink ? `<a class="btn-ghost" href="${escapeHTML(thesisLink)}" target="_blank" rel="noopener noreferrer" style="margin-left:0.4rem">Open ↗</a>` : ''}
      </div>
      ${notesHTML}
    </section>
  `;

  // Audit trail (last 20 for this holding).
  const auditRows = audit.map((a) => {
    let changes = '';
    try {
      const c = JSON.parse(a.changes || '{}');
      if (a.action === 'update') {
        changes = Object.keys(c).slice(0, 3).join(', ');
      } else if (a.action === 'create') changes = 'opened';
      else changes = a.action;
    } catch (_) { changes = a.action; }
    return `
      <tr>
        <td class="dim">${escapeHTML(new Date(a.ts).toLocaleString())}</td>
        <td>${escapeHTML(a.action)}</td>
        <td class="dim">${escapeHTML(changes)}</td>
        <td class="dim">${escapeHTML(a.reason || '')}</td>
      </tr>
    `;
  }).join('') || `<tr><td colspan="4" class="dim" style="text-align:center">No audit entries.</td></tr>`;

  const auditSection = `
    <section class="detail-section">
      <h3 class="rh-side">Audit trail <span class="dim" style="font-size:0.78rem; font-weight:normal">(last 20)</span></h3>
      <div class="tablewrap"><table class="holdings"><thead><tr>
        <th>When</th><th>Action</th><th>Changed</th><th>Reason</th>
      </tr></thead><tbody>${auditRows}</tbody></table></div>
    </section>
  `;

  content.innerHTML = header + positionRisk + scoreSection + txnSection + lotsSection + divSection + thesisSection + auditSection;

  // Wire interactions.
  $('#dh-back').addEventListener('click', () => {
    state.holdingDetail = null;
    loadActiveTab();
  });
  $('#dh-add-txn').addEventListener('click', () => openAddTxnModal({ kind, id, ticker: tickerOrSym, currentPrice }));
  // Spec 14 — in-app thesis editor.
  document.querySelector('#dh-thesis-edit')?.addEventListener('click', () => {
    openThesisEditor({ kind, id, ticker: tickerOrSym, name: holding.name, thesis: thesisData?.thesis || null });
  });
  document.querySelector('#dh-thesis-versions')?.addEventListener('click', () => {
    openThesisVersions({ kind, id, ticker: tickerOrSym });
  });
  $('#dh-thesis-save').addEventListener('click', async () => {
    const newLink = $('#dh-thesis-link').value.trim();
    try {
      // Build a minimal update body — the existing edit-modal payload shape.
      const body = kind === 'stock'
        ? { name: holding.name, ticker: holding.ticker, category: holding.category, sector: holding.sector,
            investedUsd: holding.investedUsd, avgOpenPrice: holding.avgOpenPrice, currentPrice: holding.currentPrice,
            stopLoss: holding.stopLoss, takeProfit: holding.takeProfit,
            strategyNote: holding.strategyNote || '', thesisLink: newLink }
        : { name: holding.name, symbol: holding.symbol, classification: holding.classification, isCore: holding.isCore,
            quantityHeld: holding.quantityHeld, quantityStaked: holding.quantityStaked,
            avgBuyEur: holding.avgBuyEur, costBasisEur: holding.costBasisEur,
            strategyNote: holding.strategyNote || '', volTier: holding.volTier, thesisLink: newLink };
      const path = kind === 'stock' ? 'stocks' : 'crypto';
      await api(`/api/holdings/${path}/${id}`, { method: 'PUT', body: JSON.stringify(body) });
      // Re-render to show the Open ↗ button.
      if (kind === 'stock') state.stocks = null;
      else state.crypto = null;
      renderHoldingDetail({ kind, id });
    } catch (e) {
      alert('Save failed: ' + e.message);
    }
  });
  if (kind === 'stock') {
    document.querySelector('#dh-add-div')?.addEventListener('click', () => openAddDividendModal({ id, ticker: tickerOrSym }));
  }
  // Spec 11 D3 — Add note button.
  document.querySelector('#dh-add-note')?.addEventListener('click', () => openAddNoteModal({
    targetKind: 'holding', targetId: id, ticker: tickerOrSym, holdingKind: kind,
    onSaved: () => renderHoldingDetail({ kind, id }),
  }));
  // Spec 11 D4 — note row actions (edit / delete).
  document.querySelectorAll('[data-note-edit]').forEach(btn => {
    btn.addEventListener('click', () => {
      const n = notes.find(x => String(x.id) === btn.dataset.noteEdit);
      if (!n) return;
      openAddNoteModal({
        targetKind: 'holding', targetId: id, ticker: tickerOrSym, holdingKind: kind,
        existing: n,
        onSaved: () => renderHoldingDetail({ kind, id }),
      });
    });
  });
  document.querySelectorAll('[data-note-del]').forEach(btn => {
    btn.addEventListener('click', async () => {
      if (!confirm('Soft-delete this note? It stays in history but won\'t show by default.')) return;
      try {
        await api(`/api/notes/${btn.dataset.noteDel}`, { method: 'DELETE' });
        renderHoldingDetail({ kind, id });
      } catch (e) { alert('Delete failed: ' + e.message); }
    });
  });
  // Supersede buttons.
  document.querySelectorAll('[data-supersede]').forEach(btn => {
    btn.addEventListener('click', async () => {
      if (!confirm('Supersede this transaction? It will be excluded from derivation but kept in history.')) return;
      try {
        await api(`/api/transactions/${btn.dataset.supersede}/supersede`, { method: 'POST' });
        if (kind === 'stock') state.stocks = null;
        else state.crypto = null;
        renderHoldingDetail({ kind, id });
      } catch (e) {
        alert('Supersede failed: ' + e.message);
      }
    });
  });
}

// Spec 10 D5 — Add Transaction modal.
function openAddTxnModal({ kind, id, ticker, currentPrice }) {
  closeImportModal();
  const root = document.createElement('div');
  root.id = 'modal-root';
  const nowISO = new Date().toISOString().slice(0, 16); // "2026-05-17T10:30"
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">Add transaction — ${escapeHTML(ticker)}</div>
            <div class="desc">Buy / Sell / Fee. Recomputes position FIFO after save.</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <form id="at-form" class="holding-form">
            <div class="form-row">
              <label for="at-when">Executed at</label>
              <input id="at-when" type="datetime-local" value="${nowISO}" required />
            </div>
            <div class="form-row">
              <label for="at-type">Type</label>
              <select id="at-type">
                <option value="buy">Buy</option>
                <option value="sell">Sell</option>
                <option value="fee">Fee only</option>
              </select>
            </div>
            <div class="form-row">
              <label for="at-qty">Quantity</label>
              <input id="at-qty" type="number" step="any" min="0" required />
            </div>
            <div class="form-row">
              <label for="at-price">Price (USD per unit)</label>
              <input id="at-price" type="number" step="0.0001" min="0" required value="${currentPrice ? fmtNum2.format(currentPrice) : ''}" />
            </div>
            <div class="form-row">
              <label for="at-fees">Fees (USD)</label>
              <input id="at-fees" type="number" step="0.01" min="0" value="0" />
            </div>
            <div class="form-row">
              <label for="at-venue">Venue</label>
              <input id="at-venue" type="text" placeholder="e.g. eToro, Binance, Tangem" />
            </div>
            <div class="form-row">
              <label for="at-note">Note</label>
              <input id="at-note" type="text" />
            </div>
            <div class="error" id="at-err"></div>
          </form>
        </div>
        <div class="modal-foot">
          <button class="btn-secondary" id="at-cancel">Cancel</button>
          <button class="btn-primary" id="at-save">Save</button>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#at-cancel').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
  $('#at-save').addEventListener('click', async () => {
    const err = $('#at-err'); err.textContent = '';
    const qty = parseFloat($('#at-qty').value);
    const price = parseFloat($('#at-price').value);
    if (!Number.isFinite(qty) || qty <= 0) { err.textContent = 'quantity required'; return; }
    if (!Number.isFinite(price) || price < 0) { err.textContent = 'price required'; return; }
    const body = {
      holdingKind: kind,
      holdingId: id,
      txnType: $('#at-type').value,
      executedAt: new Date($('#at-when').value).toISOString(),
      quantity: qty,
      priceUsd: price,
      feesUsd: parseFloat($('#at-fees').value) || 0,
      venue: $('#at-venue').value.trim(),
      note: $('#at-note').value.trim(),
    };
    try {
      await api('/api/transactions', { method: 'POST', body: JSON.stringify(body) });
      closeImportModal();
      if (kind === 'stock') state.stocks = null;
      else state.crypto = null;
      renderHoldingDetail({ kind, id });
    } catch (e) { err.textContent = e.message; }
  });
}

// Spec 10 D5 — Add Dividend modal (stocks only).
function openAddDividendModal({ id, ticker }) {
  closeImportModal();
  const root = document.createElement('div');
  root.id = 'modal-root';
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">Record dividend — ${escapeHTML(ticker)}</div>
            <div class="desc">Cash dividend received.</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <form id="ad-form" class="holding-form">
            <div class="form-row">
              <label for="ad-ex">Ex-date</label>
              <input id="ad-ex" type="date" required />
            </div>
            <div class="form-row">
              <label for="ad-pay">Pay date</label>
              <input id="ad-pay" type="date" />
            </div>
            <div class="form-row">
              <label for="ad-aps">Amount per share (USD)</label>
              <input id="ad-aps" type="number" step="0.0001" min="0" required />
            </div>
            <div class="form-row">
              <label for="ad-shares">Shares held</label>
              <input id="ad-shares" type="number" step="any" min="0" required />
            </div>
            <div class="form-row">
              <label for="ad-note">Note</label>
              <input id="ad-note" type="text" />
            </div>
            <div class="error" id="ad-err"></div>
          </form>
        </div>
        <div class="modal-foot">
          <button class="btn-secondary" id="ad-cancel">Cancel</button>
          <button class="btn-primary" id="ad-save">Save</button>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#ad-cancel').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
  $('#ad-save').addEventListener('click', async () => {
    const err = $('#ad-err'); err.textContent = '';
    const body = {
      holdingId: id,
      exDate: $('#ad-ex').value,
      payDate: $('#ad-pay').value || null,
      amountPerShareUsd: parseFloat($('#ad-aps').value),
      sharesHeld: parseFloat($('#ad-shares').value),
      note: $('#ad-note').value.trim(),
    };
    if (!body.exDate || !(body.amountPerShareUsd > 0) || !(body.sharesHeld > 0)) {
      err.textContent = 'all required fields must be set'; return;
    }
    try {
      await api('/api/dividends', { method: 'POST', body: JSON.stringify(body) });
      closeImportModal();
      renderHoldingDetail({ kind: 'stock', id });
    } catch (e) { err.textContent = e.message; }
  });
}

// ============================================================================
// Spec 11 — Thesis notes
// ============================================================================

// Direction → pill colour.
function noteDirCls(dir) {
  if (dir === 'confirms') return 'gain';
  if (dir === 'contradicts') return 'loss';
  if (dir === 'neutral') return 'dim';
  return '';
}

function noteDirGlyph(dir) {
  if (dir === 'confirms') return '✓';
  if (dir === 'contradicts') return '⚠';
  if (dir === 'neutral') return '·';
  return '';
}

// renderNotesList — D4 thesis notes display, newest-first.
function renderNotesList(notes) {
  if (!notes || notes.length === 0) {
    return `<p class="dim" style="font-size:0.85rem">No thesis notes yet. Click <strong>+ Add note</strong> to start capturing observations.</p>`;
  }
  const items = notes.map((n) => {
    const dirGlyph = noteDirGlyph(n.factorDirection);
    const dirCls = noteDirCls(n.factorDirection);
    const factorPill = (n.frameworkId && n.factorId)
      ? `<span class="note-factor ${dirCls}">${escapeHTML(n.frameworkId)}:${escapeHTML(n.factorId)} ${dirGlyph} ${escapeHTML(n.factorDirection || '')}</span>`
      : '';
    const srcKind = n.sourceKind ? `<span class="dim">· ${escapeHTML(n.sourceKind)}</span>` : '';
    const srcLink = n.sourceUrl
      ? `<a class="note-src" href="${escapeHTML(n.sourceUrl)}" target="_blank" rel="noopener noreferrer">source ↗</a>`
      : '';
    return `
      <li class="note-item">
        <div class="note-head">
          <span class="note-date">${escapeHTML(n.observationAt)}</span>
          ${factorPill}
          ${srcKind}
          <span class="note-actions">
            <button class="row-mini" data-note-edit="${n.id}" title="Edit">✎</button>
            <button class="row-mini danger" data-note-del="${n.id}" title="Soft-delete">×</button>
          </span>
        </div>
        <div class="note-body">${escapeHTML(n.observationText)}</div>
        ${srcLink ? `<div class="note-foot">${srcLink}</div>` : ''}
      </li>
    `;
  }).join('');
  return `<ul class="notes-list">${items}</ul>`;
}

// Cache of loaded frameworks for the cascading dropdown.
const _frameworksCache = { stock: null, crypto: null, all: null };
async function loadFrameworksFor(kind) {
  if (_frameworksCache.all) return _frameworksCache.all;
  try {
    const r = await api('/api/frameworks');
    _frameworksCache.all = r.frameworks || [];
    return _frameworksCache.all;
  } catch (_) {
    return [];
  }
}

// openAddNoteModal — D3. Triggered from detail page, news items, settings.
// args: { targetKind, targetId, ticker, holdingKind?, existing?, prefill?, onSaved? }
//   existing: full note object → edit mode
//   prefill: { observationText, sourceUrl, sourceKind } → new with seed
async function openAddNoteModal({ targetKind, targetId, ticker, holdingKind, existing, prefill, onSaved }) {
  closeImportModal();
  const today = new Date().toISOString().slice(0, 10);
  const e = existing || {};
  const p = prefill || {};
  // Load frameworks so we can render the cascading dropdown.
  const frameworks = await loadFrameworksFor(holdingKind);

  const root = document.createElement('div');
  root.id = 'modal-root';
  const fwOptions = frameworks.map((f) =>
    `<option value="${escapeHTML(f.id)}" ${e.frameworkId === f.id ? 'selected' : ''}>${escapeHTML(f.name || f.id)}</option>`
  ).join('');
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">${existing ? 'Edit' : 'Add'} thesis note — ${escapeHTML(ticker)}</div>
            <div class="desc">Observation + optional factor tagging. Confirms/contradicts feeds the next re-score.</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <form id="nt-form">
            <div class="form-row">
              <label for="nt-when">Observation date</label>
              <input id="nt-when" type="date" value="${escapeHTML(e.observationAt || today)}" required />
            </div>
            <div class="form-row">
              <label for="nt-text">Observation</label>
              <textarea id="nt-text" rows="4" maxlength="4000" required placeholder="What did you observe? Be specific.">${escapeHTML(e.observationText || p.observationText || '')}</textarea>
            </div>
            <div class="form-row">
              <label for="nt-fw">Framework (optional)</label>
              <select id="nt-fw">
                <option value="">—</option>
                ${fwOptions}
              </select>
            </div>
            <div class="form-row" id="nt-factor-row" style="display:${e.frameworkId ? 'flex' : 'none'}">
              <label for="nt-factor">Factor</label>
              <select id="nt-factor">
                <option value="">—</option>
              </select>
            </div>
            <div class="form-row" id="nt-dir-row" style="display:${e.factorId ? 'flex' : 'none'}">
              <label for="nt-dir">Direction</label>
              <select id="nt-dir">
                <option value="">—</option>
                <option value="confirms" ${e.factorDirection === 'confirms' ? 'selected' : ''}>Confirms ✓</option>
                <option value="contradicts" ${e.factorDirection === 'contradicts' ? 'selected' : ''}>Contradicts ⚠</option>
                <option value="neutral" ${e.factorDirection === 'neutral' ? 'selected' : ''}>Neutral</option>
              </select>
            </div>
            <div class="form-row">
              <label for="nt-src-url">Source URL (optional)</label>
              <input id="nt-src-url" type="text" value="${escapeHTML(e.sourceUrl || p.sourceUrl || '')}" placeholder="https://..." />
            </div>
            <div class="form-row">
              <label for="nt-src-kind">Source kind</label>
              <select id="nt-src-kind">
                <option value="">—</option>
                ${['news','earnings','youtube','twitter','manual','cowen_weekly','other'].map(k =>
                  `<option value="${k}" ${(e.sourceKind || p.sourceKind) === k ? 'selected' : ''}>${k}</option>`
                ).join('')}
              </select>
            </div>
            <div class="error" id="nt-err"></div>
          </form>
        </div>
        <div class="modal-foot">
          <button class="btn-secondary" id="nt-cancel">Cancel</button>
          <button class="btn-primary" id="nt-save">${existing ? 'Save changes' : 'Add note'}</button>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#nt-cancel').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });

  // Cascading: when framework changes, repopulate factor select.
  const factorRow = $('#nt-factor-row');
  const factorSelect = $('#nt-factor');
  const dirRow = $('#nt-dir-row');
  const dirSelect = $('#nt-dir');
  const refreshFactors = (fwId) => {
    const fw = frameworks.find(f => f.id === fwId);
    if (!fw) {
      factorRow.style.display = 'none';
      dirRow.style.display = 'none';
      factorSelect.innerHTML = '<option value="">—</option>';
      return;
    }
    factorRow.style.display = 'flex';
    factorSelect.innerHTML = '<option value="">—</option>' + (fw.questions || []).map(q =>
      `<option value="${escapeHTML(q.id)}" ${e.factorId === q.id ? 'selected' : ''}>${escapeHTML(q.label || q.id)}</option>`
    ).join('');
    dirRow.style.display = e.factorId ? 'flex' : 'none';
  };
  $('#nt-fw').addEventListener('change', () => refreshFactors($('#nt-fw').value));
  factorSelect.addEventListener('change', () => {
    dirRow.style.display = factorSelect.value ? 'flex' : 'none';
  });
  // Seed initial state.
  if (e.frameworkId) refreshFactors(e.frameworkId);

  $('#nt-save').addEventListener('click', async () => {
    const errEl = $('#nt-err'); errEl.textContent = '';
    const body = {
      targetKind,
      targetId,
      ticker,
      observationAt: $('#nt-when').value,
      observationText: $('#nt-text').value.trim(),
      frameworkId: $('#nt-fw').value || undefined,
      factorId: factorSelect.value || undefined,
      factorDirection: dirSelect.value || undefined,
      sourceUrl: $('#nt-src-url').value.trim() || undefined,
      sourceKind: $('#nt-src-kind').value || undefined,
    };
    if (!body.observationText) { errEl.textContent = 'observation text required'; return; }
    if (body.factorId && !body.frameworkId) { errEl.textContent = 'framework required when factor is set'; return; }
    try {
      if (existing) {
        await api(`/api/notes/${existing.id}`, { method: 'PUT', body: JSON.stringify(body) });
      } else {
        await api('/api/notes', { method: 'POST', body: JSON.stringify(body) });
      }
      closeImportModal();
      if (typeof onSaved === 'function') onSaved();
    } catch (ex) {
      errEl.textContent = ex.message;
    }
  });
}

// ============================================================================
// Spec 14 — Per-holding thesis editor + version history
// ============================================================================

function openThesisEditor({ kind, id, ticker, name, thesis }) {
  closeImportModal();
  const root = document.createElement('div');
  root.id = 'modal-root';
  const startingMarkdown = thesis?.markdownCurrent || `# Thesis — ${name || ticker}\n\n## Why I own this\n\n_Write the core argument here._\n\n## Catalysts\n\n- \n\n## Risks\n\n- \n\n## Exit conditions\n\n- `;
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal sc-editor-modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">${thesis ? 'Edit thesis' : 'Start thesis'} — ${escapeHTML(ticker)}</div>
            <div class="desc">${thesis ? `Current v${escapeHTML(thesis.currentVersion)}. Save (no bump) or Save as new version.` : 'First save creates v1. Use "Save as new version" for substantive rewrites.'}</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body sc-editor-body">
          <div class="sc-editor-split">
            <textarea id="th-md-text" class="sc-md-textarea" spellcheck="true">${escapeHTML(startingMarkdown)}</textarea>
            <div id="th-md-preview" class="sc-md-preview"></div>
          </div>
          <div class="error" id="th-edit-err"></div>
        </div>
        <div class="modal-foot">
          <button class="btn-secondary" id="th-cancel">Cancel</button>
          <button class="btn-ghost" id="th-save-version">Save as new version…</button>
          <button class="btn-primary" id="th-save">Save</button>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#th-cancel').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });

  const ta = $('#th-md-text');
  const pv = $('#th-md-preview');
  let previewTimer = null;
  const refreshPreview = async () => {
    try {
      const res = await fetch(`/api/holdings/${kind}/${id}/thesis/preview`, {
        method: 'POST',
        headers: { 'Content-Type': 'text/plain' },
        body: ta.value,
      });
      pv.innerHTML = await res.text();
    } catch (_) { /* keep last preview */ }
  };
  refreshPreview();
  ta.addEventListener('input', () => {
    if (previewTimer) clearTimeout(previewTimer);
    previewTimer = setTimeout(refreshPreview, 300);
  });

  $('#th-save').addEventListener('click', async () => {
    const err = $('#th-edit-err'); err.textContent = '';
    try {
      await api(`/api/holdings/${kind}/${id}/thesis`, {
        method: 'PUT',
        body: JSON.stringify({ markdown: ta.value }),
      });
      closeImportModal();
      renderHoldingDetail({ kind, id });
    } catch (e) { err.textContent = e.message; }
  });
  $('#th-save-version').addEventListener('click', async () => {
    const err = $('#th-edit-err'); err.textContent = '';
    const version = prompt('New version string?', thesis ? '' : '1');
    if (!version) return;
    const note = prompt('Changelog note (one line)?', '') || '';
    try {
      await api(`/api/holdings/${kind}/${id}/thesis`, {
        method: 'PUT',
        body: JSON.stringify({
          markdown: ta.value,
          asNewVersion: { version, changelogNote: note },
        }),
      });
      closeImportModal();
      renderHoldingDetail({ kind, id });
    } catch (e) { err.textContent = e.message; }
  });
}

async function openThesisVersions({ kind, id, ticker }) {
  closeImportModal();
  const root = document.createElement('div');
  root.id = 'modal-root';
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">Thesis history — ${escapeHTML(ticker)}</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <div id="th-versions-list">loading…</div>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
  try {
    const r = await api(`/api/holdings/${kind}/${id}/thesis/versions`);
    const versions = r.versions || [];
    if (versions.length === 0) {
      $('#th-versions-list').innerHTML = '<p class="dim">No version history yet — first save creates v1.</p>';
      return;
    }
    $('#th-versions-list').innerHTML = versions.map(v => `
      <div class="sc-version-row">
        <div><strong>v${escapeHTML(v.version)}</strong> <span class="dim">${new Date(v.createdAt).toLocaleString()}</span></div>
        ${v.changelogNote ? `<div class="dim" style="font-size:0.85rem">${escapeHTML(v.changelogNote)}</div>` : ''}
      </div>
    `).join('');
  } catch (e) {
    $('#th-versions-list').innerHTML = `<p class="loss">load failed: ${escapeHTML(e.message)}</p>`;
  }
}

// ============================================================================
// Spec 9d — Performance tab
// ============================================================================

if (state.perfWindow == null) state.perfWindow = 'all';

async function renderPerformance() {
  const content = $('#content');
  content.innerHTML = '<div class="empty">loading performance…</div>';

  let overview, cohorts, calib;
  try {
    [overview, cohorts, calib] = await Promise.all([
      api(`/api/performance/overview?window=${state.perfWindow}`),
      api(`/api/performance/cohorts?window=${state.perfWindow}`),
      api('/api/performance/calibration'),
    ]);
  } catch (err) {
    content.innerHTML = `<div class="empty"><div class="loss">performance failed: ${escapeHTML(err.message)}</div></div>`;
    return;
  }

  const m = overview.metrics || {};
  const isEmpty = (m.count || 0) === 0;

  // Window selector + export button at top.
  const windows = [['all','All-time'],['365d','Last 365 days'],['90d','Last 90 days'],['30d','Last 30 days']];
  const windowSel = windows.map(([v,l]) =>
    `<button class="ct-btn ${v === state.perfWindow ? 'active' : ''}" data-pwin="${v}">${escapeHTML(l)}</button>`).join('');

  // Headline KPIs.
  const kpiCard = (label, value, sub, tone) => `
    <div class="kpi-card">
      <div class="kpi-label">${label}</div>
      <div class="kpi-value num ${tone || ''}">${value}</div>
      <div class="kpi-sub num ${tone === 'gain' ? 'gain' : tone === 'loss' ? 'loss' : 'dim'}">${sub || '&nbsp;'}</div>
    </div>
  `;
  const winRatePct = isEmpty ? '—' : `${(m.winRate * 100).toFixed(1)}%`;
  const expectancyStr = isEmpty ? '—' : `${m.expectancy >= 0 ? '+' : ''}${m.expectancy.toFixed(2)}R`;
  const expectancyTone = isEmpty ? '' : m.expectancy >= 0.5 ? 'gain' : m.expectancy >= 0 ? '' : 'loss';
  const pnlStr = isEmpty ? '—' : `${m.totalPnlUsd >= 0 ? '+' : '-'}$${fmtNum0.format(Math.abs(m.totalPnlUsd))}`;
  const pnlTone = isEmpty ? '' : m.totalPnlUsd > 0 ? 'gain' : m.totalPnlUsd < 0 ? 'loss' : '';

  const kpiRow = `
    <div class="kpi-row">
      ${kpiCard('Trades', isEmpty ? '—' : String(m.count), isEmpty ? '' : `${m.winCount} winners · ${m.lossCount} losers`, '')}
      ${kpiCard('Win rate', winRatePct, isEmpty ? '' : `avg winner +${m.avgWinnerR.toFixed(2)}R · avg loser ${m.avgLoserR.toFixed(2)}R`, '')}
      ${kpiCard('Expectancy', expectancyStr, 'per trade', expectancyTone)}
      ${kpiCard('Realized P&L', pnlStr, isEmpty ? '' : `avg hold ${m.avgHoldDays.toFixed(0)}d`, pnlTone)}
    </div>
  `;

  // R-multiple histogram.
  const histMax = (overview.histogram || []).reduce((mx, b) => Math.max(mx, b.count), 0);
  const histBars = (overview.histogram || []).map(b => {
    const h = histMax > 0 ? (b.count / histMax) * 70 : 0;
    const tone = b.label.startsWith('+') || b.label.startsWith('≥+') ? 'gain' : b.label.startsWith('0') ? 'dim' : 'loss';
    return `
      <div class="hist-col">
        <div class="hist-bar-wrap"><div class="hist-bar ${tone}" style="height:${h}px"></div></div>
        <div class="hist-count num">${b.count || ''}</div>
        <div class="hist-label dim">${escapeHTML(b.label)}</div>
      </div>
    `;
  }).join('');

  // Equity curve (SVG line).
  const equity = overview.equity || [];
  let equityHTML = '';
  if (equity.length >= 2) {
    const W = 800, H = 200, P = 20;
    const minV = Math.min(...equity.map(e => e.portfolioValue));
    const maxV = Math.max(...equity.map(e => e.portfolioValue));
    const rng = Math.max(1, maxV - minV);
    const pts = equity.map((e, i) => {
      const x = P + (i / (equity.length - 1)) * (W - 2 * P);
      const y = H - P - ((e.portfolioValue - minV) / rng) * (H - 2 * P);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    }).join(' ');
    const peak = equity.reduce((mx, e) => e.portfolioValue > mx.portfolioValue ? e : mx, equity[0]);
    const cur = equity[equity.length - 1];
    const maxDd = equity.reduce((mn, e) => e.drawdownFromPeak < mn.drawdownFromPeak ? e : mn, equity[0]);
    equityHTML = `
      <h4 class="rh-side" style="margin-top:1rem">Equity curve · ${equity.length} days</h4>
      <div class="equity-wrap">
        <svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none" style="width:100%; height:200px; background:rgb(var(--color-surface-sunken)); border-radius:var(--radius); border:1px solid rgb(var(--color-border))">
          <polyline fill="none" stroke="rgb(var(--color-accent))" stroke-width="1.6" points="${pts}"/>
        </svg>
        <div class="equity-summary dim">
          Start $${fmtNum0.format(equity[0].portfolioValue)}
          · Peak $${fmtNum0.format(peak.portfolioValue)} (${escapeHTML(peak.date)})
          · Current $${fmtNum0.format(cur.portfolioValue)}
          · Max DD ${maxDd.drawdownFromPeak.toFixed(2)}%
          · Time underwater ${overview.underwaterPct.toFixed(0)}%
        </div>
      </div>
    `;
  }

  // Cohort breakdown table — group cohorts by family.
  const byFamily = {};
  for (const c of cohorts.cohorts || []) {
    const fam = c.key.includes(':') ? c.key.split(':')[0] : 'all';
    (byFamily[fam] = byFamily[fam] || []).push(c);
  }
  const familyOrder = ['all', 'kind', 'setup', 'regime', 'percoco', 'jordi', 'cowen', 'hold', 'exit'];
  const familyLabels = {
    all: 'Overall', kind: 'By kind', setup: 'By setup type', regime: 'By regime',
    percoco: 'By Percoco score', jordi: 'By Jordi score', cowen: 'By Cowen score',
    hold: 'By holding period', exit: 'By exit reason',
  };
  const cohortSections = familyOrder.filter(f => byFamily[f]).map(fam => {
    const rows = byFamily[fam].map(c => {
      const expTone = c.expectancy >= 0.5 ? 'gain' : c.expectancy < 0 ? 'loss' : 'dim';
      const lowConfTag = c.lowConfidence ? '<span class="dim" style="font-size:0.7rem"> (n&lt;5)</span>' : '';
      return `
        <tr data-cohort="${escapeHTML(c.key)}">
          <td>${escapeHTML(c.label)}${lowConfTag}</td>
          <td class="num">${c.tradeCount}</td>
          <td class="num">${(c.winRate * 100).toFixed(0)}%</td>
          <td class="num">${c.avgWinR ? c.avgWinR.toFixed(2) : '—'}R / ${c.avgLossR ? c.avgLossR.toFixed(2) : '—'}R</td>
          <td class="num ${expTone}">${c.expectancy >= 0 ? '+' : ''}${c.expectancy.toFixed(2)}R</td>
          <td class="num ${c.totalPnlUsd > 0 ? 'gain' : c.totalPnlUsd < 0 ? 'loss' : ''}">
            ${c.totalPnlUsd >= 0 ? '+' : '-'}$${fmtNum0.format(Math.abs(c.totalPnlUsd))}
          </td>
        </tr>
      `;
    }).join('');
    return `
      <h4 class="rh-side" style="margin-top:1rem">${escapeHTML(familyLabels[fam] || fam)}</h4>
      <div class="tablewrap"><table class="holdings perf-cohort"><thead><tr>
        <th>Cohort</th><th class="num">N</th><th class="num">Win%</th>
        <th class="num">Avg win / loss R</th><th class="num">Expectancy</th><th class="num">$ P&L</th>
      </tr></thead><tbody>${rows}</tbody></table></div>
    `;
  }).join('');

  // Calibration panel.
  const calibBlocks = (calib.frameworks || []).map(fw => {
    const buckets = ['le-8','9-12','13-16'];
    const rows = buckets.map(b => {
      const v = fw.buckets[b] || 0;
      const n = fw.counts[b] || 0;
      const tone = v >= 0.5 ? 'gain' : v >= 0 ? '' : 'loss';
      const icon = v >= 0.5 ? '✓' : v >= 0 ? '⚠' : '✗';
      return `<li><span>${escapeHTML(b)}</span> → <span class="num ${tone}">${v >= 0 ? '+' : ''}${v.toFixed(2)}R</span> <span class="dim">${icon} n=${n}</span></li>`;
    }).join('');
    let banner = '';
    if (!fw.sufficient) {
      banner = '<div class="dim" style="font-size:0.78rem; font-style:italic">Insufficient data (need ≥5 trades per bucket) — treat as informational only.</div>';
    } else if (!fw.monotonic) {
      banner = `<div class="loss" style="font-size:0.8rem">⚠ Non-monotonic: ${escapeHTML(fw.warning)}</div>`;
    } else {
      banner = '<div class="gain" style="font-size:0.8rem">✓ Monotonic — framework is well-calibrated.</div>';
    }
    return `
      <div class="calib-fw">
        <h4 class="rh-side">${escapeHTML(fw.framework)}</h4>
        <ul class="calib-list">${rows}</ul>
        ${banner}
      </div>
    `;
  }).join('');

  // Empty state guidance.
  const emptyBanner = isEmpty ? `
    <div class="stale-banner">
      ⚠ No closed trades yet. The Performance tab fills in once positions
      open + close (via soft-delete on the holdings table). Run
      <code>sudo -u ft /opt/ft/bin/ft perf-derive</code> if you've
      already had closures and want to re-derive.
    </div>
  ` : '';

  content.innerHTML = `
    <div class="perf-toolbar">
      <div class="currency-toggle" role="tablist">${windowSel}</div>
      <a class="btn-ghost" href="/api/performance/export.csv" download>Export CSV</a>
    </div>
    ${emptyBanner}
    ${kpiRow}
    <h4 class="rh-side" style="margin-top:1rem">R-multiple distribution</h4>
    <div class="hist-row">${histBars}</div>
    ${equityHTML}
    <h4 class="rh-side" style="margin-top:1.2rem">Methodology calibration <span class="dim" style="font-size:0.78rem; font-weight:normal">(does scoring trades higher produce better outcomes?)</span></h4>
    <div class="calib-row">${calibBlocks}</div>
    <h4 class="rh-side" style="margin-top:1.2rem">Cohort breakdown</h4>
    ${cohortSections || '<div class="dim">No cohort data yet.</div>'}
  `;

  // Window selector clicks.
  document.querySelectorAll('[data-pwin]').forEach(btn => {
    btn.addEventListener('click', () => {
      state.perfWindow = btn.dataset.pwin;
      renderPerformance();
    });
  });
  // Cohort row click → drill-down (alert for v1 since the modal would be huge)
  document.querySelectorAll('tr[data-cohort]').forEach(row => {
    row.style.cursor = 'pointer';
    row.addEventListener('click', () => openPerfCohortDrill(row.dataset.cohort));
  });
}

async function openPerfCohortDrill(key) {
  let d;
  try {
    d = await api(`/api/performance/cohort/${encodeURIComponent(key)}?window=${state.perfWindow}`);
  } catch (e) {
    alert('drill-down failed: ' + e.message);
    return;
  }
  closeImportModal();
  const root = document.createElement('div');
  root.id = 'modal-root';
  const tradeRows = (d.trades || []).map(t => `
    <tr>
      <td>${escapeHTML(new Date(t.openedAt).toLocaleDateString())} → ${escapeHTML(new Date(t.closedAt).toLocaleDateString())}</td>
      <td><strong>${escapeHTML(t.ticker)}</strong> <span class="dim">${escapeHTML(t.kind)}</span></td>
      <td>${escapeHTML(t.setupType || '—')}</td>
      <td>${escapeHTML(t.exitReason)}</td>
      <td class="num">$${fmtNum2.format(t.entryPrice)}</td>
      <td class="num">$${fmtNum2.format(t.exitPriceAvg)}</td>
      <td class="num ${t.realizedRMultiple > 0 ? 'gain' : 'loss'}">${t.realizedRMultiple >= 0 ? '+' : ''}${t.realizedRMultiple.toFixed(2)}R</td>
      <td class="num ${t.realizedPnlUsd > 0 ? 'gain' : 'loss'}">${t.realizedPnlUsd >= 0 ? '+' : '-'}$${fmtNum0.format(Math.abs(t.realizedPnlUsd))}</td>
    </tr>
  `).join('') || '<tr><td colspan="8" class="dim" style="text-align:center">No trades.</td></tr>';
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" style="max-width:900px" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">${escapeHTML(d.label)}</div>
            <div class="desc dim">${d.trades.length} trade${d.trades.length === 1 ? '' : 's'} · window: ${escapeHTML(d.window)}</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <div class="tablewrap"><table class="holdings"><thead><tr>
            <th>Opened → Closed</th><th>Ticker</th><th>Setup</th><th>Exit</th>
            <th class="num">Entry</th><th class="num">Exit</th>
            <th class="num">R</th><th class="num">P&L</th>
          </tr></thead><tbody>${tradeRows}</tbody></table></div>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
}

// ============================================================================
// Spec 11b — Command palette (Cmd/Ctrl+K)
// ============================================================================
//
// One overlay, one search input, fuzzy-filtered list of jumpable items:
//   - Tabs (Summary / Stocks / Crypto / …)
//   - Active holdings by ticker (jump to detail page)
//   - Watchlist entries
//   - Actions: refresh, import CSV, add stock, add crypto, …
//
// Keys when open:
//   Esc            close
//   Up/Down        move highlight
//   Enter          activate
//   Cmd/Ctrl+K     also closes (toggle)
//
// Keys when closed (only when no input is focused):
//   Cmd/Ctrl+K     open palette
//   /              same (treat as quick-search)

let _cmdPaletteEls = null; // { root, input, list }
let _cmdPaletteItems = [];
let _cmdPaletteFiltered = [];
let _cmdPaletteCursor = 0;

// Build candidate items each open — holdings/watchlist may have changed.
async function buildCommandPaletteItems() {
  const items = [];
  // Tabs.
  const tabs = [
    { id: 'summary',     label: 'Summary' },
    { id: 'stocks',      label: 'Stocks & ETFs' },
    { id: 'crypto',      label: 'Crypto' },
    { id: 'performance', label: 'Performance' },
    { id: 'screener',    label: 'Screener' },
    { id: 'watchlist',   label: 'Watchlist' },
    { id: 'heatmap',     label: 'Heatmap' },
    { id: 'news',        label: 'News' },
    { id: 'crypto-news', label: 'Crypto News' },
    { id: 'settings',    label: 'Settings' },
  ];
  for (const t of tabs) {
    items.push({
      kind: 'tab',
      label: 'Go to ' + t.label,
      hint: 'tab',
      sort: 'a' + t.id,
      action: () => switchTab(t.id),
    });
  }
  // Actions.
  items.push(
    { kind: 'action', label: 'Refresh market data',  hint: 'action', sort: 'b1', action: () => onRefresh() },
    { kind: 'action', label: 'Import xlsx master',   hint: 'action', sort: 'b2', action: () => openImportModal() },
    { kind: 'action', label: 'Export master xlsx',   hint: 'action', sort: 'b3', action: () => onExport() },
    { kind: 'action', label: 'Add stock',            hint: 'action', sort: 'b4', action: () => openHoldingModal({ kind: 'stock', mode: 'add' }) },
    { kind: 'action', label: 'Add crypto',           hint: 'action', sort: 'b5', action: () => openHoldingModal({ kind: 'crypto', mode: 'add' }) },
    { kind: 'action', label: 'Import historical transactions (CSV)', hint: 'action', sort: 'b6', action: () => openTxnImportModal() },
  );
  // Holdings — lazy-load if not in state.
  try {
    if (state.stocks == null) {
      const r = await api('/api/holdings/stocks');
      state.stocks = r.holdings || [];
    }
    if (state.crypto == null) {
      const r = await api('/api/holdings/crypto');
      state.crypto = r.holdings || [];
    }
  } catch (_) { /* tolerate */ }
  for (const h of (state.stocks || [])) {
    const tk = h.ticker || h.name;
    items.push({
      kind: 'holding',
      label: `${tk} — ${h.name}`,
      hint: 'stock',
      sort: 'c' + tk,
      action: () => openHoldingDetail('stock', h.id),
    });
  }
  for (const h of (state.crypto || [])) {
    items.push({
      kind: 'holding',
      label: `${h.symbol} — ${h.name}`,
      hint: 'crypto',
      sort: 'c' + h.symbol,
      action: () => openHoldingDetail('crypto', h.id),
    });
  }
  // Watchlist — lazy-load.
  try {
    if (state.watchlist == null) {
      const r = await api('/api/watchlist');
      state.watchlist = r.watchlist || [];
    }
  } catch (_) { /* tolerate */ }
  for (const w of (state.watchlist || [])) {
    items.push({
      kind: 'watchlist',
      label: `${w.ticker} — ${w.companyName || w.kind} (watchlist)`,
      hint: 'watchlist',
      sort: 'd' + w.ticker,
      action: () => { switchTab('watchlist'); /* future: scroll to row */ },
    });
  }
  items.sort((a, b) => a.sort.localeCompare(b.sort));
  return items;
}

// Subsequence fuzzy match: returns true if every char of q appears in s in
// order. Case-insensitive. Empty q → match everything.
function fuzzyMatch(q, s) {
  if (!q) return true;
  q = q.toLowerCase();
  s = s.toLowerCase();
  let qi = 0;
  for (let i = 0; i < s.length && qi < q.length; i++) {
    if (s[i] === q[qi]) qi++;
  }
  return qi === q.length;
}

function renderCommandPaletteList() {
  if (!_cmdPaletteEls) return;
  const html = _cmdPaletteFiltered.slice(0, 50).map((it, i) => `
    <li class="cp-item ${i === _cmdPaletteCursor ? 'active' : ''}" data-cp-idx="${i}">
      <span class="cp-label">${escapeHTML(it.label)}</span>
      <span class="cp-hint dim">${escapeHTML(it.hint)}</span>
    </li>
  `).join('') || `<li class="cp-empty dim">No matches</li>`;
  _cmdPaletteEls.list.innerHTML = html;
  // Click-to-activate.
  _cmdPaletteEls.list.querySelectorAll('[data-cp-idx]').forEach(el => {
    el.addEventListener('click', () => {
      const idx = parseInt(el.dataset.cpIdx, 10);
      activateCommandPaletteItem(idx);
    });
  });
}

function activateCommandPaletteItem(idx) {
  const it = _cmdPaletteFiltered[idx];
  if (!it) return;
  closeCommandPalette();
  try { it.action(); } catch (e) { console.warn('cmd palette action failed', e); }
}

async function openCommandPalette() {
  if (_cmdPaletteEls) return; // already open
  _cmdPaletteItems = await buildCommandPaletteItems();
  _cmdPaletteFiltered = _cmdPaletteItems;
  _cmdPaletteCursor = 0;
  const root = document.createElement('div');
  root.id = 'cmd-palette-root';
  root.innerHTML = `
    <div class="cp-overlay" id="cp-overlay">
      <div class="cp" role="dialog" aria-label="Command palette">
        <input id="cp-input" class="cp-input" type="text" placeholder="Search tabs, holdings, actions…  (Esc to close)" autocomplete="off" />
        <ul class="cp-list" id="cp-list"></ul>
        <div class="cp-foot dim">↑↓ navigate · Enter open · Esc close</div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  _cmdPaletteEls = {
    root,
    input: root.querySelector('#cp-input'),
    list:  root.querySelector('#cp-list'),
  };
  renderCommandPaletteList();
  _cmdPaletteEls.input.focus();
  _cmdPaletteEls.input.addEventListener('input', () => {
    const q = _cmdPaletteEls.input.value.trim();
    _cmdPaletteFiltered = _cmdPaletteItems.filter(it => fuzzyMatch(q, it.label));
    _cmdPaletteCursor = 0;
    renderCommandPaletteList();
  });
  _cmdPaletteEls.input.addEventListener('keydown', (ev) => {
    if (ev.key === 'ArrowDown') {
      ev.preventDefault();
      if (_cmdPaletteCursor < _cmdPaletteFiltered.length - 1) {
        _cmdPaletteCursor++;
        renderCommandPaletteList();
      }
    } else if (ev.key === 'ArrowUp') {
      ev.preventDefault();
      if (_cmdPaletteCursor > 0) {
        _cmdPaletteCursor--;
        renderCommandPaletteList();
      }
    } else if (ev.key === 'Enter') {
      ev.preventDefault();
      activateCommandPaletteItem(_cmdPaletteCursor);
    } else if (ev.key === 'Escape') {
      ev.preventDefault();
      closeCommandPalette();
    }
  });
  root.querySelector('#cp-overlay').addEventListener('click', (ev) => {
    if (ev.target.id === 'cp-overlay') closeCommandPalette();
  });
}

function closeCommandPalette() {
  if (!_cmdPaletteEls) return;
  _cmdPaletteEls.root.remove();
  _cmdPaletteEls = null;
}

// Global keyboard listener — installed once at boot.
function installCommandPaletteShortcut() {
  document.addEventListener('keydown', (ev) => {
    // Cmd/Ctrl+K toggles.
    if ((ev.metaKey || ev.ctrlKey) && (ev.key === 'k' || ev.key === 'K')) {
      ev.preventDefault();
      if (_cmdPaletteEls) closeCommandPalette();
      else openCommandPalette();
      return;
    }
    // "/" opens too — but only when nothing else has focus.
    if (ev.key === '/' && !_cmdPaletteEls) {
      const active = document.activeElement;
      const tag = active && active.tagName;
      const editable = active && (tag === 'INPUT' || tag === 'TEXTAREA' || active.isContentEditable);
      if (!editable) {
        ev.preventDefault();
        openCommandPalette();
      }
    }
  });
}

// ============================================================================
// Spec 9f — Sector Rotation tab + helpers shared with Stocks/Watchlist
// ============================================================================

// Spec 9f D6 — cache the sector_universe_id → {name, tag} lookup so the
// Stocks + Watchlist rows can show a small "Sector: ↑ Foo" pill without
// re-querying per render.
const _sectorTagCache = { byId: null, fetchedAt: 0 };

async function getSectorTagMap() {
  // 5min TTL — tags update with the 22:00 UTC daily ingest.
  if (_sectorTagCache.byId && (Date.now() - _sectorTagCache.fetchedAt) < 5 * 60 * 1000) {
    return _sectorTagCache.byId;
  }
  try {
    const r = await api('/api/sector-rotation/metrics');
    const m = new Map();
    for (const s of (r.sectors || [])) {
      m.set(s.sectorId, { name: s.displayName, tag: s.tag, code: s.code });
    }
    _sectorTagCache.byId = m;
    _sectorTagCache.fetchedAt = Date.now();
    return m;
  } catch (_) {
    return new Map();
  }
}

// Spec 9f D6 — render the sector-flow pill for one holding/watchlist row.
// Returns '' when sector is neutral / unlinked / no_data so we don't
// clutter the table.
function sectorFlowPill(sectorId, byId) {
  if (sectorId == null || !byId) return '';
  const s = byId.get(sectorId);
  if (!s || s.tag === 'neutral' || s.tag === 'no_data') return '';
  const arrow = s.tag === 'rotating_in' ? '↑' : '↓';
  const tone = s.tag === 'rotating_in' ? 'gain' : 'loss';
  return `<span class="sector-flow-pill ${tone}" title="Sector rotation tag">Sector: ${arrow} ${escapeHTML(s.name)}</span>`;
}



// Tag styling
function sectorTagPill(tag) {
  if (tag === 'rotating_in') return '<span class="tag-pill gain">↑ rotating in</span>';
  if (tag === 'rotating_out') return '<span class="tag-pill loss">↓ rotating out</span>';
  if (tag === 'no_data') return '<span class="tag-pill dim">no data</span>';
  return '<span class="tag-pill">neutral</span>';
}

// Inline sparkline SVG (26 weekly closes). Color tied to overall direction.
function sectorSparkSvg(points, tag) {
  if (!points || points.length < 2) return '<span class="dim">—</span>';
  const w = 80, h = 24;
  const min = Math.min(...points);
  const max = Math.max(...points);
  const rng = max - min || 1;
  const stepX = w / (points.length - 1);
  const coords = points.map((p, i) => {
    const x = i * stepX;
    const y = h - ((p - min) / rng) * h;
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(' ');
  const stroke = tag === 'rotating_in' ? 'rgb(16,200,124)' :
                 tag === 'rotating_out' ? 'rgb(245,80,110)' : 'rgb(150,155,165)';
  return `<svg class="sector-spark" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none"><polyline points="${coords}" fill="none" stroke="${stroke}" stroke-width="1.5"/></svg>`;
}

// Format pct return as +/−x.x%, "—" when nil.
function pctReturn(v, digits) {
  if (v == null) return '<span class="dim">—</span>';
  const x = v * 100;
  const cls = x > 0.5 ? 'gain' : x < -0.5 ? 'loss' : 'dim';
  return `<span class="${cls}">${x >= 0 ? '+' : ''}${x.toFixed(digits != null ? digits : 1)}%</span>`;
}

let _sectorSort = { key: 'return3m', dir: 'desc' };

async function renderSectorRotation() {
  const content = $('#content');
  content.innerHTML = '<div class="empty">loading sector rotation…</div>';

  let data, regime;
  try {
    [data, regime] = await Promise.all([
      api('/api/sector-rotation/metrics'),
      api('/api/regime').catch(() => null),
    ]);
  } catch (e) {
    content.innerHTML = `<div class="empty"><div class="loss">sector rotation failed: ${escapeHTML(e.message)}</div></div>`;
    return;
  }

  let sectors = data.sectors || [];
  const hasUserOrder = sectors.some(s => s.displayOrderUser != null);
  // If user has saved an order, server already sorted; otherwise apply
  // local column sort (defaults to 3M desc per spec).
  if (!hasUserOrder) {
    sectors = sortSectors(sectors, _sectorSort.key, _sectorSort.dir);
  }

  // Spec 9f D7 — Macro strip.
  const effective = regime?.effective || 'unclassified';
  const regimeTone = effective === 'stable' ? 'gain' : effective === 'shifting' ? 'amber-text' : effective === 'defensive' ? 'loss' : 'dim';
  const jordiRead = await loadJordiSectorRead();
  const macroStrip = `
    <div class="sector-macro-strip">
      <div class="sm-regime">
        <span class="dim">Regime:</span>
        <span class="${regimeTone}"><strong>${escapeHTML(effective.toUpperCase())}</strong></span>
      </div>
      <div class="sm-jordi">
        <span class="dim">Jordi's read:</span>
        <span class="sm-jordi-text" id="sm-jordi-text" contenteditable="true" spellcheck="true" title="Click to edit">${escapeHTML(jordiRead)}</span>
        <span class="dim sm-jordi-save" id="sm-jordi-save" style="display:none">(saving…)</span>
      </div>
    </div>
  `;

  // Sortable header helper.
  const hdr = (key, label, extra) => {
    const active = _sectorSort.key === key;
    const arrow = active ? (_sectorSort.dir === 'asc' ? ' ▲' : ' ▼') : '';
    const cls = ['sr-sortable'];
    if (active) cls.push('active');
    if (extra) cls.push(extra);
    const lockHint = hasUserOrder ? ' title="Manual order active — Reset to sort by columns"' : '';
    return `<th class="${cls.join(' ')}" data-sort-key="${key}"${lockHint}>${label}${arrow}</th>`;
  };

  // Build a map from sector code → scorecard.code so we know which 📋
  // button to render (jumps to existing scorecard or "Add scorecard").
  const scorecardMap = await loadScorecardCoverage();

  const rows = sectors.map((s) => {
    const sc = scorecardMap.get(s.code);
    const scBtn = sc
      ? `<button class="sr-sc-btn" title="View scorecard: ${escapeHTML(sc.displayName)}" data-sc-jump="${escapeHTML(sc.code)}">📋</button>`
      : `<button class="sr-sc-btn dim" title="No scorecard yet — opens Scorecards tab" data-sc-jump="">📋</button>`;
    return `
    <tr data-sector-id="${s.sectorId}">
      <td class="sr-handle" draggable="true" title="Drag to reorder">⋮⋮</td>
      <td class="sr-name">
        <div><strong>${escapeHTML(s.displayName)}</strong> ${scBtn}</div>
        <div class="dim" style="font-size:0.72rem">${escapeHTML(s.parentGics)} · ${escapeHTML(s.etfTickerPrimary)}${s.jordiStage != null ? ' · stage ' + s.jordiStage : ''}</div>
      </td>
      <td class="num">${pctReturn(s.return1w, 1)}</td>
      <td class="num">${pctReturn(s.return1m, 1)}</td>
      <td class="num"><strong>${s.return3m != null ? pctReturn(s.return3m, 1) : '<span class="dim">—</span>'}</strong></td>
      <td class="num">${pctReturn(s.return6m, 1)}</td>
      <td class="num">${pctReturn(s.returnYtd, 1)}</td>
      <td class="num">${s.rsVsSpy3m != null ? `<span class="${s.rsVsSpy3m >= 1.05 ? 'gain' : s.rsVsSpy3m <= 0.95 ? 'loss' : 'dim'}">${s.rsVsSpy3m.toFixed(2)}×</span>` : '<span class="dim">—</span>'}</td>
      <td>${sectorTagPill(s.tag)}</td>
      <td>${sectorSparkSvg(s.sparkline, s.tag)}</td>
      <td class="num">${(s.holdingsCount || 0) + (s.watchlistCount || 0) === 0
        ? '<span class="dim">—</span>'
        : `<span class="${s.holdingsCount > 0 ? '' : 'dim'}">${s.holdingsCount || 0}h${s.watchlistCount > 0 ? ` · ${s.watchlistCount}w` : ''}</span>`}</td>
    </tr>
  `;
  }).join('');

  // Reset-to-auto button only useful when user has an order saved.
  const resetBtn = hasUserOrder
    ? `<button class="btn-ghost" id="sr-reset">↻ Reset to auto-ranking</button>`
    : '';
  const saveBtn = `<button class="btn-ghost" id="sr-save" disabled>Save order</button>`;

  content.innerHTML = `
    ${macroStrip}
    <div class="table-toolbar">
      ${saveBtn}
      ${resetBtn}
      <span class="dim" id="sr-saved-stamp" style="margin-left:0.5rem;font-size:0.78rem">
        ${hasUserOrder ? 'Manual order active.' : 'Auto-ranking (3M descending). Drag rows to reorder.'}
      </span>
    </div>
    <div class="tablewrap">
      <table class="holdings sector-rotation">
        <thead>
          <tr>
            <th title="Drag to reorder"></th>
            <th>Sub-sector / GICS / ETF</th>
            ${hdr('return1w', '1W', 'num')}
            ${hdr('return1m', '1M', 'num')}
            ${hdr('return3m', '3M ★', 'num')}
            ${hdr('return6m', '6M', 'num')}
            ${hdr('returnYtd','YTD','num')}
            ${hdr('rsVsSpy3m','RS vs SPY (3M)','num')}
            <th title="rotating_in if RS ≥ 1.05; rotating_out if ≤ 0.95">Tag</th>
            <th title="26-week weekly closes">Spark</th>
            <th class="num" title="Holdings (h) + Watchlist (w) tagged to this sector">In portfolio</th>
          </tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
    <p class="dim" style="font-size:0.78rem; margin-top:1rem">
      <span style="color:rgb(var(--color-gain))">↑ rotating in</span> = sector's 3M return ≥ 1.05× SPY's.
      <span style="color:rgb(var(--color-loss))">↓ rotating out</span> = ≤ 0.95×.
      Daily ETF ingest runs at 22:00 UTC after US close.
    </p>
    <details class="sector-recent-reads" id="sr-recent-reads">
      <summary>Recent weekly reads</summary>
      <div id="sr-digest-list" class="dim">loading…</div>
    </details>
  `;

  // Spec 9g D4 — wire 📋 Scorecard buttons.
  for (const btn of content.querySelectorAll('.sr-sc-btn[data-sc-jump]')) {
    btn.addEventListener('click', (ev) => {
      ev.stopPropagation();
      const code = btn.dataset.scJump;
      if (code) state.selectedScorecard = code;
      switchTab('scorecards');
    });
  }

  // Wire column sort (only effective when no user order — otherwise
  // sorting fights the saved order).
  for (const th of content.querySelectorAll('.sr-sortable')) {
    th.addEventListener('click', () => {
      if (hasUserOrder) return; // disabled while manual order active
      const k = th.dataset.sortKey;
      if (_sectorSort.key === k) {
        _sectorSort.dir = _sectorSort.dir === 'asc' ? 'desc' : 'asc';
      } else {
        _sectorSort = { key: k, dir: 'desc' };
      }
      renderSectorRotation();
    });
  }

  // Spec 9f D7 — Jordi read editable.
  const jt = $('#sm-jordi-text');
  if (jt) {
    let saveTimer = null;
    jt.addEventListener('input', () => {
      if (saveTimer) clearTimeout(saveTimer);
      $('#sm-jordi-save').style.display = 'inline';
      saveTimer = setTimeout(async () => {
        try {
          await api('/api/preferences/jordi_current_sector_read', {
            method: 'PUT',
            body: JSON.stringify({ value: jt.textContent.trim() }),
          });
          $('#sm-jordi-save').textContent = '(saved)';
          setTimeout(() => { $('#sm-jordi-save').style.display = 'none'; $('#sm-jordi-save').textContent = '(saving…)'; }, 1200);
        } catch (e) {
          $('#sm-jordi-save').textContent = '(save failed)';
        }
      }, 900);
    });
  }

  // D5 — drag-and-drop reorder.
  installSectorDragReorder(content, hasUserOrder);

  // Reset button.
  $('#sr-reset')?.addEventListener('click', async () => {
    if (!confirm('Remove your custom order and revert to data-driven 3M-descending ranking?')) return;
    try {
      await api('/api/sector-rotation/ordering', { method: 'DELETE' });
      renderSectorRotation();
    } catch (e) { alert('Reset failed: ' + e.message); }
  });

  // Spec 9f D8 — populate Recent reads expander lazily on first open.
  const recentDetails = $('#sr-recent-reads');
  recentDetails?.addEventListener('toggle', async () => {
    if (!recentDetails.open) return;
    const list = $('#sr-digest-list');
    if (!list || list.dataset.loaded) return;
    list.dataset.loaded = '1';
    try {
      const r = await api('/api/sector-rotation/digests?limit=8');
      const digests = r.digests || [];
      if (digests.length === 0) {
        list.innerHTML = '<p class="dim">No digests yet. They write on Fridays at 22:00 UTC.</p>';
        return;
      }
      list.innerHTML = digests.map(d => `
        <div class="digest-card">
          <div class="digest-date">Week ending ${escapeHTML(d.weekEnding)}</div>
          ${escapeHTML(d.markdown)}
        </div>
      `).join('');
    } catch (e) {
      list.innerHTML = `<p class="loss">load failed: ${escapeHTML(e.message)}</p>`;
    }
  });
}

// D5 — drag-and-drop reorder. HTML5 native, no library.
function installSectorDragReorder(scope, hasUserOrder) {
  let dragging = null;
  const tbody = scope.querySelector('tbody');
  const saveBtn = scope.querySelector('#sr-save');
  let dirty = false;

  for (const handle of scope.querySelectorAll('.sr-handle')) {
    handle.addEventListener('dragstart', (ev) => {
      dragging = handle.closest('tr');
      dragging.classList.add('dragging');
      ev.dataTransfer.effectAllowed = 'move';
    });
    handle.addEventListener('dragend', () => {
      if (dragging) dragging.classList.remove('dragging');
      dragging = null;
    });
  }
  tbody.addEventListener('dragover', (ev) => {
    ev.preventDefault();
    const target = ev.target.closest('tr');
    if (!target || !dragging || target === dragging) return;
    const rect = target.getBoundingClientRect();
    const before = (ev.clientY - rect.top) < rect.height / 2;
    if (before) target.parentNode.insertBefore(dragging, target);
    else target.parentNode.insertBefore(dragging, target.nextSibling);
    dirty = true;
    if (saveBtn) saveBtn.disabled = false;
  });

  saveBtn?.addEventListener('click', async () => {
    if (!dirty) return;
    const pairs = [];
    let pos = 1;
    for (const tr of tbody.querySelectorAll('tr[data-sector-id]')) {
      pairs.push({ id: parseInt(tr.dataset.sectorId, 10), position: pos++ });
    }
    try {
      await api('/api/sector-rotation/ordering', {
        method: 'POST',
        body: JSON.stringify(pairs),
      });
      dirty = false;
      saveBtn.disabled = true;
      $('#sr-saved-stamp').textContent = `Saved ${new Date().toLocaleTimeString()} — manual order active.`;
      renderSectorRotation(); // re-render so reset button appears
    } catch (e) { alert('Save failed: ' + e.message); }
  });
}

function sortSectors(rows, key, dir) {
  const mult = dir === 'asc' ? 1 : -1;
  return rows.slice().sort((a, b) => {
    const av = a[key], bv = b[key];
    if (av == null && bv == null) return 0;
    if (av == null) return 1; // nils last
    if (bv == null) return -1;
    return (av - bv) * mult;
  });
}

// Spec 9g D4 — sector code → scorecard map. Cached for the page lifetime
// since scorecards rarely change. Returns Map<sectorCode, {code, displayName}>.
let _scorecardCoverageCache = null;
async function loadScorecardCoverage() {
  if (_scorecardCoverageCache) return _scorecardCoverageCache;
  const map = new Map();
  try {
    const r = await api('/api/scorecards');
    for (const sc of (r.scorecards || [])) {
      for (const code of (sc.appliesToSectors || [])) {
        if (!map.has(code)) map.set(code, { code: sc.code, displayName: sc.displayName });
      }
    }
  } catch (_) { /* empty map */ }
  _scorecardCoverageCache = map;
  return map;
}

async function loadJordiSectorRead() {
  try {
    const r = await api('/api/preferences/jordi_current_sector_read');
    return r.value || '';
  } catch (_) {
    return '';
  }
}

// ============================================================================
// Spec 9g — Scorecard Repository tab
// ============================================================================
//
// Two-pane layout: scorecard list left, viewer/editor right. URL hash (or
// state.selectedScorecard) determines which scorecard is open. Linked nav
// from the Sector Rotation tab routes here with a code preselected.

if (state.selectedScorecard == null) state.selectedScorecard = null;
let _scorecardListCache = null;

async function renderScorecards() {
  const content = $('#content');
  content.innerHTML = '<div class="empty">loading scorecards…</div>';

  let list;
  try {
    const r = await api('/api/scorecards');
    list = r.scorecards || [];
    _scorecardListCache = list;
  } catch (e) {
    content.innerHTML = `<div class="empty"><div class="loss">scorecards failed: ${escapeHTML(e.message)}</div></div>`;
    return;
  }

  // Pre-select: state.selectedScorecard → first locked → first row.
  let selected = state.selectedScorecard
    || list.find(s => s.status === 'locked' && !s.isDoctrine)?.code
    || list.find(s => s.status === 'locked')?.code
    || list[0]?.code
    || null;
  state.selectedScorecard = selected;

  const statusIcon = (s) => {
    if (s.isDoctrine) return '📖';
    if (s.status === 'locked') return '🔒';
    if (s.status === 'needs-review') return '🔄';
    return '⚠';
  };
  const statusLabel = (s) => {
    if (s.isDoctrine) return 'doctrine';
    return s.status;
  };

  const leftPane = list.map(s => {
    const cls = ['sc-row'];
    if (s.code === selected) cls.push('active');
    const hcount = s.holdingsCount || 0;
    return `
      <div class="${cls.join(' ')}" data-sc-code="${escapeHTML(s.code)}" tabindex="0">
        <div class="sc-row-title">${escapeHTML(s.displayName)}</div>
        <div class="sc-row-meta">
          <span class="sc-version">v${escapeHTML(s.currentVersion)}</span>
          <span class="sc-status">${statusIcon(s)} ${escapeHTML(statusLabel(s))}</span>
          ${hcount > 0 ? `<span class="sc-holdings">${hcount} holding${hcount === 1 ? '' : 's'}</span>` : ''}
        </div>
      </div>
    `;
  }).join('');

  content.innerHTML = `
    <div class="scorecards-layout">
      <aside class="sc-left">
        <h3 class="sc-pane-head">Scorecards <span class="dim" style="font-size:0.72rem; font-weight:normal">(${list.length})</span></h3>
        <div class="sc-list">${leftPane || '<p class="dim">No scorecards seeded.</p>'}</div>
      </aside>
      <section class="sc-right" id="sc-right">
        <div class="empty">loading…</div>
      </section>
    </div>
  `;

  // Wire left-pane clicks.
  for (const row of content.querySelectorAll('.sc-row[data-sc-code]')) {
    const open = () => {
      state.selectedScorecard = row.dataset.scCode;
      renderScorecards();
    };
    row.addEventListener('click', open);
    row.addEventListener('keydown', (ev) => {
      if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); open(); }
    });
  }

  if (selected) {
    await loadScorecardRightPane(selected);
  }
}

async function loadScorecardRightPane(code) {
  const right = $('#sc-right');
  if (!right) return;
  right.innerHTML = '<div class="empty">loading…</div>';
  let data;
  try {
    data = await api(`/api/scorecards/${encodeURIComponent(code)}`);
  } catch (e) {
    right.innerHTML = `<div class="empty"><div class="loss">load failed: ${escapeHTML(e.message)}</div></div>`;
    return;
  }
  const sc = data.scorecard;
  const html = data.html;

  // Holdings-using-this-adapter section (9g D5).
  let holdingsBlock = '';
  if (sc.appliesToSectors && sc.appliesToSectors.length > 0) {
    try {
      const r = await api('/api/holdings/stocks');
      const sectorMap = await getSectorTagMap();
      const idsByCode = new Map();
      for (const [id, info] of sectorMap.entries()) idsByCode.set(info.code, id);
      const matchedSectorIds = new Set(sc.appliesToSectors.map(c => idsByCode.get(c)).filter(Boolean));
      const matched = (r.holdings || []).filter(h => h.sectorUniverseId != null && matchedSectorIds.has(h.sectorUniverseId));
      if (matched.length > 0) {
        const chips = matched.map(h => `
          <a class="sc-holding-chip" href="#" data-sc-jump-holding-kind="stock" data-sc-jump-holding-id="${h.id}">
            ${escapeHTML(h.ticker || h.name)} <span class="dim">${escapeHTML(h.name)}</span>
          </a>
        `).join('');
        holdingsBlock = `
          <div class="sc-holdings-block">
            <h4>Holdings using this adapter</h4>
            <div class="sc-chip-row">${chips}</div>
          </div>
        `;
      } else {
        holdingsBlock = `
          <div class="sc-holdings-block">
            <h4>Holdings using this adapter</h4>
            <p class="dim">No current holdings mapped to this adapter.</p>
          </div>
        `;
      }
    } catch (_) { /* silent */ }
  }

  const editBtn = sc.isDoctrine
    ? '<span class="sc-doctrine-tag">📖 doctrine — read-only</span>'
    : `<button class="btn-ghost" id="sc-edit-btn">✎ Edit</button>`;
  const versionsBtn = `<button class="btn-ghost" id="sc-versions-btn">View versions</button>`;
  const statusBtns = sc.isDoctrine ? '' : `
    <span class="dim" style="margin:0 0.4rem">·</span>
    <span class="dim" style="font-size:0.78rem">Mark as</span>
    ${['draft','locked','needs-review'].filter(x => x !== sc.status).map(x =>
      `<button class="btn-ghost btn-mini" data-sc-status="${x}">${x}</button>`
    ).join('')}
  `;

  right.innerHTML = `
    <div class="sc-viewer-head">
      <div>
        <h2 class="sc-title">${escapeHTML(sc.displayName)}</h2>
        <div class="dim">${escapeHTML(sc.shortDescription)}</div>
        <div class="dim" style="font-size:0.78rem; margin-top:0.3rem">
          v${escapeHTML(sc.currentVersion)} · status: <strong>${escapeHTML(sc.status)}</strong> · updated ${new Date(sc.updatedAt).toLocaleDateString()}
        </div>
      </div>
      <div class="sc-actions">${editBtn} ${versionsBtn}${statusBtns}</div>
    </div>
    <div class="sc-md-rendered" id="sc-md-rendered">${html}</div>
    ${holdingsBlock}
  `;

  // Wire actions.
  if (!sc.isDoctrine) {
    $('#sc-edit-btn')?.addEventListener('click', () => openScorecardEditor(sc));
    for (const btn of right.querySelectorAll('[data-sc-status]')) {
      btn.addEventListener('click', async () => {
        try {
          await api(`/api/scorecards/${encodeURIComponent(sc.code)}/status`, {
            method: 'PUT',
            body: JSON.stringify({ status: btn.dataset.scStatus }),
          });
          renderScorecards();
        } catch (e) { alert('Status update failed: ' + e.message); }
      });
    }
  }
  $('#sc-versions-btn')?.addEventListener('click', () => openScorecardVersions(sc.code));
  // Holding chips — open detail page.
  for (const ch of right.querySelectorAll('[data-sc-jump-holding-kind]')) {
    ch.addEventListener('click', (ev) => {
      ev.preventDefault();
      openHoldingDetail(ch.dataset.scJumpHoldingKind, parseInt(ch.dataset.scJumpHoldingId, 10));
    });
  }
}

// Edit mode: textarea + live preview split view.
function openScorecardEditor(sc) {
  closeImportModal();
  const root = document.createElement('div');
  root.id = 'modal-root';
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal sc-editor-modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">Edit — ${escapeHTML(sc.displayName)}</div>
            <div class="desc">Current v${escapeHTML(sc.currentVersion)}. Save (no bump) or Save as new version.</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body sc-editor-body">
          <div class="sc-editor-split">
            <textarea id="sc-md-text" class="sc-md-textarea" spellcheck="false">${escapeHTML(sc.markdownCurrent)}</textarea>
            <div id="sc-md-preview" class="sc-md-preview"></div>
          </div>
          <div class="error" id="sc-edit-err"></div>
        </div>
        <div class="modal-foot">
          <button class="btn-secondary" id="sc-cancel">Cancel</button>
          <button class="btn-ghost" id="sc-save-version">Save as new version…</button>
          <button class="btn-primary" id="sc-save">Save</button>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#sc-cancel').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });

  const ta = $('#sc-md-text');
  const pv = $('#sc-md-preview');
  let previewTimer = null;
  const refreshPreview = async () => {
    try {
      const res = await fetch('/api/scorecards/preview', {
        method: 'POST',
        headers: { 'Content-Type': 'text/plain' },
        body: ta.value,
      });
      pv.innerHTML = await res.text();
    } catch (_) { /* keep last preview */ }
  };
  refreshPreview();
  ta.addEventListener('input', () => {
    if (previewTimer) clearTimeout(previewTimer);
    previewTimer = setTimeout(refreshPreview, 300);
  });

  $('#sc-save').addEventListener('click', async () => {
    const err = $('#sc-edit-err'); err.textContent = '';
    try {
      await api(`/api/scorecards/${encodeURIComponent(sc.code)}`, {
        method: 'PUT',
        body: JSON.stringify({ markdown: ta.value }),
      });
      closeImportModal();
      renderScorecards();
    } catch (e) { err.textContent = e.message; }
  });
  $('#sc-save-version').addEventListener('click', async () => {
    const err = $('#sc-edit-err'); err.textContent = '';
    const version = prompt('New version string?', '');
    if (!version) return;
    const note = prompt('Changelog note (one line)?', '') || '';
    try {
      await api(`/api/scorecards/${encodeURIComponent(sc.code)}`, {
        method: 'PUT',
        body: JSON.stringify({
          markdown: ta.value,
          asNewVersion: { version, changelogNote: note },
        }),
      });
      closeImportModal();
      renderScorecards();
    } catch (e) { err.textContent = e.message; }
  });
}

async function openScorecardVersions(code) {
  closeImportModal();
  const root = document.createElement('div');
  root.id = 'modal-root';
  root.innerHTML = `
    <div class="modal-overlay" id="modal-overlay">
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-header">
          <div>
            <div class="title">Version history — ${escapeHTML(code)}</div>
          </div>
          <button class="modal-close" id="modal-close" aria-label="Close">×</button>
        </div>
        <div class="modal-body">
          <div id="sc-versions-list">loading…</div>
        </div>
      </div>
    </div>
  `;
  document.body.appendChild(root);
  $('#modal-close').addEventListener('click', closeImportModal);
  $('#modal-overlay').addEventListener('click', (ev) => { if (ev.target.id === 'modal-overlay') closeImportModal(); });
  try {
    const r = await api(`/api/scorecards/${encodeURIComponent(code)}/versions`);
    const versions = r.versions || [];
    if (versions.length === 0) {
      $('#sc-versions-list').innerHTML = '<p class="dim">No version history yet.</p>';
      return;
    }
    $('#sc-versions-list').innerHTML = versions.map(v => `
      <div class="sc-version-row">
        <div><strong>v${escapeHTML(v.version)}</strong> <span class="dim">${new Date(v.createdAt).toLocaleString()}</span></div>
        ${v.changelogNote ? `<div class="dim" style="font-size:0.85rem">${escapeHTML(v.changelogNote)}</div>` : ''}
      </div>
    `).join('');
  } catch (e) {
    $('#sc-versions-list').innerHTML = `<p class="loss">load failed: ${escapeHTML(e.message)}</p>`;
  }
}

installCommandPaletteShortcut();

// Spec 12 D8 — Notes-as-hover-bubbles popover. Single floating element
// reused across Stocks / Crypto / Watchlist tables. Delegated listeners so
// per-render wiring isn't needed.
let _noteBubbleEl = null;
function ensureNoteBubble() {
  if (_noteBubbleEl) return _noteBubbleEl;
  _noteBubbleEl = document.createElement('div');
  _noteBubbleEl.className = 'note-bubble-popover';
  _noteBubbleEl.setAttribute('aria-hidden', 'true');
  document.body.appendChild(_noteBubbleEl);
  return _noteBubbleEl;
}
function showNoteBubble(el) {
  const text = el.dataset.note;
  if (!text) return;
  const pop = ensureNoteBubble();
  pop.textContent = text;
  const r = el.getBoundingClientRect();
  pop.style.display = 'block';
  // Position above the bubble, fall back to below if no room.
  const popH = pop.offsetHeight;
  const top = r.top - popH - 8 > 0 ? (r.top - popH - 8) : (r.bottom + 8);
  let left = r.left;
  // Keep it inside the viewport horizontally.
  const popW = pop.offsetWidth;
  if (left + popW > window.innerWidth - 12) left = window.innerWidth - popW - 12;
  if (left < 8) left = 8;
  pop.style.top = `${top + window.scrollY}px`;
  pop.style.left = `${left + window.scrollX}px`;
  pop.setAttribute('aria-hidden', 'false');
}
function hideNoteBubble() {
  if (!_noteBubbleEl) return;
  _noteBubbleEl.style.display = 'none';
  _noteBubbleEl.setAttribute('aria-hidden', 'true');
}
document.addEventListener('mouseover', (ev) => {
  const t = ev.target.closest('.note-bubble');
  if (t) showNoteBubble(t);
});
document.addEventListener('mouseout', (ev) => {
  const t = ev.target.closest('.note-bubble');
  if (t) hideNoteBubble();
});
document.addEventListener('focusin', (ev) => {
  const t = ev.target.closest('.note-bubble');
  if (t) showNoteBubble(t);
});
document.addEventListener('focusout', (ev) => {
  const t = ev.target.closest('.note-bubble');
  if (t) hideNoteBubble();
});

boot();
