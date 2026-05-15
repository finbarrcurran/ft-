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

  content.innerHTML = toggle + kpiRow + donutRow + footer;

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
    return `
      <tr data-row-id="${r.id}" data-row-kind="stock">
        <td>${badge}</td>
        <td>
          <div>${escapeHTML(r.name)}</div>
          <div class="ticker">${escapeHTML(r.ticker || '—')}${r.category ? ' · <span class="dim">' + escapeHTML(r.category) + '</span>' : ''}</div>
        </td>
        <td class="num">${fmtUSD.format(r.investedUsd)}</td>
        <td class="num">${dash(r.avgOpenPrice, fmtNum2)}</td>
        <td class="num" data-flash-id="stock-${r.id}-price" data-flash-value="${r.currentPrice ?? ''}">${dash(r.currentPrice, fmtNum2)}</td>
        <td class="num" data-flash-id="stock-${r.id}-pnl" data-flash-value="${m.pnlUsd ?? ''}">${dashSigned(m.pnlUsd, fmtNum2, '$')}</td>
        <td class="num">${pct(m.pnlPct, 2)}</td>
        <td class="num">${dash(r.rsi14, fmtNum2)}</td>
        <td class="num">${dash(r.stopLoss, fmtNum2)}</td>
        <td class="num">${pct(m.distanceToSlPct, 1)}</td>
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
    return `
      <tr data-row-id="${r.id}" data-row-kind="crypto">
        <td>
          <div>${escapeHTML(r.name)} <span class="ticker">${escapeHTML(r.symbol)}</span></div>
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

boot();
