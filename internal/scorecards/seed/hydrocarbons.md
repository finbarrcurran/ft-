# Sector Adapter — Hydrocarbons — v1 (Draft)

> **Status:** Second adapter built under the Cross-Sector Investment Philosophy (v1). Covers oil & gas upstream, integrated majors, LNG infrastructure, refining, and midstream — separated from the Energy (Power Infrastructure) adapter because the rotation thesis, moat structure, and bottleneck dynamics differ materially.
> **Not investment advice. Personal use only.**

---

## 1. Scope

### In-scope (this adapter covers)
- **Integrated majors:** XOM, CVX, SHEL, BP, TTE
- **E&P pure-play (upstream):** OXY, CTRA, PXD-style names
- **LNG infrastructure:** liquefaction (LNG/Cheniere), regasification, LNG shipping
- **Refining & marketing:** VLO, MPC, PSX
- **Midstream:** pipelines, storage, gathering (EPD, ET, MPLX, KMI)
- **Oilfield services:** SLB, HAL, BKR (drilling, fracking, completions)
- **Seaborne crude / product shipping:** as a sub-thesis (FRO, INSW, STNG)

### Out-of-scope (different adapter)
- **Power generation gas turbines** → Energy-Power adapter (different demand driver: data centers, not transport / petrochem)
- **Natural gas-fired power plants** → Energy-Power adapter
- **Renewables / solar / wind** → Energy-Power adapter
- **Uranium / nuclear fuel cycle** → Energy-Power adapter
- **Petrochemical specialty (Shin-Etsu, LYB)** → Materials adapter (TBD) — feedstocks come *from* hydrocarbons but the value-add is downstream chemistry, not fuel

### How the bend with Energy-Power gets handled
Some names span both adapters — e.g., XOM (huge oil + small LNG-for-power), or TTE (oil + renewables). Treat as multi-segment: score under each applicable adapter, weight by revenue mix, take the higher-conviction adapter as the *primary* thesis. Don't pretend it's a pure-play.

### The rotation thesis
This adapter exists because hydrocarbons re-rate on different forcing functions than power infrastructure:

| Power-Infra rotation thesis | Hydrocarbons rotation thesis |
|---|---|
| AI / data center demand | Energy security + supply destruction |
| Demand step-change up | Supply destruction (OPEC+ discipline, US shale exhaustion, ESG capex starvation) |
| Years of catalysts | Geopolitical shock catalysts |
| Customer = hyperscalers | Customer = global GDP |
| Lock-in via PPAs | Lock-in via reserves + infrastructure |
| Growth narrative | Value + capital-return narrative |

The two cycles can move in opposite directions — power infra can be in capex super-cycle while hydrocarbons are in discipline / buyback mode. They are not the same trade.

---

## 2. Sub-types within this adapter

| Sub-type | Code | Notes |
|---|---|---|
| Integrated major | `int-maj` | Diversified upstream + downstream + chemicals; defensive, dividend-anchored |
| E&P pure-play | `ep` | Direct leverage to oil/gas price; reserve-life matters most |
| LNG infrastructure | `lng` | Long-cycle, contracted, the most "infrastructure-like" sub-type |
| Refining | `refining` | Margin business; crack spreads, utilization, location |
| Midstream | `midstream` | Volumetric tolling; lowest commodity exposure |
| Oilfield services | `ofs` | Capex-cycle leverage; rig counts, frac spread counts |
| Seaborne shipping | `shipping` | Day rates; geopolitical re-routing creates ton-mile expansion |

---

## 3. The Eight-Question Adapter

Score each pillar **0 / 1 / 2**. Total out of 16. Strong-pass = **6+/8 pillars at score ≥1, with Q1 and Q3 ≥1.**

### Q1 — Bottleneck *(sector-specialized)*

> *Is the name sitting at a binding scarcity in hydrocarbons — either supply-side (reserves, capacity, infrastructure) or demand-side (refining mix, shipping ton-miles)?*

Sub-criteria (score 0/1/2 each, average → pillar score):

| Sub-criterion | 0 | 1 | 2 |
|---|---|---|---|
| **Reserve life / replacement** | <8 yrs reserves, no replacement | 8-15 yrs, replacing 1:1 | >15 yrs, growing reserves below market cost |
| **Position on cost curve (AISC / breakeven)** | Top quartile (high cost) | Mid-quartile | Bottom quartile (lowest breakeven) |
| **Asset jurisdiction risk** | OPEC dependence / hostile state | Mixed jurisdiction | OECD / friend-shored |
| **Infrastructure access** | No pipeline/terminal access | Adequate access | Owns / controls scarce infrastructure (Permian pipe, LNG terminal slot) |
| **Capital discipline** | Growth-at-any-cost | Mixed (some growth capex) | Disciplined: capex < 50% of CFO, surplus to buybacks/divs |
| **Demand-pull setup** | Demand stagnant/declining | Stable | Step-change up (re-routing, restocking, sanctions-driven) |

Pillar score = round(avg of applicable sub-criteria, weighted by sub-type emphasis).

### Q2 — Narrative *(universal — unchanged)*

> *Mispriced in the wrong bucket?*

Hydrocarbon-specific narrative arbitrage patterns:
- **"Stranded asset / terminal-decline"** when actually entering structural supply tightness (the 2020-2024 ESG-capex-starvation thesis still partly intact for some names)
- **"Cyclical commodity"** when actually contracted infrastructure (most midstream + LNG names)
- **"Old economy / no growth"** when actually compounding via buybacks at <8x earnings (XOM, CVX style)
- **"Refining is dying"** when actually a structurally short refining capacity story for complex sour-crude refiners (VLO style)
- **"Tankers are dying"** when geopolitical re-routing (Russia, Red Sea) expands ton-miles meaningfully

Score: 0 = consensus correct, 1 = partial mispricing, 2 = clear narrative arbitrage.

### Q3 — Moat *(sector-specialized)*

> *Which moat type is operative, and how durable?*

Hydrocarbons moats (Dorsey / Morningstar taxonomy adapted):

| Moat type | What it looks like in hydrocarbons |
|---|---|
| **Cost advantage** | Bottom-quartile breakeven (best Permian acreage, lowest-lifting-cost OPEC concessions, best refinery complexity) |
| **Efficient scale** | Geography-locked: only viable pipeline corridor, only deepwater terminal in basin, only refinery in load pocket |
| **Switching costs** | 20+ year LNG offtake contracts, integrated upstream-to-marketing, customer dependence on grade slate |
| **Regulatory** | Permits that cannot be replicated (no new US LNG terminals approved for years; no new refineries built since 1970s) |
| **Intangibles** | Reserve quality + geological data accumulated over decades |

Sub-criteria:
- Market structure (concentrated / fragmented)
- Replacement cost vs market cap (deep value flag)
- Lock-in duration of contracted revenue (years)
- Permit / regulatory irreplaceability

Pillar score = 2 if multiple moat types stack (e.g., cost + regulatory in refining); 1 if single durable moat; 0 if pure commodity exposure.

### Q4 — Intensity *(universal)*

> *Is demand-per-unit-of-output rising in the customer base?*

Hydrocarbon-specific intensity signals:
- Global vehicle miles travelled (EV penetration is the counter-force; watch the net)
- Jet fuel demand (still-recovering aviation cycle, structurally below GDP correlation)
- Petrochemical feedstock demand (ethylene, propylene) — tied to global manufacturing
- Diesel demand from re-shoring / industrial growth
- LNG demand from Europe (post-Russia), Asia (coal-to-gas switching)
- Ton-mile expansion in shipping from Russia oil re-routing, Red Sea avoidance

Score: 0 = flat / declining, 1 = stable, 2 = step-change visible.

### Q5 — Visibility *(universal)*

> *Take-or-pay or contracted revenue?*

Hydrocarbon-specific visibility:
- LNG offtake contracts (20-25 yr typical) — highest visibility
- Midstream tolling contracts (firm vs interruptible)
- Refining utilization + crack-spread futures curve
- E&P hedge book (forward sales)
- Integrated majors: blend of merchant and contracted

Score: 0 = full spot exposure, 1 = mixed, 2 = >50% revenue contracted >5 years.

### Q6 — Sovereignty *(universal)*

> *Strategic, policy-supported, friend-shored?*

Hydrocarbon-specific sovereignty:
- US-domiciled / OECD vs OPEC vs sanctioned
- Strategic Petroleum Reserve refill cycle (US gov demand)
- Permitting tailwinds vs headwinds (admin-dependent)
- Sanctions exposure (Russia, Iran, Venezuela counterparties)
- LNG export-permit status

Score: 0 = sanctioned / hostile jurisdiction, 1 = neutral, 2 = explicit sovereign tailwind (e.g., US LNG to Europe).

### Q7 — Catalyst *(universal)*

> *Near-term event forcing re-rating?*

Hydrocarbon-specific catalysts:
- OPEC+ meeting (production decision)
- Geopolitical shock (Russia, Middle East, Venezuela)
- Major announcement: new LNG FID, terminal expansion, refinery upgrade
- Quarterly earnings + buyback announcement
- SPR refill announcement
- Hurricane / weather-driven supply disruption (seasonal)
- US election / policy shift on permits

Score: 0 = none visible, 1 = within 12 months, 2 = within 90 days.

### Q8 — Technicals & Risk *(universal)*

> *Chart clean, exhaustion / turbulence signals quiet?*

Same as base framework. No hydrocarbon-specific adjustment.

---

## 4. Worked Example — XOM (ExxonMobil)

Illustrative scoring. **Not a recommendation.**

| Pillar | Sub-criteria notes | Score |
|---|---|---|
| Q1 Bottleneck (`int-maj`) | Reserves diversified incl. Guyana low-cost (2); top-quartile-low breakeven post-Guyana (2); OECD-weighted jurisdictions (2); owns scarce infra incl. LNG positions (2); capital-disciplined post-2020, surplus to buybacks (2); demand stable, not step-change (1) | **2** |
| Q2 Narrative | Partly de-rated by ESG-capex-starvation narrative; market still partially anchors on "old economy" despite Guyana growth and integrated chemicals — moderate arbitrage | **1** |
| Q3 Moat | Cost advantage (Guyana) + efficient scale (downstream integration) + reserves intangibles → multiple moats stack | **2** |
| Q4 Intensity | Global oil demand flat-ish, petrochem stable; net intensity flat | **1** |
| Q5 Visibility | Integrated → blend of merchant + contracted; LNG portfolio contracted; mid visibility | **1** |
| Q6 Sovereignty | US-domiciled, OECD assets, US LNG export tailwind | **2** |
| Q7 Catalyst | Quarterly buyback announcements + Guyana production milestones; nothing imminent in <90 days unless OPEC+ shifts | **1** |
| Q8 Technicals | Range-bound recently; check for clean breakout/breakdown | **1** |
| **Total** | | **11 / 16** |

**Interpretation:** Passes (6+ pillars ≥1, Q1 and Q3 both ≥1). The adapter correctly identifies XOM as an *infrastructure-grade integrated major with reserves + capital discipline* — not a speculative oil-price bet. That's the right framing for the holding.

**Honest caveat:** XOM has small LNG-to-power exposure that could also score under the Energy-Power adapter. Effect on total is minimal — the integrated major is overwhelmingly an oil/gas/chemicals story.

---

## 5. Worked Example — VLO (Valero) *(illustrative, not held)*

To test the adapter on a non-held sub-type (`refining`):

| Pillar | Notes | Score |
|---|---|---|
| Q1 Bottleneck (`refining`) | Complex refineries process sour crude (scarcity premium) (2); top-quartile margin position (2); US Gulf Coast assets (2); pipeline access excellent (2); buyback discipline strong (2); demand-pull: diesel + jet recovery (1-2) | **2** |
| Q2 Narrative | "Refining is dying" — clear narrative arbitrage candidate vs structurally short global complex refining capacity | **2** |
| Q3 Moat | Regulatory (no new US refineries) + cost (complexity premium) + scale → multiple moats | **2** |
| Q4 Intensity | Diesel + jet recovery; petrochem feedstock demand | **2** |
| Q5 Visibility | Merchant-exposed margins, but utilization patterns predictable | **1** |
| Q6 Sovereignty | US-domiciled, energy security positive | **2** |
| Q7 Catalyst | Crack-spread trends + quarterly buybacks | **1** |
| Q8 Technicals | Volatile — check exhaustion/turbulence carefully | **0-1** |
| **Total** | | **~12-13 / 16** |

**Interpretation:** Adapter cleanly handles a *different sub-type* with sub-type-specific bottleneck criteria (refinery complexity, crack spreads, jurisdiction). Validates that the adapter is portable across hydrocarbon sub-types, not just integrated majors.

---

## 6. Schema Sketch (for future spec)

```json
{
  "id": "hydrocarbons",
  "name": "Hydrocarbons",
  "applies_to": "stock",
  "version": "1.0",
  "scope": "Oil & gas upstream, integrated majors, LNG infrastructure, refining, midstream, oilfield services, seaborne shipping",
  "sub_types": ["int-maj", "ep", "lng", "refining", "midstream", "ofs", "shipping"],
  "questions": [
    {
      "id": "bottleneck",
      "label": "Bottleneck (Hydrocarbons)",
      "specialized": true,
      "sub_criteria": [
        { "id": "reserve_life", "weight_by_subtype": {"int-maj": 2, "ep": 2, "lng": 1, "refining": 0, "midstream": 0, "ofs": 0, "shipping": 0} },
        { "id": "cost_curve", "weight_by_subtype": {"int-maj": 2, "ep": 2, "lng": 1, "refining": 2, "midstream": 1, "ofs": 1, "shipping": 1} },
        { "id": "jurisdiction", "weight_by_subtype": {"int-maj": 2, "ep": 2, "lng": 1, "refining": 1, "midstream": 1, "ofs": 1, "shipping": 1} },
        { "id": "infrastructure_access", "weight_by_subtype": {"int-maj": 2, "ep": 2, "lng": 2, "refining": 2, "midstream": 2, "ofs": 0, "shipping": 1} },
        { "id": "capital_discipline", "weight_by_subtype": {"int-maj": 2, "ep": 2, "lng": 1, "refining": 2, "midstream": 1, "ofs": 1, "shipping": 1} },
        { "id": "demand_pull", "weight_by_subtype": {"int-maj": 1, "ep": 1, "lng": 2, "refining": 2, "midstream": 1, "ofs": 1, "shipping": 2} }
      ]
    },
    { "id": "narrative", "specialized": false, "weight": 2 },
    {
      "id": "moat",
      "label": "Moat (Hydrocarbons)",
      "specialized": true,
      "moat_types": ["cost", "scale", "switching", "regulatory", "intangibles"],
      "sub_criteria": ["market_structure", "replacement_cost", "lock_in_duration", "permit_irreplaceability"]
    },
    { "id": "intensity", "specialized": false, "weight": 1 },
    { "id": "visibility", "specialized": false, "weight": 1 },
    { "id": "sovereignty", "specialized": false, "weight": 1 },
    { "id": "catalyst", "specialized": false, "weight": 1 },
    { "id": "technicals", "specialized": false, "weight": 1 }
  ]
}
```

---

## 7. Open Decisions

1. **SU.PA's hydrocarbon exposure** — minor. Schneider is industrial electrical; their oil & gas customer base is incidental. Stays in Industrials, not split here. *Confirm.*
2. **Whether to break out "tanker shipping" as a separate adapter** — it's arguably commodity-shipping more than hydrocarbons. For now, kept as a sub-type because the rotation driver is hydrocarbon flow patterns (Russia re-routing, Red Sea). Revisit if FRO/INSW gets watchlisted.
3. **Worked-example confirmation: XOM at 11/16.** Does that match your gut? If not, recalibrate sub-criteria thresholds before locking.
4. **Capital discipline weighting.** Currently treated as bottleneck criterion (Q1) because in this cycle it's the *real* scarcity (discipline > geology). Alternative: move to Moat or Visibility. Default: leave in Q1, it's the doctrinal break from the 2010s cycle.

---

## 8. Next Steps

- User reviews this adapter, confirms open decisions in §7
- After XOM locked → write next adapter. Candidates: **Pharma** (LLY/ABBV), **Defense** (RHM.DE), **Mining/Metals** (GLD/AEM/AU/SLV stack)
- After 4-5 adapters drafted: write the formal Adapter System spec to bring them together into the FT scoring UI (separate from Spec 9f, which is dashboard-only)

---

*Draft v1. Pending user review. Personal use only. Not investment advice.*
