# Sector Adapter — Energy (Power Infrastructure) — v1 (Draft)

> **Status:** First adapter built under the Cross-Sector Investment Philosophy (v1). Scope-specialized to **electrical power infrastructure for the AI / industrial buildout** — Jordi's current "next rotation" thesis.
> **Not investment advice. Personal use only.**

---

## 1. Scope

### In-scope (this adapter covers)
- **Generation — dispatchable:** gas turbines, nuclear (large + SMR), hydro
- **Generation — intermittent:** solar, wind (only where paired with storage/firming)
- **Generation — distributed / behind-the-meter:** fuel cells, microgrids, on-site gas
- **Transmission & Distribution:** grid equipment, interconnect, transformers, switchgear
- **Storage:** BESS (battery), pumped hydro, long-duration
- **Fuel cycle adjacent:** uranium mining/conversion/enrichment, LNG infrastructure *only where directly tied to data-center / industrial power demand*

### Out-of-scope (different adapter)
- **Oil & Gas upstream (E&P):** integrated oil, refining, midstream not tied to AI/industrial power → separate **Hydrocarbons adapter** (TBD). Reason: different rotation thesis (energy security, LNG export cycle), different cost-curve dynamics, different moat structure (reserves vs technology).
- **Pure electrical equipment** (Schneider, ABB) → may sit under **Industrials / Electrical Equipment adapter** depending on portfolio decision. Open question — see §7.

### Worked example: how RR.L splits
- Gas turbines + nuclear / SMR work → **this adapter**
- Civil aero engines → **Industrials / Aerospace adapter** (TBD)
- Defense propulsion → **Defense adapter** (TBD)
- Net thesis = weighted by revenue mix. Don't pretend RR.L is a pure-play.

---

## 2. Sub-types within this adapter

Each candidate is tagged with **one or more** of these. Sub-type affects which Q1 sub-criteria are weighted heaviest:

| Sub-type | Code | Q1 emphasis |
|---|---|---|
| Generation — dispatchable | `gen-disp` | Dispatchability, fuel-cycle security, LCOE |
| Generation — intermittent | `gen-int` | Firming / storage pairing, LCOE, interconnect |
| Generation — distributed | `gen-dist` | Behind-the-meter access, deployment speed, customer pipeline |
| Transmission & Distribution | `td` | Backlog, interconnect queue position, regulated returns |
| Storage | `storage` | Duration, cycle life, contracted offtake |
| Fuel cycle | `fuel` | Reserve life, geopolitical jurisdiction, conversion/enrichment capacity |

---

## 3. The Eight-Question Adapter

Score each pillar **0 / 1 / 2** (consistent with FT Build Spec v2). Total out of 16. Strong-pass = **6+/8 pillars at score ≥1, with Q1 and Q3 ≥1**.

### Q1 — Bottleneck *(sector-specialized)*

> *Is the name sitting at the binding electrical-power constraint of the AI / industrial buildout?*

Sub-criteria (score 0/1/2 each, average → pillar score):

| Sub-criterion | 0 | 1 | 2 |
|---|---|---|---|
| **Dispatchability** | Intermittent, no storage | Intermittent + storage, or partial firm | Fully dispatchable / firm baseload |
| **LCOE / cost-curve position** | Top quartile (high-cost) | Mid-cost | Bottom-quartile (low-cost) |
| **Interconnect / PPA backlog** | None / multi-year wait | Some secured | Multi-year contracted backlog |
| **Permitting status** | Pre-permit, years away | Permitted, pre-construction | Operating or under construction |
| **Deployment speed** | 5+ years to MW | 2-5 years | <2 years / shipping now |
| **Fuel-cycle security** | Single-source / hostile jurisdiction | Diversified but exposed | Fuel-independent or secured stack |

Pillar score = round(avg of applicable sub-criteria, weighted by sub-type emphasis).

### Q2 — Narrative *(universal — unchanged)*

> *Mispriced in the wrong bucket?*

Energy-specific narrative arbitrage patterns to look for:
- **"Legacy fossil" mispriced as terminal-decline** when actually re-rating on AI/industrial power demand (e.g., gas turbine OEMs).
- **"Speculative SMR" mispriced as pre-revenue tech** when actually pre-contracted with hyperscalers (e.g., Oklo, NuScale-style).
- **"Boring utility" mispriced as low-growth bond proxy** when actually data-center-load growth re-rates earnings (e.g., regulated electrics with hyperscaler campuses).
- **"Capex-heavy industrial" mispriced as cyclical** when actually contracted-revenue infrastructure (BESS developers with PPAs).

Score: 0 = consensus correct, 1 = partial mispricing, 2 = clear narrative arbitrage.

### Q3 — Moat *(sector-specialized)*

> *Which moat type is operative, and how durable?*

Energy moats are usually one of (Dorsey / Morningstar taxonomy):

| Moat type | What it looks like in energy |
|---|---|
| **Cost advantage** | Bottom-quartile LCOE (best gas turbine efficiency, lowest AISC uranium, best wind/solar resource) |
| **Efficient scale** | Geography-locked: only viable transmission corridor, only permitted site in a load pocket |
| **Regulatory** | Rate-regulated returns, ITAR-like nuclear licensing barriers, grandfathered permits |
| **Switching costs** | Long-duration PPAs (10-25 yr), integrated grid infrastructure, customer fuel-supply lock-in |
| **Intangibles / IP** | Proprietary tech: advanced SMR designs, high-efficiency turbines, novel storage chemistry |

Sub-criteria:
- Market structure (oligopoly / fragmented)
- Customer lock-in duration (years)
- Capital-intensity barrier (replacement cost)
- IP / regulatory protection

Pillar score = 2 if multiple moat types stack (e.g., cost + regulatory); 1 if single moat; 0 if commodity-like.

### Q4 — Intensity *(universal)*

> *Is power demand-per-unit-of-output rising in the customer base?*

Energy-specific intensity signals:
- Data-center power demand growth (kW/rack rising, rack density rising, hyperscaler campus MW figures)
- AI training cluster MW ramps (10MW → 100MW → 1GW campuses)
- Industrial re-shoring electricity load (fab power, EV plant power)
- Heat-pump / electrification load growth

Score: 0 = flat demand, 1 = growing, 2 = step-change visible in customer capex / interconnect queues.

### Q5 — Visibility *(universal)*

> *Take-or-pay or contracted revenue?*

Energy-specific visibility:
- PPA portfolio length and credit quality of counterparties
- Regulated rate-base growth approvals
- Order book / backlog years
- Capacity-market payments (where applicable)

Score: 0 = merchant exposure, 1 = mixed, 2 = >50% revenue contracted >5 years.

### Q6 — Sovereignty *(universal)*

> *Strategic, policy-supported, friend-shored?*

Energy-specific sovereignty:
- Domestic generation policy support (IRA, US nuclear loan guarantees, EU nuclear taxonomy)
- "Made in USA" / friend-shore manufacturing premiums (transformers, turbines, uranium enrichment)
- Critical infrastructure designation
- Energy security trade flows (LNG exports, uranium sourcing away from Russia/China)

Score: 0 = exposed to hostile jurisdictions, 1 = neutral, 2 = explicit sovereign tailwind.

### Q7 — Catalyst *(universal)*

> *Near-term event forcing re-rating?*

Energy-specific catalysts:
- Hyperscaler PPA announcement (Meta, Google, Amazon, Microsoft)
- Regulatory approval (nuclear NRC milestones, interconnect queue advancement)
- Order book step-change (quarterly bookings)
- Capacity market clearing prices
- Major fab/data-center groundbreaking in service territory

Score: 0 = none visible, 1 = within 12mo, 2 = within 90 days.

### Q8 — Technicals & Risk *(universal)*

> *Chart clean, exhaustion / turbulence signals quiet?*

Same as base framework. No energy-specific adjustment.

---

## 4. Worked Example — RR.L (Rolls-Royce)

Illustrative scoring to test the adapter. **Not a recommendation.**

| Pillar | Sub-criteria notes | Score |
|---|---|---|
| Q1 Bottleneck (gas turbine + SMR segments only) | Dispatchable (2), mid LCOE (1), pipeline includes 470MW AI-DC claim but unverified (1), SMR pre-permit (0-1), deployment speed slow for SMR (0-1), UK/US fuel cycle secure (2) | **1** |
| Q2 Narrative | Market still partially anchors on aero-cycle; AI-power optionality real but already partly priced (forecast P/E ~34) | **1** |
| Q3 Moat | Oligopoly turbine market + regulated nuclear licensing = multiple moat types stack | **2** |
| Q4 Intensity | Data-center MW demand step-change real | **2** |
| Q5 Visibility | Long-cycle service contracts + multi-year defense — but power segment still small | **1** |
| Q6 Sovereignty | UK/US ally premium, strategic | **2** |
| Q7 Catalyst | Trading updates + any AI-DC contract confirmation could re-rate | **1** |
| Q8 Technicals | Stretched valuation, recently flagged bullish pattern post-selloff | **1** |
| **Total** | | **11 / 16** |

**Interpretation:** Passes (6+ pillars ≥1, Q1 and Q3 both ≥1). Adapter handles a multi-segment name by scoring only the in-scope revenue segments — clean.

**Honest caveat:** RR.L is partly a defense + civil aero story. The defense adapter (when written) will score the *defense leg* separately. Net portfolio view = weighted average across adapters that apply.

---

## 5. Worked Example — BE (Bloom Energy)

| Pillar | Notes | Score |
|---|---|---|
| Q1 Bottleneck (`gen-dist`) | Dispatchable fuel-cell (2), high LCOE vs grid (0-1), behind-the-meter pipeline real (1-2), deploys fast (2), fuel = nat gas → mostly secure (1-2) | **1-2** |
| Q2 Narrative | Mispriced as "legacy fuel-cell loss-maker" vs "behind-the-meter AI-DC infra" — clear narrative arbitrage candidate | **2** |
| Q3 Moat | Some IP in fuel-cell stack, but competition from gas turbines + others = moderate moat | **1** |
| Q4 Intensity | Hyperscalers explicitly seeking behind-the-meter power — intensity rising | **2** |
| Q5 Visibility | Order book exists but commercial track record patchy | **1** |
| Q6 Sovereignty | US-based, IRA-eligible | **2** |
| Q7 Catalyst | Any AWS/Meta/Oracle behind-the-meter announcement | **2** |
| Q8 Technicals | Volatile small/mid cap — check exhaustion/turbulence | **0-1** |
| **Total** | | **~11-12 / 16** |

**Interpretation:** Passes if technicals confirm. Adapter cleanly captures BE as a *distributed-generation behind-the-meter* play, not as a legacy fuel-cell company.

---

## 6. Schema Sketch (for FT_Spec_9d JSON-ification)

```json
{
  "id": "energy-power",
  "name": "Energy — Power Infrastructure",
  "applies_to": "stock",
  "version": "1.0",
  "scope": "Electrical power generation, T&D, storage, and fuel cycle for AI / industrial buildout",
  "sub_types": ["gen-disp", "gen-int", "gen-dist", "td", "storage", "fuel"],
  "questions": [
    {
      "id": "bottleneck",
      "label": "Bottleneck (Energy)",
      "specialized": true,
      "sub_criteria": [
        { "id": "dispatchability", "weight_by_subtype": {"gen-disp": 2, "gen-int": 2, "gen-dist": 1, "td": 0, "storage": 1, "fuel": 0} },
        { "id": "lcoe", "weight_by_subtype": {"gen-disp": 1, "gen-int": 2, "gen-dist": 1, "td": 0, "storage": 1, "fuel": 0} },
        { "id": "interconnect_ppa", "weight_by_subtype": {"gen-disp": 2, "gen-int": 2, "gen-dist": 1, "td": 2, "storage": 2, "fuel": 0} },
        { "id": "permitting", "weight_by_subtype": {"gen-disp": 2, "gen-int": 1, "gen-dist": 1, "td": 1, "storage": 1, "fuel": 1} },
        { "id": "deployment_speed", "weight_by_subtype": {"gen-disp": 1, "gen-int": 1, "gen-dist": 2, "td": 1, "storage": 2, "fuel": 0} },
        { "id": "fuel_security", "weight_by_subtype": {"gen-disp": 2, "gen-int": 0, "gen-dist": 1, "td": 0, "storage": 0, "fuel": 2} }
      ]
    },
    { "id": "narrative", "specialized": false, "weight": 2 },
    {
      "id": "moat",
      "label": "Moat (Energy)",
      "specialized": true,
      "moat_types": ["cost", "scale", "regulatory", "switching", "intangibles"],
      "sub_criteria": ["market_structure", "lock_in_duration", "capex_barrier", "ip_regulatory"]
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

Before this adapter is locked and Spec_9d is written:

1. **SU.PA (Schneider Electric) classification** — Energy-Power adapter, or Industrials / Electrical Equipment adapter? Argument for Energy: it sells data-center power management (UPS, switchgear). Argument for Industrials: broader portfolio, automation, building tech.
2. **XOM and the Hydrocarbons adapter** — write it next, or defer? If your XOM thesis is *AI-power-adjacent* (LNG for gas turbines), it might bend into this adapter. If it's a separate energy-security trade, keep it separate.
3. **Sub-type granularity** — six sub-types proposed. Too many → friction. Too few → loses signal. Confirm.
4. **Scoring math** — Q1 sub-criteria averaged into one 0/1/2 pillar score, or kept as a vector for richer reporting? Vector is more informative but more UI work.
5. **Worked-example confirmation** — RR.L scored 11/16 above. Does that match your gut? If not, the rubric is off and we should re-calibrate before locking.

---

## 8. Next Steps

- User reviews this adapter, confirms open decisions in §7
- Once locked: write the Hydrocarbons adapter (XOM) **or** move to a different sector (Pharma for LLY/ABBV / Defense for RHM.DE / Mining for the metals stack — your call)
- After 2-3 adapters drafted: write `FT_Spec_9d_Sector_Rotation_and_Adapters.md` as the implementation spec, bringing together the rotation tracker and the adapter system in one place

---

*Draft v1. Pending user review. Personal use only. Not investment advice.*
