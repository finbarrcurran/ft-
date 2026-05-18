# Sector Adapter — Industrial Electrical Equipment — v1 (Draft)

> **Status:** Sixth adapter built under the Cross-Sector Investment Philosophy v1.1. Covers diversified industrial-electrical companies — UPS, switchgear, building automation, electrification, industrial automation, and grid-edge equipment. Gives SU.PA (Schneider Electric) its proper home (replacing the interim Grid & transmission tag from Holdings Mapping v1.1).
> **Doctrine source:** `Cross_Sector_Investment_Philosophy_v1.1.md` §6
> **Not investment advice. Personal use only.**

---

## 1. Scope

### In-scope (this adapter covers)
- **Diversified industrial-electrical:** SU.PA (Schneider), ABB, SIE.DE (Siemens AG ex-energy), ETN (Eaton — note overlap)
- **Power-management / UPS / switchgear:** ETN, Vertiv (VRT), Legrand (LR.PA)
- **Building automation & smart buildings:** JCI, HON (segment), Trane (TT)
- **Industrial automation / motion:** ROK, ABB segment, FANUY (overlap with robotics)
- **Electrical components / connectors:** APH (Amphenol), TEL (TE Connectivity)
- **Electrification specialists:** nVent (NVT), Hubbell (HUBB)

### Out-of-scope (different adapter)
- **Pure-play grid utilities** (regulated power utilities) → if owned, score under Energy-Power generation sub-types
- **Pure-play industrial robotics** (FANUY robotics segment, ISRG-style) → future Robotics & Automation adapter (TBD)
- **HVAC equipment pure-plays** (Carrier — CARR, the air-conditioning side) → arguable inclusion; current default is *exclude* unless data-center cooling >30% of revenue
- **Heavy industrials / machinery** (CAT, DE, ETN's hydraulics) → future Heavy Industrial adapter (TBD)

### Cross-reference with Energy-Power
SU.PA, ABB, SIE.DE, ETN are *suppliers* of equipment that goes into power generation, T&D, and data centers. The Adapter Assignment Principle (Philosophy v1.1 §6.1) applies: score under whichever adapter materially drives earnings.

For SU.PA specifically: data-center & buildings (~50% of revenue) + industrial automation (~30%) + energy management (~20%). Earnings are driven by **industrial electrification + data-center capex**, not by power generation per se. Hence: scored under *this* adapter, not Energy-Power.

For ETN: ~40% electrical Americas (data-center, utility, industrial), ~20% electrical global, balance in hydraulics + aerospace + vehicle. Could fit either Industrial-Electrical or Energy-Power. **Recommendation:** score under this adapter; revisit if electrical segment becomes overwhelmingly data-center-driven.

### The rotation thesis

Industrial electrical re-rates on forcing functions that overlap with — but differ from — Energy-Power:

| Energy-Power thesis | **Industrial-Electrical thesis** |
|---|---|
| Generation capacity demand step-change | **Electrification of everything (data centers, EVs, heat pumps, industry) drives end-equipment demand** |
| Customer = hyperscaler PPAs | **Customer = mixed: hyperscalers, industrial, utilities, building owners** |
| Cycle driven by power scarcity | **Cycle driven by capex super-cycle in electrification** |
| Lock-in via PPAs | **Lock-in via installed base + service contracts + specifications** |

The binding constraint in *this* cycle is **specification-write + installed-base service revenue + supply-chain control**. The companies that get *specified into* hyperscaler / industrial / EV plant designs lock in 15-25 years of equipment + service revenue. Those that have global service networks and supply-chain depth re-rate.

### Worked example: how SU.PA gets scored

- **SU.PA** → `diversified-industrial-electrical` sub-type. Primary thesis = data-center & industrial electrification capex super-cycle.
- This replaces the interim `Grid & transmission` tag in Holdings Mapping v1.1.

---

## 2. Sub-types within this adapter

| Sub-type | Code | Notes |
|---|---|---|
| Diversified industrial-electrical | `diversified-ie` | Multi-segment giants (SU.PA, ABB, SIE.DE, ETN) |
| Power management / UPS / switchgear | `power-management` | Vertiv-style data-center-power-equipment focus |
| Building automation | `building-automation` | JCI, Trane, Honeywell (segments) |
| Industrial automation / motion | `industrial-automation` | ROK, ABB automation segment |
| Electrical components / connectors | `components` | APH, TEL — pick-and-shovel for everything electrical |
| Electrification specialists | `electrification` | NVT, HUBB — narrower segment focus |

---

## 3. The Eight-Question Adapter

Score each pillar **0 / 1 / 2**. Total out of 16. Strong-pass = **6+/8 pillars ≥1, with Q1 and Q3 ≥1.**

### Q1 — Bottleneck *(sector-specialized)*

> *Is the name sitting at a binding scarcity in industrial-electrical — specification position, supply-chain control, service-network density, or scarce engineering capacity?*

Sub-criteria:

| Sub-criterion | 0 | 1 | 2 |
|---|---|---|---|
| **Specification position** | Generic / unspecified | Specified into some designs | "Default-specified" by major hyperscalers / EPCs / industrials |
| **Installed base / service revenue** | Low recurring | Mixed | Large installed base driving high-margin service annuity |
| **Supply-chain control on critical inputs** | Dependent on third-party constrained inputs | Adequate sourcing | Vertically integrated or scarce-input long-term contracts |
| **Engineering / FE capacity** | Engineering-light commoditized | Mixed | Scarce field-engineering bandwidth = competitive moat |
| **Geographic service footprint** | Single-region | Multi-region | Global, dense local service density |
| **Backlog vs revenue** | <0.5x annual revenue | 0.5-1.5x | >1.5x backlog/revenue |

### Q2 — Narrative *(universal)*

Industrial-electrical narrative arbitrage patterns:
- **"Industrial cyclical"** when actually riding multi-year electrification capex super-cycle.
- **"Boring multi-industrial"** when actually a data-center-power-equipment supplier with hyperscaler exposure that the market hasn't fully repriced.
- **"European industrial = slow growth"** when actually European industrial-electrical leaders are the global leaders on energy management.
- **"Software-defined disruption"** when actually the hardware spec-position is the moat, not the software layer.
- **"Pure-play UPS is the only data-center play"** when actually diversified players with full electrical-room offerings win larger share.

Score: 0 = consensus correct, 1 = partial mispricing, 2 = clear narrative arbitrage.

### Q3 — Moat *(sector-specialized)*

Industrial-electrical moats:

| Moat type | What it looks like in industrial-electrical |
|---|---|
| **Specification-write** | Defaulted into hyperscaler reference designs, EPC standards, building codes |
| **Switching costs** | Installed base lock-in over 20-40 year equipment lives, spare parts dependency, engineer training |
| **Efficient scale** | Global service networks that smaller players can't replicate |
| **Intangibles** | Brand trust with industrial buyers ("Schneider/ABB/Eaton all bid this job") |
| **Scale** | R&D budgets and product portfolio breadth |

Sub-criteria:
- Number of moat types stacked
- Installed base service-annuity margin %
- Specification position with top-10 hyperscalers / EPCs / industrial customers
- Switching evidence (revenue retention rates)

### Q4 — Intensity *(universal)*

Industrial-electrical intensity:
- Data-center MW build trajectory × electrical content per MW (the GPU-to-rack-to-room intensity flowing into electrical equipment)
- EV plant capex × electrical content per plant
- Heat pump installation rates × control equipment intensity
- Industrial reshoring × greenfield electrical content
- Grid-edge digitalization spending

Score: 0 = flat, 1 = growing, 2 = step-change visible.

### Q5 — Visibility *(universal)*

Industrial-electrical visibility:
- Multi-year project backlog with hyperscaler / industrial customer anchors
- Service contracts on installed base
- Frame agreements / global supplier contracts
- Long-cycle EPC project pipeline

Score: 0 = book-and-bill cyclical, 1 = mixed, 2 = multi-year backlog with high installed-base service ratio.

### Q6 — Sovereignty *(universal)*

Industrial-electrical sovereignty:
- Domicile + manufacturing footprint vs customer location (European leaders win European projects; US-domiciled wins on-shoring)
- Critical-infrastructure designation
- China revenue exposure (negative under current regime)
- Friend-shore manufacturing capacity

Score: 0 = exposed to hostile-jurisdiction revenue concentration, 1 = neutral, 2 = explicit sovereign tailwind.

### Q7 — Catalyst *(universal)*

Industrial-electrical catalysts:
- Hyperscaler quarterly capex guidance
- Major project announcements (gigafactories, AI-DC campuses)
- Quarterly orderbook updates
- Margin expansion announcements (service mix shifts)
- M&A in the supply chain
- Major customer reference wins (case studies)

Score: 0 = none visible, 1 = within 12 months, 2 = within 90 days.

### Q8 — Technicals & Risk *(universal)*

Same as base framework.

---

## 4. Worked Example — SU.PA (Schneider Electric)

Illustrative scoring. **Not a recommendation.**

| Pillar | Sub-criteria notes (`diversified-ie`) | Score |
|---|---|---|
| Q1 Bottleneck | Default-specified by major hyperscalers + EPCs (2); huge installed base driving multi-decade service annuity (2); supply chain control adequate, semiconductor inputs occasionally constrained (1); global FE capacity is a real moat (2); ~100 countries service density (2); 1.5x+ backlog/revenue range (2) | **2** |
| Q2 Narrative | Market increasingly pricing "data-center play" but European-discount partly intact; partial arbitrage remains | **1** |
| Q3 Moat | Spec-write + global service network + brand trust + scale = 4 moats stack | **2** |
| Q4 Intensity | Data-center MW step-change + EV plant electrical content step-change + heat pump electrification | **2** |
| Q5 Visibility | Multi-year backlog + service annuity from installed base | **2** |
| Q6 Sovereignty | French-domiciled, global manufacturing footprint including US (IRA-friendly), some China exposure but managed | **1** |
| Q7 Catalyst | Quarterly orderbook, hyperscaler capex announcements, ongoing margin guidance | **1** |
| Q8 Technicals | Tracked broader European industrials uptrend; check exhaustion/regime | **1** |
| **Total** | | **12 / 16** |

**Interpretation:** Strong pass. Scores similarly to AEM but for entirely different reasons — SU.PA's strength is *spec-write moat + multi-decade installed-base annuity*, AEM's is *cost-advantage + jurisdiction premium*. The framework is correctly identifying both as high-quality multi-moat names without forcing artificial comparability.

**Note:** This replaces the interim Grid & transmission classification in Holdings Mapping v1.1. SU.PA should be re-tagged to `industrial-electrical:diversified-ie` once this adapter is locked.

---

## 5. (No second worked example — only one current holding fits this adapter)

Spec discipline: don't manufacture worked examples for names you don't intend to score. If/when ETN, ABB, VRT, or SIE.DE enter the watchlist, score them then.

---

## 6. Schema Sketch

```json
{
  "id": "industrial-electrical",
  "name": "Industrial Electrical Equipment",
  "applies_to": "stock",
  "version": "1.0",
  "scope": "Diversified industrial-electrical companies — UPS, switchgear, building automation, electrification, industrial automation, components.",
  "sub_types": ["diversified-ie", "power-management", "building-automation", "industrial-automation", "components", "electrification"],
  "questions": [
    {
      "id": "bottleneck",
      "label": "Bottleneck (Industrial-Electrical)",
      "specialized": true,
      "sub_criteria": [
        { "id": "specification_position", "weight_by_subtype": {"diversified-ie": 2, "power-management": 2, "building-automation": 2, "industrial-automation": 2, "components": 1, "electrification": 2} },
        { "id": "installed_base_service", "weight_by_subtype": {"diversified-ie": 2, "power-management": 2, "building-automation": 2, "industrial-automation": 2, "components": 1, "electrification": 1} },
        { "id": "supply_chain_control",   "weight_by_subtype": {"diversified-ie": 1, "power-management": 2, "building-automation": 1, "industrial-automation": 1, "components": 2, "electrification": 1} },
        { "id": "engineering_capacity",   "weight_by_subtype": {"diversified-ie": 2, "power-management": 1, "building-automation": 1, "industrial-automation": 2, "components": 0, "electrification": 1} },
        { "id": "geographic_footprint",   "weight_by_subtype": {"diversified-ie": 2, "power-management": 1, "building-automation": 2, "industrial-automation": 1, "components": 1, "electrification": 1} },
        { "id": "backlog_ratio",          "weight_by_subtype": {"diversified-ie": 2, "power-management": 2, "building-automation": 1, "industrial-automation": 1, "components": 1, "electrification": 1} }
      ]
    },
    { "id": "narrative", "specialized": false, "weight": 2 },
    {
      "id": "moat",
      "label": "Moat (Industrial-Electrical)",
      "specialized": true,
      "moat_types": ["specification-write", "switching", "efficient-scale", "intangibles", "scale"],
      "sub_criteria": ["moat_stack_count", "service_annuity_margin", "top_customer_specification", "switching_evidence"]
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

1. **SU.PA re-tag from interim Grid & transmission → industrial-electrical:diversified-ie** once this adapter is locked. *Confirm.* Update Holdings Mapping to v1.2.

2. **ETN classification** — currently proposed under this adapter (`diversified-ie`). Alternative: split or assign to Energy-Power. *Recommendation:* keep under Industrial-Electrical; revisit if ETN's electrical-Americas segment becomes overwhelmingly data-center-driven.

3. **Robotics/automation overlap** — `industrial-automation` sub-type includes FANUY robotics segment. Future Robotics adapter (TBD) may better serve pure-play robotics. *Confirm interim handling.*

4. **HVAC pure-plays** (CARR) — currently excluded. Argument for inclusion: increasing data-center cooling exposure. Argument against: still majority commercial/residential HVAC. *Default: exclude unless data-center cooling >30% of revenue.* Confirm.

5. **Sub-type granularity (6 proposed)** — sensible? Or collapse `electrification` into `diversified-ie`? *Recommendation:* keep 6; electrification specialists (NVT, HUBB) have meaningfully different scale profiles.

6. **SU.PA worked example: 12/16 — gut check?** Honest reflection or too high/low?

---

## 8. Next Steps

- User reviews this adapter, confirms open decisions in §7
- Continues batch session: AI-Infra / Semiconductor → Cloud-Infra

---

## 9. Version History

| Version | Date | Notes |
|---|---|---|
| v1 draft | 2026-05-17 | Initial draft with 6 open items |

---

*Draft v1. Pending user review. Personal use only. Not investment advice.*
