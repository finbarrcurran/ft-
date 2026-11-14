// ft-bot — standalone Telegram daemon for FT (Finance Tracker).
//
// Two duties:
//   1. Proactive: every ALERT_CRON tick, hit /api/bot/alerts?only_unnotified=1,
//      DM Fin the RED/AMBER summary, ACK each so it doesn't re-fire.
//   2. Reactive: long-poll Telegram getUpdates; handle /alerts /movers /summary
//      /help. Same content as the cron path but without ACK and without
//      filtering by notified state.
//
// No frameworks. Node 22+, global fetch, native crypto for stable polling.
//
// Config via env (loaded by systemd from /etc/ft-bot/env):
//   TELEGRAM_BOT_TOKEN  — BotFather token for @FinsFTAlerts_bot
//   TELEGRAM_CHAT_ID    — numeric chat id (the only ID we'll DM/accept from)
//   FT_BASE_URL         — e.g. https://ft.curranhouse.dev
//   FT_TOKEN            — bearer token ft_st_…
//   ALERT_CRON          — optional override for cron times (default below)

'use strict';

const fs = require('node:fs');

const TELEGRAM_BOT_TOKEN = mustEnv('TELEGRAM_BOT_TOKEN');
const TELEGRAM_CHAT_ID   = mustEnv('TELEGRAM_CHAT_ID');
const FT_BASE_URL        = (process.env.FT_BASE_URL || 'https://ft.curranhouse.dev').replace(/\/+$/, '');
const FT_TOKEN           = mustEnv('FT_TOKEN');

// Cron-ish times in UTC for the proactive alert push.
// 13:00 ≈ NYSE open in summer, 17:00 ≈ midday, 21:00 ≈ NYSE close. Weekdays only.
const ALERT_HOURS = (process.env.ALERT_HOURS || '13,17,21').split(',').map((s) => parseInt(s, 10));
const WEEKDAYS_ONLY = process.env.ALERT_WEEKENDS !== '1';

// Spec 9b D12 — Sunday review nudge (regime capture reminder). Default 18:00
// UTC = 18:00 GMT / 19:00 BST (Dublin late afternoon, post-Cowen YouTube).
const SUNDAY_NUDGE_HOUR = parseInt(process.env.SUNDAY_NUDGE_HOUR || '18', 10);

// Offset persistence for long-polling Telegram.
const OFFSET_FILE = process.env.OFFSET_FILE || '/var/lib/ft-bot/offset.json';

/* ============================================================== */
/* Telegram                                                       */
/* ============================================================== */

async function tg(method, params) {
  const url = `https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/${method}`;
  const resp = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(params || {}),
  });
  const body = await resp.json().catch(() => ({}));
  if (!body.ok) throw new Error(`telegram ${method}: ${body.description || resp.status}`);
  return body.result;
}

async function tgSend(text) {
  return tg('sendMessage', {
    chat_id: TELEGRAM_CHAT_ID,
    text,
    parse_mode: 'MarkdownV2',
    disable_web_page_preview: true,
  });
}

async function tgReply(chatId, text) {
  return tg('sendMessage', {
    chat_id: chatId,
    text,
    parse_mode: 'MarkdownV2',
    disable_web_page_preview: true,
  });
}

/* ============================================================== */
/* FT API                                                         */
/* ============================================================== */

async function ftGet(path) {
  const r = await fetch(FT_BASE_URL + path, {
    headers: { Authorization: `Bearer ${FT_TOKEN}` },
  });
  if (!r.ok) throw new Error(`FT ${r.status} ${r.statusText}`);
  return r.json();
}

async function ftPost(path, body) {
  const r = await fetch(FT_BASE_URL + path, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${FT_TOKEN}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(body),
  });
  if (!r.ok) {
    const txt = await r.text().catch(() => '');
    throw new Error(`FT ${r.status} ${txt.slice(0, 120)}`);
  }
  return r.json().catch(() => ({}));
}

/* ============================================================== */
/* Message formatting                                             */
/* ============================================================== */

// MarkdownV2 escape — Telegram is fussy.
function esc(s) {
  return String(s || '').replace(/([_*\[\]()~`>#+\-=|{}.!\\])/g, '\\$1');
}

function fmt0(v) { return v == null ? '—' : Number(v).toLocaleString('en-US', { maximumFractionDigits: 0 }); }
function fmt2(v) { return v == null ? '—' : Number(v).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 }); }
function signedDollars(v) {
  if (v == null) return '—';
  const cls = v > 0 ? '🟢' : v < 0 ? '🔴' : '⚪';
  return `${cls} ${v >= 0 ? '\\+' : '\\-'}\\$${esc(fmt0(Math.abs(v)))}`;
}
function humanRefresh(iso) {
  if (!iso) return 'never';
  const sec = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
  if (sec < 60) return `${sec}s ago`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`;
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`;
  return `${Math.floor(sec / 86400)}d ago`;
}

// FT-039 (SC-85): the server sends more alert kinds than RED/AMBER (proximity_*, stage events, nexus_anomaly).
// Every kind is rendered, so no message is ever sent with an empty body, and only alerts that were rendered are
// acknowledged (nothing is acked unseen).
const STAGE_LABELS = {
  tp1_proximity: 'TP1 proximity', tp1_hit: 'TP1 hit', tp2_proximity: 'TP2 proximity', tp2_hit: 'TP2 hit',
  bounce: 'bounce', time_stop: 'time stop',
};

function alertGroup(a) {
  const k = String((a && a.kind) || '');
  if (k === 'red') return 'red';
  if (k === 'amber') return 'amber';
  if (k.startsWith('proximity_')) return 'proximity';
  if (Object.prototype.hasOwnProperty.call(STAGE_LABELS, k)) return 'stage';
  if (k === 'nexus_anomaly') return 'nexus';
  return 'other';
}

function proximityTag(kind) {
  const side = String(kind).endsWith('_tp') ? 'TP' : 'SL';
  return `${side} proximity`;
}

// renderAlertsDetailed returns the message text and the list of alerts it actually shows.
function renderAlertsDetailed(alerts, proactive) {
  const list = (alerts || []).filter((a) => a && typeof a === 'object');
  const groups = { red: [], amber: [], proximity: [], stage: [], nexus: [], other: [] };
  for (const a of list) groups[alertGroup(a)].push(a);
  const out = [proactive ? '🚨 *FT alerts*' : '🚨 *Active FT alerts*', ''];
  const rendered = [];
  const head = (a, emoji, tag) => {
    const price = a.currentPrice != null ? ` @ \\$${esc(fmt2(a.currentPrice))}` : '';
    const who = esc(a.ticker || a.name || a.kind || 'FT');
    out.push(`  ${emoji} *${who}*${price}${tag ? ` · ${esc(tag)}` : ''}`);
  };
  const body = (a, max, withAction) => {
    for (const t of (a.triggers || []).slice(0, max)) out.push(`    • ${esc(t)}`);
    if (withAction && a.action) out.push(`    → ${esc(a.action)}`);
  };
  const section = (label, items, render) => {
    if (!items.length) return;
    out.push(`*${esc(`${label} (${items.length})`)}*`);
    for (const a of items) {
      render(a);
      rendered.push(a);
    }
    out.push('');
  };
  section('RED', groups.red, (a) => { head(a, '🔴'); body(a, 3, false); });
  section('AMBER', groups.amber, (a) => { head(a, '🟠'); body(a, 3, false); });
  section('PROXIMITY', groups.proximity, (a) => {
    head(a, String(a.kind).includes('_red_') ? '🔴' : '🟠', proximityTag(a.kind));
    body(a, 3, true);
  });
  section('TRADE EVENTS', groups.stage, (a) => {
    head(a, String(a.kind).endsWith('_hit') ? '🟢' : '🟠', STAGE_LABELS[a.kind]);
    body(a, 3, true);
  });
  section('AI NEXUS', groups.nexus, (a) => { head(a, '⚙️', 'health check'); body(a, 5, false); });
  section('OTHER', groups.other, (a) => { head(a, '▫️', a.kind || 'alert'); body(a, 3, true); });
  if (!rendered.length) return { text: '', rendered };
  if (proactive) out.push('_Reply /snooze to mute proactive alerts for 24h\\._');
  return { text: out.join('\n').trim(), rendered };
}

function renderAlerts(alerts, proactive) {
  return renderAlertsDetailed(alerts, proactive).text;
}

function renderSummary(s) {
  const a = s.alerts || {};
  const st = s.stocks || {};
  const cr = s.crypto || {};
  const tally = `🔴 ${a.red || 0}  🟠 ${a.amber || 0}  🟢 ${a.green || 0}  ⚪ ${a.neutral || 0}`;
  return [
    '💼 *Portfolio rollup*',
    '',
    `*Stocks*  ${st.count || 0} holdings  ·  invested \\$${esc(fmt0(st.investedUsd))}  ·  value \\$${esc(fmt0(st.valueUsd))}  ·  P\\&L ${signedDollars(st.pnlUsd)}`,
    `*Crypto*  ${cr.count || 0} holdings  ·  cost \\$${esc(fmt0(cr.costBasisUsd))}  ·  value \\$${esc(fmt0(cr.valueUsd))}  ·  P\\&L ${signedDollars(cr.pnlUsd)}`,
    '',
    `*Alerts*  ${tally}`,
    '',
    `_Last refreshed ${esc(humanRefresh(s.lastRefreshedAt))}\\._`,
  ].join('\n');
}

function renderMovers(m) {
  const out = ['📊 *Today\\\'s movers*', ''];
  const section = (title, rows) => {
    if (!rows || !rows.length) return;
    out.push(`*${esc(title)}*`);
    for (const r of rows) {
      const arrow = r.changePct > 0 ? '🟢' : r.changePct < 0 ? '🔴' : '⚪';
      const sign  = r.changePct >= 0 ? '\\+' : '\\-';
      const price = r.currentPrice != null ? ` @ \\$${esc(fmt2(r.currentPrice))}` : '';
      out.push(`  ${arrow} ${esc(r.symbol)} ${sign}${esc(fmt2(Math.abs(r.changePct)))}%${price}`);
    }
    out.push('');
  };
  section('Stocks — best', m.stocks?.best);
  section('Stocks — worst', m.stocks?.worst);
  section('Crypto — best', m.crypto?.best);
  section('Crypto — worst', m.crypto?.worst);
  return out.join('\n').trim();
}

/* ============================================================== */
/* Command handlers                                               */
/* ============================================================== */

async function handleAlertsCmd(chatId) {
  const j = await ftGet('/api/bot/alerts?only_unnotified=0');
  const alerts = j.alerts || [];
  const text = renderAlerts(alerts, false);
  if (!text) return tgReply(chatId, '🟢 No active alerts\\.');
  return tgReply(chatId, text);
}

async function handleMoversCmd(chatId) {
  const j = await ftGet('/api/bot/holdings/movers?limit=5');
  return tgReply(chatId, renderMovers(j));
}

async function handleSummaryCmd(chatId) {
  const j = await ftGet('/api/bot/holdings/summary');
  return tgReply(chatId, renderSummary(j));
}

async function handleHelpCmd(chatId) {
  return tgReply(chatId, [
    '*FT bot commands*',
    '',
    '/alerts — RED/AMBER list \\(does not ACK\\)',
    '/summary — portfolio rollup',
    '/movers — today\\\'s biggest moves',
    '/regime — current Jordi / Cowen / effective regime',
    '/skip — dismiss this Sunday\\\'s regime nudge',
    '/snooze \\[h\\] — mute proactive alerts \\(default 24h, max 168h\\)',
    '/unsnooze — resume proactive alerts',
    '/levels TICKER — S/R \\+ suggested SL/TP \\+ R\\-mults',
    '/size TICKER ENTRY STOP — position size calculator',
    '/risk — portfolio risk dashboard',
    '/positions — open positions grouped by stage',
    '/done TICKER — mark TP1/TP2 hit \\(after manual sell on eToro\\)',
    '/llm — LLM spend snapshot',
    '/llm pause — pause all LLM features',
    '/llm resume — resume LLM features',
    '/llm override AMOUNT HOURS REASON — emergency override',
    '/perf — performance dashboard \\(WTD by default\\)',
    '/perf month \\| quarter \\| ytd \\| all — change window',
    '/note TICKER text — quick\\-capture a thesis observation',
    '/help — this message',
    '',
    `_Proactive pings at ${ALERT_HOURS.join(', ')} UTC ${WEEKDAYS_ONLY ? 'weekdays' : 'every day'}\\._`,
    `_Sunday review nudge ${SUNDAY_NUDGE_HOUR}:00 UTC unless captured in last 7d\\._`,
  ].join('\n'));
}

// Spec 9d D9 — /perf command + Sunday 19:00 UTC weekly summary.
//
// Window aliases recognised:
//   /perf            → 30d
//   /perf wtd        → last 7 days (uses 30d data + filters in formatter)
//   /perf month      → 30d
//   /perf quarter    → 90d
//   /perf ytd        → 365d (rough)
//   /perf all        → all
async function handlePerfCmd(chatId, text) {
  const parts = text.split(/\s+/);
  const arg = (parts[1] || '').toLowerCase();
  const apiWindow = ({
    wtd: '30d', week: '30d', month: '30d',
    quarter: '90d', ytd: '365d', '90d': '90d', '30d': '30d',
    '365d': '365d', all: 'all',
  })[arg] || '30d';
  const label = ({ wtd: 'WTD', week: 'Week', month: 'Month', quarter: 'Quarter', ytd: 'YTD', all: 'All-time' })[arg] || 'Last 30d';

  let d;
  try {
    d = await ftGet('/api/performance/overview?window=' + apiWindow);
  } catch (e) {
    return tgReply(chatId, `Couldn\\'t fetch performance: ${esc(e.message)}`);
  }
  const m = d.metrics || {};
  if ((m.count || 0) === 0) {
    return tgReply(chatId, `📊 *Performance · ${esc(label)}*\n\nNo closed trades in window yet\\. Once positions open + close, this tab fills in\\.`);
  }
  const expectancySign = m.expectancy >= 0 ? '\\+' : '';
  const pnlSign = m.totalPnlUsd >= 0 ? '\\+' : '\\-';
  const dot = m.expectancy >= 0.5 ? '🟢' : m.expectancy >= 0 ? '🟡' : '🔴';
  const lines = [
    `📊 *Performance · ${esc(label)}*`,
    '',
    `Trades: ${m.count}  ·  Win rate: ${(m.winRate * 100).toFixed(1)}%`,
    `Expectancy: ${dot} ${expectancySign}${esc(m.expectancy.toFixed(2))}R per trade`,
    `Realized P\\&L: ${pnlSign}\\$${esc(fmt0(Math.abs(m.totalPnlUsd)))}`,
    `Avg winner: \\+${esc(m.avgWinnerR.toFixed(2))}R  ·  Avg loser: ${esc(m.avgLoserR.toFixed(2))}R`,
    `Avg hold: ${esc(m.avgHoldDays.toFixed(0))} days`,
    `Max drawdown: ${esc(m.maxDrawdownPct.toFixed(2))}%`,
    `Time underwater: ${esc((d.underwaterPct || 0).toFixed(0))}%`,
  ];
  return tgReply(chatId, lines.join('\n'));
}

// Sunday weekly perf summary — 19:00 UTC, one hour after the regime nudge.
async function runWeeklyPerfSummary() {
  let d;
  try {
    d = await ftGet('/api/performance/overview?window=30d');
  } catch (e) {
    console.error('weekly perf: fetch failed', e.message);
    return;
  }
  const m = d.metrics || {};
  if ((m.count || 0) === 0) {
    console.log('weekly perf: no closed trades; skipping');
    return;
  }
  const dot = m.expectancy >= 0.5 ? '🟢' : m.expectancy >= 0 ? '🟡' : '🔴';
  const expectancySign = m.expectancy >= 0 ? '\\+' : '';
  const pnlSign = m.totalPnlUsd >= 0 ? '\\+' : '\\-';
  const text = [
    `📊 *Weekly performance · ${esc(new Date().toISOString().slice(0, 10))}*`,
    '',
    `Trades: ${m.count}  ·  Win rate: ${(m.winRate * 100).toFixed(1)}%`,
    `Expectancy: ${dot} ${expectancySign}${esc(m.expectancy.toFixed(2))}R`,
    `Realized P\\&L: ${pnlSign}\\$${esc(fmt0(Math.abs(m.totalPnlUsd)))}`,
    `Equity from peak: ${esc(m.maxDrawdownPct.toFixed(2))}% max DD this window`,
    '',
    '_Open /perf for full dashboard_',
  ].join('\n');
  try {
    await tgSend(text);
    console.log('weekly perf: sent');
  } catch (e) {
    console.error('weekly perf: send failed', e.message);
  }
}

// Spec 11 D8 — /note TICKER observation text…
//
// Resolves TICKER → holding (or watchlist) via /api/notes/resolve. If
// multiple matches exist (e.g. same ticker as stock + crypto), picks
// the first holding match (stocks before crypto before watchlist) and
// tells Fin which target it used. Source kind = 'manual'. Observation
// date = today. Factor tagging is left blank; edit via web UI to tag.
async function handleNoteCmd(chatId, text) {
  // Strip the leading "/note" and re-tokenize.
  const rest = text.replace(/^\/note(@\S+)?\s+/i, '').trim();
  if (!rest) {
    return tgReply(chatId, 'Usage: `/note TICKER observation text…`\nExample: `/note NVDA Q1 beat strong; hyperscaler rev +14%`');
  }
  const m = rest.match(/^([A-Za-z0-9.\-]+)\s+(.+)$/s);
  if (!m) {
    return tgReply(chatId, 'Couldn\\\'t parse\\. Usage: `/note TICKER text`');
  }
  const ticker = m[1].toUpperCase();
  const observation = m[2].trim();
  if (observation.length < 3) {
    return tgReply(chatId, 'Observation too short \\(min 3 chars\\)\\.');
  }
  // Resolve target.
  let targets;
  try {
    const r = await ftGet('/api/notes/resolve?ticker=' + encodeURIComponent(ticker));
    targets = r.targets || [];
  } catch (e) {
    return tgReply(chatId, `Lookup failed: ${esc(e.message)}`);
  }
  if (targets.length === 0) {
    return tgReply(chatId, `No matching holding or watchlist entry for *${esc(ticker)}*\\. Add it as a watchlist entry first?`);
  }
  const t = targets[0]; // pick first; resolve endpoint orders stock→crypto→watchlist
  const body = {
    targetKind: t.targetKind,
    targetId: t.targetId,
    ticker: t.ticker,
    observationText: observation,
    sourceKind: 'manual',
  };
  let res;
  try {
    res = await ftPost('/api/notes', body);
  } catch (e) {
    return tgReply(chatId, `Save failed: ${esc(e.message)}`);
  }
  const where = t.targetKind === 'holding'
    ? `${t.holdingKind} holding`
    : 'watchlist entry';
  const extra = targets.length > 1
    ? `\n_${targets.length - 1} other match\\(es\\) for ${esc(ticker)} — edit via web UI to retarget if needed\\._`
    : '';
  return tgReply(chatId, [
    `📝 *Note saved* → ${esc(t.ticker)} \\(${esc(where)}\\)`,
    `id: ${esc(String(res.id || '?'))}`,
    '',
    `_${esc(observation.slice(0, 280))}_`,
    extra,
  ].join('\n'));
}

function scheduleNextWeeklyPerf() {
  const now = new Date();
  let next = null;
  for (let dayOffset = 0; dayOffset <= 7; dayOffset++) {
    const d = new Date(Date.UTC(
      now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + dayOffset, 19, 0, 0,
    ));
    if (d.getUTCDay() === 0 && d > now) {
      next = d;
      break;
    }
  }
  if (!next) next = new Date(Date.now() + 14 * 24 * 3600 * 1000);
  const delay = next.getTime() - Date.now();
  console.log('next weekly perf summary at', next.toISOString(), `(in ${Math.round(delay / 60_000)} min)`);
  setTimeout(async () => {
    try { await runWeeklyPerfSummary(); }
    catch (e) { console.error('runWeeklyPerfSummary', e); }
    scheduleNextWeeklyPerf();
  }, delay);
}

// Spec 9c.1 D11 — /llm command family.
// Variants: /llm | /llm pause | /llm resume | /llm override <amount> <hours> <reason...>
async function handleLLMCmd(chatId, text) {
  const parts = text.split(/\s+/);
  const sub = (parts[1] || '').toLowerCase();

  if (sub === 'pause') {
    try {
      await ftPost('/api/llm/pause', { paused: true });
      return tgReply(chatId, '⏸ LLM features paused\\. Resume with `/llm resume`\\.');
    } catch (e) { return tgReply(chatId, 'Pause failed: ' + esc(e.message)); }
  }
  if (sub === 'resume') {
    try {
      await ftPost('/api/llm/pause', { paused: false });
      return tgReply(chatId, '▶ LLM features resumed\\.');
    } catch (e) { return tgReply(chatId, 'Resume failed: ' + esc(e.message)); }
  }
  if (sub === 'override') {
    if (parts.length < 5) {
      return tgReply(chatId, 'Usage: `/llm override AMOUNT HOURS REASON`\nExample: `/llm override 5 24 thesis on VST`');
    }
    const capUsd = parseFloat(parts[2]);
    const durationHours = parseInt(parts[3], 10);
    const reason = parts.slice(4).join(' ');
    if (!Number.isFinite(capUsd) || capUsd <= 0) {
      return tgReply(chatId, 'AMOUNT must be > 0');
    }
    if (!Number.isFinite(durationHours) || durationHours <= 0 || durationHours > 168) {
      return tgReply(chatId, 'HOURS must be 1\\.\\.168');
    }
    try {
      const r = await ftPost('/api/llm/override', { capUsd, durationHours, reason });
      return tgReply(chatId, `✓ Override active: \\+\\$${esc(String(capUsd))} until ${esc(r.until)}\nReason: ${esc(reason)}`);
    } catch (e) { return tgReply(chatId, 'Override failed: ' + esc(e.message)); }
  }
  if (sub === 'clear') {
    try {
      await ftPost('/api/llm/override/clear', {});
      return tgReply(chatId, '✓ Override cleared\\.');
    } catch (e) { return tgReply(chatId, 'Clear failed: ' + esc(e.message)); }
  }

  // Default: spend snapshot.
  let s;
  try {
    s = await ftGet('/api/llm/spend');
  } catch (e) {
    return tgReply(chatId, `Couldn\\'t fetch LLM spend: ${esc(e.message)}`);
  }
  const monthly = s.caps.effectiveMonthly || s.caps.monthlyUsd || 5;
  const daily = s.caps.dailyUsd || 0.5;
  const monthPct = monthly > 0 ? (s.month / monthly) * 100 : 0;
  const dayPct = daily > 0 ? (s.today / daily) * 100 : 0;
  const status = s.caps.globallyPaused ? '⏸ PAUSED' : '🟢 Normal';
  const topFeature = Object.entries(s.byFeature || {}).sort((a, b) => b[1] - a[1])[0];
  const lines = [
    '💸 *LLM spend*',
    '',
    `This month: \\$${esc(fmt2(s.month))} / \\$${esc(fmt2(monthly))} \\(${monthPct.toFixed(0)}%\\)`,
    `Today: \\$${esc(fmt2(s.today))} / \\$${esc(fmt2(daily))} \\(${dayPct.toFixed(0)}%\\)`,
    `Status: ${status}`,
    topFeature ? `Top feature: ${esc(topFeature[0])} \\(\\$${esc(fmt2(topFeature[1]))}\\)` : '',
  ];
  if (s.override && s.override.extraUsd > 0) {
    lines.push('', `_Override: \\+\\$${esc(fmt2(s.override.extraUsd))} until ${esc(s.override.until)}_`);
  }
  return tgReply(chatId, lines.filter(Boolean).join('\n'));
}

// Spec 9c D15 — /levels TICKER. Returns S/R candidates + suggestions.
async function handleLevelsCmd(chatId, text) {
  const parts = text.split(/\s+/);
  if (parts.length < 2 || !parts[1]) {
    return tgReply(chatId, 'Usage: `/levels TICKER` \\(e\\.g\\. `/levels NVDA`\\)');
  }
  const ticker = parts[1].toUpperCase();
  // Find holding by ticker. Look up the stock list, find matching id.
  let holdings;
  try {
    holdings = await ftGet('/api/holdings/stocks');
  } catch (e) {
    return tgReply(chatId, `Couldn\\'t fetch holdings: ${esc(e.message)}`);
  }
  const h = (holdings.holdings || []).find((x) => x.ticker && x.ticker.toUpperCase() === ticker);
  if (!h) {
    return tgReply(chatId, `No stock holding matching *${esc(ticker)}*\\.`);
  }
  let lv;
  try {
    lv = await ftGet(`/api/holdings/stocks/${h.id}/levels`);
  } catch (e) {
    return tgReply(chatId, `Couldn\\'t fetch levels: ${esc(e.message)}`);
  }
  const s = lv.suggestions || {};
  const lines = [
    `📐 *${esc(ticker)}* levels`,
    '',
    `Current: \\$${esc(fmt2(lv.currentPrice))}  ·  ATR\\(14w\\): \\$${esc(fmt2(lv.atrWeekly))}  ·  Tier: ${esc(s.usingTier || '—')} \\(${esc(s.usingTierSource || '?')}\\)`,
    '',
    `*Levels \\(user\\-set\\)*`,
    `  S1: ${lv.support1 ? `\\$${esc(fmt2(lv.support1))}` : '—'}  ·  S2: ${lv.support2 ? `\\$${esc(fmt2(lv.support2))}` : '—'}`,
    `  R1: ${lv.resistance1 ? `\\$${esc(fmt2(lv.resistance1))}` : '—'}  ·  R2: ${lv.resistance2 ? `\\$${esc(fmt2(lv.resistance2))}` : '—'}`,
    '',
    `*Suggestions*`,
    `  SL: ${s.sl > 0 ? `\\$${esc(fmt2(s.sl))}` : '—'}`,
    `  TP1: ${s.tp1 > 0 ? `\\$${esc(fmt2(s.tp1))} \\(R ${esc(fmt2(s.rMultipleTp1))}\\)` : '—'}`,
    `  TP2: ${s.tp2 > 0 ? `\\$${esc(fmt2(s.tp2))} \\(R ${esc(fmt2(s.rMultipleTp2))}\\)` : '—'}`,
  ];
  // Candidates.
  if (lv.candidates) {
    const candFmt = (rows) => (rows || []).slice(0, 3).map(c => `\\$${esc(fmt2(c.price))} \\(${c.touches}×\\)`).join('  ·  ');
    const sCands = candFmt(lv.candidates.supports);
    const rCands = candFmt(lv.candidates.resistances);
    if (sCands) lines.push('', `*Support candidates:* ${sCands}`);
    if (rCands) lines.push(`*Resistance candidates:* ${rCands}`);
  }
  lines.push('', `_Stage: ${esc(lv.stage)}_`);
  return tgReply(chatId, lines.join('\n'));
}

// Spec 9c D15 — /size TICKER ENTRY STOP. Position size calculator.
async function handleSizeCmd(chatId, text) {
  const parts = text.split(/\s+/);
  if (parts.length < 4) {
    return tgReply(chatId, 'Usage: `/size TICKER ENTRY STOP` \\(e\\.g\\. `/size NVDA 1100 882`\\)');
  }
  const ticker = parts[1].toUpperCase();
  const entry = parseFloat(parts[2]);
  const stop = parseFloat(parts[3]);
  if (!Number.isFinite(entry) || !Number.isFinite(stop) || entry <= stop) {
    return tgReply(chatId, 'Bad inputs\\. Entry must be \\> stop\\.');
  }
  // Pull portfolio value from /api/bot/holdings/summary.
  let s;
  try {
    s = await ftGet('/api/bot/holdings/summary');
  } catch (e) {
    return tgReply(chatId, `Couldn\\'t fetch summary: ${esc(e.message)}`);
  }
  const portValue = (s.stocks?.valueUsd || 0) + (s.crypto?.valueUsd || 0);
  // Use default 1% per-trade risk; spec lets the regime tighten it but for
  // a quick CLI command we use 1%.
  const riskPct = 1.0;
  const riskUsd = portValue * (riskPct / 100);
  const units = riskUsd / (entry - stop);
  const sizeUsd = units * entry;
  const pctOfPort = (sizeUsd / portValue) * 100;
  return tgReply(chatId, [
    `💰 *${esc(ticker)}* position size`,
    '',
    `Portfolio: \\$${esc(fmt0(portValue))}  ·  Per\\-trade risk: ${riskPct.toFixed(1)}% \\= \\$${esc(fmt0(riskUsd))}`,
    `Entry: \\$${esc(fmt2(entry))}  ·  Stop: \\$${esc(fmt2(stop))}  ·  Distance: \\$${esc(fmt2(entry - stop))}`,
    '',
    `→ *${esc(fmt2(units))}* units  \\(\\$${esc(fmt0(sizeUsd))}, ${pctOfPort.toFixed(1)}% of portfolio\\)`,
    `→ Max loss if stopped: \\$${esc(fmt0(riskUsd))}`,
  ].join('\n'));
}

// Spec 9c D15 — /risk. Portfolio risk dashboard summary.
async function handleRiskCmd(chatId) {
  let r;
  try {
    r = await ftGet('/api/risk/dashboard');
  } catch (e) {
    return tgReply(chatId, `Couldn\\'t fetch risk: ${esc(e.message)}`);
  }
  const cb = r.circuitBreakerActive ? `🛑 ARMED until ${esc(r.circuitBreakerUntil)}` : '🟢 OK';
  const concEntries = Object.entries(r.concentration || {}).sort((a, b) => b[1] - a[1]).slice(0, 5);
  const themeEntries = Object.entries(r.themeConcentration || {}).sort((a, b) => b[1] - a[1]).slice(0, 3);
  const concList = concEntries.map(([t, p]) =>
    `  ${esc(t)}: ${p.toFixed(1)}%${p > r.caps.concentrationPct ? ' ⚠' : ''}`).join('\n');
  const themeList = themeEntries.map(([t, p]) =>
    `  ${esc(t)}: ${p.toFixed(1)}%${p > r.caps.themeConcentrationPct ? ' ⚠' : ''}`).join('\n');
  const warns = (r.warnings || []).map(w => `  • ${esc(w)}`).join('\n');
  return tgReply(chatId, [
    '🛡️ *Portfolio risk*',
    '',
    `Portfolio value: \\$${esc(fmt0(r.portfolioValue))}`,
    `Drawdown: ${r.drawdownPct.toFixed(2)}%  \\(cap: ${r.caps.drawdownCircuitPct}%\\)`,
    `Total active risk: ${r.totalActiveRiskPct.toFixed(2)}%  \\(cap: ${r.caps.totalActivePct}%\\)`,
    `Circuit breaker: ${cb}`,
    '',
    `*Concentration* \\(cap ${r.caps.concentrationPct}%\\)`,
    concList || '  \\(none\\)',
    '',
    `*Theme* \\(cap ${r.caps.themeConcentrationPct}%\\)`,
    themeList || '  \\(none\\)',
    warns ? '\n*Warnings*\n' + warns : '',
  ].join('\n').trim());
}

// Spec 9c D15 — /positions. Open positions grouped by stage.
async function handlePositionsCmd(chatId) {
  let h;
  try {
    h = await ftGet('/api/holdings/stocks');
  } catch (e) {
    return tgReply(chatId, `Couldn\\'t fetch: ${esc(e.message)}`);
  }
  const buckets = { pre_tp1: [], post_tp1: [], runner: [], stopped: [] };
  for (const x of h.holdings || []) {
    const stage = x.stage || 'pre_tp1';
    if (buckets[stage]) buckets[stage].push(x);
  }
  const lines = ['📋 *Open positions by stage*', ''];
  for (const stage of ['pre_tp1', 'post_tp1', 'runner', 'stopped']) {
    const items = buckets[stage];
    if (!items || items.length === 0) continue;
    lines.push(`*${esc(stage)}* \\(${items.length}\\)`);
    for (const it of items.slice(0, 10)) {
      const sym = it.ticker || it.symbol || it.name;
      const value = it.metrics?.currentValueUsd ?? null;
      lines.push(`  • ${esc(sym)}${value ? ` — \\$${esc(fmt0(value))}` : ''}`);
    }
    lines.push('');
  }
  return tgReply(chatId, lines.join('\n').trim());
}

// Spec 9c D15 — /done TICKER. Advance stage when user has manually sold
// the partial on eToro. Detects whether we're advancing TP1 → post_tp1
// or TP2 → runner based on current stage.
async function handleDoneCmd(chatId, text) {
  const parts = text.split(/\s+/);
  if (parts.length < 2) {
    return tgReply(chatId, 'Usage: `/done TICKER` after manually selling on eToro');
  }
  const ticker = parts[1].toUpperCase();
  let holdings;
  try {
    holdings = await ftGet('/api/holdings/stocks');
  } catch (e) {
    return tgReply(chatId, `Couldn\\'t fetch: ${esc(e.message)}`);
  }
  const h = (holdings.holdings || []).find((x) => x.ticker && x.ticker.toUpperCase() === ticker);
  if (!h) {
    return tgReply(chatId, `No holding matching *${esc(ticker)}*\\.`);
  }
  const currentStage = h.stage || 'pre_tp1';
  if (currentStage === 'stopped') {
    return tgReply(chatId, `*${esc(ticker)}* is already stopped\\.`);
  }
  const nextStage = currentStage === 'pre_tp1' ? 'post_tp1' : currentStage === 'post_tp1' ? 'runner' : 'stopped';
  // PUT via update endpoint. Carrying over name/invested/etc. would be
  // overkill — we use the /stage convenience by PUTing a minimal body.
  try {
    const body = {
      name: h.name,
      ticker: h.ticker,
      category: h.category,
      sector: h.sector,
      investedUsd: h.investedUsd,
      avgOpenPrice: h.avgOpenPrice,
      currentPrice: h.currentPrice,
      stopLoss: h.stopLoss,
      takeProfit: h.takeProfit,
      strategyNote: h.strategyNote,
      stage: nextStage,
      reason: `manual /done via bot (${currentStage} → ${nextStage})`,
    };
    await ftPut(`/api/holdings/stocks/${h.id}`, body);
    return tgReply(chatId, `✓ *${esc(ticker)}* advanced ${esc(currentStage)} → *${esc(nextStage)}*\\.`);
  } catch (e) {
    return tgReply(chatId, `Failed: ${esc(e.message)}`);
  }
}

// Spec 9b D12 — /regime command. Returns current state.
async function handleRegimeCmd(chatId) {
  const r = await ftGet('/api/regime');
  const eff = (r.effective || 'unclassified').toUpperCase();
  const dotFor = (x) => x === 'stable' ? '🟢' : x === 'shifting' ? '🟡' : x === 'defensive' ? '🔴' : '⚪';
  const stale = (side) => side.stale ? ` ⚠ ${esc(humanRefresh(side.set_at))}` : '';
  const lines = [
    '🎛️ *Regime state*',
    '',
    `${dotFor(r.jordi.regime)} *Jordi*  ${esc(r.jordi.regime.toUpperCase())}${stale(r.jordi)}`,
    `${dotFor(r.cowen.regime)} *Cowen*  ${esc(r.cowen.regime.toUpperCase())}${stale(r.cowen)}`,
    `${dotFor(r.effective)} *Effective*  ${esc(eff)}`,
    '',
    `_Margin multiplier: ${esc(String(r.alert_margin_multiplier))}×_`,
    `_Watchlist entry-zone alerts: ${r.watchlist_alerts_active ? 'ACTIVE' : 'SUPPRESSED'}_`,
  ];
  return tgReply(chatId, lines.join('\n'));
}

// Spec 9b D12 — /skip command. Stamps regime_skip_week with this week's ISO key.
async function handleSkipCmd(chatId) {
  const wk = isoWeekKey(new Date());
  try {
    await ftPut('/api/preferences/regime_skip_week', { value: wk });
    return tgReply(chatId, `✓ Sunday nudge skipped for ${esc(wk)}\\.`);
  } catch (e) {
    return tgReply(chatId, `Couldn\\'t skip: ${esc(e.message)}`);
  }
}

// Backlog polish 2026-05-17 — /snooze [hours]
//   /snooze         → mute proactive alerts for 24h
//   /snooze 4       → mute for 4h
//   /unsnooze       → clear immediately
async function handleSnoozeCmd(chatId, text) {
  const parts = text.split(/\s+/);
  let hours = parseFloat(parts[1] || '24');
  if (!Number.isFinite(hours) || hours <= 0 || hours > 168) {
    return tgReply(chatId, 'Usage: `/snooze [hours]` \\(1\\-168\\)');
  }
  const until = Math.floor(Date.now() / 1000) + Math.round(hours * 3600);
  try {
    await ftPut('/api/preferences/alerts_snooze_until', { value: String(until) });
    const untilStr = new Date(until * 1000).toISOString().replace('T', ' ').slice(0, 16);
    return tgReply(chatId, `🤫 Proactive alerts snoozed for ${esc(String(hours))}h \\(until ${esc(untilStr)} UTC\\)\\. \`/alerts\` still works on demand\\.`);
  } catch (e) {
    return tgReply(chatId, `Couldn\\'t snooze: ${esc(e.message)}`);
  }
}

async function handleUnsnoozeCmd(chatId) {
  try {
    await ftPut('/api/preferences/alerts_snooze_until', { value: '0' });
    return tgReply(chatId, '🔔 Proactive alerts resumed\\.');
  } catch (e) {
    return tgReply(chatId, `Couldn\\'t unsnooze: ${esc(e.message)}`);
  }
}

async function ftPut(path, body) {
  const r = await fetch(FT_BASE_URL + path, {
    method: 'PUT',
    headers: { Authorization: `Bearer ${FT_TOKEN}`, 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!r.ok) {
    const t = await r.text().catch(() => '');
    throw new Error(`FT ${r.status} ${t.slice(0, 120)}`);
  }
  return r.json().catch(() => ({}));
}

// ISO week key — "YYYY-Www" — matches what /skip stores so we can dedup.
function isoWeekKey(d) {
  // Standard ISO week math: Thursday in current week decides the year.
  const t = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate()));
  const day = t.getUTCDay() || 7;
  t.setUTCDate(t.getUTCDate() + 4 - day);
  const yearStart = new Date(Date.UTC(t.getUTCFullYear(), 0, 1));
  const wk = Math.ceil((((t - yearStart) / 86400000) + 1) / 7);
  return `${t.getUTCFullYear()}-W${String(wk).padStart(2, '0')}`;
}

/* ============================================================== */
/* Proactive cron path                                            */
/* ============================================================== */

// Backlog polish 2026-05-17 — /snooze checks `alerts_snooze_until` (unix
// seconds, stored as a string in user_preferences). If now < that, skip
// the send. The user_preferences endpoints already accept the bot token.
async function isAlertsSnoozed() {
  try {
    const r = await ftGet('/api/preferences/alerts_snooze_until');
    const ts = parseInt(r.value, 10);
    if (Number.isFinite(ts) && ts > Math.floor(Date.now() / 1000)) {
      return ts;
    }
  } catch (_) { /* preference absent → not snoozed */ }
  return 0;
}

async function runProactiveAlerts() {
  const snoozedUntil = await isAlertsSnoozed();
  if (snoozedUntil) {
    console.log('proactive: snoozed until', new Date(snoozedUntil * 1000).toISOString());
    return;
  }
  await proactiveRound({ ftGet, ftPost, tgSend });
}

// One proactive round. deps are injected so the behaviour can be tested without a network:
//   * nothing to show -> nothing is sent and nothing is acknowledged;
//   * the message is sent first; only then are the alerts it showed acknowledged, one by one;
//   * a failed send acknowledges nothing, so the alerts are tried again at the next poll.
async function proactiveRound(deps) {
  let j;
  try {
    j = await deps.ftGet('/api/bot/alerts?only_unnotified=1');
  } catch (e) {
    console.error('proactive: fetch failed', e.message);
    return { sent: 0, acked: 0 };
  }
  const alerts = j.alerts || [];
  if (alerts.length === 0) {
    console.log('proactive: no unnotified alerts');
    return { sent: 0, acked: 0 };
  }
  const { text, rendered } = renderAlertsDetailed(alerts, true);
  if (!rendered.length || !text) {
    console.warn('proactive: nothing renderable among', alerts.length, 'alert(s); not sending, not acknowledging');
    return { sent: 0, acked: 0 };
  }
  try {
    await deps.tgSend(text);
  } catch (e) {
    console.error('proactive: send failed', e.message);
    return { sent: 0, acked: 0 };
  }
  // ACK only what the message showed, so it doesn't re-fire today.
  let acked = 0;
  for (const a of rendered) {
    try {
      await deps.ftPost('/api/bot/alerts/ack', {
        holdingKind: a.holdingKind,
        holdingId: a.holdingId,
        kind: a.kind,
      });
      acked++;
    } catch (e) {
      console.warn('proactive: ack failed', a.ticker || a.holdingId, a.kind, e.message);
    }
  }
  console.log('proactive: sent', rendered.length, 'alerts, acked', acked);
  return { sent: rendered.length, acked };
}

// Schedule the next cron tick. Fires at the next configured HH:00 UTC.
function scheduleNextAlert() {
  const now = new Date();
  let next = null;
  // Try today and tomorrow.
  for (let dayOffset = 0; dayOffset <= 7 && !next; dayOffset++) {
    const d = new Date(Date.UTC(
      now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + dayOffset, 0, 0, 0,
    ));
    const dow = d.getUTCDay(); // 0=Sun, 6=Sat
    if (WEEKDAYS_ONLY && (dow === 0 || dow === 6)) continue;
    for (const h of [...ALERT_HOURS].sort((a, b) => a - b)) {
      const candidate = new Date(d);
      candidate.setUTCHours(h, 0, 0, 0);
      if (candidate > now) {
        next = candidate;
        break;
      }
    }
  }
  if (!next) {
    console.warn('no upcoming alert tick found; falling back to +1h');
    next = new Date(Date.now() + 3600_000);
  }
  const delay = next.getTime() - Date.now();
  console.log('next proactive alert at', next.toISOString(), `(in ${Math.round(delay / 60_000)} min)`);
  setTimeout(async () => {
    try { await runProactiveAlerts(); }
    catch (e) { console.error('runProactiveAlerts', e); }
    scheduleNextAlert();
  }, delay);
}

/* ============================================================== */
/* Spec 9b D12 — Sunday review nudge                              */
/* ============================================================== */

async function runSundayNudge() {
  let r;
  try {
    r = await ftGet('/api/regime');
  } catch (e) {
    console.error('sunday nudge: fetch regime failed', e.message);
    return;
  }
  // Skip if user already /skip'd this week.
  const wk = isoWeekKey(new Date());
  try {
    const sk = await ftGet('/api/preferences/regime_skip_week');
    if (sk && sk.value === wk) {
      console.log('sunday nudge: skipped for', wk);
      return;
    }
  } catch (_) { /* preference may not exist yet */ }
  // Skip if Cowen form was submitted within the last 7 days AND not stale.
  if (r.cowen && r.cowen.set_at && r.cowen.source === 'auto_cowen_form') {
    const ageDays = (Date.now() - new Date(r.cowen.set_at).getTime()) / (1000 * 60 * 60 * 24);
    if (ageDays < 7) {
      console.log('sunday nudge: cowen captured recently, skipping');
      return;
    }
  }
  const eff = (r.effective || 'unclassified').toUpperCase();
  const stale = r.cowen.stale ? ` ⚠ Cowen regime is ${esc(humanRefresh(r.cowen.set_at))} old\\.\n` : '';
  const lines = [
    '🗓️ *Sunday review time*',
    '',
    `Cowen weekly capture due\\. Current effective: *${esc(eff)}*\\.`,
    '',
    `Open: ${esc(FT_BASE_URL)}/${''}#regime\\-capture`,
    '',
    stale,
    'Or reply /skip to dismiss this week\\.',
  ];
  try {
    await tgSend(lines.join('\n'));
    console.log('sunday nudge: sent');
  } catch (e) {
    console.error('sunday nudge: send failed', e.message);
  }
}

function scheduleNextSundayNudge() {
  const now = new Date();
  // Find next Sunday at SUNDAY_NUDGE_HOUR:00 UTC.
  let next = null;
  for (let dayOffset = 0; dayOffset <= 7; dayOffset++) {
    const d = new Date(Date.UTC(
      now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + dayOffset, SUNDAY_NUDGE_HOUR, 0, 0,
    ));
    if (d.getUTCDay() === 0 /* Sunday */ && d > now) {
      next = d;
      break;
    }
  }
  if (!next) {
    // Belt-and-braces: try +14d
    next = new Date(Date.now() + 14 * 24 * 3600 * 1000);
  }
  const delay = next.getTime() - Date.now();
  console.log('next sunday nudge at', next.toISOString(), `(in ${Math.round(delay / 60_000)} min)`);
  setTimeout(async () => {
    try { await runSundayNudge(); }
    catch (e) { console.error('runSundayNudge', e); }
    scheduleNextSundayNudge();
  }, delay);
}

/* ============================================================== */
/* Reactive Telegram polling                                      */
/* ============================================================== */

function loadOffset() {
  try {
    return JSON.parse(fs.readFileSync(OFFSET_FILE, 'utf8')).offset || 0;
  } catch (_) {
    return 0;
  }
}
function saveOffset(offset) {
  try {
    fs.mkdirSync(require('node:path').dirname(OFFSET_FILE), { recursive: true });
    fs.writeFileSync(OFFSET_FILE, JSON.stringify({ offset }));
  } catch (e) {
    console.warn('offset persist failed', e.message);
  }
}

async function pollLoop() {
  let offset = loadOffset();
  console.log('long-poll loop started; offset =', offset);
  while (true) {
    let updates;
    try {
      updates = await tg('getUpdates', { offset, timeout: 50 });
    } catch (e) {
      console.error('getUpdates', e.message);
      await sleep(5000);
      continue;
    }
    for (const u of updates) {
      offset = u.update_id + 1;
      try {
        await handleUpdate(u);
      } catch (e) {
        console.error('handleUpdate', e);
      }
    }
    if (updates.length) saveOffset(offset);
  }
}

async function handleUpdate(u) {
  const msg = u.message || u.edited_message || u.channel_post;
  if (!msg || !msg.text) return;
  const chatId = String(msg.chat.id);
  if (chatId !== TELEGRAM_CHAT_ID) {
    // Ignore strangers. Spec is single-user.
    console.warn('ignored msg from unknown chat', chatId);
    return;
  }
  const text = msg.text.trim();
  const cmd  = text.split(/\s+/)[0].toLowerCase().replace(/@.+$/, ''); // /alerts@FinsFTAlerts_bot → /alerts
  console.log('cmd:', cmd);
  try {
    switch (cmd) {
      case '/alerts':    return await handleAlertsCmd(chatId);
      case '/summary':   return await handleSummaryCmd(chatId);
      case '/movers':    return await handleMoversCmd(chatId);
      case '/regime':    return await handleRegimeCmd(chatId);
      case '/skip':      return await handleSkipCmd(chatId);
      // Backlog polish — proactive-alert snooze.
      case '/snooze':    return await handleSnoozeCmd(chatId, text);
      case '/unsnooze':  return await handleUnsnoozeCmd(chatId);
      // Spec 9c D15 — Percoco execution commands.
      case '/levels':    return await handleLevelsCmd(chatId, text);
      case '/size':      return await handleSizeCmd(chatId, text);
      case '/risk':      return await handleRiskCmd(chatId);
      case '/positions': return await handlePositionsCmd(chatId);
      case '/done':      return await handleDoneCmd(chatId, text);
      // Spec 9c.1 D11 — /llm command family.
      case '/llm':       return await handleLLMCmd(chatId, text);
      // Spec 9d D9 — /perf.
      case '/perf':      return await handlePerfCmd(chatId, text);
      // Spec 11 D8 — /note TICKER text...
      case '/note':      return await handleNoteCmd(chatId, text);
      case '/start':
      case '/help':      return await handleHelpCmd(chatId);
      default:
        // Soft-fail unknown commands; non-commands ignored silently.
        if (cmd.startsWith('/')) {
          await tgReply(chatId, `Unknown command: ${esc(cmd)}\\.  Try /help\\.`);
        }
    }
  } catch (e) {
    console.error('cmd handler', cmd, e.message);
    await tgReply(chatId, `Error: ${esc(e.message)}`);
  }
}

/* ============================================================== */
/* Bootstrap                                                      */
/* ============================================================== */

function mustEnv(k) {
  const v = process.env[k];
  if (!v) {
    console.error(`Missing env: ${k}. Did /etc/ft-bot/env load correctly?`);
    process.exit(2);
  }
  return v;
}

function sleep(ms) { return new Promise((res) => setTimeout(res, ms)); }

async function boot() {
  console.log('ft-bot starting');
  console.log('  base url:', FT_BASE_URL);
  console.log('  chat id :', TELEGRAM_CHAT_ID);
  console.log('  cron    :', `${ALERT_HOURS.join(',')} UTC${WEEKDAYS_ONLY ? ' (weekdays)' : ''}`);

  // Sanity-check FT auth at startup.
  try {
    await ftGet('/api/bot/holdings/summary');
    console.log('  ft auth : ok');
  } catch (e) {
    console.error('FT auth failed at startup:', e.message);
    process.exit(3);
  }
  // Sanity-check Telegram auth.
  try {
    const me = await tg('getMe');
    console.log('  bot     :', `@${me.username} (id ${me.id})`);
  } catch (e) {
    console.error('Telegram auth failed at startup:', e.message);
    process.exit(4);
  }

  // Send a hello once per process start? No — that'd spam on every restart.
  // Instead: log the next cron time and start polling.
  scheduleNextAlert();
  scheduleNextSundayNudge();    // Spec 9b D12
  scheduleNextWeeklyPerf();     // Spec 9d D9 — Sunday 19:00 UTC
  pollLoop().catch((e) => {
    console.error('pollLoop crashed:', e);
    process.exit(5);
  });
}

if (require.main === module) {
  boot();
} else {
  module.exports = { renderAlerts, renderAlertsDetailed, alertGroup, proactiveRound };
}
