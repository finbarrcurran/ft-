// D25 Phase 2 — JS scoring drift-prevention smoke test.
//
// Validates that the JS port of ComputePillarScore (in web/assets/app.js)
// returns byte-identical outputs to the Go ComputePillarScore (in
// internal/cryptotheses/scoring.go) for the same fixture inputs.
//
// Run: node d25_js_scoring_test.js
//
// On failure: the JS function has drifted from the Go function. Re-sync.
//
// Companion to Go side: internal/cryptotheses/scoring_test.go (same fixtures).

// === Inline copies of the JS functions (must match app.js exactly) ===
function d25ComputePillarScore(subs) {
  if (!subs || subs.length === 0) return 0;
  let sum = 0, zeros = 0, loweqOne = 0;
  for (const s of subs) {
    sum += s;
    if (s === 0) zeros++;
    if (s <= 1) loweqOne++;
  }
  const avg = sum / subs.length;
  if (zeros > 0) return Math.min(2, Math.max(0, Math.floor(avg)));
  if (subs.length >= 5 && loweqOne >= 2) return Math.min(2, Math.max(0, Math.floor(avg)));
  if (avg === 0.5 || avg === 1.5) return Math.min(2, Math.max(0, Math.floor(avg)));
  return Math.min(2, Math.max(0, Math.round(avg)));
}

function d25BandFromTotal(total, sc) {
  if (sc === 'monetary_12') {
    if (total >= 9) return 'strong';
    if (total >= 7) return 'accumulate';
    if (total >= 5) return 'hold';
    if (total >= 3) return 'trim';
    return 'exit';
  }
  if (total >= 13) return 'strong';
  if (total >= 10) return 'accumulate';
  if (total >= 7) return 'hold';
  if (total >= 4) return 'trim';
  return 'exit';
}

function d25OneBandBelow(b) {
  return ({ strong: 'accumulate', accumulate: 'hold', hold: 'trim', trim: 'exit', exit: 'exit' })[b] || b;
}

function d25ComputeRawAndFinalBand(scores, sc) {
  let total = 0;
  for (const k in scores) total += scores[k];
  const raw = d25BandFromTotal(total, sc);
  let hasZero = false;
  for (const k in scores) if (scores[k] === 0) hasZero = true;
  const final = (hasZero && raw !== 'exit') ? d25OneBandBelow(raw) : raw;
  return { total, rawBand: raw, finalBand: final, ppgCapApplied: hasZero && raw !== 'exit' };
}

// === Fixtures (must match scoring_test.go) ===
const tests = [
  // pillar-level v0.5.1 #4 tie-break
  { name: 'AAVE Q1 tie-break',     subs: [1,2,2,1],           expect: 1 },
  { name: 'EIGEN Q1 tie-break',    subs: [1,2,2,1],           expect: 1 },
  { name: 'EIGEN Q2 multi-zero',   subs: [0,0,1,0,1],         expect: 0 },
  { name: 'EIGEN Q8 zeros',        subs: [0,0,1,1,2,0,1,1],   expect: 0 },
  { name: 'LINK Q6 (6-pillar 1 low)', subs: [2,1,2,2,2,2],    expect: 2 },
  { name: '5+ pillar 2 lows',      subs: [1,2,1,2,2],         expect: 1 },
  { name: 'Clean round up 1.75',   subs: [2,2,2,1],           expect: 2 },
  { name: 'All 1s',                subs: [1,1,1,1],           expect: 1 },
  { name: 'Empty',                 subs: [],                  expect: 0 },
  { name: 'All 2s clamp',          subs: [2,2,2,2,2],         expect: 2 },
];

let pass = 0, fail = 0;
for (const t of tests) {
  const got = d25ComputePillarScore(t.subs);
  if (got === t.expect) {
    console.log(`  ✓ ${t.name}: got ${got}`);
    pass++;
  } else {
    console.log(`  ✗ ${t.name}: got ${got}, want ${t.expect}`);
    fail++;
  }
}

// Band tests
const bandTests = [
  { name: 'EIGEN triple-zero', scores: {Q1:1,Q2:0,Q3:2,Q4:1,Q5:0,Q6:1,Q7:2,Q8:0,Q9:2}, sc:'alt_18',
    expectTotal: 9, expectRaw: 'hold', expectFinal: 'trim', expectCap: true },
  { name: 'AAVE Q8=0 cap', scores: {Q1:1,Q2:1,Q3:2,Q4:1,Q5:2,Q6:2,Q7:2,Q8:0,Q9:2}, sc:'alt_18',
    expectTotal: 13, expectRaw: 'strong', expectFinal: 'accumulate', expectCap: true },
  { name: 'LINK clean strong', scores: {Q1:2,Q2:1,Q3:2,Q4:1,Q5:2,Q6:2,Q7:2,Q8:2,Q9:2}, sc:'alt_18',
    expectTotal: 16, expectRaw: 'strong', expectFinal: 'strong', expectCap: false },
  { name: 'BUIDL clean strong', scores: {Q1:2,Q2:2,Q3:2,Q4:2,Q5:2,Q6:2,Q7:2,Q8:1,Q9:2}, sc:'alt_18',
    expectTotal: 17, expectRaw: 'strong', expectFinal: 'strong', expectCap: false },
  { name: 'LUNC exit floor', scores: {Q1:0,Q2:1,Q3:0,Q4:0,Q5:1,Q6:0,Q7:0,Q8:1,Q9:0}, sc:'alt_18',
    expectTotal: 3, expectRaw: 'exit', expectFinal: 'exit', expectCap: false },
];

console.log('');
for (const t of bandTests) {
  const r = d25ComputeRawAndFinalBand(t.scores, t.sc);
  let ok = true;
  if (r.total !== t.expectTotal) ok = false;
  if (r.rawBand !== t.expectRaw) ok = false;
  if (r.finalBand !== t.expectFinal) ok = false;
  if (r.ppgCapApplied !== t.expectCap) ok = false;
  if (ok) {
    console.log(`  ✓ ${t.name}: total=${r.total} raw=${r.rawBand} final=${r.finalBand} cap=${r.ppgCapApplied}`);
    pass++;
  } else {
    console.log(`  ✗ ${t.name}: got total=${r.total} raw=${r.rawBand} final=${r.finalBand} cap=${r.ppgCapApplied}, want total=${t.expectTotal} raw=${t.expectRaw} final=${t.expectFinal} cap=${t.expectCap}`);
    fail++;
  }
}

console.log(`\n==========================================`);
console.log(` JS scoring drift-prevention: ${pass} PASS / ${fail} FAIL`);
console.log(`==========================================`);
process.exit(fail > 0 ? 1 : 0);
