// SC-49b — browser test of the Crypto Indicators freshness badges (real Chromium, real app.js,
// fixture API). Run: NODE_PATH=$(npm root -g) [CHROME_PATH=/usr/bin/google-chrome] node tools/e2e/indicator_freshness.js
const http = require('http');
const fs = require('fs');
const path = require('path');
const { chromium } = require('playwright');
const WEB = path.join(__dirname, '..', '..', 'web');

const card = (id, bucket, name, extra) => Object.assign({ id, bucket, displayName: name, source: 'x', currentValue: 1.5, tooltip: '' }, extra);
const INDICATORS = [
  card('pal_cfnai', 'pal', 'CFNAI red', { dataAsOf: '2026-06-10', dataAge: 125, dataAgeUnit: 'calendar', staleLevel: 'red', amberAt: 95, redAt: 120 }),
  card('pal_dxy', 'pal', 'DXY amber', { dataAsOf: '2026-09-28', dataAge: 15, dataAgeUnit: 'calendar', staleLevel: 'amber' }),
  card('pal_us2y', 'pal', 'US2Y amber', { dataAsOf: '2026-11-20', dataAge: 5, dataAgeUnit: 'trading', dataAgeTradingDays: 5, staleLevel: 'amber' }),
  card('universal_stablecoin_supply', 'universal', 'Stablecoin fresh', { dataAsOf: '2026-10-13', dataAge: 0, dataAgeUnit: 'calendar', staleLevel: '' }),
  card('universal_etf_flow_7d', 'universal', 'ETF errored', { dataAsOf: '2026-09-01', dataAge: 30, dataAgeUnit: 'trading', dataAgeTradingDays: 30, staleLevel: 'red', fetchError: 'farside: boom' }),
];
const server = http.createServer((req, res) => {
  const u = new URL(req.url, 'http://x');
  if (u.pathname.startsWith('/api/')) {
    res.setHeader('Content-Type', 'application/json');
    if (u.pathname === '/api/auth/state') return res.end(JSON.stringify({ state: 'authenticated', user: { id: 1, email: 't@example.com', username: 't' } }));
    if (u.pathname === '/api/crypto-indicators') return res.end(JSON.stringify({ indicators: INDICATORS, available: true, historyDays: 30, bucketLabels: { cowen: 'Cowen', pal: 'Pal', universal: 'Universal', sentiment: 'Sentiment' } }));
    if (u.pathname.endsWith('/history')) return res.end(JSON.stringify({ history: [] }));
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
  const browser = await chromium.launch(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {});
  const page = await (await browser.newContext({ viewport: { width: 1400, height: 1000 } })).newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(String(e)));
  await page.goto(`http://127.0.0.1:${server.address().port}/`);
  await page.click('button.tab[data-tab="crypto-indicators"]');
  await page.waitForSelector('.ci-card-v2');
  const meta = (id) => page.$eval(`.ci-card[data-indicator-id="${id}"] .ci-card-meta`, (e) => e.textContent.replace(/\s+/g, ' ').trim());
  const cfnai = await meta('pal_cfnai'), dxy = await meta('pal_dxy'), us2y = await meta('pal_us2y'), stable = await meta('universal_stablecoin_supply'), etf = await meta('universal_etf_flow_7d');
  check('CFNAI shows as-of date and a red badge in days', cfnai.includes('as of 2026-06-10') && cfnai.includes('stale · 125 days') && !cfnai.includes('trading'), cfnai);
  check('CFNAI badge is red', (await page.$$('.ci-card[data-indicator-id="pal_cfnai"] .ci-pill.err')).length === 1);
  check('DXY amber badge in days', dxy.includes('stale · 15 days') && (await page.$$('.ci-card[data-indicator-id="pal_dxy"] .ci-pill.stale')).length === 1, dxy);
  check('US2Y badge in trading days', us2y.includes('stale · 5 trading days'), us2y);
  check('fresh stablecoin: date only, no badge', stable.includes('as of 2026-10-13') && !stable.includes('stale'), stable);
  check('fetch error takes precedence over the freshness badge', !etf.includes('stale ·') && etf.includes('as of 2026-09-01'), etf);
  check('no page errors', errors.length === 0, errors.join(' | '));
  await browser.close(); server.close();
  console.log(failures ? `\n${failures} FAILED` : '\nALL PASSED');
  process.exit(failures ? 1 : 0);
})().catch((e) => { console.error(e); process.exit(1); });
