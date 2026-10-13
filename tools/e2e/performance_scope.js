// SC-51 follow-up — browser test of the Performance tab's single scope control.
// Real Chromium (Playwright) against the real web/app.js, with a tiny stub server that serves
// fixture JSON (no login, no live data). Run:
//   NODE_PATH=$(npm root -g) [CHROME_PATH=/usr/bin/google-chrome] node tools/e2e/performance_scope.js
// Exit code 1 on any failed assertion.
const http = require('http');
const fs = require('fs');
const path = require('path');
const { chromium } = require('playwright');

const WEB = path.join(__dirname, '..', '..', 'web');

const asset = (assetType, disc, copy, nDisc, nCopy, div = 0, fees = 0) => ({
  assetType, realisedPnlUsd: +(disc + copy).toFixed(2), realisedDiscUsd: disc, realisedCopyUsd: copy,
  realisedPnlEur: 0, realisedDiscEur: 0, realisedCopyEur: 0, dividendsUsd: div, feesUsd: fees,
  tradeCount: nDisc + nCopy, tradeCountDisc: nDisc, tradeCountCopy: nCopy,
});
const YEARS = [
  { year: 2026, rangeStart: '2026-01-01', rangeEnd: '2026-10-09', isYtd: true, realisedPnlUsd: 321.13, dividendsUsd: 53.94, feesUsd: -27.54,
    interestUsd: 199.52, refundsUsd: 100, otherIncomeUsd: 0, unclassifiedUsd: 0, netUsd: 647.05, reconDeltaUsd: 0, sourceFile: 'fixture.xlsx',
    assets: [asset('Stocks', 1384.49, -1118.99, 214, 1217, 53.94, -27.54), asset('CFDs', 96.77, -45.23, 9, 19), asset('ETFs', 0, 10.48, 0, 7), asset('Crypto', 0, -6.39, 0, 2)] },
  { year: 2025, rangeStart: '2025-01-01', rangeEnd: '2025-12-31', isYtd: false, realisedPnlUsd: -76.63, dividendsUsd: 46.76, feesUsd: -14.76,
    interestUsd: 0, refundsUsd: 0, otherIncomeUsd: 0, unclassifiedUsd: 0, netUsd: -44.63, reconDeltaUsd: 0, sourceFile: 'fixture.xlsx',
    assets: [asset('Stocks', 0, -76.73, 0, 266, 46.76, -14.76), asset('CFDs', 0, 0.10, 0, 1)] },
];
const SCOPE_N = { discretionary: 223, copy: 1245, all: 1468 };
const SCOPE_TOTAL = { discretionary: 1481.26, copy: -1160.13, all: 321.13 };

function tradeStats(q) {
  const scope = q.get('scope') || 'discretionary';
  const s = { n: SCOPE_N[scope], wins: 1, losses: 1, breakEven: 0, totalUsd: SCOPE_TOTAL[scope], winRate: 0.5, avgWinner: 1, avgLoser: -1,
    payoffRatio: 1, profitFactor: 1, expectancy: 1, medianHoldDays: 3, holdKnown: SCOPE_N[scope], largestWinner: 1, largestLoser: -1,
    top5WinnersShare: null, smallSample: false };
  return { scope, year: q.get('year') || 'all', currency: 'USD', stats: s, byAsset: [], years: [2025, 2026], minAmountUsd: +(q.get('minAmount') || 0),
    excludedN: 0, excludedUsd: 0, amountUnknown: 0 };
}

const requests = [];
const server = http.createServer((req, res) => {
  const u = new URL(req.url, 'http://x');
  if (u.pathname.startsWith('/api/')) {
    requests.push(u.pathname + u.search);
    res.setHeader('Content-Type', 'application/json');
    if (u.pathname === '/api/auth/state') return res.end(JSON.stringify({ state: 'authenticated', user: { id: 1, email: 'test@example.com', username: 'test' } }));
    if (u.pathname === '/api/etoro/performance') return res.end(JSON.stringify({ years: YEARS, coverage: { has: true, start: '2025-01-01', end: '2026-10-09', nextStart: '2026-10-10', ageDays: 4, stale: false } }));
    if (u.pathname === '/api/etoro/trade-stats') return res.end(JSON.stringify(tradeStats(u.searchParams)));
    return res.end('{}');
  }
  let f = u.pathname === '/' ? '/index.html' : u.pathname;
  const file = path.join(WEB, f);
  if (!file.startsWith(WEB) || !fs.existsSync(file)) { res.statusCode = 404; return res.end('nf'); }
  const types = { '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html' };
  res.setHeader('Content-Type', types[path.extname(file)] || 'application/octet-stream');
  res.end(fs.readFileSync(file, 'utf8').replace(/__VERSION__/g, 'test'));
});

let failures = 0;
const check = (name, ok, extra) => { if (!ok) failures++; console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${ok ? '' : '  -> ' + extra}`); };

(async () => {
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  const base = `http://127.0.0.1:${server.address().port}`;
  const browser = await chromium.launch(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {});
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 1000 } });
  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(String(e)));

  // Previously saved "all" under the OLD key must not stick: first load is Discretionary.
  await page.addInitScript(() => {
    if (!sessionStorage.getItem('seeded')) { localStorage.clear(); localStorage.setItem('ft.etoroStrategyView', 'all'); sessionStorage.setItem('seeded', '1'); }
  });
  await page.goto(base + '/');
  await page.click('button.tab[data-tab="performance"]');
  await page.waitForSelector('.etoro-year-block');
  await page.waitForSelector('.ts-panel');

  const active = () => page.$eval('#etoro-perf-panel .etoro-strat-btn.active', (e) => e.dataset.strat);
  const text = (sel) => page.$eval(sel, (e) => e.textContent.replace(/\s+/g, ' ').trim());
  const yearBlock = (y) => page.$$eval('.etoro-year-block', (bs, yr) => {
    const b = bs.find((x) => x.querySelector('.etoro-year-title').textContent.trim().startsWith(String(yr)));
    return { text: b.textContent.replace(/\s+/g, ' ').trim(), heads: [...b.querySelectorAll('thead th')].map((t) => t.textContent.trim()),
      total: [...b.querySelectorAll('tr.etoro-total-row td')].map((t) => t.textContent.trim()), account: b.querySelector('.etoro-account-row') ? b.querySelector('.etoro-account-row').textContent.replace(/\s+/g, ' ').trim() : null };
  }, y);

  check('first load defaults to Discretionary (old saved "all" ignored)', (await active()) === 'discretionary', await active());
  check('exactly one scope control on the tab', (await page.$$('#etoro-perf-panel .etoro-strat-toggle')).length === 1 && (await page.$$('.ts-scope')).length === 0);
  check('YTD card title is "YTD 2026 · realised"', (await text('.etoro-ytd-label')).startsWith('YTD 2026 · realised'), await text('.etoro-ytd-label'));

  const baseline = { ytd: await page.$eval('.etoro-ytd-card', (e) => e.innerHTML), cvs: await page.$eval('.cvs-panel', (e) => e.innerHTML) };
  const expect = {
    discretionary: { label: 'Discretionary', total: '$1481.26', trades: '223', n: 223 },
    copy: { label: 'Copy / CFD', total: '-$1160.13', trades: '1245', n: 1245 },
    all: { label: 'All', total: '$321.13', trades: null, n: 1468 },
  };
  const acct = {};
  for (const scope of ['copy', 'all', 'discretionary', 'all', 'copy', 'discretionary']) {
    const label = { discretionary: 'Discretionary', copy: 'Copy / CFD', all: 'All' }[scope];
    requests.length = 0;
    await page.click(`#etoro-perf-panel .etoro-strat-btn[data-strat="${scope}"]`);
    await page.waitForFunction((l) => document.querySelector('.ts-scope-label') && document.querySelector('.ts-scope-label').textContent.includes(l), label);
    const e = expect[scope];
    const sreq = requests.filter((r) => r.startsWith('/api/etoro/trade-stats')).pop() || '';
    check(`[${scope}] stats request carries scope=${scope}`, sreq.includes(`scope=${scope}`), sreq);
    check(`[${scope}] stats panel label and n`, (await text('.ts-scope-label')).includes(e.label) && (await text('.ts-grid .ts-metric')).includes(String(e.n)), await text('.ts-scope-label'));
    check(`[${scope}] active button`, (await active()) === scope);
    const y26 = await yearBlock(2026), y25 = await yearBlock(2025);
    if (scope === 'all') {
      check('[all] year table has Net column', y26.heads.includes('Net') && y25.heads.includes('Net'), y26.heads.join('|'));
      check('[all] 2026 total net = $647.05 and 2025 = -$44.63', y26.total.includes('$647.05') && y25.total.includes('-$44.63'), y26.total.join('|') + ' / ' + y25.total.join('|'));
      check('[all] 2026 total realised = $321.13', y26.total.includes('$321.13'), y26.total.join('|'));
      check('[all] income row shows refunds $100.00', y26.text.includes('refunds $100.00'), y26.text);
      check('[all] no account-level row', y26.account === null);
    } else {
      check(`[${scope}] no Net column and no Net cell`, !y26.heads.includes('Net') && !y25.heads.includes('Net'), y26.heads.join('|'));
      check(`[${scope}] 2026 total realised ${e.total}`, y26.total.includes(e.total), y26.total.join('|'));
      check(`[${scope}] 2026 trades ${e.trades}`, y26.total.includes(e.trades), y26.total.join('|'));
      check(`[${scope}] account-level row has dividends/fees/interest/refunds, unsplit`,
        !!y26.account && y26.account.includes('dividends $53.94') && y26.account.includes('fees -$27.54') && y26.account.includes('interest $199.52') && y26.account.includes('refunds $100.00'), y26.account);
      if (acct.prev !== undefined) check(`[${scope}] account-level row identical to the other filtered view`, y26.account === acct.prev, y26.account + ' vs ' + acct.prev);
      acct.prev = y26.account;
      check(`[${scope}] 647.05 / 1707.18 / -934.21 absent from the year table`, !/647\.05|1707\.18|934\.21/.test(y26.text), y26.text);
    }
    check(`[${scope}] YTD card and Copy-vs-Systematic panel unchanged`,
      (await page.$eval('.etoro-ytd-card', (x) => x.innerHTML)) === baseline.ytd && (await page.$eval('.cvs-panel', (x) => x.innerHTML)) === baseline.cvs);
    check(`[${scope}] YTD card net is $647.05`, (await text('.etoro-ytd-card')).includes('net $647.05'), await text('.etoro-ytd-card'));
  }

  // The statistics panel's own controls keep the scope.
  await page.click('#etoro-perf-panel .etoro-strat-btn[data-strat="copy"]');
  await page.waitForFunction(() => document.querySelector('.ts-scope-label').textContent.includes('Copy'));
  requests.length = 0;
  await page.selectOption('.ts-year', '2025');
  await page.waitForFunction(() => true);
  await page.waitForTimeout(200);
  check('changing the stats year keeps the scope', (requests.filter((r) => r.startsWith('/api/etoro/trade-stats')).pop() || '').includes('scope=copy'), JSON.stringify(requests));

  // Saved state survives a reload (new key), without the one-time seed overriding it.
  await page.reload();
  await page.click('button.tab[data-tab="performance"]');
  await page.waitForSelector('.etoro-year-block');
  check('chosen scope (copy) is remembered after reload', (await active()) === 'copy', await active());

  check('no page errors', errors.length === 0, errors.join(' | '));
  await browser.close();
  server.close();
  console.log(failures ? `\n${failures} FAILED` : '\nALL PASSED');
  process.exit(failures ? 1 : 0);
})().catch((e) => { console.error(e); process.exit(1); });
