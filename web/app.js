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
function scoreCell(score) {
  if (!score) return '<span class="dim">—</span>';
  const stale = score.staleDays > 90;
  const cls = ['score-badge'];
  cls.push(score.passes ? 'pass' : 'fail');
  if (stale) cls.push('stale');
  const tickMark = score.passes ? ' ✓' : '';
  const staleMark = stale ? ` ⚠ ${score.staleDays}d` : '';
  return `<span class="${cls.join(' ')}" title="Scored ${score.scoredAt.slice(0,10)} · framework: ${escapeHTML(score.frameworkId)}">${score.totalScore}/${score.maxScore}${tickMark}${staleMark}</span>`;
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
          <span>${escapeHTML(user.displayName || user.email)}</span>
          <button class="btn-ghost" id="logout">sign out</button>
        </div>
      </div>
      <div class="tabbar">
        <button class="tab ${state.tab === 'summary' ? 'active' : ''}" data-tab="summary">Summary</button>
        <button class="tab ${state.tab === 'stocks' ? 'active' : ''}" data-tab="stocks">Stocks &amp; ETFs</button>
        <button class="tab ${state.tab === 'crypto' ? 'active' : ''}" data-tab="crypto">Crypto</button>
        <button class="tab ${state.tab === 'screener' ? 'active' : ''}" data-tab="screener">Screener</button>
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
  if (!el || !marketState || !marketState.summary) return;
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
  const rows = marketState.exchanges.map((e) => {
    const dot = e.open ? '🟢' : e.onBreak ? '🟡' : '🔴';
    const label = e.open ? 'Open' : e.onBreak ? 'Break' : 'Closed';
    const verb  = e.nextChangeKind === 'close' ? 'closes' :
                  e.nextChangeKind === 'break_start' ? 'breaks' :
                  e.nextChangeKind === 'break_end' ? 'resumes' : 'opens';
    const when = formatLocalTimeShort(e.nextChange);
    const dur  = formatCountdown(e.nextChange);
    return `
      <div class="md-row${e.open ? ' open' : e.onBreak ? ' break' : ''}">
        <span class="md-dot">${dot}</span>
        <span class="md-name">${escapeHTML(e.name)}</span>
        <span class="md-status">${escapeHTML(label)}</span>
        <span class="md-when dim">${escapeHTML(verb)} ${escapeHTML(when)} <span class="num">(${escapeHTML(dur)})</span></span>
      </div>
    `;
  }).join('');
  dd.innerHTML = `<div class="md-head">Markets</div>${rows}`;
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

      const meta = (p.schemaVersion != null || p.fxSnapshotEurUsd != null) ? `
        <div class="dim" style="font-size:0.7rem; letter-spacing:0.12em; text-transform:uppercase; margin-top:0.8rem">
          ${p.schemaVersion != null ? `schema v${p.schemaVersion}` : ''}
          ${p.fxSnapshotEurUsd != null ? ` · fx eur→usd ${Number(p.fxSnapshotEurUsd).toFixed(4)}` : ''}
        </div>
      ` : '';

      body.innerHTML = sections.join('') + warnPanel + meta;

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
  for (const el of document.querySelectorAll('.tab')) {
    el.classList.toggle('active', el.dataset.tab === tab);
  }
  loadActiveTab();
}

async function loadActiveTab() {
  const content = $('#content');
  content.innerHTML = '<div class="empty">loading…</div>';
  try {
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
    } else if (state.tab === 'screener') {
      await renderScreener();
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

  const kpiRow = `
    <div class="kpi-row">
      ${kpiCard('Total Value',     valueStr,                          `invested ${investedStr}`,         '',         'kpi-total-value', k.totalValue)}
      ${kpiCard('Total P&amp;L',   pnlStr,                            pnlPctStr,                          pnlTone,    'kpi-total-pnl',   k.totalPnl)}
      ${kpiCard('Today\'s Change', todayStr,                          todayPctStr,                        todayTone,  'kpi-today',       k.todayChange)}
      ${kpiCard('Cash',            '<span class="dim">—</span>',      '<span class="dim">unset</span>',   'dim',      null,              null)}
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

  content.innerHTML = staleBanner + toggle + kpiRow + donutRow + tagDonuts + footer;

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
  let unscored = 0;
  let stale = 0;
  for (const h of [...(stocks || []), ...(crypto || [])]) {
    if (!h.score) unscored++;
    else if (h.score.staleDays > 90) stale++;
  }
  const total = unscored + stale;
  if (total === 0) {
    el.innerHTML = '';
    return;
  }
  const parts = [];
  if (unscored) parts.push(`${unscored} unscored`);
  if (stale) parts.push(`${stale} stale (>90d)`);
  el.innerHTML = `
    <div class="stale-banner">
      ⚠ <strong>${total}</strong> holding${total === 1 ? '' : 's'} need${total === 1 ? 's' : ''} framework scoring — ${parts.join(', ')}.
      <span class="dim">Open Stocks or Crypto tab and click the score badge to update.</span>
    </div>
  `;
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
      state.heatmapMode = r.value === 'my_holdings' ? 'my_holdings' : 'market_cap';
    } catch (_) { state.heatmapMode = 'market_cap'; }
  }
  const mode = state.heatmapMode;

  const legendStops = [-3, -2, -1, 0, 1, 2, 3];

  const sectorOptions = ['', ...SECTORS]
    .map((s) => `<option value="${escapeHTML(s)}" ${s === state.heatmapSector ? 'selected' : ''}>${s === '' ? 'All sectors' : escapeHTML(s)}</option>`)
    .join('');

  const modeOptions = [
    { v: 'market_cap', l: 'Market cap (S&P 500)' },
    { v: 'my_holdings', l: 'My holdings' },
  ].map(o => `<option value="${o.v}" ${o.v === mode ? 'selected' : ''}>${o.l}</option>`).join('');

  const caption = mode === 'my_holdings'
    ? 'Your portfolio · sized by position value · colored by daily change'
    : 'S&P 500 · sized by market cap · colored by daily change';

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
      ${mode === 'my_holdings'
        ? 'Only your active stock holdings are shown. Empty sectors hidden.'
        : 'Live prices populated on each refresh; tiles update silently when the background scheduler runs.'}
    </div>`;

  $('#heatmap-mode').addEventListener('change', async (ev) => {
    const v = ev.target.value === 'my_holdings' ? 'my_holdings' : 'market_cap';
    state.heatmapMode = v;
    try {
      await api('/api/preferences/heatmap_mode', {
        method: 'PUT',
        body: JSON.stringify({ value: v }),
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

  const fgChip = scope === 'crypto' ? `<div id="fg-chip" class="fg-chip">fear &amp; greed: loading…</div>` : '';

  if (articles.length === 0) {
    content.innerHTML = filterToggle + macroHTML + banner + fgChip +
      `<div class="empty">${filterMode === 'mine' ? 'No matching articles for your holdings + watchlist.' : 'No articles to show.'}</div>`;
    wireNewsToggle(scope);
    return;
  } else {
    const list = articles.map((a) => {
      const sentClass = a.sentiment === 'positive' ? 'gain' : a.sentiment === 'negative' ? 'loss' : 'dim';
      const time = a.publishedAt ? new Date(a.publishedAt).toLocaleString() : '';
      return `
        <article class="news-item">
          <div class="news-meta">
            <span class="news-source">${escapeHTML(a.source || 'unknown')}</span>
            <span class="news-time">${escapeHTML(time)}</span>
            ${a.sentiment ? `<span class="news-sent ${sentClass}">${a.sentiment}</span>` : ''}
          </div>
          <a class="news-title" href="${escapeHTML(a.url)}" target="_blank" rel="noopener noreferrer">${escapeHTML(a.title)}</a>
          ${a.summary ? `<p class="news-summary">${escapeHTML(a.summary)}</p>` : ''}
        </article>
      `;
    }).join('');
    content.innerHTML = filterToggle + macroHTML + banner + fgChip + `<div class="news-list">${list}</div>`;
  }

  wireNewsToggle(scope);
  if (scope === 'crypto') loadFearGreed();
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

async function loadFearGreed() {
  const el = $('#fg-chip');
  if (!el) return;
  try {
    const fg = await api('/api/feargreed');
    if (fg.value == null) {
      el.textContent = 'fear & greed: unavailable';
      el.classList.add('dim');
      return;
    }
    const v = fg.value;
    const tone = v >= 75 ? 'gain' : v >= 55 ? 'gain dim' : v >= 45 ? 'dim' : v >= 25 ? 'loss dim' : 'loss';
    el.innerHTML = `fear &amp; greed: <span class="${tone}" style="font-weight:600">${v}</span> · ${escapeHTML(fg.classification || '')}`;
  } catch (_) {
    el.textContent = 'fear & greed: error';
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

function renderStocks() {
  const rows = state.stocks;
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

  // Table
  const body = rows.map((r) => {
    const m = r.metrics;
    const a = r.alert || { status: 'neutral', triggers: [] };
    const badge = `
      <span class="alert-badge ${a.status}" title="${escapeHTML(a.triggers.join(' · ') || 'no triggers')}">
        <span class="dot"></span>${a.status}
      </span>
    `;
    const noteCell = r.note
      ? `<td class="note-cell" title="${escapeHTML(r.note)}">${escapeHTML(r.note.length > 30 ? r.note.slice(0, 28) + '…' : r.note)}</td>`
      : `<td class="dim">—</td>`;
    // SL/TP suggestion comparison: arrow only when manual is set and the
    // direction is clear. ↑ means manual SL is tighter (less drawdown allowed),
    // ↓ means manual SL is looser (more drawdown allowed). Both render in cell.
    const slDelta = suggestedDelta(r.avgOpenPrice, r.stopLoss, r.suggestedSlPct, 'sl');
    const tpDelta = suggestedDelta(r.avgOpenPrice, r.takeProfit, r.suggestedTpPct, 'tp');
    const sparkSvg = r.sparklineSvg || '<span class="sparkline-empty">—</span>';
    const tickerCell = `<span class="ticker-hover" data-row-id="${r.id}" data-row-kind="stock" tabindex="0">${escapeHTML(r.ticker || '—')}</span>`;
    return `
      <tr data-row-id="${r.id}" data-row-kind="stock">
        <td>${badge}</td>
        <td>
          <div>${escapeHTML(r.name)}</div>
          <div class="ticker">${tickerCell}${r.category ? ' · <span class="dim">' + escapeHTML(r.category) + '</span>' : ''}</div>
        </td>
        <td class="num">${fmtUSD.format(r.investedUsd)}</td>
        <td class="num">${dash(r.avgOpenPrice, fmtNum2)}</td>
        <td class="num" data-flash-id="stock-${r.id}-price" data-flash-value="${r.currentPrice ?? ''}">${dash(r.currentPrice, fmtNum2)}</td>
        <td class="num" data-flash-id="stock-${r.id}-pnl" data-flash-value="${m.pnlUsd ?? ''}">${dashSigned(m.pnlUsd, fmtNum2, '$')}</td>
        <td class="num">${pct(m.pnlPct, 2)}</td>
        <td class="num">${dash(r.rsi14, fmtNum2)}</td>
        <td class="num">${dash(r.stopLoss, fmtNum2)}</td>
        <td class="num">${pct(m.distanceToSlPct, 1)}</td>
        <td class="num suggested" title="${slDelta.title}">${fmtNum1.format(r.suggestedSlPct)}%${slDelta.icon}</td>
        <td class="num suggested" title="${tpDelta.title}">${fmtNum1.format(r.suggestedTpPct)}%${tpDelta.icon}</td>
        <td>${earningsCell(r.earningsDate)}</td>
        <td>${exDivCell(r.exDividendDate)}</td>
        <td class="sparkline-cell">${sparkSvg}</td>
        <td>${scoreCell(r.score)}</td>
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
            <th class="num">P&amp;L $</th>
            <th class="num">P&amp;L %</th>
            <th class="num">RSI(14)</th>
            <th class="num">Stop Loss</th>
            <th class="num">Dist to SL</th>
            <th class="num" title="Suggested stop-loss % per beta">Sug SL</th>
            <th class="num" title="Suggested take-profit % per beta">Sug TP</th>
            <th title="Next earnings">Earn</th>
            <th title="Next ex-dividend">Ex-Div</th>
            <th>30-day</th>
            <th title="Latest framework score">Score</th>
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

function renderCrypto() {
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

  const body = rows.map((r) => {
    const m = r.metrics;
    const noteCell = r.note
      ? `<td class="note-cell" title="${escapeHTML(r.note)}">${escapeHTML(r.note.length > 30 ? r.note.slice(0, 28) + '…' : r.note)}</td>`
      : `<td class="dim">—</td>`;
    const sparkSvg = r.sparklineSvg || '<span class="sparkline-empty">—</span>';
    const symbolCell = `<span class="ticker-hover" data-row-id="${r.id}" data-row-kind="crypto" tabindex="0">${escapeHTML(r.symbol)}</span>`;
    return `
      <tr data-row-id="${r.id}" data-row-kind="crypto">
        <td>
          <div>${escapeHTML(r.name)} ${symbolCell}</div>
          <div class="ticker">${r.category ? escapeHTML(r.category) : '—'}${r.wallet ? ' · <span class="dim">' + escapeHTML(r.wallet) + '</span>' : ''}</div>
        </td>
        <td><span class="tag ${r.classification === 'core' ? 'core' : ''}">${escapeHTML(r.classification)}</span></td>
        <td class="num">${fmtNum6.format(m.totalQuantity)}</td>
        <td class="num" data-flash-id="crypto-${r.id}-price" data-flash-value="${r.currentPriceUsd ?? ''}">${dash(r.currentPriceUsd, fmtNum4)}</td>
        <td class="num">${dash(r.costBasisUsd, fmtNum2)}</td>
        <td class="num" data-flash-id="crypto-${r.id}-value" data-flash-value="${m.currentValueUsd ?? ''}">${dash(m.currentValueUsd, fmtNum2)}</td>
        <td class="num" data-flash-id="crypto-${r.id}-pnl" data-flash-value="${m.pnlUsd ?? ''}">${dashSigned(m.pnlUsd, fmtNum2, '$')}</td>
        <td class="num">${pct(m.pnlPct, 2)}</td>
        <td class="num">${pct(r.change7dPct, 1)}</td>
        <td class="num">${pct(r.change30dPct, 1)}</td>
        <td class="num suggested" title="Suggested SL for ${escapeHTML(r.volTier || 'medium')}-vol">${fmtNum1.format(r.suggestedSlPct)}%</td>
        <td class="num suggested" title="Suggested TP for ${escapeHTML(r.volTier || 'medium')}-vol">${fmtNum1.format(r.suggestedTpPct)}%</td>
        <td class="sparkline-cell">${sparkSvg}</td>
        <td>${scoreCell(r.score)}</td>
        <td class="market-cell"><span class="dim" title="Crypto trades 24/7">—</span></td>
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
            <th>Class</th>
            <th class="num">Qty</th>
            <th class="num">Price $</th>
            <th class="num">Cost $</th>
            <th class="num">Value $</th>
            <th class="num">P&amp;L $</th>
            <th class="num">P&amp;L %</th>
            <th class="num">7d %</th>
            <th class="num">30d %</th>
            <th class="num" title="Suggested stop-loss % per vol tier">Sug SL</th>
            <th class="num" title="Suggested take-profit % per vol tier">Sug TP</th>
            <th>30-day</th>
            <th title="Latest framework score">Score</th>
            <th title="Crypto trades 24/7">Market</th>
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
  { name: 'strategyNote', label: 'Strategy note',  type: 'textarea' },
  { name: 'note',         label: 'Note',           type: 'textarea' },
];
const cryptoFields = [
  { name: 'name',           label: 'Name',                 type: 'text',     required: true },
  { name: 'symbol',         label: 'Symbol',               type: 'text',     required: true },
  { name: 'classification', label: 'Classification',       type: 'select', options: ['core', 'alt'] },
  { name: 'volTier',        label: 'Volatility tier',      type: 'select', options: ['low','medium','high','extreme'] },
  { name: 'category',       label: 'Category',             type: 'text' },
  { name: 'wallet',         label: 'Wallet',               type: 'text' },
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

  const reasonField = isEdit ? `
    <div class="form-row">
      <label for="hm-reason">Reason (optional)</label>
      <input id="hm-reason" name="reason" type="text" placeholder="e.g. moved stop after technical break" />
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
        v = null;
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
  // Update reason
  if (mode === 'edit') {
    const reasonEl = form.querySelector('[name="reason"]');
    if (reasonEl && reasonEl.value.trim()) {
      body.reason = reasonEl.value.trim();
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

  const [delStocks, delCrypto, audit, regimeHist, risk, llmSpend] = await Promise.all([
    api('/api/holdings/stocks/deleted').catch(() => ({ holdings: [] })),
    api('/api/holdings/crypto/deleted').catch(() => ({ holdings: [] })),
    api('/api/audit?limit=100').catch(() => ({ audit: [] })),
    api('/api/regime/history?limit=50').catch(() => ({ history: [] })),
    api('/api/risk/dashboard').catch(() => null),
    api('/api/llm/spend').catch(() => null),
  ]);

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
    return `
      <tr>
        <td class="num dim">${escapeHTML(new Date(a.ts).toLocaleString())}</td>
        <td>${escapeHTML(a.action)}</td>
        <td>${escapeHTML(a.holdingKind)}</td>
        <td class="ticker">${escapeHTML(tickerOrSymbol)}</td>
        <td class="dim" style="font-size:0.78rem">${escapeHTML(changesPreview)}</td>
        <td class="dim" style="font-size:0.78rem">${escapeHTML(a.reason || '')}</td>
      </tr>
    `;
  }).join('') || `<tr><td colspan="6" class="dim" style="text-align:center; padding:0.7rem">No audit entries yet. Audit log fills as you create / edit / delete / restore holdings.</td></tr>`;

  content.innerHTML = `
    <h2 class="settings-h">Settings</h2>

    ${portfolioRiskHTML}
    ${llmSpendHTML}
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

async function renderWatchlist() {
  const res = await api('/api/watchlist');
  state.watchlist = res.watchlist || [];

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

  const rows = state.watchlist.map((e) => {
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
      ? `<span class="note-cell" title="${escapeHTML(e.note)}">${escapeHTML(e.note.length > 30 ? e.note.slice(0, 28) + '…' : e.note)}</span>`
      : '<span class="dim">—</span>';
    return `
      <tr data-wid="${e.id}">
        <td><strong>${escapeHTML(e.ticker)}</strong></td>
        <td>${escapeHTML(e.companyName || '—')}</td>
        <td><span class="dim">${escapeHTML(e.sector || '—')}</span></td>
        <td class="num">${dash(e.currentPrice, fmtNum2)}</td>
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

  $('#content').innerHTML = `
    <div class="table-toolbar">
      <button class="btn-ghost" id="add-watchlist-stock">+ Add stock</button>
      <button class="btn-ghost" id="add-watchlist-crypto">+ Add crypto</button>
    </div>
    <div class="tablewrap">
      <table class="holdings watchlist">
        <thead>
          <tr>
            <th>Ticker</th>
            <th>Company</th>
            <th>Sector</th>
            <th class="num">Price</th>
            <th class="num">Target</th>
            <th>Distance</th>
            <th>Score</th>
            <th>Tag</th>
            <th>Added</th>
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

async function openScoreScreen(wid) {
  const entry = (state.watchlist || []).find((e) => e.id === wid);
  if (!entry) return;
  // Load the framework definition + any existing latest score in parallel.
  const fwID = entry.kind === 'stock' ? 'jordi' : 'cowen';
  let fw, prior;
  try {
    [fw, prior] = await Promise.all([
      api(`/api/frameworks/${fwID}`),
      api(`/api/scores?targetKind=watchlist&targetId=${entry.id}`),
    ]);
  } catch (e) {
    alert('couldn\'t load framework: ' + e.message);
    return;
  }
  const priorScores = (prior.score && prior.score.scoresJson) ? JSON.parse(prior.score.scoresJson) : {};
  const priorTags = (prior.score && prior.score.tagsJson) ? JSON.parse(prior.score.tagsJson) : {};
  const strongSet = new Set(fw.scoring.strong_signals || []);
  const tagKeys = Object.keys(fw.tags || {});

  const questionsHtml = fw.questions.map((q) => {
    const cur = priorScores[q.id] ? priorScores[q.id].score : null;
    const note = priorScores[q.id] ? priorScores[q.id].note : '';
    const radios = [0, 1, 2].map((v) => `
      <label class="score-radio">
        <input type="radio" name="q-${q.id}" value="${v}" ${cur === v ? 'checked' : ''} />
        <span>${v}${v === 0 ? ' — no' : v === 1 ? ' — partial' : ' — yes'}</span>
      </label>
    `).join('');
    return `
      <div class="score-q ${strongSet.has(q.id) ? 'strong' : ''}" data-qid="${q.id}">
        <div class="score-q-head">
          <span class="score-q-label">${escapeHTML(q.label)}${strongSet.has(q.id) ? ' <span class="strong-pill">strong</span>' : ''}</span>
          <span class="score-q-prompt">${escapeHTML(q.prompt)}</span>
        </div>
        ${q.guidance ? `<div class="score-q-guidance">${escapeHTML(q.guidance)}</div>` : ''}
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
  $('#score-save').addEventListener('click', () => submitScore(fw, entry, prior.score));
}

function humanizeKey(k) {
  return k.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}

async function submitScore(fw, entry, priorScore) {
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
        targetKind: 'watchlist',
        targetId: entry.id,
        frameworkId: fw.id,
        scores,
        tags: Object.keys(tags).length ? tags : undefined,
        reviewerNote: reviewerNote || undefined,
      }),
    });
    closeImportModal();
    state.watchlist = null;
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

boot();
