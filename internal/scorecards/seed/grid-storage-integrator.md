# Sector Adapter — Grid-Scale Storage Integrator — v1.1 (Locked)

> **Status:** Locked v1.1 (2026-06-13). Covers companies whose primary earnings driver is the integration, deployment, and software management of utility-scale and behind-the-meter battery energy storage systems (BESS). Distinct from Energy-Power (generation assets), Industrial-Electrical (OEM equipment), and Cloud-Infra (hyperscaler capacity). The thesis is a bet on the structural electrification inflection: grid storage as the binding enabler of the AI-load + renewables era.
> **Doctrine sources:** `Cross_Sector_Investment_Philosophy_v1_1.md`, `Industrial_Electrical_Equipment_Sector_Adapter_v1.md` (Q1 sub-criteria template), `Energy_Sector_Adapter_v1.md` (sovereign policy framing), `Software_SaaS_Sector_Adapter_v1.md` (ARR / services-attach rate framing).
> **Calibration status:** **Calibrated v1.1 — two anchors locked 2026-06-13: FLNC 10/16 (`multi-storage`, Passes Screen) and STEM 8/16 (`sw-storage`, Marginal). `multi-storage` + `sw-storage` ratified; `util-storage` + `dc-storage` calibration-pending.**
> **Slug:** `grid-storage-integrator` (DB code) ↔ `grid_storage_integrator` (parser slug). Registered via SC-36.
> **Not investment advice. Personal use only.**

---

## 1. Scope

### In-scope
- **Pure-play BESS integrators**: companies whose primary revenue is design, supply, integration, and commissioning of utility-scale battery storage systems (Fluence Energy, Powin, Eos Energy)
- **Software-led storage platforms**: companies selling optimisation software + services attached to deployed BESS fleets (Fluence Mosaic, Stem's Athena — where the software is the primary thesis, not just an attach)
- **Storage-dominant EPC names**: EPC contractors where BESS integration is the dominant and growing revenue segment (score by segment, not consolidated, when ≥50% storage)

### Out-of-scope (different adapter)
- **Battery cell / module manufacturers** (CATL, LG Energy Solution, BYD's cell division) → AI-Infra-Semi or Industrial-Electrical by production economics; these are cell manufacturers, not integrators
- **Dispatchable generation + storage hybrids** (utility with storage bolted on) → Energy-Power adapter; score storage segment separately only if material
- **EV battery / automotive** → separate sector; different economics, different demand drivers
- **Small/behind-the-meter residential** (Sunrun, Sunnova) → not this adapter; distributed-energy economics are different
- **Hyperscaler buying storage** → not the vendor, not this adapter

### The integration boundary
The line: **this adapter scores companies whose revenue is earned by assembling, delivering, and optimising energy storage systems — not by manufacturing the cells inside them, not by owning the generation asset, and not by being the end customer.** Fluence buys cells, assembles systems, delivers to utilities, and charges recurring fees to manage the fleet. That is the integrator model. A utility owning a battery farm for its own balance sheet is an Energy-Power name.

When a company straddles (e.g. a company that both manufactures cells and integrates), route by dominant revenue source and earnings driver. Flag for boundary review.

### What this adapter pays for
The thesis is a bet on three converging structural forces: (1) AI-load and electrification creating grid instability that only storage can address, (2) intermittent renewables (solar/wind) requiring firming that only storage can provide at scale, and (3) a regulatory regime (IRA domestic content, FEOC rules in the US; EU battery passport regime) that creates pricing power for domestically-sourced or regulator-compliant integrators. The storage integrator is the pick-and-shovel of the energy transition — it is needed whether solar, wind, nuclear, or gas wins the generation debate.

---

## 2. Sub-types within this adapter

| Sub-type | Code | Notes |
|---|---|---|
| Utility-scale integrator | `util-storage` | Primary customer is utility / IPP / developer; revenue is project-by-project + services tail; highest volume, thinner margin |
| Data-centre / hyperscaler storage | `dc-storage` | Behind-the-meter for AI data centres; power quality + fast response + footprint efficiency; higher margin, qualification moat |
| Software-led storage | `sw-storage` | Revenue primarily ARR from fleet optimisation software; hardware is secondary or third-party; re-rates on SaaS multiple if ARR dominates |
| Diversified / multi-market | `multi-storage` | Serves utility + C&I + data centre; default for large integrators with growing DC exposure |

Sub-type governs Q1 weighting (which bottleneck is most binding) and Q5 framing (backlog + ARR vs. pure ARR).

**FLNC routing:** `multi-storage` — primarily utility-scale with active DC/hyperscaler segment opening. Revisit as `dc-storage` if data-centre revenue exceeds 30% of backlog.

**Ratification status (v1.1):**

| Sub-type | Status |
|---|---|
| `multi-storage` | **Ratified** — FLNC lock (10/16, 2026-06-13) |
| `sw-storage` | **Ratified** — STEM lock (8/16, 2026-06-13) |
| `util-storage` | **Calibration-pending** — documented-but-uninstantiated; first lock ratifies |
| `dc-storage` | **Calibration-pending** — first lock ratifies; candidate name FLNC itself if data-centre revenue exceeds 30% of backlog (§2 re-route trigger) |

---

## 3. The Eight-Question Adapter (/16)

Score each pillar 0/1/2, total /16. **Pass gate: ≥6 pillars ≥1, with Q1 ≥1 and Q3 ≥1 mandatory (a storage integrator without a bottleneck position or a moat is a commodity EPC contractor — it does not pass).**

---

### Q1 — Bottleneck / Supply Chain Position *(sector-specialized)*

**The core question:** Is the company positioned at the binding constraint in the storage value chain — and is that position durable?

In grid-scale storage the binding constraints are: (a) domestic-content-eligible battery cell supply, (b) system design and integration IP that qualifies for hyperscaler technical requirements, (c) software controls and BMS that enable premium pricing vs. commodity integrators. A company that controls none of these is a project coordinator; a company that controls two or more is a genuine bottleneck occupant.

**Sub-criteria (score the vector, collapse to 0/1/2 for the pillar):**

| Sub-criterion | What to verify | Sub-type weight |
|---|---|---|
| **Supply chain control** | Domestic or FEOC-compliant cell supply locked (contracted or owned); single-source risk vs. multi-supplier diversification | `util-storage`: 2 · `dc-storage`: 2 · `sw-storage`: 1 · `multi-storage`: 2 |
| **Backlog / demand coverage** | Contracted backlog as months of forward revenue; YTD order intake trajectory; pipeline GWh growth rate | `util-storage`: 2 · `dc-storage`: 1 · `sw-storage`: 0 · `multi-storage`: 2 |
| **Technical qualification** | Qualified on hyperscaler MSA / utility RFP short lists; demonstrated fast-response and power-quality capability | `util-storage`: 1 · `dc-storage`: 2 · `sw-storage`: 1 · `multi-storage`: 2 |
| **Geographic / market-type diversification** | Not single-geography or single customer class; multiple ISOs or international presence reduces concentration | `util-storage`: 1 · `dc-storage`: 0 · `sw-storage`: 0 · `multi-storage`: 1 |
| **Domestic content / regulatory positioning** | IRA domestic-content bonus eligible (US); EU Battery Passport compliance (EU); FEOC rules navigated | `util-storage`: 2 · `dc-storage`: 2 · `sw-storage`: 0 · `multi-storage`: 2 |
| **Software-and-services attach** | ARR as % of total revenue; O&M contract fleet size; optimisation software installed base | `util-storage`: 1 · `dc-storage`: 2 · `sw-storage`: 2 · `multi-storage`: 1 |

**Scoring guidance:**
- Score **2** if company occupies ≥3 sub-criteria with strong evidence, including the highest-weight items for its sub-type.
- Score **1** if present in 2–3 but with meaningful gaps (e.g. domestic supply locked but no software attach, or software attach but thin backlog).
- Score **0** if the company is a commodity assembler with no locked supply, no software, and no qualification differentiation.

---

### Q2 — Narrative *(universal)*

Is the company being scored in a mispriced bucket? Grid-scale storage is a widely-followed thematic. Key tests: (a) is the name being dragged down by a clean-energy narrative discount (rate-sensitive, "green bubble" framing) while its fundamental drivers are actually AI infrastructure? (b) Is it being priced as a hardware-cycle name when its ARR base is growing toward SaaS-level visibility? (c) Is a revenue miss (timing) being treated as a demand failure?

**Anti-inflation guard:** the storage sector has hype risk. Penalise names whose valuation already prices perfection vs. early/delivery-stage companies with visible unpriced backlog.

---

### Q3 — Moat *(sector-specialized)*

**The core question:** What protects the integrator's margin from commoditisation by cell manufacturers going direct (CATL/BYD vertical integration), hyperscaler self-build, or low-cost EPC competitors?

Dominant moat types in this sector:

| Moat type | Description | When strong |
|---|---|---|
| **Regulatory moat** | Domestic-content eligibility and FEOC compliance as a price-premium gate | US market, while IRA domestic-content rules hold |
| **Technical qualification moat** | Hyperscaler/utility approved vendor list (AVL) placement after rigorous qualification | DC-storage sub-type primarily; hard to replicate quickly |
| **Software / BMS moat** | Fleet optimisation software (Mosaic-class) with high switching costs once fleet is managed | sw-storage + multi-storage; the ARR compounding mechanism |
| **Installed-base moat** | O&M relationships across a large deployed GWh base; customer inertia for follow-on orders | util-storage; compounding with fleet age |
| **Switching cost moat** | BMS and controls integration; changing integrator mid-life requires hardware replacement or recertification | dc-storage and util-storage |

**Note:** this is explicitly NOT an efficient-scale moat (the market is large enough that multiple integrators can co-exist). The moat must come from regulatory positioning, qualification, or software — not from market size alone.

**Scoring guidance:**
- Score **2** if company holds ≥2 moat types with verifiable evidence (e.g. domestic-content premium pricing + hyperscaler qualification + ARR base growing).
- Score **1** if one moat type is clearly operative but others are thin or unproven.
- Score **0** if the thesis relies solely on market growth (TAM) with no structural competitive protection.

**Default inversion note (adapted from IEE adapter):** Pure commodity integrators default to Q3=0 unless proven otherwise. The burden of proof for moat is on the evidence, not on sector tailwinds.

---

### Q4 — Intensity *(universal)*

Is demand per unit of storage rising? Key signals: GW/GWh per data-centre rack density increasing (AI); utility storage duration extending (from 2h to 4h+ projects); government mandates for storage-to-generation ratios in new renewable licences. Rising intensity means the TAM is growing even without new customer counts. The AI-load inflection is the primary intensity driver right now — validate that the company is positioned to capture it, not just that the sector is growing.

---

### Q5 — Visibility *(universal — weighted for this sector)*

**The two visibility signals in storage integration:**

1. **Contracted backlog coverage:** backlog / annual run-rate revenue. A ratio >1.0x is minimum; >2.0x is strong; midpoint of guidance fully covered by backlog = exceptional (FLNC's stated position as of Q2 FY2026).
2. **ARR durability:** recurring revenue from O&M and software as % of total. Growing ARR reduces project-timing volatility (the core risk in this sector). Target: ARR trajectory toward 15%+ of revenue.

**Differentiation from Energy-Power visibility:** Energy-Power uses regulatory rate-base or PPA as the visibility mechanism. Storage integration relies on backlog + ARR — higher execution risk, but faster growth when the model works.

**Sub-type-conditional weighting (v1.1, adopted from §7-C):** for **`sw-storage`**, score Q5 primarily on **ARR durability** — contracted backlog is structurally thin for software-led names (STEM's $23M demonstrated <2 quarters' coverage), so backlog should not dominate the Q5 read. For **`util-storage`** and **`multi-storage`**, **contracted backlog coverage leads** (FLNC's midpoint-fully-covered $5.6B backlog is the exemplar), with ARR as the secondary durability signal.

---

### Q6 — Sovereignty *(universal)*

Storage infrastructure is explicitly strategic. Signals: IRA domestic-content eligibility; FEOC compliance; government procurement preference; ally-shoring of cell supply. Non-compliant sourcing structures are a Q6 risk, not just a supply chain risk. For non-US names, apply the equivalent framework (EU Battery Regulation, Japanese/Korean domestic policy).

**Q6 Symmetric Inversion rule (per note #6):** Chinese-domiciled storage integrators (if ever evaluated) face the inversion: they have sovereign *home-market* advantage but structural exclusion from US/EU IRA/Battery-Regulation markets. Score Q6 reflecting that dual reality — not just one side.

---

### Q7 — Catalyst *(universal)*

Near-term binary forcing events in this sector: first hyperscaler PO conversion (from MSA to purchase order), quarterly backlog-to-revenue conversion print, major government procurement announcement, ARR milestone disclosure, domestic-cell supply agreement extension, IRA safe-harbour deadline clarification. Earnings quarterly cadence is a valid catalyst here because backlog conversion is the primary investor concern.

**Note:** battery cell cost declines are **not** a Q7 catalyst in isolation — they help margins but they also compress ASPs. The net effect is ambiguous; require a specific project or contract event.

---

### Q8 — Technicals & Risk *(universal)*

Standard Percoco 50-week MA gate and regime check. Sector-specific risk overlay:
- **Balance sheet / covenant risk:** pre-profitability integrators carry working capital burden (inventory, project deposits); covenant headroom is a live risk
- **Revenue concentration risk:** H2-weighted revenue recognition is structural to project-delivery models; watch H1/H2 split and full-year conversion
- **Cell manufacturer vertical integration risk:** CATL/BYD direct-to-developer pricing — monitor international margin trends
- **Regulatory rollback risk:** IRA domestic-content amendment; treat as a tail risk, not a base case, but flag when legislative environment deteriorates

---

## 4. Sub-type Q1 weighting summary

| Sub-criterion | `util-storage` | `dc-storage` | `sw-storage` | `multi-storage` |
|---|---|---|---|---|
| Supply chain control | 2 | 2 | 1 | 2 |
| Backlog / demand coverage | 2 | 1 | 0 | 2 |
| Technical qualification | 1 | 2 | 1 | 2 |
| Geographic / market diversification | 1 | 0 | 0 | 1 |
| Domestic content / regulatory positioning | 2 | 2 | 0 | 2 |
| Software-and-services attach | 1 | 2 | 2 | 1 |

---

## 5. VETO / Kill Criteria

### Universal VETOs apply (fraud, governance failures, etc.)

### Storage-integrator-specific
- **Negative gross margin trend** (adjusted gross margin falling below 8% and declining) → veto. The thesis is margin expansion through software attach; if hardware is structurally loss-making and ARR is insufficient to offset, the model is broken.
- **Single-source cell supply with FEOC-non-compliant supplier** → veto flag (not automatic veto, but requires explicit Stage-4 adjudication). Supply chain concentration in a sanctionable source is an existential policy risk.
- **Backlog cancellation rate >10%** → veto flag. Backlog quality is the primary valuation input; systematic cancellations indicate customer financial stress or competitive displacement.
- **Covenant breach or liquidity <3 months operating runway** → veto. Pre-profitability integrators are existentially dependent on liquidity.

---

## 6. Calibration — two locked anchors (v1.1)

The v1.0 draft carried a provisional, pre-blind FLNC worked example at 14/16. The blind 4-stage runs scored materially lower and more disciplined; that single example is **struck as too generous** and replaced by the two-anchor calibration below.

| Ticker | Sub-type | Score | Band | Decisive pillars |
|---|---|---|---|---|
| FLNC | `multi-storage` | 10 / 16 | Passes Screen | Q1=2, Q5=2, Q6=2 strong; Q3=1 (contractor floor, note #6), Q4=1 (note #9 operational-pressure) cap it |
| STEM | `sw-storage` | 8 / 16 | Marginal | Q6=2 (clean domestic software footprint, note #2); Q8=0 (distress); thin backlog caps Q1/Q5 at 1 |

**Calibration finding:** the adapter produces a disciplined ~2-point spread between two pre-profitability integrators (FLNC 10 vs STEM 8) rather than uniformly rewarding the sector — the moat-count ceiling (both Q3=1) and the operational-pressure / distress reality (Q4, Q8) do the differentiating work. The original provisional 14/16 is struck. Both anchors locked 2026-06-13 (HITL, 4-stage protocol).

## 7. Open Decisions

**Resolutions (v1.1 lock supplement, 2026-06-13):**
- **(A) slug** — RESOLVED (SC-36; DB `grid-storage-integrator` ↔ parser `grid_storage_integrator`).
- **(B) four sub-types** — RETAINED; `multi-storage` + `sw-storage` ratified (FLNC, STEM), `util-storage` + `dc-storage` calibration-pending.
- **(C) Q5 sub-type-conditional weighting** — ADOPTED (see §5: `sw-storage` → ARR durability leads; `util-storage`/`multi-storage` → backlog coverage leads).
- **(D) pre-profitability adjustment** — CONFIRMED: no Q4 hard penalty; profitability risk lives in Q8 (validated — FLNC Q8=1, STEM Q8=0 carried the signal correctly).
- **(E) Energy-Power boundary** — CONFIRMED integration-vs-ownership.
- **(F) two-lock ratification** — SATISFIED for the two instantiated sub-types.

**Candidate methodology note (unnumbered, routed to `_methodology_notes_registry.md`):** *`sw-storage` sovereignty-tends-high* — software-led storage integrators tend to score Q6 high (often 2) on a domestic software/IP footprint (note #2) even when the rest of the profile is weak (cf. STEM Q6=2 at 8/16). Q6 is not a business-quality signal for this sub-type. One instance only — promote to a numbered note only on a second `sw-storage` case; explicitly NOT "#28."

The original open decisions are retained below as the authored record.

Before this adapter is locked:

**A. Adapter slug registration**
Proposed slug: `grid-storage-integrator`. Requires Claude Code to register in the parser before any FLNC thesis can upload. Flag for SC-series handover.

**B. Sub-type count (4 proposed)**
Four sub-types: `util-storage`, `dc-storage`, `sw-storage`, `multi-storage`. Are these sufficiently distinct? Recommendation: keep all four. The `dc-storage` sub-type is genuinely different (power quality requirements, footprint efficiency, qualification bar) from `util-storage` (project delivery, backlog coverage). `sw-storage` is an important future case (a company like Stem / AutoGrid where software is dominant). If in practice no `sw-storage` names are evaluated, it can be collapsed in a v1.1 supplement.

**C. Q5 backlog vs. ARR weighting**
For `util-storage` names, ARR may be small and backlog dominates Q5. For `sw-storage` names, ARR dominates and backlog may be minimal. Should Q5 scoring guidance formally split by sub-type? Recommendation: yes — add a sub-type-conditional note to Q5 guidance at v1.1 after first calibration.

**D. Pre-profitability adjustment**
All current candidates (FLNC, Eos, Powin) are pre-profitability or marginally profitable. Should Q4 carry a hard penalty for sustained losses? Recommendation: no — Q4 tests intensity and unit economics trajectory, not absolute profitability. Q8 (technicals/balance-sheet) carries the profitability-risk signal. Profitability is not a veto criterion if backlog is growing and margins are demonstrably expanding.

**E. Boundary with Energy-Power adapter**
A utility that owns a BESS asset (e.g. NextEra's storage portfolio) is NOT this adapter — they are an Energy-Power name. Only score an energy company under this adapter if storage integration (not ownership) is the dominant earnings driver. Confirmed: boundary is integration vs. ownership.

**F. Calibration two-lock requirement**
Per doctrine: sub-types require two locked theses to ratify. FLNC would be the first calibration anchor. A second name (e.g. Powin, Eos Energy, or Stem/AutoGrid if SW-storage) is needed before sub-types are formally ratified.

---

## 8. Schema Sketch

```json
{
  "id": "grid-storage-integrator",
  "name": "Grid-Scale Storage Integrator",
  "applies_to": "stock",
  "version": "1.0",
  "scope": "Utility-scale BESS integration, deployment, and fleet software management",
  "sub_types": ["util-storage", "dc-storage", "sw-storage", "multi-storage"],
  "questions": [
    {
      "id": "bottleneck",
      "label": "Bottleneck / Supply Chain Position",
      "specialized": true,
      "sub_criteria": [
        { "id": "supply_chain_control",         "weight_by_subtype": {"util-storage": 2, "dc-storage": 2, "sw-storage": 1, "multi-storage": 2} },
        { "id": "backlog_demand_coverage",       "weight_by_subtype": {"util-storage": 2, "dc-storage": 1, "sw-storage": 0, "multi-storage": 2} },
        { "id": "technical_qualification",       "weight_by_subtype": {"util-storage": 1, "dc-storage": 2, "sw-storage": 1, "multi-storage": 2} },
        { "id": "geo_market_diversification",    "weight_by_subtype": {"util-storage": 1, "dc-storage": 0, "sw-storage": 0, "multi-storage": 1} },
        { "id": "domestic_content_regulatory",   "weight_by_subtype": {"util-storage": 2, "dc-storage": 2, "sw-storage": 0, "multi-storage": 2} },
        { "id": "software_services_attach",      "weight_by_subtype": {"util-storage": 1, "dc-storage": 2, "sw-storage": 2, "multi-storage": 1} }
      ]
    },
    { "id": "narrative",    "specialized": false },
    { "id": "moat",         "specialized": true,
      "moat_types": ["regulatory", "technical-qualification", "software-bms", "installed-base", "switching-cost"] },
    { "id": "intensity",    "specialized": false },
    { "id": "visibility",   "specialized": false },
    { "id": "sovereignty",  "specialized": false, "applies_note_6_inversion": true },
    { "id": "catalyst",     "specialized": false },
    { "id": "technicals",   "specialized": false }
  ]
}
```

---

## 9. Version History

| Version | Date | Notes |
|---|---|---|
| v1 draft | 2026-06-13 | Initial draft, spawned from FLNC hedge-fund-analyst memo. 4 sub-types, provisional FLNC worked example at 14/16 (Q8 PROVISIONAL). 6 open decisions. |
| **v1.1 locked** | 2026-06-13 | Lock supplement folded. Two calibration anchors locked (FLNC 10/16 `multi-storage`, STEM 8/16 `sw-storage`); provisional 14/16 struck and replaced by the §6 two-anchor table. `multi-storage` + `sw-storage` ratified; `util-storage`/`dc-storage` calibration-pending. §7 A–F resolved; Q5 sub-type-conditional weighting adopted (§5); unnumbered `sw-storage`-Q6 candidate note routed to the registry. Status draft → locked. |

---

*Draft v1. Pending user review and redline. Requires Claude Code slug registration before any thesis can upload. Calibration requires two locked theses (FLNC + one further name). Personal use only. Not investment advice.*
