# Sector Adapter — Mining & Metals — v1 (Draft)

> **Status:** Fifth adapter built under the Cross-Sector Investment Philosophy v1.1. Covers physical-asset miners and metals streaming companies across precious metals (gold/silver), battery metals (lithium, copper, nickel), and critical minerals. Excludes physical commodity ETFs which are tracked at the sub-sector level but not scored individually.
> **Doctrine source:** `Cross_Sector_Investment_Philosophy_v1.1.md` §6
> **Not investment advice. Personal use only.**

---

## 1. Scope

### In-scope (this adapter covers)
- **Gold miners — tier-1 producers:** NEM, AEM, AU, KGC, GOLD (Barrick)
- **Gold miners — mid-tier:** AGI, EQX, OR, BTG
- **Silver miners — primary producers:** PAAS, HL, FSM
- **Streaming / royalty companies:** WPM, FNV, RGLD, SAND
- **Battery / critical metals — lithium:** ALB, SQM, LTHM
- **Battery / critical metals — copper:** FCX, SCCO, TECK
- **Battery / critical metals — nickel:** VALE (nickel-relevant segment), Norilsk (geopolitically excluded)
- **Critical minerals — uranium:** CCJ, NXE, DNN (note: also fits Energy-Power `fuel` sub-type — see §1 cross-reference)
- **Critical minerals — rare earths:** MP Materials, Lynas

### Out-of-scope
- **Physical commodity ETFs** (GLD, SLV, IAU, PSLV) — these are *price-tracking instruments* of the underlying, not operating businesses. Tagged at sub-sector level in rotation tracker; **not scored under this adapter**. The 8-Q screen doesn't apply to a spot-price proxy.
- **Diversified mining majors** (BHP, RIO) — possible to score here but their iron ore exposure dominates revenue mix; iron ore is a steel-cycle commodity not a critical-metal thesis. *Recommendation:* score under this adapter only if the critical-metals segment >40% of revenue.
- **Coal mining** — out of personal portfolio scope.

### Cross-reference: uranium overlap with Energy-Power
Uranium miners (CCJ etc.) fit both this adapter (`uranium`) and Energy-Power's `fuel` sub-type. Per Adapter Assignment Principle (Philosophy v1.1 §6.1): score under whichever adapter's rotation thesis materially drives earnings. For uranium right now, nuclear power demand (Energy-Power rotation thesis) drives the bull case more directly than monetary/critical-metal hedging. **Default: score uranium under Energy-Power, not Mining.** Re-evaluate if monetary regime becomes the dominant uranium thesis.

### The rotation thesis

Mining re-rates on forcing functions that mix monetary, industrial, and physical-scarcity drivers — varies by sub-type:

| Sub-type | Primary rotation driver |
|---|---|
| Gold | **Monetary regime + central bank buying + fiscal dominance hedge** |
| Silver | **Hybrid: monetary + industrial (solar, EV, electronics)** |
| Streaming | **Leveraged exposure to underlying metal with capital-discipline moat** |
| Lithium | **EV / battery storage demand vs supply-curtailment cycles** |
| Copper | **AI-power grid expansion + EV electrification + supply-constrained reserves** |
| Rare earths | **Sovereignty / de-China supply chain rerouting** |

The binding constraint varies by sub-type. For gold: reserve life + production discipline + jurisdiction. For copper: grade decline + permitting backlog + new-mine timelines (10-15 years from discovery to production). For lithium: project pipeline + cost-curve position vs Chinese refining. For rare earths: ex-China refining capacity.

### Worked example: how this splits across the current holdings

- **GLD, SLV** → out of scope (physical ETFs); tagged at sub-sector level only.
- **AEM, AU** → `gold-tier1` sub-type.
- **WPM** → `streaming` sub-type. Per Holdings Mapping v1.1, tagged as gold (revenue mix slightly gold-weighted).
- **PAAS** → `silver-primary` sub-type.
- **ALB** → `lithium` sub-type.

---

## 2. Sub-types within this adapter

| Sub-type | Code | Notes |
|---|---|---|
| Gold — tier-1 producer | `gold-tier1` | Multi-mine, multi-jurisdiction, >500koz annual production |
| Gold — mid-tier producer | `gold-midtier` | Single or few mines, growth-orientated |
| Silver — primary producer | `silver-primary` | >50% revenue from silver |
| Streaming / royalty | `streaming` | Capital-light model; revenue from delivered ounces under stream contracts |
| Lithium | `lithium` | Brine + hard rock producers |
| Copper | `copper` | Pure-play or copper-dominant diversified |
| Nickel | `nickel` | Class-1 battery-grade producers |
| Uranium *(cross-ref Energy-Power `fuel`)* | `uranium` | Score under Energy-Power by default |
| Rare earths | `rare-earths` | Ex-China processing capacity |

---

## 3. The Eight-Question Adapter

Score each pillar **0 / 1 / 2**. Total out of 16. Strong-pass = **6+/8 pillars ≥1, with Q1 and Q3 ≥1.**

### Q1 — Bottleneck *(sector-specialized)*

> *Is the name sitting at a binding physical scarcity — reserves, grade, jurisdiction, AISC position, processing capacity, or by-product economics?*

Sub-criteria:

| Sub-criterion | 0 | 1 | 2 |
|---|---|---|---|
| **Reserve life** | <8 yrs at current production | 8-15 yrs | >15 yrs, replacing reserves |
| **Grade vs sector median** | Below median | At median | Top-quartile grade |
| **Jurisdiction risk** | Hostile / nationalisation-risk states | Mixed jurisdictions | OECD / friend-shore |
| **All-In Sustaining Cost (AISC) vs spot** | Top quartile (high cost) | Mid-quartile | Bottom-quartile (lowest AISC) |
| **By-product credits / co-product economics** | Single-metal, no credits | Some credits | Significant by-product offset (copper-gold porphyries, nickel-copper-PGM) |
| **Permitting / project pipeline** | Permitting stalled | Permitted, pre-construction | Operating + expansion permitted |
| **Capital discipline** | Growth-at-any-cost dilution risk | Mixed | Disciplined: capex <70% CFO, surplus to dividends/buybacks |

Sub-criterion weighting by sub-type emphasis applied in JSON schema (§6).

### Q2 — Narrative *(universal)*

Mining-specific narrative arbitrage patterns:
- **"Gold has had its run"** when actually central bank buying has structurally shifted gold demand for 5+ years (post-2022 sanctions regime).
- **"Lithium is in oversupply / dead"** when actually the cost curve is brutal for high-cost producers but low-cost producers re-rate on volume.
- **"Mining majors are value traps"** when actually capital discipline post-2015 has fundamentally changed shareholder return profiles.
- **"Streamers under-perform underlying"** when actually optionality on new streams compounds at higher returns than spot.
- **"Copper is a slow grind"** when actually AI-grid copper intensity is a step-change demand pull priced in over years not quarters.
- **"Rare earths are over-hyped"** when ex-China processing genuinely doesn't exist at scale and sovereign customers will pay premiums.

Score: 0 = consensus correct, 1 = partial mispricing, 2 = clear narrative arbitrage.

### Q3 — Moat *(sector-specialized)*

Mining moats are unusual — most miners are pure commodity producers with no moat. Real moats come from:

| Moat type | What it looks like in mining |
|---|---|
| **Cost advantage** | Bottom-quartile AISC (lowest-cost gold mines, lowest-cost brine lithium) |
| **Geological irreplaceability** | Unique grade or scale (Tier-1 deposits cannot be replicated by capital alone) |
| **Capital discipline** | Management track record of *not* destroying value at cycle peaks |
| **Streaming business model** | Capital-light, optionality-rich (FNV, WPM model) |
| **Regulatory / sovereign** | Permitted ex-China processing (rare earths), grandfathered permits |

Sub-criteria:
- Number of moat types stacked
- Cost-curve position (AISC quartile)
- Replacement cost vs market cap (deep value flag)
- Track record of capital discipline (5-year FCF / dividend / buyback consistency)

Pillar score = 2 if multiple moats stack (cost + geological irreplaceability + discipline); 1 if single durable moat; 0 if pure commodity exposure with no differentiation.

### Q4 — Intensity *(universal)*

Mining-specific intensity:
- **Gold:** central bank net buying (annual tonnage), retail bar / coin demand
- **Silver:** solar PV silver intensity, EV silver content, electronics demand
- **Lithium:** EV penetration × battery size × lithium content trajectory
- **Copper:** AI-grid copper kg/MW, EV copper kg/vehicle, grid investment cycle
- **Rare earths:** EV motor magnet demand, wind turbine demand, defense magnet demand
- **Uranium:** nuclear reactor restart count, SMR pipeline, ex-Russia conversion/enrichment demand

Score: 0 = flat / declining, 1 = stable growth, 2 = step-change visible (central bank gold buying 2022+ = step-change; AI-grid copper = step-change).

### Q5 — Visibility *(universal)*

Mining-specific visibility:
- Long-term offtake agreements (lithium converters, copper smelters)
- Streaming contracts (for streamers — fixed price per delivered oz over decades)
- Reserve-life × current production = revenue visibility horizon
- Hedge book (rare in modern miners post-2010s discipline reset; presence often signals weakness)

Score: 0 = full spot exposure with short reserve life, 1 = mixed, 2 = long reserve life + contracted offtake / streams.

### Q6 — Sovereignty *(universal)*

Mining-specific sovereignty:
- Production jurisdiction (Canada / Australia / US = max premium; Africa / LatAm = mixed; Russia / DRC certain assets = penalty)
- Critical-minerals designation (DPA Title III, EU Critical Raw Materials Act)
- US Inflation Reduction Act eligibility (battery-metal qualification for EV credits)
- Ex-China supply chain positioning (rare earths, lithium processing)

Score: 0 = hostile jurisdiction concentration, 1 = neutral, 2 = explicit sovereign tailwind + critical-minerals designation.

### Q7 — Catalyst *(universal)*

Mining-specific catalysts:
- Quarterly production reports
- New resource / reserve estimate (NI 43-101, JORC updates)
- Project sanctioning / FID announcement
- Major M&A in sub-sector
- Central bank gold purchase data (monthly WGC reports)
- Spot price breakouts (commodity price action itself is often the catalyst)
- Government supply-chain policy (e.g., DPA grant, IRA qualification)

Score: 0 = none visible, 1 = within 12 months, 2 = within 90 days.

### Q8 — Technicals & Risk *(universal)*

Same as base framework. **Mining-specific idiosyncratic risk:** commodity-price moves dominate equity returns over short windows. Mining technicals often follow spot-price technicals with leverage. Worth tracking the underlying commodity chart alongside the equity chart.

---

## 4. Worked Example — AEM (Agnico Eagle Mines)

Illustrative scoring. **Not a recommendation.**

| Pillar | Sub-criteria notes (`gold-tier1`) | Score |
|---|---|---|
| Q1 Bottleneck | Multi-asset reserve life >15 yrs (2); above-median grade portfolio (1-2); Canada + Finland + Australia jurisdictions = OECD-only (2); mid-to-bottom-quartile AISC (1-2); minimal by-product credits (0-1); operating + expansion pipeline (2); capital-disciplined post-2015 reset (2) | **2** |
| Q2 Narrative | Market partly anchored on "gold has had its run" despite central bank buying step-change; partial arbitrage | **1** |
| Q3 Moat | Cost advantage + OECD jurisdiction premium + capital discipline + multi-asset scale = multiple moats stack | **2** |
| Q4 Intensity | Central bank net gold buying step-change (2022+); retail demand stable; ETF flows variable | **2** |
| Q5 Visibility | Long reserve life + operating consistency = high visibility for a miner; no contracted offtake (gold sells at spot) | **1** |
| Q6 Sovereignty | Canadian-domiciled, all OECD jurisdictions, friend-shore | **2** |
| Q7 Catalyst | Quarterly production + WGC monthly central bank data + spot breakout patterns | **1** |
| Q8 Technicals | Tracking spot gold which has been in uptrend; check exhaustion on stock-specific level | **1** |
| **Total** | | **12 / 16** |

**Interpretation:** Solid pass (6+ pillars ≥1, Q1 and Q3 both 2). Scores below LLY (13) and RHM.DE (14) because gold's rotation thesis is *defensive / monetary hedge* rather than *demand step-change*, and the binding constraint is less acute than ammunition capacity or GLP-1 manufacturing. The framework correctly identifies AEM as a *high-quality, multi-moat miner in a structurally favorable but not exceptional regime*. Worth holding; not screaming-buy territory unless gold breaks out further.

---

## 5. Worked Example — ALB (Albemarle)

Illustrative scoring. **Not a recommendation.**

| Pillar | Sub-criteria notes (`lithium`) | Score |
|---|---|---|
| Q1 Bottleneck | Significant reserves at Salar de Atacama + Greenbushes (2); top-quartile grade brine + hard rock (2); Chile + Australia + US jurisdictions (1-2 — Chile politics is the wildcard); cost position varies by asset (1); lithium by-product credits minimal (0); permitting in place for current scale (1); capital discipline tested by 2024-25 lithium crash (1) | **1** |
| Q2 Narrative | Market has aggressively priced "lithium is dead" narrative; if EV demand re-accelerates or supply curtails, narrative arbitrage real | **2** |
| Q3 Moat | Cost advantage on brine + geological irreplaceability (Atacama is irreplaceable) + Greenbushes share = multiple moats | **2** |
| Q4 Intensity | EV penetration trajectory + battery storage demand rising; near-term intensity *flat* due to inventory overhang; medium-term step-change intact | **1** |
| Q5 Visibility | Some long-term offtake contracts; recent price collapse exposed merchant exposure; reserve life long | **1** |
| Q6 Sovereignty | US-domiciled, IRA-qualified, but Chilean asset exposure | **1** |
| Q7 Catalyst | Supply curtailment announcements (Chinese lepidolite shutdowns), EV demand re-acceleration, spot-price floor | **1** |
| Q8 Technicals | Stock has been brutal; check for basing pattern + spot lithium chart | **0-1** |
| **Total** | | **~10-11 / 16** |

**Interpretation:** Marginal pass (just barely 6 pillars ≥1, Q1 and Q3 both ≥1). Reflects that ALB sits at a *real geological scarcity* (Atacama is irreplaceable) but in a *cyclically broken regime* (lithium oversupply 2024-25). Framework correctly flags as "hold or size carefully" rather than "screaming buy" or "exit." Honest scoring vs forcing a higher number.

**Cross-adapter sanity check so far:**
- RHM.DE (Defense) = 14/16
- LLY, ABBV (Pharma) = 13/16 each
- AEM (Mining) = 12/16
- RR.L (Energy-Power), XOM (Hydrocarbons) = 11/16 each
- ALB (Mining) = 10-11/16

This is the right distribution. Names at the binding scarcity of a step-change-up rotation (RHM, LLY) score high. Names in structurally favorable but not exceptional regimes (AEM, RR.L, XOM) score in the strong-pass range. Names with genuine scarcity but cyclically broken regime (ALB) score at the margin.

---

## 6. Schema Sketch

```json
{
  "id": "mining-metals",
  "name": "Mining & Metals",
  "applies_to": "stock",
  "version": "1.0",
  "scope": "Physical-asset miners and streaming companies in precious metals, battery metals, and critical minerals. Excludes physical commodity ETFs.",
  "sub_types": ["gold-tier1", "gold-midtier", "silver-primary", "streaming", "lithium", "copper", "nickel", "uranium", "rare-earths"],
  "questions": [
    {
      "id": "bottleneck",
      "label": "Bottleneck (Mining)",
      "specialized": true,
      "sub_criteria": [
        { "id": "reserve_life",       "weight_by_subtype": {"gold-tier1": 2, "gold-midtier": 2, "silver-primary": 2, "streaming": 1, "lithium": 2, "copper": 2, "nickel": 2, "uranium": 2, "rare-earths": 2} },
        { "id": "grade",              "weight_by_subtype": {"gold-tier1": 2, "gold-midtier": 2, "silver-primary": 2, "streaming": 0, "lithium": 2, "copper": 2, "nickel": 2, "uranium": 2, "rare-earths": 2} },
        { "id": "jurisdiction",       "weight_by_subtype": {"gold-tier1": 2, "gold-midtier": 2, "silver-primary": 2, "streaming": 1, "lithium": 2, "copper": 2, "nickel": 2, "uranium": 2, "rare-earths": 2} },
        { "id": "aisc",               "weight_by_subtype": {"gold-tier1": 2, "gold-midtier": 2, "silver-primary": 2, "streaming": 0, "lithium": 2, "copper": 2, "nickel": 2, "uranium": 2, "rare-earths": 1} },
        { "id": "byproduct_credits",  "weight_by_subtype": {"gold-tier1": 1, "gold-midtier": 1, "silver-primary": 1, "streaming": 0, "lithium": 1, "copper": 2, "nickel": 2, "uranium": 1, "rare-earths": 1} },
        { "id": "permitting_pipeline","weight_by_subtype": {"gold-tier1": 1, "gold-midtier": 2, "silver-primary": 1, "streaming": 1, "lithium": 2, "copper": 2, "nickel": 2, "uranium": 2, "rare-earths": 2} },
        { "id": "capital_discipline", "weight_by_subtype": {"gold-tier1": 2, "gold-midtier": 1, "silver-primary": 1, "streaming": 2, "lithium": 1, "copper": 2, "nickel": 1, "uranium": 1, "rare-earths": 1} }
      ]
    },
    { "id": "narrative", "specialized": false, "weight": 2 },
    {
      "id": "moat",
      "label": "Moat (Mining)",
      "specialized": true,
      "moat_types": ["cost-advantage", "geological-irreplaceability", "capital-discipline", "streaming-model", "regulatory-sovereign"],
      "sub_criteria": ["moat_stack_count", "aisc_quartile", "replacement_cost_ratio", "discipline_track_record"]
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

1. **Physical ETF exclusion (GLD, SLV, IAU, PSLV)** — currently out of scope (§1). Sub-sector level only (rotation tracker tags them) but no 8-Q score. *Confirm.* Alternative: simplified 4-pillar score covering monetary regime + intensity + sovereignty + technicals.

2. **Uranium adapter assignment** — current default (§1): score under Energy-Power, not Mining. *Confirm.*

3. **Sub-type granularity (9 proposed)** — most extensive of any adapter so far. Possible collapses: `gold-tier1` + `gold-midtier` into single `gold` sub-type; `nickel` is currently un-held and could be deferred. *Recommendation:* keep 9 to handle future watchlist additions cleanly without re-architecting.

4. **AEM worked example: 12/16 — gut check?** Sits between RR.L (11) and LLY (13). Honest reflection of gold's defensive-monetary regime vs exceptional step-change setups? Or should gold miners score higher given the central bank step-change?

5. **ALB worked example: 10-11/16 — gut check?** Sits at the margin. Honest reflection of lithium's broken-cycle regime? Or too punitive given the geological scarcity is real?

6. **Streamers (WPM, FNV) sub-type treatment** — `streaming` sub-type gets reserve_life weight=1 (not 2), AISC weight=0, grade weight=0 because the model is different. The capital-light optionality is captured in `capital_discipline` weight=2. *Confirm this weighting reflects the streaming model honestly.*

7. **Should physical-ETF holdings (GLD, SLV) get a separate "Asset Hedge" simplified scorecard?** This is the question the original Philosophy doc deferred. Decision needed.

---

## 8. Next Steps

- User reviews this adapter, confirms open decisions in §7
- Continues batch session: Industrial Electrical Equipment → AI-Infra → Cloud-Infra

---

## 9. Version History

| Version | Date | Notes |
|---|---|---|
| v1 draft | 2026-05-17 | Initial draft with 7 open items |

---

*Draft v1. Pending user review. Personal use only. Not investment advice.*
