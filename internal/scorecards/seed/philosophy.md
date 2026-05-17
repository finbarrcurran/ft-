# Cross-Sector Investment Philosophy — v1.1

> **Status:** Locked. Sits above the Jordi Stock Picking Framework and the Cowen Crypto Framework. Generalizes Jordi's AI-specific work into a portable, multi-sector philosophy.
> **Purpose:** Doctrinal anchor for all sector adapters and the FT rotation tracker.
> **Changes from v1:** Adds §6.1 Adapter Assignment Principle. Locks all §8 open decisions.
> **Personal use only. Not investment advice.**

---

## 1. The Universal Law

Jordi's AI-infrastructure work is the application of a more general law:

> **Demand for an end-product can grow exponentially. The physical inputs that produce it grow linearly. Wherever that gap is binding, capital is forced to rotate in, and the equity at the binding point re-rates.**

Jordi's *Scarcity Migration Map* (GPUs → Optical → HBM → CPUs → Power → Cooling → Specialty Chemicals → Battery Storage → Edge Inference → Embodiment) is the **AI-cycle instance** of this law. The law itself is sector-agnostic. It also produces:

- **Healthcare:** demand for metabolic, obesity, and longevity treatment > supply of efficacious branded therapies → GLP-1 platform re-rating (LLY).
- **Monetary regime:** demand for non-sovereign collateral > new supply of trusted reserve assets → gold/silver re-rating.
- **Defense:** demand for ammunition, air defense, drones > NATO production capacity → European primes re-rating (RHM.DE).
- **Materials:** demand for critical inputs (lithium, copper, uranium) > permitted/operating supply → upstream re-rating.

This is the philosophy. Everything else is implementation.

---

## 2. The Two-Layer Decision Model

Every entry is a product of two independent judgments. **Both must agree.**

### Layer 1 — Sector Rotation (top-down: *where*)
> *Where is capital being forced next? What sector is the binding constraint about to migrate into?*

Inputs:
- **Relative-strength leaderboard** (1W/1M/3M/6M/YTD vs SPY) — *measured*
- **Jordi macro thesis** — *narrative-driven* (currently: energy is next)
- **Macro regime overlay** — *timing* (stable / shifting / defensive)
- **Catalyst stack** — *what would force the rotation* (Jensen's 1000x energy comment, NATO 3%+ defense GDP commitments, hyperscaler PPA announcements, central bank gold buying data)

Output: a small list of sectors flagged **"rotating in,"** **"in distribution,"** or **"rotating out."**

### Layer 2 — Within-Sector Scarcity (bottom-up: *which*)
> *Inside the rotating sector, which company sits at the binding constraint and is mispriced for what it actually is?*

Inputs: the 8-Question Screen, with Q1 and Q3 specialized per sector (see §6).

Output: scored watchlist candidates.

### Combined rule
A name enters the watchlist **only if**:
1. Its sector is **"rotating in"** OR is held as an explicit, named non-rotational thesis (e.g., monetary-regime hedge), **AND**
2. It scores 6+/8 on the sector-adapter screen with strong Q1 and Q3.

This rule is what prevents concentration risk — a portfolio of 24 names all sitting in one AI-infrastructure rotation.

---

## 3. Why This Matters

| Problem | What this philosophy fixes |
|---|---|
| 80%+ of watchlist work has been AI-infra-themed | Sector rotation forces diversification by *capital flow*, not by guessing |
| Non-AI holdings get strained Jordi theses | They get their own honest rotation thesis or are tagged as ballast |
| No explicit exit signal | "Sector has rotated out" is now a defined trigger |
| Macro regime overlay had no anchor | Now drives the rotation read directly |
| Concentration is hard to see | Rotation tracker exposes it on a single chart |

---

## 4. Sector Taxonomy — Locked List

23 sub-sectors + 11 GICS top-level = **34 rows** seeded into FT's `sector_universe` table (per Spec 9f §3 D1).

### 4.1 Sub-sectors mapped to Jordi's AI Migration (17)

| Sub-sector | Jordi stage | ETF proxy | Sample holdings/candidates |
|---|---|---|---|
| GPUs & AI accelerators | 1 | SOXX | NVDA, AMD |
| Chip design / IP | 1, 9 | SOXX | ARM, LSCC, RGTI |
| Optical / networking | 2 | — | COHR, ANET |
| HBM / advanced packaging | 3 | — | Micron, SK Hynix, Amkor |
| Foundry | 1-4 | SOXX | TSM |
| Semicap (litho, etch, dep) | 1-4 | SOXX | ASML |
| Specialty semi chemicals | 7 | — | 4063.T (Shin-Etsu), Entegris |
| Power generation — gas turbines | 5 | XLI / FXR | GEV, Siemens Energy |
| Power generation — nuclear / SMR | 5 | URA + URNM | CCJ, CEG, OKLO, BWXT |
| Power generation — distributed / fuel cells | 5 | — | BE, Bloom |
| Power generation — diversified industrial | 5 | XLI | RR.L (incl. defense) |
| Grid & transmission | 5 | GRID / PAVE | ETN, PWR, **SU.PA (interim tag)** |
| Cooling & thermal management | 6 | — | MOD, Vertiv |
| Battery storage & metals | 8 | LIT + BATT | ALB |
| Data center REITs | 5, 6 | — | EQIX, DLR |
| Edge / industrial silicon | 9 | SOXX | LSCC |
| Embodiment — robotics, EV, drones | 10 | ROBO, IDRV | (none held) |

### 4.2 Sub-sectors with independent rotation theses (6 — non-AI)

| Sub-sector | Rotation thesis | ETF proxy | Sample holdings |
|---|---|---|---|
| Pharma — metabolic / obesity | Demographics + obesity epidemic; payer capitulation forces volume | XLV / IHE | LLY |
| Pharma — diversified immunology | Post-Humira pipeline transition; chronic-disease demand | XLV / IHE | ABBV |
| Precious metals — gold | Fiscal/monetary regime + sovereign de-dollarisation | GLD, GDX | GLD, AEM, AU |
| Precious metals — silver | Monetary + industrial hybrid; physical/paper dislocation | SLV, SIL | SLV, PAAS, WPM |
| Defense — sovereign re-arming | NATO 3%+ commitments, European production gap | ITA / EUAD | RHM.DE, RR.L (defense leg) |
| Oil & gas — integrated | Energy security + LNG export cycle | XLE / XOP | XOM |

### 4.3 GICS top-level sectors tracked (11)

All 11 GICS sectors tracked via standard SPDR ETFs (XLE, XLV, XLK, XLF, XLI, XLU, XLP, XLY, XLB, XLRE, XLC). Most have no active rotation thesis today; the tracker measures them so that future rotations (e.g., capital flowing into financials) are visible.

---

## 5. The Rotation Tracker — Design Principles

Detailed mechanics → `FT_Spec_9f_Sector_Rotation_Tracker_v1.md`. Locked principles:

1. **Baseline + relative measurement.** Always show vs SPY (US) or VWRL (global) to reveal *true* rotation.
2. **Multi-window view.** 1W / 1M / 3M / 6M / YTD. Different timeframes catch different rotations.
3. **Universe is the locked taxonomy above** — 23 sub-sectors + 11 GICS = 34 rows.
4. **"Rotating in" threshold:** RS vs SPY (3M) ≥ 1.05. Auto-flagged in UI.
5. **Free tier only.** Reuses existing Yahoo provider in FT.
6. **Weekly digest cadence.** Daily data ingestion, weekly review rhythm.
7. **Pair quant signal with qualitative read.** Macro strip surfaces both relative-strength data and Jordi's current narrative thesis.

---

## 6. The Sector Adapter Principle

The 8-Question Screen is preserved. Two pillars become **sector-specialized** via JSON adapters. The other six stay universal.

### Universal pillars (apply across all sectors)
- **Q2 Narrative** — mispriced bucket?
- **Q4 Intensity** — demand/intensity per unit rising?
- **Q5 Visibility** — contracted / take-or-pay / regulated revenue?
- **Q6 Sovereignty** — strategic, policy-supported, friend-shored?
- **Q7 Catalyst** — near-term forcing event?
- **Q8 Technicals & Risk** — chart clean, regime quiet?

### Sector-specialized pillars
- **Q1 Bottleneck** — what is the binding *physical/structural* constraint *in this sector*?
- **Q3 Moat** — which moat type is operative *in this sector*?

### 6.1 Adapter Assignment Principle *(new in v1.1)*

> A name is scored under an adapter only if the rotation thesis driving that adapter materially drives the name's earnings — not merely because the name has incidental customer or supplier exposure to that sector.

**Worked examples:**
- **RR.L** → Energy-Power adapter (power-leg revenue genuinely scales with AI/industrial demand) AND Defense adapter (defense-leg revenue scales with NATO re-arming). Net thesis = weighted by revenue mix. ✅
- **SU.PA** → Industrial / Electrical Equipment adapter (when written). NOT Hydrocarbons, despite some O&G customers — Schneider's earnings are not driven by hydrocarbons. ✅
- **NVDA** → Semiconductor / AI-Infra adapter only. NOT Healthcare, despite hospitals buying GPUs — that's customer exposure, not earnings driver. ✅

**Why this matters:** without this principle, the framework drifts. Every industrial conglomerate would end up scored under every adapter that touches its customer base. The signal becomes noise.

### Adapter sketches (illustrative — full versions in dedicated MDs)

**Pharma adapter (TBD):**
- Q1: patent runway, pipeline NPV, regulatory exclusivity, biosimilar threat, in-house replacement.
- Q3 moat type: intangibles (patents) + regulatory.

**Energy-Power adapter (v1 drafted):**
- Q1: dispatchability, LCOE, interconnect/PPA, permitting, deployment speed, fuel security.
- Q3 moat type: cost advantage + efficient scale.

**Hydrocarbons adapter (v1 drafted):**
- Q1: reserve life, cost curve, jurisdiction, infrastructure access, capital discipline, demand-pull.
- Q3 moat type: cost + scale + switching + regulatory + intangibles.

**Mining/Metals adapter (TBD):**
- Q1: reserve life, grade, jurisdiction, AISC, by-product credits.
- Q3 moat type: cost + geological irreplaceability.

**Defense adapter (TBD):**
- Q1: programme of record positioning, production capacity, munitions backlog, customer concentration, sovereign barriers.
- Q3 moat type: switching + regulatory + scale.

**Semi / AI-Infra adapter (TBD — formalisation of existing Jordi framework):**
- Q1: position on AI migration map, intensity per GPU/rack/GW.
- Q3 moat type: scale + switching + IP.

### Scoring math
Each Q1 sub-criterion scored 0/1/2. Collapsed to single 0/1/2 for table displays; full vector stored in DB for per-holding detail-page breakdown (per Spec 10 integration).

---

## 7. How This Treats the Current 24 Holdings

| Bucket | Holdings | Adapter |
|---|---|---|
| AI-rotation core | NVDA, TSM, ASML, ARM, LSCC, COHR, MOD, ORCL, 4063.T, RGTI | Semi/AI-Infra (TBD) |
| Energy-rotation | RR.L (power leg), BE, XOM | Energy-Power / Hydrocarbons (v1 drafted) |
| Defense | RR.L (defense leg), RHM.DE | Defense (TBD) |
| Healthcare demand | LLY, ABBV | Pharma (TBD) |
| Monetary regime | GLD, AEM, AU | Mining/Metals (TBD) |
| Materials hybrid | SLV, WPM, PAAS, ALB | Mining/Metals (TBD) |
| Industrial / electrical | SU.PA | Industrial Electrical Equipment (TBD) |

Detailed per-holding sector mapping for the rotation tracker → `Sector_Holdings_Mapping_v1.md`.

---

## 8. Locked Decisions *(was open in v1)*

All previously-open decisions from v1 §8 are now locked. Full decisions log in `HANDOFF_PACKAGE_README.md`. Summary:

- **Final sub-sector list:** confirmed (§4 above)
- **Cadence:** weekly review rhythm; daily data ingestion
- **Visualization:** table + inline sparkline (v1); heatmap deferred
- **First adapter authored:** Energy-Power → Hydrocarbons → Pharma (next) → Defense → Mining → Industrial → Semi/AI-Infra
- **"Rotating in" threshold:** RS vs SPY (3M) ≥ 1.05
- **Non-rotational holdings:** assigned to an adapter when one exists (Mining/Metals will cover GLD); held as ballast only if no adapter applies

---

## 9. Relationship to Existing Spec Files

| File | Status under this philosophy |
|---|---|
| `Jordi_Visser_Stock_Picking_Strategy_Framework.md` | Preserved. Will become the "Semi/AI-Infra adapter" when formalised. |
| `Jordi_Visser_Cheat_Sheet_v2.md` | Preserved. Specialisation of this philosophy for AI-infra context. |
| `Cowen_Crypto_Strategy_Framework.md` | Preserved. Separate framework, separate asset class. |
| `Percoco_Trading_Strategy_Framework.md` | Preserved. Execution layer, downstream of selection. |
| `FT_Build_Specifications_v2.md` | Already supports JSON-defined frameworks. No architectural change. |
| `FT_Spec_9b_Regime_Overlay.md` | Feeds Layer 1 (sector rotation) timing input via macro strip. |
| **`FT_Spec_9f_Sector_Rotation_Tracker_v1.md`** | NEW — Layer 1 implementation. |
| **`FT_Spec_9g_Scorecard_Repository_v1.md`** | NEW — Layer 2 reference library. |
| `FT_Spec_9h` (queued) | Future — real-time technicals monitoring. |

---

## 10. Version History

| Version | Date | Changes |
|---|---|---|
| v1 | 2026-05-17 | Initial draft. Open items in §8. |
| **v1.1** | **2026-05-17** | **Adds §6.1 Adapter Assignment Principle. Locks all §8 open decisions. Confirms sector taxonomy as 23 sub-sectors + 11 GICS.** |

---

*Locked. Personal use only. Not investment advice.*
