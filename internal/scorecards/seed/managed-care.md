# Sector Adapter — Managed-Care / Health-Insurance — v1 (Draft)

> **Status:** Adapter built to close the Managed-Care / health-payer NULL-adapter gap surfaced by CLOV routing (2026-06-09). Pharma explicitly excludes payers ("healthcare services / payers (UNH, CI) → out of personal portfolio scope"); the Financials `insurer` sub-type is scoped to P&C / life / reinsurance (combined ratio, float, reserves) and does not model a health plan's economics. Health payers run on the **medical cost ratio, Star ratings, the CMS risk-adjustment regime, and membership retention** — none of which the existing adapters score.
> **Doctrine sources:** `REIT_Sector_Adapter_v1.md` (remapped-pillar precedent for a regime-driven business), `Financials_Sector_Adapter_v1.md` (remapped /16 + survival-pillar precedent), `Clinical_Stage_Biotech_Sector_Adapter_v1.md` (mandatory position-cap precedent for binary/subscale names), `Cross_Sector_Investment_Philosophy_v1_1.md`, `Pal_Macro_Liquidity_Framework.md` (rate sensitivity is *secondary* here — see §1).
> **Calibration note:** Template per user sign-off (D-A→D-H, 2026-06-09). **Uncalibrated** — CLOV worked example in §4 is the first calibration anchor, illustrative only. First lock calibrates (same discipline that calibrated Asset-Hedge vs GLD/SLV and Financials/REIT against their first names).
> **Build dependency:** adapter slug `managed_care` + the §2 sub-type slugs must be registered in the FT parser before any locked thesis in this sector can upload (same gate as `heavy_machinery` / `utilities_ipp`). *(Registered in `sector_scorecards` under hyphenated code `managed-care` per the live naming convention — see §8.)*
> **Not investment advice. Personal use only.**

---

## 1. Scope

### In-scope (D-A: all three managed-care types, sub-typed)
- **Medicare Advantage–focused payers** — CLOV, HUM archetype; capitated MA plans, Star-driven economics.
- **Diversified managed care** — UNH (+ Optum), CI (+ Evernorth), ELV archetype; multi-line payers with PBM/services arms (multi-segment, see D-F/D-G).
- **Medicaid MCOs** — MOH, CNC archetype; state-contracted, rate-adequacy and redetermination-driven.
- **Commercial / group health** — employer-sponsored books within diversified payers.

### Out-of-scope (different adapter)
- **Branded pharma / biotech** → Pharma / Clinical-Stage Biotech.
- **P&C / life / reinsurance** → Financials `insurer` (combined ratio / float / reserves business — different survival axis).
- **Pure healthtech SaaS** (no insurance balance sheet) → future SaaS adapter. *A payer's in-house tech arm is scored here via multi-segment, not routed out (D-F).*
- **Providers / hospitals / device / diagnostics / tools** → respective healthcare adapters (TBD).

### The rotation thesis (how managed care differs)

| AI-Infra rotation | Pharma rotation | **Managed-Care rotation** |
|---|---|---|
| Demand step-change | Demographic + payer regime | **CMS reimbursement-regime shift + medical-cost control** |
| Contracted backlog | Patent runway | **Star ratings + member retention (recurring capitation)** |
| Customer = hyperscalers | Customer = payers + patients | **Customer = government (CMS/states) + members — government-dominant** |
| Lock-in via PPAs | Lock-in via patents | **Lock-in via Star bonuses, network, member stickiness** |

The binding constraint is **not physical infrastructure and not patents** — it is **a durable medical-cost advantage operating under a regulated reimbursement ceiling**. The scarcity is *the ability to hold the medical cost ratio below the capitation rate, sustainably, while retaining members*. When the reimbursement regime tightens (adverse risk-adjustment/Star) or cost trend runs away, the equity de-rates regardless of current membership growth.

**Macro link is reimbursement-regime-dominant, NOT rate-dominant.** This is the key contrast with Financials: a health payer is sensitive primarily to CMS rate notices, the risk-adjustment model (RAF / V28), and Star-rating thresholds — the Pal/9p rate regime is secondary (it touches the investment float, not the core economics).

---

## 2. Sub-types within this adapter (D-C confirmed)

| Sub-type | Code | Notes |
|---|---|---|
| MA-focused | `ma-focused` | Capitated Medicare Advantage; Star + RAF + cohort MCR central |
| Diversified managed care | `diversified-managed-care` | Multi-line payer + PBM/services arms; multi-segment (D-G) |
| Medicaid MCO | `medicaid-mco` | State-contracted; rate adequacy + redetermination cycle central |
| Commercial / group | `commercial-group` | Employer-sponsored; pricing/renewal + utilisation trend central |

**`turnaround/subscale` flag (D-E):** an orthogonal tag (not a sub-type) applied to subscale or newly-profitable names with thin margin-of-safety (CLOV archetype: one profitable quarter, thin guided GAAP NI, regulation-exposed). Triggers **mandatory caution + a position cap** (Clinical-Stage Biotech precedent). The flag caps conviction band; it does not change the pillars.

---

## 3. The Eight-Pillar Adapter (/16) — managed-care-specific pillars (D-B: remapped)

Health payers don't fit the operating 8-Q, so the pillars are **remapped** (still 0/1/2, total /16). **Pass = 6+ pillars ≥1, with M3 + M5 + M6 emphasis, and M1 ≥1 and M3 ≥1 required.** Same "right scorecard for the right asset" move as REIT R1–R8 / Financials F1–F8. **M3 (medical cost ratio) and M6 (reimbursement regime) are the survival pillars — score them conservatively.**

### M1 — Cost Moat / Bottleneck *(the "bottleneck" analogue — required ≥1)*
The durable medical-cost advantage: clinical-cost engineering (Clover Assistant archetype — AI-driven early intervention lowering downstream acute cost), scale economies, provider-network alignment, proprietary clinical data, vertical integration (owned care delivery / PBM). Sub-criteria: structural MCR advantage vs peers; durability/widening with cohort tenure; defensibility (can a peer replicate it?). Score: 0 = no cost edge / pure rate-taker, 1 = emerging or unproven-at-scale edge, 2 = proven, widening, defensible cost advantage.

### M2 — Narrative *(universal)*
The managed-care-cycle narrative — current / mature / stale. Anti-inversion + reset/re-discovery (note #11): a fully-priced "managed-care rising tide" narrative scores lower; an unpriced reset with observable re-discovery scores higher.

### M3 — Medical Cost Ratio Discipline (MCR / BER) *(SURVIVAL pillar — required ≥1, score conservatively)*
Is the benefit-expense ratio stable/improving and *sustainably* so? Cohort maturation curve, medical-cost-trend management, newer-cohort behaviour, seasonality adjustment. Sub-criteria: BER level vs sustainable target; trend (improving / stable / drifting); proven durability vs single-quarter. Score: 0 = runaway or deteriorating BER, 1 = stable but unproven / single-quarter, 2 = multi-quarter proven discipline with structural support.

### M4 — Profitability & Statutory Capital
GAAP/adjusted profitability, through-cycle margin, **risk-based capital (RBC) adequacy**, liquidity/cash, debt. Sub-criteria: profitability quality + durability; RBC/regulatory-capital headroom; balance-sheet strength. Score: 0 = loss-making or capital-strained, 1 = nascent profitability / adequate capital, 2 = durably profitable + strong RBC headroom.

### M5 — Membership Visibility & Retention *(the "take-or-pay" analogue — emphasis)*
Recurring capitated revenue durability: retention rate, membership-growth *quality* (not just volume), Star-driven enrollment stickiness, contract tenure (Medicaid). Sub-criteria: retention level; growth quality vs margin; revenue recurrence. Score: 0 = churning / volume-over-quality growth, 1 = solid retention, 2 = high retention + quality growth + durable recurring base.

### M6 — Reimbursement & Regulatory Regime *(DOMINANT macro link — emphasis, score conservatively)*
Reads the CMS / state reimbursement environment directly. Sub-criteria: **risk-adjustment regime** (RAF, V28 phase-in — direction and magnitude of impact); **Star ratings & quality-bonus exposure** (above/below the 4-star bonus threshold; trajectory); **rate-notice / IRA Part D mechanics** (MA) or **rate adequacy + redetermination** (Medicaid); CMS audit/sanction risk. Score: 0 = adverse regime + below-bonus Star + sanction risk, 1 = mixed / exposed, 2 = favorable rate environment + ≥4-star + clean regulatory standing.

### M7 — Catalyst *(universal)*
Near-term re-rating event: Star-ratings release, CMS rate notice, earnings profitability-durability print, in-house-tech external contract (Counterpart archetype), M&A. Score: 0 = none visible, 1 = within 12 months, 2 = within 90 days.

### M8 — Technicals & Risk *(universal)*
Chart clean, exhaustion / turbulence signals quiet. Same as base framework. **Note on idiosyncratic payer risk:** Star-rating cuts and adverse rate notices produce gaps no chart predicts — that *regulatory-binary* risk lives in M6, not M8. Position-sizing discipline (Spec 9c Percoco layer + the `turnaround/subscale` cap) addresses it; the score does not.

---

## 4. Worked Example — CLOV (Clover Health) · `ma-focused` + `turnaround/subscale`

**Illustrative calibration anchor, uncalibrated. NOT a recommendation, NOT a lock.** Anchored to Q1 2026 primary financials; the real process runs Gemini Stage-1 (score-blind) → Claude Stage-2 audit → human lock — and cannot run until the `managed_care` slug is registered.

| Pillar | Sub-criteria notes | Score |
|---|---|---|
| M1 Cost Moat | Clover Assistant MCR curve (~8% yr-1 → ~20% yr-4) is a real, differentiated clinical-cost engine — but unproven at scale, subscale today | **1** |
| M2 Narrative | "Managed-care rising tide + digital-health re-rating" partly priced after ~+66% YTD; not stale, no longer unpriced | **1** |
| M3 MCR/BER *(survival)* | BER 86.5% vs 86.1% PY — broadly stable; one profitable quarter, newer cohorts unproven, mgmt flagged "appropriate discipline" → conservative | **1** |
| M4 Profitability & Capital | First-ever GAAP NI ($27.3M); $418M cash, no debt, $108M OCF — strong liquidity, but thin guided FY GAAP NI ($0–20M) | **1** |
| M5 Membership & Retention | Retention >95%, avg MA membership +51.6% with a stated quality-over-volume shift — genuinely strong | **2** |
| M6 Reimbursement Regime *(dominant)* | MA risk-adjustment (V28) overhang + Star exposure + thin margin sensitive to RAF — the dominant risk vector → conservative | **1** |
| M7 Catalyst | Q2 2026 print (durability test) within 90 days; Star cycle; potential Counterpart contracts | **2** |
| M8 Technicals & Risk | Strong uptrend but pressing 52-wk high with high realized vol (36 moves >5%/yr) → exhaustion risk | **1** |
| **Total** | | **10 / 16** |

**Interpretation:** Passes the gate (all 8 pillars ≥1; M1, M3, M5, M6 each clear), but lands **low** — a *scoreable, real inflection with capped conviction*. The `turnaround/subscale` flag enforces a position cap regardless of band. This is the correct framework result for CLOV: the funnel found a genuine fundamental turn, the adapter says "investable but conviction-limited, size small, resolve M6 before committing." Consistent with RR.L (11/16) / XOM (11/16) landing zones for real-but-constrained names.

**Multi-segment note (D-F):** Counterpart Health (external SaaS) is scored as a *separate segment* under the multi-segment doctrine (Note #4), not blended into M1–M8 above. Today it is pre-material; if it ever dominates revenue, the name re-routes toward a future SaaS adapter.

---

## 5. VETO / kill criteria

### Universal VETOs apply.
### Managed-care-specific
- **Statutory capital / RBC breach** (below required authorized control level) → veto.
- **CMS sanction / enrollment suspension / marketing freeze** → veto.
- **Star-rating collapse below the quality-bonus threshold** sustained with no credible recovery path → veto check.
- **MCR / BER runaway** — medical-cost trend out of control beyond pricing recovery, sustained → veto check.
- **Medicaid contract loss / non-renewal** in a concentration state (MCOs) → veto check.
- **Going-concern / regulatory-seizure risk** → veto.

---

## 6. Open decisions (resolved per D-A→D-H, 2026-06-09)

1. **Scope (D-A):** all three managed-care types, sub-typed. ✓
2. **Pillar approach (D-B):** remapped M1–M8 /16. ✓
3. **Sub-types (D-C):** `ma-focused`, `diversified-managed-care`, `medicaid-mco`, `commercial-group`. ✓
4. **Gate / dominant pillars (D-D):** pass = 6+ ≥1, M3+M5+M6 emphasis, M1 & M3 required ≥1. ✓
5. **Position caps (D-E):** `turnaround/subscale` flag → mandatory caution + position cap. ✓
6. **Healthtech-hybrid (D-F):** multi-segment doctrine (Note #4), no separate sub-type. ✓
7. **Mega-diversified payers (D-G):** folded in as `diversified-managed-care`, multi-segment for PBM/services. ✓
8. **Slug (D-H):** `managed_care` + sub-type slugs — register in parser before first lock (Claude Code). ✓

*Remaining for first lock:* calibrate M1–M8 against the first real lock; confirm the BER-vs-MCR labelling convention matches each name's reported metric (some report MCR, some BER — anchor to the company's own definition per data-discipline doctrine).

---

## 7. Schema sketch (for future spec)

```json
{
  "id": "managed_care",
  "name": "Managed Care / Health Insurance",
  "applies_to": "stock",
  "version": "1.0",
  "scope": "Health payers — Medicare Advantage, Medicaid MCO, commercial/group, and diversified managed care. Excludes P&C/life insurers, pharma, providers, and pure healthtech SaaS.",
  "pillars": ["M1_cost_moat","M2_narrative","M3_mcr_discipline","M4_profitability_capital","M5_membership_retention","M6_reimbursement_regime","M7_catalyst","M8_technicals_risk"],
  "max_score": 16,
  "pass_gate": "6+ pillars >=1; M1>=1 AND M3>=1; M3/M5/M6 emphasis",
  "sub_types": ["ma-focused","diversified-managed-care","medicaid-mco","commercial-group"],
  "flags": ["turnaround_subscale"],
  "multi_segment": true
}
```

> **Live-schema note (Claude Code, 2026-06-09):** `sector_scorecards` is a free-form markdown store — there are no structured `pillars` / `pass_gate` / `sub_types` / `flags` / `multi_segment` columns. The JSON above is preserved as the doctrine sketch; in the live DB it is encoded in this prose body. The slug is registered under hyphenated code `managed-care` (live convention; the parser-layer alias remains the underscored `managed_care`). `turnaround_subscale` and `multi_segment` are doctrine carried in §2/§4 prose, not enum columns.

---

## 8. Version history

| Version | Date | Notes |
|---|---|---|
| v1 draft | 2026-06-09 | Initial draft. Closes the managed-care NULL-adapter gap (CLOV trigger). D-A→D-H resolved at authoring. Uncalibrated; CLOV illustrative anchor (10/16). Registered in `sector_scorecards` (code `managed-care`, status needs-review) by Claude Code. |
| **v1.1 Lock Supplement** | **2026-06-09** | Calibrated against CLOV (Q1 FY26-anchored) at **9/16**. Pillars/pass-gate held; M7 catalyst convention adjudicated (routine print = 1); BER-vs-MCR labelling resolved. Multi-segment confirmed. Status `needs-review → locked`. Supplement appended below. |

---

*Base draft v1 above; v1.1 Lock Supplement appended below. Managed-care-remapped /16. Authored 2026-06-09, Claude.ai. Personal use only. Not investment advice.*

---
---

# Sector Adapter — Managed-Care / Health-Insurance — v1.1 Lock Supplement

> **Status:** Calibration supplement closing `Managed_Care_Sector_Adapter_v1.md` against its first anchor, **CLOV (Clover Health)**. Pillars and pass-gate confirmed; one pillar convention adjudicated (M7). Worked example moved from provisional to calibrated. Flips adapter status `needs-review → locked`.
> **Doctrine sources:** `Managed_Care_Sector_Adapter_v1.md` (base), `Aerospace_Sector_Adapter_v1_1_Lock_Supplement.md` (calibration-supplement pattern + adjudication-of-contested-pillar precedent), `Financials_Sector_Adapter_v1.md` (remapped-pillar precedent), `Clinical_Stage_Biotech_Sector_Adapter_v1.md` (position-cap precedent), `Cross_Sector_Investment_Philosophy_v1_1.md`.
> **Calibration anchor:** CLOV, `ma-focused` sub-type + `turnaround/subscale` flag, anchored to Q1 FY2026 results (quarter ended 31 Mar 2026, reported May 2026).
> **Date:** 2026-06-09 · Claude.ai. **Not investment advice. Personal use only.**

---

## §A — Calibration summary

CLOV scores **9 / 16** under the Managed-Care adapter (`ma-focused`, `turnaround/subscale`). The pillars and pass-gate held at first real use — **one convention adjudicated** (M7 catalyst: a routine scheduled earnings print is a 1, not a 2 — see §C). The score lands at the **bottom of the cross-adapter distribution**, which is the correct neighbourhood for a newly-profitable, regulation-exposed, subscale turnaround: it passes the gate (investable, scoreable) but conviction is capped, and the `turnaround/subscale` flag enforces a position cap regardless of band. Status flips `needs-review → locked`.

## §B — Worked example, calibrated (CLOV · `ma-focused` + `turnaround/subscale`)

Anchored to Q1 FY2026 primary financials. **Illustrative calibration, not a recommendation, not a locked thesis** (a formal CLOV lock would run separately through `ft-thesis`: Gemini Stage-1 → audit → human lock, and only after the slug is registered).

| Pillar | Read (Q1 FY2026) | Score |
|---|---|---|
| M1 Cost Moat *(req ≥1)* | Clover Assistant clinical-cost engine (assoc. MCR benefit ~8% yr-1 → ~20% yr-4) is real and differentiated — but unproven at scale and subscale today. Emerging, not yet durable. | **1** |
| M2 Narrative | "Managed-care rising tide + digital-health re-rating" partly priced after ~+66% YTD; not stale, no longer unpriced. Anti-inversion (note #11) caps it. | **1** |
| M3 MCR/BER *(SURVIVAL, req ≥1, conservative)* | Insurance BER 86.5% vs 86.1% PY — broadly stable; one profitable quarter, newer cohorts unproven, mgmt flagged "appropriate discipline." Stable but not yet proven-durable → conservative. | **1** |
| M4 Profitability & Capital | First-ever GAAP NI ($27.3M, $0.05 dil.); cash & investments $418.2M, no debt, OCF $107.9M — strong liquidity, but thin guided FY GAAP NI ($0–20M). | **1** |
| M5 Membership & Retention *(emphasis)* | Retention >95%; avg MA membership +51.6% with a stated quality-over-volume shift. Genuinely strong recurring base. | **2** |
| M6 Reimbursement Regime *(DOMINANT, conservative)* | MA risk-adjustment (V28) overhang + Star/quality-bonus exposure + thin margin sensitive to RAF. The dominant risk vector → conservative. | **1** |
| M7 Catalyst | Q2 FY26 print (a genuine profitability-durability test) within 90 days; Star cycle is >90 days out; Counterpart contracts unscheduled. **Adjudicated to 1** (routine scheduled print = 1; see §C). | **1** |
| M8 Technicals & Risk | Strong uptrend but pressing 52-wk high (~$4.18) with high realized vol (36 moves >5%/yr) → exhaustion risk; plus an insider-sell cluster into strength (sub-threshold, flagged). | **1** |
| **Total** | | **9 / 16** |

**Pass-gate result:** **PASSED.** All 8 pillars ≥1; M1 ≥1 and M3 ≥1 satisfied; M3/M5/M6 emphasis observed. The gate is met as a *result* of the scores, not engineered toward.

**Interpretation:** a *scoreable, real inflection with capped conviction*. The framework correctly says "investable but conviction-limited; size small via the `turnaround/subscale` cap; resolve M6 (V28/Star) before committing." Drops one notch below the v1-draft illustrative (10/16) purely on the M7 adjudication — a deliberate anti-leniency tightening, not a change to the CLOV facts.

**Multi-segment note (D-F):** Counterpart Health (external SaaS) is scored as a *separate segment* under the multi-segment doctrine (Note #4), not blended into M1–M8 above. Pre-material today; if it ever dominates revenue, the name re-routes toward a future SaaS adapter.

## §C — Convention adjudicated + open item resolved

1. **M7 catalyst convention (adjudicated).** A **routine scheduled earnings print scores 1** (catalyst within 12 months), not 2. **M7 = 2 requires a discrete, potentially-unpriced near-term event** — a Star-ratings release with real bonus-threshold stakes, a transformational deal close, a major external (Counterpart) contract, or a binding rate-notice within ~90 days. This mirrors the anti-narrative-inversion spirit (don't reward the already-on-the-calendar) and keeps M7 consistent with how the other adapters treat routine cadence. *(Applies adapter-wide, not just CLOV.)*
2. **BER-vs-MCR labelling (v1 §6 open item — resolved).** M3 anchors to **the company's own reported metric** — CLOV reports *Insurance Benefits Expense Ratio (BER)*; others report MCR / MLR. Score M3 on the name's stated ratio and its trend; do not normalise across differing definitions (data-discipline doctrine: anchor to the primary metric as reported).

## §D — Cross-adapter calibration distribution (updated)

| Score | Holdings |
|---|---|
| 14/16 | RHM.DE (Defense), ASML (AI-Infra) |
| 13/16 | LLY, ABBV, NVO (Pharma), ORCL (Cloud-Infra) |
| 12/16 | AEM, NVDA, SU.PA, MRVL, RR.L (civil + defense legs) |
| 11/16 | RR.L power leg, XOM, CLS |
| 10/16 | ALB |
| **9/16** | **CLOV (Managed-Care — illustrative calibration anchor, `turnaround/subscale`; not a held position)** |

CLOV at 9/16 as the new distribution floor is coherent: a subscale, single-profitable-quarter, regulation-exposed turnaround *should* sit below a quality compounder band. The rubric is discriminating correctly.

## §E — Build dependency (for Claude Code)

- Flip `sector_scorecards.status` **`needs-review` → `locked`** for adapter slug `managed_care`; bump `current_version` → **1.1**.
- **Slug reconciliation (flag):** the DB row is `managed-care` (hyphen); the parser alias was named `managed_care` (underscore). **Confirm these are deliberately mapped to each other** before any managed-care thesis routes, or routing will silently fail at lock time.
- Register the four sub-type slugs (`ma-focused`, `diversified-managed-care`, `medicaid-mco`, `commercial-group`), the `turnaround_subscale` flag, and `multi_segment: true`.
- **Store this supplement's markdown as TEXT, not BLOB** (per the readfile()→BLOB issue caught on the aerospace W1 row — otherwise SQL `LIKE`/search silently skips it).
- Registration ≠ lock: this supplement calibrates the rubric. A formal CLOV (or any managed-care) thesis lock remains a separate human step via `ft-thesis`.

## §F — Version history

| Version | Date | Notes |
|---|---|---|
| v1 draft | 2026-06-09 | Initial draft; closes managed-care NULL gap (CLOV trigger); D-A→D-H resolved; uncalibrated; CLOV illustrative 10/16. |
| **v1.1 Lock Supplement** | **2026-06-09** | Calibrated against CLOV (Q1 FY26-anchored) at **9/16**. Pillars/pass-gate held; M7 catalyst convention adjudicated (routine print = 1); BER-vs-MCR labelling resolved. Multi-segment handling confirmed. Status `needs-review → locked`. |

---

*Managed-Care adapter v1.1 (calibrated against CLOV). Authored 2026-06-09, Claude.ai. Slug registration + hyphen/underscore reconciliation applied by Claude Code. Personal use only. Not investment advice.*
