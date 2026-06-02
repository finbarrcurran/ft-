# Sector Adapter — Aerospace — v1 (Draft)

> **Status:** Twelfth adapter under the Cross-Sector Investment Philosophy v1.1. Covers civil aerospace and space companies — commercial aircraft OEMs, aero-engine makers, aerostructures, and space/launch — where the thesis is the multi-decade order backlog and aftermarket annuity, distinct from Defense (gov-revenue-driven) and Industrial-Electrical. **Has a live trigger: RR.L currently sits NULL pending this adapter** (its defense leg is scored under Defense; its civil aero-engine + power leg needs a home).
> **Doctrine sources:** `Defense_Sector_Adapter_v1_1_Lock_Supplement.md` (dual-use / multi-segment scoring pattern RR.L needs), `Industrial_Electrical_Sector_Adapter_v1.md` (long-cycle capital-goods DNA), `Cross_Sector_Investment_Philosophy_v1_1.md`, `Pharma_Sector_Adapter_v1.md` (8-Q template).
> **Calibration note:** Complete template per user request. **Trigger case = RR.L civil leg.** First scoring of RR.L's aero-engine + power segment under this adapter is the calibration anchor; thresholds may adjust at that review. Resolves the long-standing RR.L NULL tag (v1.4 trigger).
> **Not investment advice. Personal use only.**

---

## 1. Scope

### In-scope
- **Commercial aircraft OEMs**: Boeing (civil), Airbus, Embraer, COMAC-adjacent suppliers
- **Aero-engine makers**: Rolls-Royce (RR.L civil), GE Aerospace, Safran, Pratt & Whitney (RTX)
- **Aerostructures & components**: Howmet, TransDigm, Spirit AeroSystems, Heico
- **Space / launch / satellite**: Rocket Lab, space-infrastructure names where civil/commercial revenue dominates

### Out-of-scope (different adapter)
- **Pure-defense primes** (≥50% gov revenue) → Defense adapter (the RHM.DE / RR.L-defense-leg case)
- **Airlines** (operators, not makers) → would be a Transportation adapter (not built; backlog)
- **Diversified industrials** where aero is a minor segment → Industrial-Electrical
- **Engine/power for non-aero** (grid, marine) → Industrial-Electrical or Energy-Power

### Dual-segment rule (the RR.L case — inherited from Defense supplement)
Rolls-Royce is the archetype: a **civil aero-engine + power** business AND a **defense** business. Per the locked Defense multi-segment convention: **score the dominant segment by gross-profit contribution as primary; secondary segments scored prose-only.** RR.L's civil aerospace (Trent engine franchise + aftermarket) is scored here as its primary leg; its defense leg remains prose-referenced under Defense. This finally gives RR.L a complete home and resolves the NULL.

### What this pays for
Aerospace pays for **a multi-decade installed-base annuity**. The aircraft/engine sale is often near-cost; the money is in the **aftermarket** — spare parts, maintenance, "power-by-the-hour" service contracts on engines that fly for 25-30 years. The thesis is a bet on installed-base growth + aftermarket capture + the duopoly/oligopoly structure that protects pricing.

---

## 2. Sub-types within this adapter

| Sub-type | Code | Notes |
|---|---|---|
| Aircraft OEM | `aircraft-oem` | Boeing/Airbus duopoly; order backlog + production rate dominate Q1 |
| Aero-engine | `aero-engine` | RR.L civil, GE Aero, Safran; aftermarket annuity is the whole thesis — Q5 visibility dominant |
| Aerostructures / components | `aerostructures` | Howmet, TransDigm, Heico; content-per-aircraft + sole-source positions |
| Space / launch | `space-launch` | Rocket Lab et al.; earlier-stage, Q7 catalyst + Q4 intensity weighted, higher risk |

Sub-type affects Q1 and Q5 weighting — backlog/production-rate for OEM, aftermarket-annuity visibility for engines.

---

## 3. The Eight-Question Adapter (/16)
Score each pillar 0/1/2, total /16. Same structure and pass gate as the locked operating-stock adapters: **pass = 6+ pillars ≥ 1, with Q1 and Q3 emphasis; no pillar = 0 in a passing thesis.**

### Q1 — Bottleneck *(sector-specialized)*
*Is the company in a binding aerospace bottleneck?* Sub-criteria: order backlog depth (years of production), production-rate ramp constraint (the post-COVID supply-chain bottleneck), engine/aftermarket installed-base scarcity, certification/regulatory moat (FAA/EASA type certificates as a barrier). Engine sub-type weights aftermarket-installed-base highest; OEM weights backlog + production rate.

### Q2 — Narrative *(universal)*
Is the aerospace recovery/cycle narrative current, mature, or stale? (post-COVID travel recovery, narrowbody supercycle, space commercialization). Anti-narrative-inversion: a fully-priced "recovery" narrative scores lower than an emerging structural shift.

### Q3 — Moat *(sector-specialized)*
*This is where aerospace's exceptional moat scores.* Sub-criteria: duopoly/oligopoly position (Boeing-Airbus; the big-3 engine makers), certification barrier (years + cost to certify a new engine/airframe — enormous), switching costs (an airline committed to an engine type for decades), sole-source content positions (TransDigm/Heico model). Aerospace has among the deepest moats of any sector — score it honestly.

### Q4 — Intensity *(universal)*
Capital intensity, R&D burden, balance-sheet health through the long development cycles (RR.L's near-death and recovery is the cautionary tale here — engine development can sink a company before the aftermarket pays off).

### Q5 — Visibility *(universal — DOMINANT for engine sub-type)*
*The aftermarket annuity.* Sub-criteria: aftermarket revenue as % of total + its durability, long-term service-agreement coverage, order-book-to-revenue conversion visibility. For aero-engine names this is the heart of the thesis — predictable, high-margin, multi-decade service revenue on the installed fleet.

### Q6 — Sovereignty *(universal)*
Jurisdiction, supply-chain sovereignty (titanium/critical-material exposure), trade/tariff risk, the civil-vs-defense regulatory split for dual names.

### Q7 — Catalyst *(universal)*
Near-term inflections: production-rate increases, new engine/airframe certification, major order wins, margin-recovery milestones (RR.L's turnaround was a catalyst story), space-program milestones.

### Q8 — Technicals & Risk *(universal)*
Reads 9e/macro regime + sector technicals. Aerospace-specific risks: safety incidents (a crash or grounding — the 737 MAX archetype — is an instant re-score), single-program dependency, cycle-downturn exposure.

---

## 4. Worked example — RR.L (civil leg) [calibration anchor — to confirm at first scoring]
RR.L civil = `aero-engine` sub-type. Trent engine franchise + power systems; the thesis is aftermarket annuity recovery post-turnaround. Defense leg scored prose-only under Defense (12/16 there). Provisional read pending real scoring: strong Q3 (engine oligopoly + certification moat) + recovering Q5 (aftermarket annuity) + live Q7 (margin turnaround), tempered by Q4 (balance-sheet scars). **Score to be confirmed when RR.L civil is formally run — this resolves the NULL tag.**

---

## 5. VETO / kill criteria
### Universal VETOs apply (founder/governance, fraud, etc.).
### Aerospace-specific
- **Major safety grounding** (fleet-wide grounding of a key program — 737 MAX archetype) → re-score + likely veto until resolved
- **Certification failure** on a bet-the-company program → veto check
- **Single-program dependency collapse** (the one airframe/engine the thesis rests on is cancelled) → veto
- **Balance-sheet distress** through a development cycle (the pre-turnaround RR.L state) → veto check

## 6. Open decisions
1. Sub-type granularity (4 proposed) — confirm at first use.
2. RR.L civil-leg score — the calibration gut-check, same as LLY/ABBV were for Pharma.
3. Airlines explicitly excluded → confirm a Transportation adapter stays on backlog rather than folding operators in here.

*Aerospace adapter v1 (template, uncalibrated; resolves RR.L NULL trigger). Authored 2026-06-01, Claude.ai. Personal use only. Not investment advice.*
