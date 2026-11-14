// FT-039 (SC-85): every alert kind the server sends is rendered, nothing is sent empty, and only what was shown is
// acknowledged. Run with: node --test ops/ft-bot/render.test.js
'use strict';
const test = require('node:test');
const assert = require('node:assert');

process.env.TELEGRAM_BOT_TOKEN = 'test-token';
process.env.TELEGRAM_CHAT_ID = '1';
process.env.FT_BASE_URL = 'https://ft.example.test';
process.env.FT_TOKEN = 'test-ft-token';
const { renderAlertsDetailed, renderAlerts, alertGroup, proactiveRound } = require('./index.js');

const plain = (t) => t.replace(/\\([_*\[\]()~`>#+\-=|{}.!$\\])/g, '$1').replace(/[*_]/g, '');

const proximity = { holdingKind: 'stock', holdingId: 12, ticker: 'NVDA', name: 'NVIDIA', kind: 'proximity_amber_sl', currentPrice: 181.2,
  triggers: ['Price 82% of the way to the stop-loss'], action: 'Review the stop; hold the position' };
const stage = { holdingKind: 'stock', holdingId: 7, ticker: 'GLD', name: 'SPDR Gold', kind: 'tp1_hit', currentPrice: 301.5,
  triggers: ['Price reached TP1 (300)'], action: 'Consider taking the first partial' };
const nexus = { holdingKind: 'nexus', holdingId: 0, name: 'AI Nexus', kind: 'nexus_anomaly',
  triggers: ['Exhaustion snapshot is 3 days old', 'Universe has 2 names without prices'] };
const red = { holdingKind: 'stock', holdingId: 3, ticker: 'MSTR', kind: 'red', currentPrice: 150, triggers: ['RSI 82'] };

test('each kind has a group and a body', () => {
  assert.strictEqual(alertGroup(red), 'red');
  assert.strictEqual(alertGroup({ kind: 'amber' }), 'amber');
  assert.strictEqual(alertGroup(proximity), 'proximity');
  assert.strictEqual(alertGroup({ kind: 'proximity_red_tp' }), 'proximity');
  assert.strictEqual(alertGroup(stage), 'stage');
  assert.strictEqual(alertGroup({ kind: 'time_stop' }), 'stage');
  assert.strictEqual(alertGroup(nexus), 'nexus');
  assert.strictEqual(alertGroup({ kind: 'something_new' }), 'other');
});

test('a proximity-only message is not empty', () => {
  const { text, rendered } = renderAlertsDetailed([proximity], true);
  const p = plain(text);
  assert.strictEqual(rendered.length, 1);
  assert.match(p, /PROXIMITY \(1\)/);
  assert.match(p, /NVDA @ \$181\.20 · SL proximity/);
  assert.match(p, /Price 82% of the way to the stop-loss/);
  assert.match(p, /→ Review the stop; hold the position/);
});

test('stage events and the nexus health check are rendered', () => {
  const p = plain(renderAlerts([stage, nexus], true));
  assert.match(p, /TRADE EVENTS \(1\)/);
  assert.match(p, /GLD @ \$301\.50 · TP1 hit/);
  assert.match(p, /AI NEXUS \(1\)/);
  assert.match(p, /AI Nexus · health check/);
  assert.match(p, /Exhaustion snapshot is 3 days old/);
  assert.match(p, /Universe has 2 names without prices/);
});

test('RED and AMBER keep their old shape and come first', () => {
  const p = plain(renderAlerts([proximity, red], false));
  assert.ok(p.indexOf('RED (1)') < p.indexOf('PROXIMITY (1)'));
  assert.match(p, /MSTR @ \$150\.00/);
  assert.match(p, /RSI 82/);
  assert.doesNotMatch(plain(renderAlerts([{ ...red, action: 'Trim' }], false)), /→/); // red/amber lines carry no action line, as before
});

test('an unknown kind is shown, never dropped', () => {
  const { text, rendered } = renderAlertsDetailed([{ holdingKind: 'stock', holdingId: 1, name: 'Foo', kind: 'something_new', triggers: ['x'] }], true);
  assert.strictEqual(rendered.length, 1);
  assert.match(plain(text), /OTHER \(1\)/);
});

test('no alerts: no text', () => {
  assert.deepStrictEqual(renderAlertsDetailed([], true), { text: '', rendered: [] });
  assert.strictEqual(renderAlerts([], false), '');
});

function fakes(alerts, opts = {}) {
  const calls = { sent: [], acks: [] };
  return {
    calls,
    deps: {
      ftGet: async () => ({ alerts }),
      tgSend: async (t) => { if (opts.sendFails) throw new Error('telegram down'); calls.sent.push(t); },
      ftPost: async (path, body) => { assert.strictEqual(path, '/api/bot/alerts/ack'); calls.acks.push(body); },
    },
  };
}

test('proactive: one message, then an ack for exactly what it showed (including the nexus alert, id 0)', async () => {
  const f = fakes([proximity, stage, nexus, red]);
  const r = await proactiveRound(f.deps);
  assert.deepStrictEqual(r, { sent: 4, acked: 4 });
  assert.strictEqual(f.calls.sent.length, 1);
  assert.deepStrictEqual(f.calls.acks.map((a) => a.kind).sort(), ['nexus_anomaly', 'proximity_amber_sl', 'red', 'tp1_hit']);
  assert.ok(f.calls.acks.some((a) => a.holdingKind === 'nexus' && a.holdingId === 0));
});

test('proactive: a failed send acknowledges nothing', async () => {
  const f = fakes([proximity], { sendFails: true });
  assert.deepStrictEqual(await proactiveRound(f.deps), { sent: 0, acked: 0 });
  assert.strictEqual(f.calls.acks.length, 0);
});

test('proactive: nothing to show sends and acknowledges nothing', async () => {
  const f = fakes([]);
  assert.deepStrictEqual(await proactiveRound(f.deps), { sent: 0, acked: 0 });
  assert.strictEqual(f.calls.sent.length, 0);
  assert.strictEqual(f.calls.acks.length, 0);
  // entries that are not alert objects are not renderable: no send, no ack
  const g = fakes([null, 'x', 5]);
  assert.deepStrictEqual(await proactiveRound(g.deps), { sent: 0, acked: 0 });
  assert.strictEqual(g.calls.sent.length, 0);
});
