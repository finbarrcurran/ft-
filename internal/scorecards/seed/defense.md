# Sector Adapter — Defense — v1 (Draft)

> **Status:** Fourth adapter built under the Cross-Sector Investment Philosophy v1.1. Covers defense primes, munitions, air defense, drones, military propulsion, and defense electronics. Separated by sub-type because programme-of-record positioning and capacity-cycle dynamics differ materially.
> **Doctrine source:** `Cross_Sector_Investment_Philosophy_v1.1.md` §6
> **Not investment advice. Personal use only.**

---

## 1. Scope

### In-scope (this adapter covers)
- **Primes — diversified:** LMT, NOC, RTX, GD, BAE.L, AIR.PA (defense leg)
- **Primes — European sovereign-rearm:** RHM.DE, HO.PA (Thales), LDO.MI (Leonardo), SAAB-B.ST
- **Munitions / ammunition / explosives:** RHM.DE (also fits prime), AMMO, Hanwha
- **Air defense / missile defense:** RTX (Raytheon segment), LDO.MI, MBDA-related
- **Drones / unmanned systems:** KTOS, AVAV
- **Military propulsion / aero engines:** RR.L (defense leg), GE Aerospace (military)
- **Defense electronics / C4ISR:** HII (mission-systems segment), L3Harris
- **Shipbuilding (naval):** HII, GD (naval segment)

### Out-of-scope (different adapter)
- **Civil aerospace** (Boeing commercial, Airbus commercial) → future Aerospace adapter (TBD)
- **Pure cybersecurity** (PANW, CRWD) → future Cybersecurity adapter (TBD)
- **Dual-use AI / autonomy** (PLTR, AI-defense pure-plays) → ambiguous; tag under AI-Infra if revenue is platform-licensing, under Defense if revenue is government-programme contracts. Default: Defense when government revenue >50%.

### The rotation thesis

Defense re-rates on forcing functions that are distinctly different from AI infra, hydrocarbons, or pharma:

| Other rotation theses | **Defense rotation thesis** |
|---|---|
| Customer = hyperscalers / GDP / payers | **Customer = governments (single-buyer power, sticky)** |
| Cycle driven by demand step-change | **Cycle driven by geopolitical inflection points + procurement supercycles** |
| Lock-in via contracts or patents | **Lock-in via programmes of record + sovereign ITAR-style barriers** |
| Capex cycles are commercial | **Capex cycles are sovereign multi-year commitments (NATO 3%+)** |

The binding constraint in defense in *this* cycle is **production capacity vs sovereign demand**: ammunition, air defense interceptors, drones, and shipbuilding are all capacity-short relative to commitments made by US/EU/NATO governments. Names that own the scarce production lines re-rate.

### Worked example: how this splits across the current holdings

- **RHM.DE** → `prime-european-rearm` sub-type. Primary thesis = European NATO 3%+ commitments + ammunition/air-defense capacity scarcity.
- **RR.L (defense leg)** → `military-propulsion` sub-type. Secondary tag (RR.L's primary is Power per holdings mapping).

---

## 2. Sub-types within this adapter

Each candidate is tagged with **one** sub-type (primary). Sub-type affects which Q1 sub-criteria are weighted heaviest:

| Sub-type | Code | Notes |
|---|---|---|
| Prime — diversified | `prime-diversified` | US/UK primes with cross-domain portfolios (LMT, NOC, BAE) |
| Prime — European sovereign rearm | `prime-european-rearm` | Beneficiaries of NATO 3%+ commitments (RHM.DE, HO.PA, LDO.MI) |
| Munitions / ammunition | `munitions` | Powder, shells, missiles consumables — high-burn, capacity-short |
| Air & missile defense | `air-defense` | Patriot, IRIS-T, SAMP/T, NASAMS systems |
| Drones / unmanned systems | `drones` | Loitering munitions, ISR drones, autonomous systems |
| Military propulsion | `military-propulsion` | Aero engines, naval propulsion (RR.L defense, GE) |
| Defense electronics / C4ISR | `c4isr` | Sensors, comms, electronic warfare |
| Naval shipbuilding | `shipbuilding` | Submarine, surface combatant, carrier builders |

---

## 3. The Eight-Question Adapter

Score each pillar **0 / 1 / 2**. Total out of 16. Strong-pass = **6+/8 pillars at score ≥1, with Q1 and Q3 ≥1.**

### Q1 — Bottleneck *(sector-specialized)*

> *Is the name sitting at a binding scarcity in defense — production capacity, programme-of-record position, sovereign barriers, or strategic input access?*

Sub-criteria (score 0/1/2 each, weighted average → pillar score):

| Sub-criterion | 0 | 1 | 2 |
|---|---|---|---|
| **Programme of record positioning** | Subcontractor only / not on PoR | One major PoR or competitive | Multi-PoR sole-source or lead integrator |
| **Production capacity vs orderbook** | Excess capacity, weak utilization | Adequate | Capacity-constrained, multi-year backlog beyond physical throughput |
| **Backlog duration / years of coverage** | <1 yr book-to-bill | 1-3 yrs | >3 yrs contracted backlog |
| **Government customer concentration / sovereign barriers** | Single-customer dependence with weak ITAR/equivalent | Diversified gov customers | Multi-government + sovereign ITAR-equivalent barriers |
| **Munitions / consumables burn rate** | Not applicable / low | Some recurring consumable revenue | High-burn consumable franchise (ammo, missiles being expended faster than replaced) |
| **Strategic-input control** | Dependent on hostile-jurisdiction inputs | Mixed sourcing | Vertically integrated or friend-shored critical inputs |

Sub-criterion weighting by sub-type emphasis applied in JSON schema (§6).

### Q2 — Narrative *(universal)*

Defense-specific narrative arbitrage patterns:
- **"Peak defense / cycle topping"** when actually mid-supercycle (NATO commitments span 10-year procurement, not 2-year).
- **"Europe can't execute / no orderbook follow-through"** when ammunition contracts are being announced quarterly with multi-year delivery schedules.
- **"ESG-excluded defense"** when ESG funds are quietly reclassifying defense as "permissible" given the geopolitical regime.
- **"Old-economy primes"** when actually scaling munitions / autonomy faster than the market has priced.
- **"Drone disruption replaces primes"** when actually primes are acquiring drone companies and integrating autonomy at scale.

Score: 0 = consensus correct, 1 = partial mispricing, 2 = clear narrative arbitrage.

### Q3 — Moat *(sector-specialized)*

Defense moats:

| Moat type | What it looks like in defense |
|---|---|
| **Regulatory / sovereign** | ITAR / export-control barriers, security clearances, sovereign-only certifications |
| **Switching costs** | Programme-of-record lock-in over 20-40 year platform lives, integration with sovereign architectures |
| **Efficient scale** | Only one or two qualified suppliers per platform (sole-source PoRs) |
| **Intangibles** | Decades of platform-specific IP, classified know-how, qualified production tooling |
| **Customer relationships** | Multi-decade pentagon / MoD / NATO relationships, embedded engineering teams |

Sub-criteria:
- Number of moat types stacked
- Programme-of-record durability (years remaining on platform)
- Replacement cost / sovereign-qualification timeline (years it would take a competitor to qualify)
- Customer relationship depth (revenue concentration with top customers)

Pillar score = 2 if multiple moats stack (e.g., regulatory + sole-source PoR + switching); 1 if single durable moat; 0 if commodity defense supplier.

### Q4 — Intensity *(universal)*

Defense-specific intensity signals:
- NATO % of GDP commitments rising (2% → 3%+)
- Ammunition burn rate vs production rate (Ukraine consumption pattern)
- New conflict zones expanding addressable customer base
- Programme-of-record growth in unit counts (e.g., F-35 production rate)
- Munitions / interceptors expended per conflict-day

Score: 0 = flat / declining defense spend, 1 = stable growth, 2 = step-change visible (post-2022 NATO commitments are step-change for European primes).

### Q5 — Visibility *(universal)*

Defense-specific visibility:
- Multi-year contracted backlog
- Programme-of-record budget line items in approved defense budgets
- Long-cycle service / sustainment contracts attached to platforms
- IDIQ / framework contracts with ceiling values
- Production rate ramp commitments (not just orders but capacity commitments)

Score: 0 = single-year contracts, 1 = mixed, 2 = >3yr contracted backlog with sovereign budget anchoring.

### Q6 — Sovereignty *(universal)*

Defense-specific sovereignty:
- Domicile relative to procurement customer (German prime selling to Germany = max sovereignty premium)
- ITAR / export-control regime (US ITAR, UK Strategic Export, EU equivalents)
- Friend-shore manufacturing footprint
- Critical-infrastructure / strategic-supplier designation
- Sanctions resilience (no exposure to hostile jurisdictions in supply chain)

Score: 0 = exposed to hostile jurisdiction in supply chain, 1 = neutral, 2 = explicit sovereign tailwind (European prime in NATO 3%+ context).

### Q7 — Catalyst *(universal)*

Defense-specific catalysts:
- New programme-of-record award announcement
- Major contract option exercise
- NATO summit commitments (annual)
- Conflict-zone procurement supplemental
- Budget approval / continuing resolution outcomes (US fiscal years)
- M&A in the supply chain (consolidation, divestiture)

Score: 0 = none visible, 1 = within 12 months, 2 = within 90 days.

### Q8 — Technicals & Risk *(universal)*

Same as base framework. **Defense-specific idiosyncratic risk:** geopolitical de-escalation can compress multiples even with backlog intact (the "ceasefire risk" — markets price defense names off perceived threat level as much as actual orders). Score captures *price-action* risk; doesn't capture binary geopolitical regime shifts.

---

## 4. Worked Example — RHM.DE (Rheinmetall)

Illustrative scoring. **Not a recommendation.**

| Pillar | Sub-criteria notes (`prime-european-rearm`) | Score |
|---|---|---|
| Q1 Bottleneck | Multi-PoR lead integrator on European ammunition + Lynx IFV + Leopard upgrades (2); capacity-constrained — building new powder plants (2); >3yr backlog (~€55B order book scale) (2); diversified European customer base + sovereign barriers (2); ammo burn rate central — Ukraine consumption + NATO stockpile rebuild (2); German + diversified European industrial base (2) | **2** |
| Q2 Narrative | Market has priced rearm, but Europe-execution skepticism creates partial arbitrage on actual capacity delivery (capacity expansions ahead of consensus) | **1** |
| Q3 Moat | Sovereign-qualified ammo production + 20-yr Leopard tail + customer-relationship depth = multiple moats stack | **2** |
| Q4 Intensity | NATO 3%+ step-change + ammunition burn step-change + IFV/tank programme expansions | **2** |
| Q5 Visibility | Multi-year backlog with sovereign budget anchoring (German/EU rearm budgets approved in legislation) | **2** |
| Q6 Sovereignty | German-domiciled, EU rearm tailwind, ITAR-equivalent barriers, friend-shore production | **2** |
| Q7 Catalyst | Quarterly orderbook updates, NATO summit, German budget cycles — multiple within 12 months | **1** |
| Q8 Technicals | Stretched valuation post 2022-25 re-rating; check exhaustion / regime quiet carefully | **1** |
| **Total** | | **14 / 16** |

**Interpretation:** Strong pass (6+ pillars ≥1, Q1 and Q3 both 2). Highest worked example across adapters so far — reflects that RHM.DE genuinely sits at the binding scarcity of European sovereign rearm in an exceptionally favorable regime. Comparable LLY scored 13/16; RHM.DE edging higher reflects the multi-moat sovereign-anchored backlog vs LLY's single-franchise concentration.

**Honest caveat:** Q1 scored 2 across nearly all sub-criteria. Risk: any sub-criterion turning (capacity catches up to backlog, ceasefire reduces ammo burn, ESG funds re-exclude) compresses score materially. Worth re-scoring quarterly. Q8 technicals at 1 captures stretched-valuation risk that's real.

---

## 5. Worked Example — RR.L defense leg (`military-propulsion`)

Scored as a *segment*, not the whole company (per holdings mapping doc: RR.L primary = Power; defense is secondary).

| Pillar | Sub-criteria notes (`military-propulsion`) | Score |
|---|---|---|
| Q1 Bottleneck | Sole-source on Eurofighter Typhoon, Tornado, F-35 lift-fan (2); capacity adequate but not constrained (1); long programme tails (2); UK MoD + Eurofighter consortium + US JSF programme (2); not ammo-burn driven (0); UK industrial base secure (2) | **2** |
| Q2 Narrative | Defense leg under-discussed vs power leg in current narrative; partial arbitrage | **1** |
| Q3 Moat | ITAR + sole-source PoR + multi-decade platform tails + customer-relationship depth = multiple moats | **2** |
| Q4 Intensity | NATO 3%+ benefits propulsion via accelerated platform refresh + AUKUS submarine propulsion expansion | **1** |
| Q5 Visibility | Long-cycle service contracts + multi-decade platform tails | **2** |
| Q6 Sovereignty | UK-domiciled, AUKUS partner, ITAR-equivalent | **2** |
| Q7 Catalyst | AUKUS milestones, Tempest programme, trading updates | **1** |
| Q8 Technicals | Per primary RR.L scoring (1) | **1** |
| **Total** | | **12 / 16** |

**Interpretation:** Passes (6+ pillars ≥1, Q1 and Q3 both 2). The defense leg, scored standalone, is a genuinely strong setup but not as concentrated-bottleneck as RHM.DE. RR.L's *combined* scoring across Power + Defense adapters likely sits in a 11-13 range depending on segment weighting (single-tag handling per Holdings Mapping v1.1).

---

## 6. Schema Sketch

```json
{
  "id": "defense",
  "name": "Defense",
  "applies_to": "stock",
  "version": "1.0",
  "scope": "Defense primes, munitions, air defense, drones, military propulsion, C4ISR, naval shipbuilding",
  "sub_types": ["prime-diversified", "prime-european-rearm", "munitions", "air-defense", "drones", "military-propulsion", "c4isr", "shipbuilding"],
  "questions": [
    {
      "id": "bottleneck",
      "label": "Bottleneck (Defense)",
      "specialized": true,
      "sub_criteria": [
        { "id": "por_positioning",     "weight_by_subtype": {"prime-diversified": 2, "prime-european-rearm": 2, "munitions": 1, "air-defense": 2, "drones": 1, "military-propulsion": 2, "c4isr": 2, "shipbuilding": 2} },
        { "id": "capacity_vs_orderbook","weight_by_subtype": {"prime-diversified": 1, "prime-european-rearm": 2, "munitions": 2, "air-defense": 2, "drones": 1, "military-propulsion": 1, "c4isr": 1, "shipbuilding": 2} },
        { "id": "backlog_duration",    "weight_by_subtype": {"prime-diversified": 2, "prime-european-rearm": 2, "munitions": 2, "air-defense": 2, "drones": 1, "military-propulsion": 2, "c4isr": 2, "shipbuilding": 2} },
        { "id": "customer_sovereignty","weight_by_subtype": {"prime-diversified": 2, "prime-european-rearm": 2, "munitions": 1, "air-defense": 2, "drones": 1, "military-propulsion": 2, "c4isr": 2, "shipbuilding": 2} },
        { "id": "consumables_burn",    "weight_by_subtype": {"prime-diversified": 1, "prime-european-rearm": 2, "munitions": 2, "air-defense": 2, "drones": 1, "military-propulsion": 0, "c4isr": 0, "shipbuilding": 0} },
        { "id": "strategic_inputs",    "weight_by_subtype": {"prime-diversified": 1, "prime-european-rearm": 1, "munitions": 2, "air-defense": 1, "drones": 1, "military-propulsion": 1, "c4isr": 1, "shipbuilding": 1} }
      ]
    },
    { "id": "narrative", "specialized": false, "weight": 2 },
    {
      "id": "moat",
      "label": "Moat (Defense)",
      "specialized": true,
      "moat_types": ["regulatory-sovereign", "switching", "scale", "intangibles", "customer-relationships"],
      "sub_criteria": ["moat_stack_count", "por_durability_years", "replacement_qualification_timeline", "customer_concentration"]
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

1. **Sub-type granularity (8 proposed)** — feels comprehensive but may be too granular. Possible collapses: `drones` into `air-defense`; `c4isr` into `prime-diversified`. *Recommendation:* keep 8. Drones is a genuinely different rotation thesis (autonomy + low-cost mass) vs air defense (high-cost interceptors).

2. **Dual-use AI/autonomy classification** — PLTR-style names blur Defense vs AI-Infra. Current proposal (§1): tag Defense when government revenue >50%, AI-Infra otherwise. *Confirm.*

3. **Civil aerospace exclusion** — Boeing commercial, Airbus commercial, GE Aerospace civil division all excluded. Future Aerospace adapter (TBD). *Confirm.*

4. **RHM.DE worked example: 14/16 — gut check?** Highest score across all worked examples so far. Is RHM.DE *genuinely* a higher-conviction setup than LLY (13/16), RR.L (11/16), or XOM (11/16)? Or is the Defense rubric too lenient?

5. **RR.L defense leg: 12/16 — gut check?** Reasonable for a segment score, but worth confirming. Multi-segment names will produce *multiple* scores per ticker; how do we display this in the future Scoring Engine (Spec 9i)?

6. **Munitions burn rate as Q1 sub-criterion** — controversial because it's not applicable to all sub-types (propulsion, C4ISR, shipbuilding all get weight=0). Sub-type weighting handles this, but reasonable people could argue it belongs as its own pillar. *Recommendation:* keep as Q1 sub-criterion with sub-type weighting.

---

## 8. Next Steps

- User reviews this adapter, confirms open decisions in §7
- Continues batch session: Mining/Metals → Industrial Electrical Equipment → AI-Infra → Cloud-Infra

---

## 9. Version History

| Version | Date | Notes |
|---|---|---|
| v1 draft | 2026-05-17 | Initial draft with 6 open items |

---

*Draft v1. Pending user review. Personal use only. Not investment advice.*
