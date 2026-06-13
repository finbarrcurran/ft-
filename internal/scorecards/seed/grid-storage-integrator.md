# Sector Adapter — Grid-Scale Storage Integrator — v1 (Draft)

> **Status:** New adapter, spawned June 2026. Covers companies whose primary earnings driver is the integration, deployment, and software management of utility-scale and behind-the-meter battery energy storage systems (BESS). Distinct from Energy-Power (generation assets), Industrial-Electrical (OEM equipment), and Cloud-Infra (hyperscaler capacity). The thesis is a bet on the structural electrification inflection: grid storage as the binding enabler of the AI-load + renewables era.
> **Doctrine sources:** `Cross_Sector_Investment_Philosophy_v1_1.md`, `Industrial_Electrical_Equipment_Sector_Adapter_v1.md` (Q1 sub-criteria template), `Energy_Sector_Adapter_v1.md` (sovereign policy framing), `Software_SaaS_Sector_Adapter_v1.md` (ARR / services-attach rate framing).
> **Calibration status:** **Uncalibrated — v1 draft pending first worked example (FLNC is the intended calibration anchor).**
> **Slug (proposed):** `grid-storage-integrator`
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

## 6. Worked Example — FLNC (Fluence Energy) *(Provisional — for calibration)*

**Sub-type:** `multi-storage`

| Pillar | Notes | Provisional score |
|---|---|---|
| Q1 Bottleneck | Domestic cell supply (LG ES Smyrna, second supplier from FY2027); $5.6B contracted backlog (midpoint guidance fully covered); hyperscaler MSA qualification achieved after technical review; US domestic-content eligible; ARR ~$180M guided for FY2026 end. Strong across 4 of 5 weighted sub-criteria for `multi-storage`. Second supplier not yet live (FY2027) is the gap. | **2** |
| Q2 Narrative | Mispricing case: Q2 FY2026 revenue miss (-25% vs consensus) driven by project timing not demand destruction. Market conflating hardware-cycle risk with a business that has >$5.6B backlog and $2B YTD order intake. However, valuation post the +27% after-hours re-rate is less clearly mispriced — not a deep-value setup. Honest Q2. | **1** |
| Q3 Moat | Regulatory moat (IRA domestic-content eligibility, FEOC compliance, price premium vs. Chinese-cell-sourced competitors); technical qualification moat (hyperscaler MSA qualification — rigorous, not easily replicated quickly); software moat (Mosaic OS, Nispera fleet management — growing ARR). Three operative moat types with evidence. Strong. | **2** |
| Q4 Intensity | AI data-centre power demand driving storage intensity step-change (behind-the-meter and co-located); utility storage duration extending (4h+ projects); data-centre pipeline grew >30% Q-on-Q. Clear intensity inflection. | **2** |
| Q5 Visibility | FY2026 midpoint ($3.4B) fully covered by contracted backlog as of Q2 FY2026 — exceptional by this adapter's standard. ARR guided to $180M by year-end. H2 concentration is a risk to quarterly timing but not to full-year delivery. Exceptional on both measures. | **2** |
| Q6 Sovereignty | IRA domestic-content bonus eligible; FEOC-compliant supply chain; US production facilities (Utah modules, Tennessee cells); second-largest contracted US BESS capacity per S&P Global (Dec 2025). Strong. | **2** |
| Q7 Catalyst | First hyperscaler PO conversion expected Q3 FY2026 (near-term, named, specific). Q3 earnings (~Aug 2026) is the conversion confirmation event. Also: second domestic cell supply agreement (FY2027 activation). Genuine binary forcing event within 90 days. | **2** |
| Q8 Technicals | Requires audit at lock. Stock has been volatile (−24% on Q1 FY2026, +27% after Q2 FY2026 after-hours). Covenant amendment April 2026 is a balance-sheet watch item. Regime and 50-week MA check required — **PROVISIONAL.** | **1 (PROVISIONAL)** |
| **Total** | | **~14/16 (PROVISIONAL — Q8 requires live audit)** |

**Calibration note:** If this score holds after the 4-stage protocol, FLNC would be the highest-conviction calibration anchor for this adapter. The score reflects genuinely exceptional backlog visibility and a three-moat structure; Q2 is compressed honestly because the narrative trade is partially played following the Q2 re-rate. Q8 is provisional and could fall to 0 depending on the technical and covenant picture at lock time. A Q8=0 print would move the total to 13/16 — still Strong band.

**Gut-check:** Is 14/16 too generous for a pre-profitability integrator? Counter-argument: the framework is testing the *thesis* not the P&L today. FLNC's thesis (storage integrator in an AI-load + renewables inflection, with regulatory moat + technical qualification moat + software ARR) is genuinely exceptional on the framework criteria. The profitability gap is the Q8 risk and the implicit Q2 discount already applied. The score is honest.

---

## 7. Open Decisions

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

---

*Draft v1. Pending user review and redline. Requires Claude Code slug registration before any thesis can upload. Calibration requires two locked theses (FLNC + one further name). Personal use only. Not investment advice.*
