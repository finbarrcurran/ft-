# Sector Adapter — REIT / Real Estate — v1 (Draft)

> **Status:** Fifteenth adapter under the Cross-Sector Investment Philosophy v1.1. Covers real estate investment trusts and property companies whose economics are rent + asset value + cost of capital — a fundamentally different scorecard from operating companies (no "moat/bottleneck" in the usual sense; the drivers are FFO, cap rates, leverage, and rate sensitivity). **First operating-stock adapter built around a balance-sheet/yield model rather than a product/franchise model.**
> **Doctrine sources:** `Asset_Hedge_Adapter_v1.md` (the precedent for a *different scorecard shape* when the asset isn't an operating franchise), `Cloud_Infra_Sector_Adapter_v1_1_Lock_Supplement.md` (data-center-REIT boundary), `Pal_Macro_Liquidity_Framework.md` (rate sensitivity is central), `Cross_Sector_Investment_Philosophy_v1_1.md`.
> **Calibration note:** Complete template per user request. **Uncalibrated** — no REIT held (data-center REITs currently route to Cloud-Infra per its supplement). First REIT evaluation calibrates. **Uses a modified /16 with REIT-specific pillars** — see §3.
> **Not investment advice. Personal use only.**

---

## 1. Scope

### In-scope
- **Equity REITs by property type**: residential, retail, office, industrial/logistics, data-center (pure-play), healthcare, self-storage, towers (cell-tower REITs)
- **Specialty REITs**: data centers (EQIX, DLR — pure-property thesis), towers (AMT, CCI), timberland, gaming/net-lease
- **Property companies / developers** with REIT-like economics

### Out-of-scope (different adapter)
- **Data-center capacity as a hyperscaler-adjacent thesis** (where ≥50% revenue is hyperscaler-driven compute capacity) → Cloud-Infra (per its supplement — the ORCL/DLR boundary)
- **Mortgage REITs (mREITs)** → flagged distinct (they're leveraged rate-spread vehicles, not property — score with extra caution or exclude; backlog)
- **Physical-metal/commodity stores** → Asset-Hedge adapter
- **Homebuilders** (operating cyclicals, not rent-yield) → Industrial/Consumer-Discretionary (not built)

### The data-center boundary (inherited from Cloud-Infra supplement)
A data-center REIT can be scored *either* way: as a **property/rent thesis** (here) or a **hyperscaler-capacity thesis** (Cloud-Infra). Route by what drives the return: if it's rent escalators + cap rates + the real-estate cycle → here; if it's hyperscaler compute demand + AI-buildout capacity → Cloud-Infra. When ambiguous, run both (primary + advisory) per §13 hybrid handling.

### What this pays for
A REIT pays for **a rent-yield annuity plus property-value appreciation, financed by debt** — so the return is driven by occupancy/rent growth, the spread between cap rates and cost of debt, and the property-type's secular demand. The thesis is fundamentally **rate-sensitive** (REITs are long-duration income assets) — which ties this adapter tightly to the Pal/9p macro regime. The bet is on a property type with secular tailwinds + disciplined leverage + a favorable rate path.

---

## 2. Sub-types within this adapter

| Sub-type | Code | Notes |
|---|---|---|
| Industrial / logistics | `industrial-logistics` | Warehouses/fulfillment; e-commerce secular tailwind |
| Data-center (pure-property) | `datacenter-reit` | EQIX/DLR as property thesis; AI-demand overlay (Cloud-Infra advisory) |
| Towers / infrastructure | `tower-reit` | AMT/CCI; long leases, oligopoly, but rate-sensitive + carrier-capex dependent |
| Residential | `residential-reit` | Apartments/SFR; demographic + housing-shortage thesis |
| Retail / net-lease | `retail-netlease` | Net-lease (O) vs malls; quality varies enormously |
| Healthcare / specialty | `healthcare-specialty` | Senior housing, medical office, self-storage, timber |

Sub-type drives which secular-demand and lease-structure sub-criteria dominate.

---

## 3. The Eight-Pillar Adapter (/16) — REIT-specific pillars
REITs don't fit the operating-company 8-Q cleanly, so the pillars are **remapped** (still 0/1/2, total /16, pass = 6+ pillars ≥1 with R1 + R5 emphasis). This is the same "right scorecard for the right asset" move as Asset-Hedge's /8 and BTC's /12 — but kept at /16 because REITs are full operating entities, just with different drivers.

### R1 — Property-Type Secular Demand *(the "bottleneck" analogue)*
Is the property type in structural demand growth or structural decline? (Industrial/logistics + data-center + towers = tailwind; office + malls = secular headwind). Sub-criteria: secular demand direction, supply constraint (is new supply easy or hard to add), pricing power on lease renewals.

### R2 — Narrative *(universal)*
The property-cycle/rate narrative — current/mature/stale. Anti-inversion: a "REITs are cheap because rates will fall" narrative that's fully priced scores lower.

### R3 — Asset & Lease Quality *(the "moat" analogue)*
Sub-criteria: lease structure (long net-lease + escalators = strong; short/gross = weak), tenant quality + diversification, asset location/irreplaceability, occupancy trend. This is the REIT moat — irreplaceable assets with long, escalating, well-tenanted leases.

### R4 — Balance Sheet & Leverage *(intensity analogue — CRITICAL for REITs)*
Sub-criteria: debt/EBITDA + LTV, debt-maturity ladder (refinancing wall risk in a high-rate world), fixed-vs-floating mix, cost of capital vs cap rates. **A REIT's balance sheet is its survival** — over-leveraged REITs die in rate-hike cycles. Score conservatively.

### R5 — FFO / Distribution Quality *(visibility analogue — DOMINANT)*
Sub-criteria: FFO/AFFO growth, payout ratio sustainability (is the dividend covered by AFFO), distribution-growth track record, occupancy/rent-roll visibility. This is the income annuity — the heart of a REIT thesis. An uncovered distribution is a red flag.

### R6 — Sovereignty / Jurisdiction *(universal)*
Geographic concentration, regulatory (rent control, zoning, REIT-tax-status compliance), currency for international property.

### R7 — Catalyst *(universal)*
Rate-cut cycle inflection (the big one — REITs re-rate hard on rate direction), portfolio repositioning, major development delivery, cap-rate compression, M&A.

### R8 — Technicals & Rate Regime *(universal — DOMINANT macro link)*
**This pillar reads the Pal/9p macro regime directly.** REITs are long-duration income assets — a rising-2Y / strong-DXY regime crushes them; a falling-rate regime is their tailwind. Sub-criteria: rate-regime read (from 9e/9p), sector technicals, REIT-vs-bond-yield spread. Tightest macro-linkage of any stock adapter.

---

## 4. Worked example
*None yet.* First candidate likely an `industrial-logistics` or `datacenter-reit` name. Provisional expectation: a quality industrial REIT with strong secular demand (R1) + long escalating leases (R3) + a clean balance sheet (R4) + covered growing distribution (R5) in a *favorable* rate regime (R8) scores high; the same REIT in a rising-rate regime scores materially lower via R8 — which is correct and is the adapter's signature feature.

## 5. VETO / kill criteria
### Universal VETOs apply.
### REIT-specific
- **Distribution cut or uncovered payout** (AFFO < distribution sustained) → veto check — the income thesis is broken
- **Refinancing wall** in a high-rate environment with no plan (large debt maturing, can't refinance affordably) → veto
- **Occupancy collapse** in a structurally-declining property type (office archetype) → veto check
- **REIT-status loss** (fails the tax-qualification tests) → veto
- **Covenant breach / forced asset sales** → veto

## 6. Open decisions
1. The remapped pillars (R1–R8) — confirm the mapping holds at first use, or whether REITs warrant a fully custom scorecard like Asset-Hedge's /8.
2. mREITs — confirm excluded (leveraged rate-spread vehicles, genuinely different) rather than a sub-type here.
3. Data-center-REIT routing — confirm the Cloud-Infra-vs-here boundary rule (drive-by-return-source).
4. R8 macro-link — confirm it reads 9e/9p regime state directly (it should, given REIT rate-sensitivity).

*REIT/Real Estate adapter v1 (template, uncalibrated; REIT-remapped /16). Authored 2026-06-01, Claude.ai. Personal use only. Not investment advice.*
