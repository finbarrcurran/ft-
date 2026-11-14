// FT-148: what the "What Jordi & Cowen said" panel renders for each situation, proven on fixtures by running the
// real renderer from web/app.js. Run with: node --test ops/ui-tests/commentary_render.test.js
'use strict';
const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

const src = fs.readFileSync(path.join(__dirname, '../../web/app.js'), 'utf8');
const a = src.indexOf('function renderCommentarySection(res) {');
const b = src.indexOf('function bindCommentaryMore(');
assert.ok(a > 0 && b > a, 'renderCommentarySection not found in web/app.js');
const escapeHTML = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
const render = new Function('escapeHTML', `${src.slice(a, b)}; return renderCommentarySection;`)(escapeHTML);

const base = { date: '2026-11-11', source: 'jordi', videoId: 'dQw4w9WgXcQ', title: 'Weekly', link: 'https://youtu.be/dQw4w9WgXcQ?t=120', linkKind: 'youtube',
  stance: 'bullish', snippet: 'solana is up thirty eight percent', snippetKind: 'passage', method: 'alias' };
const payload = (rows, over = {}) => ({ enabled: true, commentary: { ticker: 'SOL', days: 90, total: rows.length, shown: rows.length, hidden: 0,
  last: { jordi: { date: '2026-11-11', daysAgo: 3, stance: 'bullish' } }, rows, context: 'Context only', ...over } });

test('off: nothing is rendered', () => {
  assert.strictEqual(render({ enabled: false }), '');
});

test('direction flag renders when the item contradicts his net view', () => {
  const h = render(payload([{ ...base, direction: 'contradicts', directionView: 'net bearish on crypto' }]));
  assert.match(h, /⚠ against his net view/);
  assert.match(h, /title="net bearish on crypto"/);
  assert.doesNotMatch(render(payload([{ ...base, direction: 'supports' }])), /against his net view/);
});

test('cited loosely renders for wrong_place and far items only', () => {
  assert.match(render(payload([{ ...base, gate: 'wrong_place', citedLoosely: true }])), /cited loosely/);
  assert.doesNotMatch(render(payload([{ ...base, gate: 'grounded' }])), /cited loosely/);
});

test('the hidden-by-gate count renders, singular and plural, and is absent at zero', () => {
  assert.match(render(payload([base], { hidden: 2 })), /2 items hidden by the grounding gate/);
  assert.match(render(payload([base], { hidden: 1 })), /1 item hidden by the grounding gate/);
  assert.doesNotMatch(render(payload([base], { hidden: 0 })), /hidden by the grounding gate/);
});

test('a passage is quoted; a digest note is labelled; no snippet says "mentioned in this video"', () => {
  assert.match(render(payload([base])), /“…solana is up thirty eight percent…”/);
  const note = render(payload([{ ...base, snippetKind: 'note', snippet: 'Solana up 38% quarter to date' }]));
  assert.match(note, /Solana up 38% quarter to date/);
  assert.match(note, /\(digest note\)/);
  const none = render(payload([{ ...base, snippetKind: 'none', snippet: '' }]));
  assert.match(none, /mentioned in this video/);
  assert.doesNotMatch(none, /“…/);
});

test('links: YouTube with and without a second, 22v page, and no link at all', () => {
  const yt = render(payload([base]));
  assert.match(yt, /href="https:\/\/youtu\.be\/dQw4w9WgXcQ\?t=120"/);
  assert.match(yt, /▶ watch/);
  const plain = render(payload([{ ...base, link: 'https://youtu.be/dQw4w9WgXcQ' }]));
  assert.match(plain, /href="https:\/\/youtu\.be\/dQw4w9WgXcQ"/);
  assert.doesNotMatch(plain, /[?&]t=/);
  const v22 = render(payload([{ ...base, videoId: '22v-2538', link: 'https://ai.22vresearch.com/video-stream/2538/', linkKind: '22v' }]));
  assert.match(v22, /href="https:\/\/ai\.22vresearch\.com\/video-stream\/2538\/"/);
  assert.match(v22, /open on 22v/);
  assert.doesNotMatch(v22, /youtu\.be/);
  assert.doesNotMatch(v22, /22v-2538/);
  const dead = render(payload([{ ...base, link: '', linkKind: 'none' }]));
  assert.doesNotMatch(dead, /<a href/);
});
