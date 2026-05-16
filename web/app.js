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
        <div class="market-pill" id="market-pill" title="US markets (Spec 5 extends to multi-market)">—</div>
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
}

// ---------- market status pill (top bar) -------------------------------

let marketState = null;
let marketTicker = null;

async function startMarketPill() {
  await refreshMarketStatus();
  setInterval(refreshMarketStatus, 5 * 60 * 1000);
  if (marketTicker) clearInterval(marketTicker);
  marketTicker = setInterval(updateMarketPillText, 1000);
}

async function refreshMarketStatus() {
  try {
    marketState = await api('/api/marketstatus');
    updateMarketPillText();
  } catch (_) { /* leave pill at last known state */ }
}

function updateMarketPillText() {
  const el = $('#market-pill');
  if (!el || !marketState) return;
  const us = marketState.us;
  const dot = us.open ? '🟢' : '🔴';
  const label = us.open ? 'US open' : 'US closed';
  const remaining = formatCountdown(us.nextChange);
  const verb = us.nextChangeKind === 'close' ? 'closes' : 'opens';
  el.innerHTML = `${dot} <span class="mp-label">${escapeHTML(label)}</span> · ${escapeHTML(verb)} in <span class="num mp-eta">${escapeHTML(remaining)}</span>`;
  el.classList.toggle('market-pill--open', us.open);
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

  content.innerHTML = staleBanner + toggle + kpiRow + donutRow + footer;

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

  const legendStops = [-3, -2, -1, 0, 1, 2, 3];

  const sectorOptions = ['', ...SECTORS]
    .map((s) => `<option value="${escapeHTML(s)}" ${s === state.heatmapSector ? 'selected' : ''}>${s === '' ? 'All sectors' : escapeHTML(s)}</option>`)
    .join('');

  const legendHTML = `
    <div class="heatmap-legend">
      <span>Sector</span>
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
  `;

  content.innerHTML = legendHTML + `<div class="heatmap-wrap" id="heatmap-svg">loading…</div>
    <div class="heatmap-note">
      Tile size = market cap · color = today's % change. Live prices populated
      on each refresh; tiles update silently when the background scheduler runs.
    </div>`;

  $('#heatmap-sector').addEventListener('change', (ev) => {
    state.heatmapSector = ev.target.value;
    renderHeatmap();
  });

  try {
    const params = new URLSearchParams({ w: String(w), h: String(h) });
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
  try {
    const feed = await api(scope === 'market' ? '/api/news/market' : '/api/news/crypto');
    renderFeed(feed, scope);
  } catch (err) {
    content.innerHTML = `<div class="empty"><div class="loss">news failed: ${escapeHTML(err.message)}</div></div>`;
  }
}

function renderFeed(feed, scope) {
  const content = $('#content');
  const articles = feed.articles || [];

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
    content.innerHTML = banner + fgChip + `<div class="empty">No articles to show.</div>`;
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
    content.innerHTML = banner + fgChip + `<div class="news-list">${list}</div>`;
  }

  if (scope === 'crypto') loadFearGreed();
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
  { name: 'strategyNote',   label: 'Strategy note',        type: 'textarea' },
  { name: 'note',           label: 'Note',                 type: 'textarea' },
];

function openHoldingModal({ kind, mode, holding }) {
  const fields = kind === 'stock' ? stockFields : cryptoFields;
  const isEdit = mode === 'edit';
  const title = `${isEdit ? 'Edit' : 'Add'} ${kind === 'stock' ? 'stock' : 'crypto'} holding`;

  // Build form HTML
  const fieldRows = fields.map((f) => {
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
  }).join('');

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

async function renderSettings() {
  const content = $('#content');
  content.innerHTML = '<div class="empty">loading…</div>';

  const [delStocks, delCrypto, audit] = await Promise.all([
    api('/api/holdings/stocks/deleted').catch(() => ({ holdings: [] })),
    api('/api/holdings/crypto/deleted').catch(() => ({ holdings: [] })),
    api('/api/audit?limit=100').catch(() => ({ audit: [] })),
  ]);

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

// Distance-to-entry classification for the watchlist table.
function distanceToEntry(currentPrice, low, high) {
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
  return { label: 'In range', cls: 'dist-in' };
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
    const dist = distanceToEntry(e.currentPrice, e.targetEntryLow, e.targetEntryHigh);
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
        <td><span class="${dist.cls}">${dist.label}</span></td>
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

function openWatchlistModal({ kind, mode, entry }) {
  const isEdit = mode === 'edit';
  const t = (k) => isEdit ? (entry[k] ?? '') : '';
  const safeTicker = isEdit ? escapeHTML(entry.ticker) : '';
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
                <input id="wl-ticker" name="ticker" type="text" required />
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
