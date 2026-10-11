#!/usr/bin/env node
/**
 * capitol-trades-fetch.js — daily scrape of US Congress stock trades
 * (House + Senate combined) from capitoltrades.com via headless
 * Chromium. The original House/Senate Stock Watcher S3 buckets that
 * FT's IngestCongress was built against have been 403'd indefinitely;
 * this Playwright route bypasses the gap.
 *
 * Writes the parsed table to /var/lib/ft/data/capitol-trades/trades.json
 * as JSON of shape:
 *
 *   {
 *     "fetchedAt": "2026-05-28T22:00:00Z",
 *     "source":    "capitoltrades.com/trades",
 *     "trades":    [
 *       {
 *         "tradeDate":      "2026-05-25",
 *         "disclosureDate": "2026-05-27",
 *         "politician":     "Pelosi, Nancy",
 *         "party":          "D",
 *         "chamber":        "house",
 *         "ticker":         "NOW",
 *         "type":           "BUY" | "SELL" | "EXCH",
 *         "amount":         "$15,001 - $50,000",
 *         "assetName":      "ServiceNow Inc",
 *         "sourceUrl":      "https://www.capitoltrades.com/trades/..."
 *       }, ...
 *     ]
 *   }
 *
 * FT's congress.go reads this file at refresh time and falls through
 * to a stub if missing. Same fallback pattern as the Farside scraper.
 *
 * Cron: 5 0 * * *  curran  /usr/local/bin/node /home/curran/scripts/capitol-trades-fetch.js
 */

const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');

const PAGES_TO_FETCH = 25; // ~25 pages × 12 rows = 300 trades (~3-4 months of activity)
const PER_PAGE = 96;       // capitoltrades ignores >12 — kept for explicitness
const OUT_DIR = '/var/lib/ft/data/capitol-trades';
const OUT_FILE = path.join(OUT_DIR, 'trades.json');
const DEBUG_SCREENSHOT = path.join(OUT_DIR, 'last-debug.png');
const FETCH_TIMEOUT_MS = 90000;

function parseTradeDate(s) {
  // Capitol-trades uses formats like "25 May 2026", "May 25 2026", or relative ("2 days").
  if (!s) return null;
  const m = s.match(/(\d{1,2})\s+([A-Za-z]{3,})\s+(\d{4})/);
  if (m) {
    const months = { Jan: '01', Feb: '02', Mar: '03', Apr: '04', May: '05', Jun: '06',
                     Jul: '07', Aug: '08', Sep: '09', Oct: '10', Nov: '11', Dec: '12' };
    const mm = months[m[2].slice(0, 3)];
    if (mm) return `${m[3]}-${mm}-${m[1].padStart(2, '0')}`;
  }
  // YYYY-MM-DD already
  if (/^\d{4}-\d{2}-\d{2}/.test(s)) return s.slice(0, 10);
  return null;
}

function normaliseType(s) {
  if (!s) return null;
  s = s.toLowerCase();
  if (s.includes('buy') || s.includes('purchase')) return 'BUY';
  if (s.includes('sell') || s.includes('sale')) return 'SELL';
  if (s.includes('exch')) return 'EXCH';
  return null;
}

(async () => {
  let browser;
  try {
    browser = await chromium.launch({
      headless: true,
      channel: 'chromium',
      args: [
        '--no-sandbox',
        '--disable-setuid-sandbox',
        '--disable-dev-shm-usage',
        '--disable-blink-features=AutomationControlled',
      ],
    });
    const ctx = await browser.newContext({
      userAgent: 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
      viewport: { width: 1440, height: 900 },
      locale: 'en-US',
    });

    fs.mkdirSync(OUT_DIR, { recursive: true });

    const allTrades = [];
    const seenSourceUrls = new Set();

    for (let pageNum = 1; pageNum <= PAGES_TO_FETCH; pageNum++) {
      // No date filter — capitoltrades' txDate query param is JS-applied
      // and returns "No results" when set via direct URL. The unfiltered
      // /trades view returns newest-first, which is what we want anyway.
      const url = `https://www.capitoltrades.com/trades?per_page=${PER_PAGE}&page=${pageNum}`;
      const page = await ctx.newPage();
      await page.goto(url, { waitUntil: 'domcontentloaded', timeout: FETCH_TIMEOUT_MS });
      // Take a screenshot regardless so we can debug on failures.
      try { await page.screenshot({ path: DEBUG_SCREENSHOT, fullPage: false }); } catch (_) {}
      // Wait for ANY table row anywhere — capitol-trades is server-rendered
      // so this should fire near-immediately.
      await page.waitForSelector('table tr', { timeout: 30000 });
      // Tiny extra wait for hydration.
      await page.waitForTimeout(800);
      try { if (pageNum === 1) await page.screenshot({ path: DEBUG_SCREENSHOT, fullPage: false }); } catch (_) {}

      const trades = await page.evaluate(() => {
        // Capitol-trades' Next.js renders 10 cells per row in known order:
        //   0: politician + "PartyChamberState" (e.g. "DemocratHouseIL")
        //   1: issuer name + "TICKER:EXCHANGE" on second line
        //   2: published time + "Today"/"Yesterday"
        //   3: trade date "DD Mon" + year
        //   4: filed-after days
        //   5: owner ("Joint" | "Spouse" | "Undisclosed" | …)
        //   6: trade type — "BUY" | "SELL" | "EXCH"
        //   7: size bucket — "1K–15K", "15K–50K", "50K–100K", "100K–250K", "250K–500K", "500K–1M", "1M–5M", "5M+"
        //   8: trade price
        //   9: detail-page link
        const rows = Array.from(document.querySelectorAll('table tbody tr'));
        const out = [];
        for (const tr of rows) {
          const polLink = tr.querySelector('a[href*="/politicians/"]');
          const issLink = tr.querySelector('a[href*="/issuers/"]');
          const tradeLink = tr.querySelector('a[href*="/trades/"]');
          const cells = Array.from(tr.querySelectorAll('td')).map((c) => c.innerText.trim());
          out.push({
            politicianName: polLink?.innerText.trim() || null,
            politicianHref: polLink?.getAttribute('href') || null,
            issuerName:     issLink?.innerText.trim() || null,
            sourceUrl:      tradeLink ? new URL(tradeLink.getAttribute('href'), location.origin).href : null,
            cells,
          });
        }
        return out;
      });

      let added = 0;
      for (const t of trades) {
        if (!t.politicianName || !t.issuerName) continue;
        if (t.sourceUrl && seenSourceUrls.has(t.sourceUrl)) continue;
        if (t.sourceUrl) seenSourceUrls.add(t.sourceUrl);

        // Cell 0: "Jonathan Jackson\nDemocratHouseIL"
        const polCell = t.cells[0] || '';
        const polLines = polCell.split('\n');
        const partyChamberState = polLines[1] || '';
        // Party / chamber / state regex split — examples observed:
        //   DemocratHouseIL, RepublicanSenateTX, IndependentSenateME
        let party = null, chamber = 'unknown', state = null;
        const pcs = partyChamberState.match(/^(Democrat|Republican|Independent|Libertarian|Green|Other)(House|Senate)([A-Z]{2})/);
        if (pcs) {
          party = pcs[1].charAt(0);
          chamber = pcs[2].toLowerCase();
          state = pcs[3];
        }

        // Cell 1: "O'Reilly Automotive Inc\nORLY:US"
        const issCell = t.cells[1] || '';
        const issLines = issCell.split('\n');
        const assetName = issLines[0] || t.issuerName;
        const tickerPart = issLines[1] || '';
        let ticker = null;
        const tm = tickerPart.match(/^([A-Z][A-Z0-9.\-]{0,10})(?::|$)/);
        if (tm) ticker = tm[1];

        // Cell 3: trade date "24 Apr\n2026"
        const dateCell = t.cells[3] || '';
        const dateLines = dateCell.split('\n');
        const tradeDate = parseTradeDate(`${dateLines[0]} ${dateLines[1] || ''}`.trim());

        // Cell 2: published / disclosure ("13:00\nToday" or "13:00\n3 days")
        // Capitol-trades doesn't surface a parseable disclosure date in
        // a single cell — use tradeDate as the disclosureDate fallback.
        const disclosureDate = tradeDate;

        // Cell 6: trade type
        const type = normaliseType(t.cells[6] || '');
        // Cell 7: size bucket
        const amount = t.cells[7] || null;

        allTrades.push({
          tradeDate,
          disclosureDate,
          politician: t.politicianName,
          party,
          chamber,
          state,
          ticker,
          type,
          amount,
          assetName,
          sourceUrl: t.sourceUrl,
        });
        added++;
      }
      await page.close();
      console.log(`page ${pageNum}: ${trades.length} rows scraped, ${added} new`);
      if (added === 0) break; // empty page — stop paginating
    }

    const output = {
      fetchedAt: new Date().toISOString(),
      source: 'capitoltrades.com/trades',
      tradeCount: allTrades.length,
      trades: allTrades,
    };
    fs.writeFileSync(OUT_FILE, JSON.stringify(output, null, 2));
    try { fs.chmodSync(OUT_FILE, 0o644); } catch (_) {}
    console.log(`ok trades=${allTrades.length} → ${OUT_FILE}`);
    await browser.close();
    process.exit(0);
  } catch (err) {
    console.error(`ERROR capitol-trades-fetch: ${err.message}`);
    if (browser) await browser.close().catch(() => {});
    process.exit(1);
  }
})();
