# Sector Adapter — AI-Infrastructure & Semiconductor — v1 (Draft)

> **Status:** Seventh adapter built under the Cross-Sector Investment Philosophy v1.1. Formalises the existing Jordi Visser stock-picking framework into the adapter schema. Covers the AI compute supply chain — GPUs, foundry, semicap, chip IP, memory/HBM, optical, cooling, specialty semi chemicals, and edge silicon. This is the *largest* adapter by holdings count (9 of 24 current positions).
> **Doctrine source:** `Cross_Sector_Investment_Philosophy_v1.1.md` §6 + `Jordi_Visser_Stock_Picking_Strategy_Framework.md` (preserved)
> **Not investment advice. Personal use only.**

---

## 1. Scope

### In-scope (this adapter covers)
- **GPUs & AI accelerators:** NVDA, AMD MI-series, TPU-adjacent silicon
- **Foundry:** TSM, Samsung Foundry
- **Semicap (litho / etch / dep / inspection):** ASML, AMAT, LRCX, KLAC, TER
- **Chip design / IP:** ARM, SNPS, CDNS, Imagination Tech
- **Memory & HBM:** MU, SK Hynix, Samsung Memory
- **Advanced packaging (CoWoS, fan-out):** Amkor, ASE
- **Optical & datacom networking:** COHR, ANET, AVGO (datacom segment)
- **Cooling & thermal management (data-center focus):** MOD, VRT
- **Specialty semi chemicals:** 4063.T (Shin-Etsu), Entegris, Mitsui Chemicals
- **Edge / industrial silicon:** LSCC, Microchip (MCHP)
- **Quantum compute IP:** RGTI (speculative, frontier sub-type)

### Out-of-scope (different adapter)
- **AI software platforms** (PLTR, AI applications) → ambiguous; if government-revenue-heavy → Defense; if enterprise SaaS → future Software adapter
- **Hyperscaler cloud infrastructure** (ORCL, AMZN AWS segment) → Cloud-Infra / Hyperscaler adapter
- **Pure consumer-tech** (AAPL hardware) → out of personal portfolio scope
- **EV / robotics applications of AI silicon** → future Embodiment adapter (TBD)

### Cross-reference with existing Jordi framework
This adapter is the *operational specialization* of the existing Jordi Visser stock-picking framework. The Cross-Sector Philosophy (v1.1) generalizes Jordi's law; this adapter is the *AI-instance* of that generalization. **The Jordi cheat sheet and framework docs remain authoritative for narrative judgment**; this adapter provides the scoring structure that operationalizes Jordi's thinking inside FT.

Jordi's Scarcity Migration Map (10 stages: GPUs → Optical → HBM → CPUs → Power → Cooling → Specialty Chemicals → Battery Storage → Edge → Embodiment) maps to the sub-types below. Stages 5 (Power) and 8 (Battery Storage) are *handled by other adapters* — Energy-Power and Mining-Metals respectively. Stage 10 (Embodiment) is out of current scope.

### The rotation thesis

The AI-infra thesis is the *origin* of the Cross-Sector framework. Its forcing function is:

> Demand for AI compute is growing exponentially. The physical inputs (chips, memory, optical, packaging, cooling, specialty chemicals) are growing linearly. Wherever the gap is binding, the equity at the binding point re-rates. The binding point migrates over time.

Current binding constraint (mid-2026): **memory/HBM** (stage 3) is the most acute scarcity; **advanced packaging** (CoWoS) follows closely. **GPUs themselves** (stage 1) are still scarce but supply is ramping. **Specialty semi chemicals** (stage 7) are quietly scarce. **Cooling** (stage 6) is constrained but more elastic.

### Worked example: how this splits across current holdings

- **NVDA** → `gpu-accelerator`. Bull thesis intact but maturing.
- **TSM** → `foundry`. Stage 1-4 enabler.
- **ASML** → `semicap-litho`. Stage 1-4 enabler with EUV monopoly.
- **ARM** → `chip-ip`. Stage 1, 9 cross-cutting.
- **LSCC** → `edge-silicon`. Stage 9.
- **COHR** → `optical-datacom`. Stage 2.
- **MOD** → `cooling-thermal`. Stage 6.
- **4063.T** → `specialty-chemicals`. Stage 7.
- **RGTI** → `chip-ip` (quantum frontier; treat speculatively).

---

## 2. Sub-types within this adapter

Mapped to Jordi's Scarcity Migration Map stages where applicable:

| Sub-type | Code | Jordi stage |
|---|---|---|
| GPU & AI accelerator | `gpu-accelerator` | 1 |
| Foundry | `foundry` | 1-4 |
| Semicap — litho / EUV | `semicap-litho` | 1-4 |
| Semicap — etch / dep / inspection | `semicap-etch-dep` | 1-4 |
| Chip design / IP | `chip-ip` | 1, 9 |
| Memory & HBM | `memory-hbm` | 3 |
| Advanced packaging | `advanced-packaging` | 3 |
| Optical / datacom networking | `optical-datacom` | 2 |
| Cooling / thermal | `cooling-thermal` | 6 |
| Specialty semi chemicals | `specialty-chemicals` | 7 |
| Edge / industrial silicon | `edge-silicon` | 9 |

---

## 3. The Eight-Question Adapter

Score each pillar **0 / 1 / 2**. Total out of 16. Strong-pass = **6+/8 pillars ≥1, with Q1 and Q3 ≥1.**

### Q1 — Bottleneck *(sector-specialized)*

> *Is the name sitting at the binding scarcity stage on the AI Scarcity Migration Map? How acute and how durable is the bottleneck position?*

Sub-criteria:

| Sub-criterion | 0 | 1 | 2 |
|---|---|---|---|
| **Scarcity Migration Map stage position** | At a stage where bottleneck has eased / capacity has caught up | At a stage with moderate scarcity | At the *currently most-binding* stage (today: HBM, advanced packaging, specialty chemicals) |
| **Intensity per AI rack / per training cluster** | Flat or declining intensity per unit | Stable | Rising intensity (more GPUs per rack, more HBM per GPU, more optical per cluster, more cooling per kW) |
| **Capacity vs demand** | Demand soft / capacity ample | Balanced | Capacity-constrained for multi-year horizon |
| **Substitution risk** | Easily substituted (commodity silicon) | Some substitution risk | Effectively unsubstitutable for years (EUV, CoWoS, HBM-grade DRAM) |
| **Technology node leadership** | Trailing-edge only | Mid-node | Leading-edge (3nm / 2nm / next) participation |
| **Customer concentration with hyperscalers** | No hyperscaler revenue | Some hyperscaler exposure | Top-3 hyperscalers = significant revenue share + multi-year design wins |

### Q2 — Narrative *(universal)*

AI-infra narrative arbitrage patterns:
- **"AI bubble / capex peak"** when actually mid-buildout with multi-year visibility on memory/packaging.
- **"GPU disruption — custom silicon kills NVDA"** when actually hyperscaler custom silicon complements GPU spend rather than replaces it.
- **"Semicap is cyclical"** when actually riding multi-year EUV adoption + advanced-packaging build-out that's structural not cyclical.
- **"Memory is commodity"** when actually HBM is increasingly a specialty product with switching costs.
- **"Cooling is boring HVAC"** when actually data-center cooling intensity is doubling on rack density.
- **"Edge silicon is irrelevant"** when actually edge inference is the next bottleneck stage on the map.

Score: 0 = consensus correct, 1 = partial mispricing, 2 = clear narrative arbitrage.

### Q3 — Moat *(sector-specialized)*

AI-infra moats:

| Moat type | What it looks like in AI-infra |
|---|---|
| **Intangibles — process/tech IP** | EUV mastery, CoWoS expertise, GPU architecture, HBM packaging know-how |
| **Switching costs** | Multi-year design wins, software ecosystem (CUDA), foundry process customization |
| **Efficient scale** | Capex levels and R&D budgets that smaller players cannot match (~$30B/yr at top of stack) |
| **Cost advantage** | Yield + scale efficiencies at leading-edge nodes |
| **Customer relationships** | Decades-long hyperscaler design partnerships, multi-generation roadmap visibility |

Sub-criteria:
- Number of moat types stacked
- Time-to-replicate by a competitor (years)
- Software ecosystem depth (especially CUDA-like)
- Multi-generation customer roadmap visibility

### Q4 — Intensity *(universal)*

AI-infra intensity signals:
- GPUs per training cluster (10k → 100k → 1M)
- HBM stacks per GPU (rising every generation)
- Optical transceivers per AI rack (rising on rack density)
- kW per rack (rising — drives cooling intensity)
- Specialty chemical kg per wafer (rising on node complexity)

Score: 0 = flat intensity, 1 = growing, 2 = step-change (current HBM stack count growth = step-change).

### Q5 — Visibility *(universal)*

AI-infra visibility:
- Multi-year hyperscaler capex commitments (Microsoft, Meta, Google, Amazon, Oracle quarterly capex guidance)
- Foundry capacity reservation contracts
- Design-win pipeline (multi-generation)
- Backlog at semicap leaders (ASML: 60+ EUV tools/yr through ~2027)
- Long-cycle service revenue on installed semicap base

Score: 0 = book-and-bill cyclical exposure only, 1 = mixed, 2 = multi-year contracted backlog + capex visibility from anchor customers.

### Q6 — Sovereignty *(universal)*

AI-infra sovereignty:
- Taiwan/TSMC geographic concentration risk (negative for TSM if priced cleanly)
- US CHIPS Act tailwinds / friend-shore manufacturing premium
- ITAR / export-control exposure (chips to China)
- Korean / Japanese sovereign positioning (Samsung, SK Hynix, Shin-Etsu)
- Dutch / European semicap leadership (ASML)

Score: 0 = exposed to hostile-jurisdiction geographic concentration, 1 = neutral, 2 = explicit sovereign tailwind + friend-shore positioning.

### Q7 — Catalyst *(universal)*

AI-infra catalysts:
- Quarterly earnings + capex guidance from hyperscalers (immediate read-through)
- New node ramp (3nm, 2nm production milestones)
- HBM4 / HBM4E ramp announcements
- New GPU architecture launches (Blackwell-class)
- Major customer design-win disclosures
- CHIPS Act grant announcements
- Geopolitical events (Taiwan, Korea, export controls)

Score: 0 = none visible, 1 = within 12 months, 2 = within 90 days.

### Q8 — Technicals & Risk *(universal)*

Same as base framework. **AI-infra-specific idiosyncratic risk:** the entire sector trades on hyperscaler capex sentiment, so single-company technicals are often dominated by sector-level moves. The macro/regime overlay (Spec 9b) matters especially here.

---

## 4. Worked Example — NVDA (NVIDIA)

| Pillar | Sub-criteria notes (`gpu-accelerator`) | Score |
|---|---|---|
| Q1 Bottleneck | Stage 1 of migration map, less acute today than HBM but still binding (1-2); intensity per training cluster rising step-change (2); supply has improved but still tight at leading-edge (1-2); software ecosystem (CUDA) is unsubstitutable for years (2); leading-edge node access secured at TSM (2); top hyperscalers all = anchor customers (2) | **2** |
| Q2 Narrative | Market increasingly pricing peak; custom silicon narrative present; partial arbitrage remains on continued hyperscaler capex | **1** |
| Q3 Moat | CUDA + GPU architecture + customer roadmap depth + scale R&D = multiple moats stack | **2** |
| Q4 Intensity | Rising GPUs per cluster + rising HBM per GPU + new architectures every 12-18 months | **2** |
| Q5 Visibility | Multi-quarter visibility from hyperscaler capex; some commitment to multi-year forward orders | **2** |
| Q6 Sovereignty | US-domiciled, China export-control exposure, TSM manufacturing dependence | **1** |
| Q7 Catalyst | Quarterly hyperscaler capex calls + architecture launches | **1** |
| Q8 Technicals | Volatile; check exhaustion / regime carefully | **1** |
| **Total** | | **12 / 16** |

**Interpretation:** Strong pass. Notably, NVDA scores *lower than peak* because the stage 1 bottleneck has eased somewhat (supply ramping, custom silicon emerging) and consensus has caught up to the bull narrative. Framework correctly identifies NVDA as a *strong-hold* rather than a *new-buy* in 2026. Compare to mid-2023 when the same pillar scoring would have produced 14-15/16.

---

## 5. Worked Example — ASML

| Pillar | Sub-criteria notes (`semicap-litho`) | Score |
|---|---|---|
| Q1 Bottleneck | Stage 1-4 enabler; effectively unsubstitutable for leading-edge (EUV monopoly) (2); intensity per wafer rising (2); multi-year backlog (2); zero substitution risk near-term (2); leading-edge node enabler (2); all major foundries = anchor customers (2) | **2** |
| Q2 Narrative | Market has aggressively re-rated ASML on China export restrictions and "semicap cyclical" framing; partial arbitrage on multi-year EUV adoption visibility | **1-2** |
| Q3 Moat | Effective EUV monopoly + decades of IP + customer roadmap depth + scale = strongest moat in the sector | **2** |
| Q4 Intensity | Rising EUV layers per wafer + High-NA EUV adoption + node complexity step-change | **2** |
| Q5 Visibility | 60+ EUV tools/yr through 2027 + High-NA backlog | **2** |
| Q6 Sovereignty | Dutch-domiciled, China export-control exposure (negative), friend-shore semiconductor build-out (positive) | **1** |
| Q7 Catalyst | Quarterly bookings, High-NA milestones, China-policy events | **1** |
| Q8 Technicals | Volatile; check exhaustion / regime | **1** |
| **Total** | | **13-14 / 16** |

**Interpretation:** Strong pass. ASML scores higher than NVDA in this regime because the EUV monopoly is *currently the most durable moat in the sector* and the backlog provides visibility that NVDA's quarterly hyperscaler dependence cannot match. This is exactly what the framework should show.

**Cross-adapter sanity check (all worked examples to date):**
- RHM.DE (Defense) = 14/16
- ASML (AI-Infra) = 13-14/16
- LLY, ABBV (Pharma) = 13/16
- AEM (Mining), NVDA (AI-Infra), SU.PA (Industrial-Electrical) = 12/16
- RR.L Energy-Power, XOM (Hydrocarbons) = 11/16
- ALB (Mining), RR.L defense leg = 10-12/16

The distribution is coherent. Names at the *most acute* binding scarcity in the *most favorable* regime score highest. Names in good but not exceptional setups cluster at 11-13. Names with real scarcity in broken regimes score at the margin. This is the rubric working as intended.

---

## 6. Schema Sketch

```json
{
  "id": "ai-infra-semi",
  "name": "AI-Infrastructure & Semiconductor",
  "applies_to": "stock",
  "version": "1.0",
  "scope": "AI compute supply chain — GPUs, foundry, semicap, chip IP, memory/HBM, advanced packaging, optical, cooling, specialty chemicals, edge silicon.",
  "doctrine_reference": "Jordi_Visser_Stock_Picking_Strategy_Framework.md (preserved)",
  "sub_types": ["gpu-accelerator", "foundry", "semicap-litho", "semicap-etch-dep", "chip-ip", "memory-hbm", "advanced-packaging", "optical-datacom", "cooling-thermal", "specialty-chemicals", "edge-silicon"],
  "questions": [
    {
      "id": "bottleneck",
      "label": "Bottleneck (AI-Infra)",
      "specialized": true,
      "sub_criteria": [
        { "id": "migration_map_stage",  "weight_by_subtype": "see_table" },
        { "id": "intensity_per_unit",   "weight_by_subtype": "see_table" },
        { "id": "capacity_vs_demand",   "weight_by_subtype": "see_table" },
        { "id": "substitution_risk",    "weight_by_subtype": "see_table" },
        { "id": "node_leadership",      "weight_by_subtype": "see_table" },
        { "id": "hyperscaler_concentration", "weight_by_subtype": "see_table" }
      ]
    },
    { "id": "narrative", "specialized": false, "weight": 2 },
    {
      "id": "moat",
      "label": "Moat (AI-Infra)",
      "specialized": true,
      "moat_types": ["intangibles-tech-ip", "switching-costs", "efficient-scale", "cost-advantage", "customer-relationships"],
      "sub_criteria": ["moat_stack_count", "replicate_timeline_years", "ecosystem_depth", "customer_roadmap_visibility"]
    },
    { "id": "intensity", "specialized": false, "weight": 1 },
    { "id": "visibility", "specialized": false, "weight": 1 },
    { "id": "sovereignty", "specialized": false, "weight": 1 },
    { "id": "catalyst", "specialized": false, "weight": 1 },
    { "id": "technicals", "specialized": false, "weight": 1 }
  ]
}
```

(Sub-type weighting matrix is large — 11 sub-types × 6 sub-criteria. Deferred to detailed implementation; placeholder `see_table` above.)

---

## 7. Open Decisions

1. **Migration Map stage as scoring sub-criterion** — currently I score it 0/1/2 based on *current* bottleneck acuity. This means a name's score changes as the bottleneck migrates. **Confirm this dynamic-scoring model.** Alternative: static scoring + flag stage on a separate map dashboard.

2. **NVDA at 12/16 — gut check?** This is a notable de-rating from its 2023-24 setup. Honest reflection of bottleneck migrating away from raw GPU supply, or rubric over-penalizing maturing winners?

3. **ASML at 13-14/16 — gut check?** Adapter rates ASML *above* NVDA. Match your gut?

4. **RGTI handling** — quantum chip IP, speculative. Currently in scope as `chip-ip` sub-type. Alternative: treat as out-of-scope (binary-outcome speculative bet), similar to pre-commercial biotech. *Recommendation:* keep in-scope but acknowledge that the score is highly volatile and any "pass" should be size-disciplined.

5. **Embodiment (stage 10) exclusion** — currently out of scope (TSLA, robotics). Future Robotics & Embodiment adapter (TBD). *Confirm.*

6. **Relationship to existing Jordi cheat sheet** — this adapter operationalizes the framework; the cheat sheet remains the *narrative judgment* reference. Both should coexist. *Confirm.*

7. **Memory/HBM as separate sub-type vs combined with foundry** — current proposal: separate (`memory-hbm`) because the rotation thesis is acute right now and the competitive structure differs (3 players: SK Hynix, Samsung, Micron). *Confirm.*

---

## 8. Next Steps

- User reviews this adapter, confirms open decisions in §7
- Continues batch session: Cloud-Infra / Hyperscaler Capex (final adapter)

---

## 9. Version History

| Version | Date | Notes |
|---|---|---|
| v1 draft | 2026-05-17 | Initial draft formalising Jordi framework into adapter schema |

---

*Draft v1. Pending user review. Personal use only. Not investment advice.*
