#!/usr/bin/env node
/**
 * farside-fetch.js — daily scrape of BTC spot-ETF net flows from
 * farside.co.uk via headless Chromium (bypasses Cloudflare bot block
 * that defeated FT's native Go HTTP client).
 *
 * Writes the parsed flow table to /var/lib/ft/data/farside/etf-flow.json
 * as JSON of shape:
 *
 *   {
 *     "fetchedAt":   "2026-05-28T00:25:00Z",  // ISO UTC
 *     "source":      "farside.co.uk/bitcoin-etf-flow-all-data/",
 *     "rolling7dM":  1234.5,                  // 7d sum, USD millions
 *     "rows": [
 *       { "date": "2026-05-27", "totalM": -123.4 },
 *       ...
 *     ]
 *   }
 *
 * FT's Farside provider reads this file at refresh time and uses the
 * rolling7dM value directly. Falls back to live HTTP scrape if file
 * missing or older than 36 hours.
 *
 * Run: node /home/curran/scripts/farside-fetch.js
 * Cron: 25 0 * * *  curran  /usr/local/bin/node /home/curran/scripts/farside-fetch.js >> /var/log/ft-farside-fetch.log 2>&1
 */

const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');

const URL = 'https://farside.co.uk/bitcoin-etf-flow-all-data/';
const OUT_DIR = '/var/lib/ft/data/farside';
const OUT_FILE = path.join(OUT_DIR, 'etf-flow.json');
const DEBUG_SCREENSHOT = path.join(OUT_DIR, 'last-debug.png');
const FETCH_TIMEOUT_MS = 90000;

function parseFarsideNumber(s) {
  if (!s || typeof s !== 'string') return null;
  s = s.trim();
  if (s === '' || s === '-' || s === '—' || /^n\/a$/i.test(s)) return null;
  let neg = false;
  if (s.startsWith('(') && s.endsWith(')')) {
    neg = true;
    s = s.slice(1, -1);
  }
  if (s.startsWith('-')) {
    neg = true;
    s = s.slice(1);
  }
  s = s.replace(/[$,]/g, '').trim();
  const v = parseFloat(s);
  if (Number.isNaN(v)) return null;
  return neg ? -v : v;
}

function parseFarsideDate(s) {
  if (!s) return null;
  s = s.trim();
  // Farside uses "02 Jan 2026" most commonly.
  const m = s.match(/^(\d{1,2})\s+([A-Za-z]{3})\s+(\d{4})$/);
  if (!m) return null;
  const months = { Jan: '01', Feb: '02', Mar: '03', Apr: '04', May: '05', Jun: '06', Jul: '07', Aug: '08', Sep: '09', Oct: '10', Nov: '11', Dec: '12' };
  const mm = months[m[2]];
  if (!mm) return null;
  return `${m[3]}-${mm}-${m[1].padStart(2, '0')}`;
}

(async () => {
  let browser;
  try {
    // Use the full chromium build, NOT the lighter chrome-headless-shell.
    // The headless-shell variant has a more distinct fingerprint that
    // Cloudflare bot management catches more often. Full chromium with
    // headless: 'new' (Chrome's modern headless mode) is the most
    // browser-like configuration available without going non-headless.
    browser = await chromium.launch({
      headless: true,
      channel: 'chromium', // force full chromium build
      args: [
        '--no-sandbox',
        '--disable-setuid-sandbox',
        '--disable-dev-shm-usage',
        '--disable-blink-features=AutomationControlled',
      ],
    });
    const ctx = await browser.newContext({
      userAgent:
        'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
      viewport: { width: 1280, height: 800 },
      locale: 'en-GB',
      timezoneId: 'Europe/London',
    });
    const page = await ctx.newPage();
    await page.goto(URL, { waitUntil: 'networkidle', timeout: FETCH_TIMEOUT_MS });
    // Wait for the table that holds the data. Farside's table has many
    // numeric columns; we wait for the body to contain at least one
    // date-shaped string. NOTE: Playwright's waitForFunction signature is
    // (fn, arg, options) — options is the THIRD positional. Use null for
    // arg when we don't need to pass one.
    await page.waitForFunction(
      () => {
        const tables = document.querySelectorAll('table');
        for (const t of tables) {
          if (/\d{1,2}\s+[A-Za-z]{3}\s+\d{4}/.test(t.innerText)) return true;
        }
        return false;
      },
      null,
      { timeout: FETCH_TIMEOUT_MS, polling: 1000 },
    );
    // Belt-and-braces screenshot of whatever we landed on, for debugging
    // if extraction fails.
    try { await page.screenshot({ path: DEBUG_SCREENSHOT, fullPage: false }); } catch (_) {}

    // Extract rows: each <tr> has a date in cell 0 and Total in the last cell.
    // We find the header row to confirm column positions defensively.
    const data = await page.evaluate(() => {
      const tables = Array.from(document.querySelectorAll('table'));
      // Heuristic: the right table has > 6 columns and contains "Total"
      // somewhere in its header.
      const target = tables.find((t) => {
        const head = (t.querySelector('thead') || t.rows[0])?.innerText.toLowerCase() || '';
        return head.includes('total') && t.rows.length > 10;
      });
      if (!target) return { rows: [], reason: 'no candidate table found' };

      // Find header indices for Date and Total.
      const headRow = target.querySelector('thead tr') || target.rows[0];
      const heads = Array.from(headRow.cells).map((c) => c.innerText.trim().toLowerCase());
      const dateIdx = heads.findIndex((h) => h === 'date');
      const totalIdx = heads.findIndex((h) => h === 'total');
      if (dateIdx < 0 || totalIdx < 0) {
        return { rows: [], reason: `header indices missing (date=${dateIdx}, total=${totalIdx})` };
      }

      const out = [];
      const bodyRows = Array.from(target.querySelectorAll('tbody tr'));
      for (const tr of bodyRows) {
        const cells = Array.from(tr.cells);
        if (cells.length <= Math.max(dateIdx, totalIdx)) continue;
        const dateText = cells[dateIdx]?.innerText.trim() || '';
        const totalText = cells[totalIdx]?.innerText.trim() || '';
        if (!dateText) continue;
        out.push({ dateText, totalText });
      }
      return { rows: out, reason: null };
    });

    if (!data.rows || data.rows.length === 0) {
      throw new Error(`Parsed 0 rows. Reason: ${data.reason || 'unknown'}`);
    }

    // Normalise + sort by date desc.
    const rows = data.rows
      .map((r) => ({ date: parseFarsideDate(r.dateText), totalM: parseFarsideNumber(r.totalText) }))
      .filter((r) => r.date && r.totalM !== null);
    rows.sort((a, b) => (a.date < b.date ? 1 : -1));

    // 7-day rolling sum of the most recent 7 rows.
    const last7 = rows.slice(0, 7);
    const rolling7dM = last7.length === 7
      ? last7.reduce((s, r) => s + r.totalM, 0)
      : null;

    const output = {
      fetchedAt: new Date().toISOString(),
      source: URL,
      rolling7dM,
      totalRows: rows.length,
      rows: rows.slice(0, 90), // keep 90 days for future charting
    };

    fs.mkdirSync(OUT_DIR, { recursive: true });
    fs.writeFileSync(OUT_FILE, JSON.stringify(output, null, 2));
    // Make file readable by the ft user (cron runs as curran; ft is the
    // service user. Both are in the curran group on this box; rw-r--r--
    // covers it).
    try { fs.chmodSync(OUT_FILE, 0o644); } catch (e) { /* ignore */ }

    console.log(
      `ok rows=${rows.length} latest=${rows[0].date} rolling7dM=${rolling7dM != null ? rolling7dM.toFixed(1) : 'null'} → ${OUT_FILE}`,
    );
    await browser.close();
    process.exit(0);
  } catch (err) {
    console.error(`ERROR farside-fetch: ${err.message}`);
    if (browser) await browser.close().catch(() => {});
    process.exit(1);
  }
})();
