// SC-52 P1 — real-Chromium test of the login regime proposal card (fixture API, no login).
// Run: NODE_PATH=$(npm root -g) CHROME_PATH=/usr/bin/google-chrome node tools/e2e/regime_proposal.js
const http = require('http');
const fs = require('fs');
const path = require('path');
const { chromium } = require('playwright');
const WEB = path.join(__dirname, '..', '..', 'web');

const cons = (now, ifp, mNow, mIf, eNow, eIf, tk) => ({ EffectiveNow: now, EffectiveIf: ifp, MultiplierNow: mNow, MultiplierIf: mIf, MarginPctNow: 5 * mNow, MarginPctIf: 5 * mIf, EntryAlertsNow: eNow, EntryAlertsIf: eIf, AffectedTickers: tk });
const CONSEQUENCES = {
  stable: cons('stable', 'stable', 1, 1, true, true, []),
  shifting: cons('stable', 'shifting', 1, 0.6, true, false, ['AVGO', 'CLS', 'RTX']),
  defensive: cons('stable', 'defensive', 1, 0.6, true, false, ['AVGO', 'CLS', 'RTX']),
};
const PROPOSAL = (over) => Object.assign({ digestId: 77, videoId: 'TB_-4pN1dsA', title: 'The AI Bubble Didn’t Pop. Hedge Fund Leverage Did.', publishedAt: '2026-10-18',
  narrative: 'He says hedge funds are deleveraging and recommends waiting for stability before adding.', catalysts: [{ event: 'US midterm elections', date: 'November 2026' }],
  state: 'shifting', confidence: 'medium', quote: 'you want to wait and wait for stability and wait for the hedge funds to be done liquidating', quoteMatch: 'nearest', quoteTs: 1231,
  link: 'https://youtu.be/TB_-4pN1dsA?t=1231', tilt: 'crypto', tiltQuote: 'The majority of my portfolio moved into crypto', stance: 'waiting', stanceQuote: 'wait another month' }, over || {});

let scenario = {};
const posts = [];
const server = http.createServer((req, res) => {
  const u = new URL(req.url, 'http://x');
  if (u.pathname.startsWith('/api/')) {
    res.setHeader('Content-Type', 'application/json');
    if (req.method === 'POST') {
      let body = '';
      req.on('data', (c) => (body += c));
      return req.on('end', () => { posts.push({ path: u.pathname, body: JSON.parse(body || '{}') }); if (scenario.postStatus) { res.statusCode = scenario.postStatus; return res.end(JSON.stringify({ error: 'that proposal is no longer the newest; reload' })); } res.end('{"ok":true}'); });
    }
    if (u.pathname === '/api/auth/state') return res.end(JSON.stringify({ state: 'authenticated', user: { id: 1, email: 't@example.com', username: 't' } }));
    if (u.pathname === '/api/regime/proposal') return res.end(JSON.stringify(scenario.proposal));
    if (u.pathname === '/api/regime') return res.end(JSON.stringify({ jordi: { regime: 'stable', stale: true, source: 'manual' }, cowen: { regime: 'unclassified', stale: true }, effective: 'stable' }));
    return res.end('{}');
  }
  const file = path.join(WEB, u.pathname === '/' ? '/index.html' : u.pathname);
  if (!file.startsWith(WEB) || !fs.existsSync(file)) { res.statusCode = 404; return res.end('nf'); }
  const types = { '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html' };
  res.setHeader('Content-Type', types[path.extname(file)] || 'application/octet-stream');
  res.end(fs.readFileSync(file, 'utf8').replace(/__VERSION__/g, 'test'));
});

let failures = 0;
const check = (n, ok, x) => { if (!ok) failures++; console.log(`${ok ? 'PASS' : 'FAIL'}  ${n}${ok ? '' : '  -> ' + x}`); };

(async () => {
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  const base = `http://127.0.0.1:${server.address().port}`;
  const browser = await chromium.launch(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {});
  const errors = [];
  const fresh = async () => {
    const ctx = await browser.newContext({ viewport: { width: 1300, height: 950 } });
    const page = await ctx.newPage();
    page.on('pageerror', (e) => errors.push(String(e)));
    return page;
  };
  const cardText = (page) => page.$eval('#regime-proposal-root', (e) => e.textContent.replace(/\s+/g, ' ').trim());

  // 1. A new proposal shows with everything the handover asks for.
  scenario = { proposal: { show: true, reason: 'new_proposal', current: { state: 'stable', setAt: '2026-07-03T20:15:53Z', ageDays: 116, stale: true, source: 'manual' },
    macro: { suggestedJordi: 'defensive', quadrant: 'Q4', shorthand: 'Deflation/Recession' }, proposal: PROPOSAL(), consequence: CONSEQUENCES.shifting, consequences: CONSEQUENCES } };
  let page = await fresh();
  await page.goto(base + '/');
  await page.waitForSelector('#rp-overlay');
  let t = await cardText(page);
  check('narrative shown', t.includes('recommends waiting for stability before adding'), t);
  check('proposed state and confidence are prominent', (await page.$$eval('#regime-proposal-root .rp-proposal .rp-chip', (e) => e[0].textContent)) === 'SHIFTING' && t.includes('CONFIDENCE MEDIUM'), t);
  check('quote with the closest-passage label, video title, date and timestamp link', t.includes('Closest passage in the transcript') && t.includes('2026-10-18') && (await page.$eval('#regime-proposal-root .rp-proposal a', (a) => a.href)).endsWith('?t=1231'), t);
  check('current state, set date, age and stale flag', t.includes('STABLE') && t.includes('set 2026-07-03 (116 days ago') && t.includes('STALE'), t);
  check('FT macro suggestion beside it and the disagreement said plainly', t.includes('FT macro regime (Spec 9p) suggests') && t.includes('DEFENSIVE') && t.includes('The two suggestions disagree. FT does not pick one.'), t);
  check('consequence preview: margin, entry alerts, affected tickers', t.includes('alert margin 5.0% → 3.0%') && t.includes('entry-zone alerts ON → OFF') && t.includes('AVGO, CLS, RTX'), t);
  check('stance and tilt shown as context only', t.includes('Context only (not a trade trigger)') && t.includes('waiting') && t.includes('crypto'), t);
  check('three set buttons, one approve, one dismiss', (await page.$$('#regime-proposal-root .rp-set')).length === 3 && !!(await page.$('#rp-approve')) && !!(await page.$('#rp-dismiss')));
  // Approve posts the digest id only (the server decides the state).
  posts.length = 0;
  await page.click('#rp-approve');
  await page.waitForFunction(() => !document.querySelector('#regime-proposal-root'));
  check('approve posts only the digest id and closes the card', posts.length === 1 && posts[0].path === '/api/regime/proposal/approve' && JSON.stringify(posts[0].body) === '{"digestId":77}', JSON.stringify(posts));

  // 2. Set a different state, dismiss, decide later.
  page = await fresh(); await page.goto(base + '/'); await page.waitForSelector('#rp-overlay');
  posts.length = 0;
  await page.click('.rp-set[data-state="defensive"]');
  await page.waitForFunction(() => !document.querySelector('#regime-proposal-root'));
  check('set a different state posts that state', posts.length === 1 && posts[0].path === '/api/regime/proposal/set' && posts[0].body.state === 'defensive' && posts[0].body.digestId === 77, JSON.stringify(posts));
  page = await fresh(); await page.goto(base + '/'); await page.waitForSelector('#rp-overlay');
  posts.length = 0;
  await page.click('#rp-dismiss');
  await page.waitForFunction(() => !document.querySelector('#regime-proposal-root'));
  check('dismiss posts the digest id', posts.length === 1 && posts[0].path === '/api/regime/proposal/dismiss', JSON.stringify(posts));
  page = await fresh(); await page.goto(base + '/'); await page.waitForSelector('#rp-overlay');
  posts.length = 0;
  await page.click('#rp-later');
  await page.waitForFunction(() => !document.querySelector('#regime-proposal-root'));
  check('decide later changes nothing', posts.length === 0);
  await page.reload();
  await page.waitForSelector('.tab', { timeout: 5000 }).catch(() => {});
  await page.waitForTimeout(500);
  check('decide later stays hidden for the rest of the session', !(await page.$('#regime-proposal-root')));

  // 3. A stale regime with no usable proposal still prompts.
  scenario = { proposal: { show: true, reason: 'stale_no_proposal', current: { state: 'stable', setAt: '2026-07-03T20:15:53Z', ageDays: 116, stale: true, source: 'manual' },
    proposal: PROPOSAL({ state: null, confidence: null, quote: null, quoteMatch: null }), consequences: CONSEQUENCES } };
  page = await fresh(); await page.goto(base + '/'); await page.waitForSelector('#rp-overlay');
  t = await cardText(page);
  check('stale with no proposal: says so, offers manual set, no approve button', t.includes('Jordi regime is stale') && t.includes('no usable proposal') && !(await page.$('#rp-approve')) && (await page.$$('.rp-set')).length === 3, t);

  // 3b. A split vote shows "2 OF 3 RUNS" next to the (low) confidence.
  scenario = { proposal: { show: true, reason: 'new_proposal', current: { state: 'stable', setAt: '2026-07-03T20:15:53Z', ageDays: 116, stale: true, source: 'manual' },
    proposal: PROPOSAL({ confidence: 'low', runs: 3, agree: 2 }), consequence: CONSEQUENCES.shifting, consequences: CONSEQUENCES } };
  page = await fresh(); await page.goto(base + '/'); await page.waitForSelector('#rp-overlay');
  t = await cardText(page);
  check('a split vote shows 2 OF 3 RUNS and the low confidence', t.includes('2 OF 3 RUNS') && t.includes('CONFIDENCE LOW'), t);
  scenario.proposal.proposal = PROPOSAL({ runs: 3, agree: 3 });
  page = await fresh(); await page.goto(base + '/'); await page.waitForSelector('#rp-overlay');
  check('a unanimous vote shows 3 OF 3 RUNS', (await cardText(page)).includes('3 OF 3 RUNS'));

  // 4. Nothing to show.
  scenario = { proposal: { show: false, reason: 'none' } };
  page = await fresh(); await page.goto(base + '/'); await page.waitForTimeout(1200);
  check('no card when there is nothing to decide', !(await page.$('#regime-proposal-root')));

  // 5. A rejected action shows the server message and keeps the card.
  scenario = { postStatus: 409, proposal: { show: true, reason: 'new_proposal', current: { state: 'stable', setAt: '2026-07-03T20:15:53Z', ageDays: 116, stale: true, source: 'manual' }, proposal: PROPOSAL(), consequence: CONSEQUENCES.shifting, consequences: CONSEQUENCES } };
  page = await fresh(); await page.goto(base + '/'); await page.waitForSelector('#rp-overlay');
  await page.click('#rp-approve');
  await page.waitForSelector('#rp-error', { state: 'visible' });
  check('a refused approval shows the reason and keeps the card', (await page.$eval('#rp-error', (e) => e.textContent)).includes('no longer the newest') && !!(await page.$('#regime-proposal-root')));

  check('no page errors', errors.length === 0, errors.join(' | '));
  await browser.close(); server.close();
  console.log(failures ? `\n${failures} FAILED` : '\nALL PASSED');
  process.exit(failures ? 1 : 0);
})().catch((e) => { console.error(e); process.exit(1); });
