# Sector Adapter — Cloud-Infrastructure & Hyperscaler Capex — v1 (Draft)

> **Status:** Eighth adapter built under the Cross-Sector Investment Philosophy v1.1. Covers cloud-infrastructure providers and hyperscaler-adjacent companies whose primary thesis is **AI compute demand monetization through infrastructure**, not chips or power. This adapter exists because the OCI/AI-cloud thesis for ORCL fits neither AI-Infra/Semi (no chip exposure) nor Software (the bull thesis is capex-driven infrastructure, not SaaS multiples).
> **Doctrine source:** `Cross_Sector_Investment_Philosophy_v1.1.md` §6
> **Not investment advice. Personal use only.**

---

## 1. Scope

### In-scope (this adapter covers)
- **Hyperscaler cloud platforms — pure-play:** ORCL (OCI), potentially CRWV (CoreWeave), NBIS-style neoclouds
- **Hyperscaler segments of mega-caps:** MSFT Azure, AMZN AWS, GOOGL Cloud — scored as *segments*, not whole companies (similar to RR.L multi-segment handling)
- **Data-center REITs with hyperscaler concentration:** EQIX, DLR
- **AI inference-as-a-service / GPU-cloud:** CRWV, NBIS, RNDR (speculative)
- **Sovereign / regional cloud:** future expansion if any sovereign-cloud names enter watchlist

### Out-of-scope (different adapter)
- **Enterprise SaaS / horizontal software** → future Software adapter (TBD)
- **Pure consumer cloud / streaming infrastructure** (NFLX hosting) → out of personal portfolio scope
- **Semiconductor side of cloud** (NVDA, ASML, etc.) → AI-Infra/Semi adapter
- **Power infrastructure side of cloud** (gas turbines, nuclear, transmission) → Energy-Power adapter
- **Cooling infrastructure side of cloud** (MOD, VRT) → AI-Infra/Semi adapter (handled there as `cooling-thermal`)

### Why this adapter exists separately
ORCL's bull thesis under the user's holdings mapping (v1.1) is **OCI/AI-cloud monetization** — the company has emerged as a credible #4 hyperscaler with multi-year AI training contracts (OpenAI, Meta, others). Scoring this thesis under:
- *AI-Infra/Semi* doesn't fit: ORCL doesn't make chips or own foundry capacity.
- *Software* doesn't fit: the bull thesis is *infrastructure capex monetization*, not SaaS valuation.
- *Industrial-Electrical* doesn't fit: ORCL isn't selling equipment.

This adapter provides the right home: hyperscaler infrastructure-as-a-business.

### The rotation thesis

Cloud-infra/hyperscaler capex re-rates on forcing functions that *partially overlap* but are distinct from AI-Infra/Semi:

| AI-Infra/Semi thesis | **Cloud-Infra thesis** |
|---|---|
| Supply of inputs (chips, memory) | **Supply of compute capacity at scale** |
| Customer = hyperscalers | **Customer = AI labs + enterprises + governments paying for hyperscaler compute** |
| Cycle driven by chip generation cycles | **Cycle driven by hyperscaler RPO (remaining performance obligations) growth + utilisation** |
| Lock-in via design wins / contracted backlog | **Lock-in via multi-year AI training contracts + reserved capacity commitments** |
| Bottleneck = physical inputs | **Bottleneck = capacity-build pace + power access + customer winning** |

The binding constraint for cloud-infra in *this* cycle is **capacity-build pace relative to AI training demand**, gated by **power access** and **chip allocation**. Companies that can build capacity fastest (or have it pre-built) at hyperscale and convert it into contracted RPO re-rate.

### Worked example: how ORCL gets scored

- **ORCL** → `hyperscaler-pure-play` sub-type. Primary thesis = OCI capacity build-out + AI training contract wins.

(This replaces the Software-vs-Data-Center-REITs question from the original Holdings Mapping — ORCL now has its own honest adapter home.)

---

## 2. Sub-types within this adapter

| Sub-type | Code | Notes |
|---|---|---|
| Hyperscaler pure-play | `hyperscaler-pure-play` | ORCL OCI; future CRWV if commercial scale |
| Hyperscaler segment (within mega-cap) | `hyperscaler-segment` | MSFT Azure, AMZN AWS, GOOGL Cloud — segment scoring only |
| Neocloud / GPU-as-a-service | `neocloud` | CRWV, NBIS — speculative AI-training-cloud pure-plays |
| Data-center REIT (hyperscaler-concentrated) | `dc-reit` | EQIX, DLR — landlord-style exposure |
| Sovereign / regional cloud | `sovereign-cloud` | Future — Europe sovereign cloud, Saudi/UAE clouds |

---

## 3. The Eight-Question Adapter

Score each pillar **0 / 1 / 2**. Total out of 16. Strong-pass = **6+/8 pillars ≥1, with Q1 and Q3 ≥1.**

### Q1 — Bottleneck *(sector-specialized)*

> *Is the name sitting at a binding scarcity in cloud-infra — capacity-build pace, power access, chip allocation, customer winning, or contracted RPO depth?*

Sub-criteria:

| Sub-criterion | 0 | 1 | 2 |
|---|---|---|---|
| **Capacity-build pace vs demand** | Slow build, missing demand window | Adequate | Aggressive capacity scaling, multi-region, capturing market share |
| **Power access secured** | Constrained by grid / PPA gaps | Adequate | Multi-GW pipeline of secured PPAs + behind-the-meter |
| **Chip allocation** | Limited or unfavored allocation | Adequate hyperscaler customer status with chip suppliers | Top-tier allocation from NVDA / AMD / hyperscaler custom silicon |
| **Contracted RPO depth (years of committed revenue)** | <1 yr forward visibility | 1-3 yrs | >3 yrs of contracted RPO from anchor customers |
| **Anchor-customer wins (AI labs, frontier models)** | None | Some enterprise | Multiple frontier-AI customers (OpenAI, Anthropic, Meta-class) |
| **Differentiation vs AWS/Azure/GCP** | Pure commodity capacity | Some differentiation (price, region, performance) | Genuine technical / commercial differentiation (e.g., ORCL networking, dedicated regions) |

### Q2 — Narrative *(universal)*

Cloud-infra narrative arbitrage patterns:
- **"ORCL is legacy database"** when actually OCI revenue + RPO trajectory is reshaping the company's earnings mix (the classic mid-2026 ORCL re-rate setup).
- **"Three-hyperscaler market"** when actually the AI training cloud market is structurally a four-or-more-player market with capacity scarcity.
- **"Neoclouds are unprofitable AI-bubble plays"** when actually they're building the bridge while hyperscalers' own capacity catches up.
- **"Data-center REITs are bond proxies"** when actually they're now growth proxies for AI capex.
- **"Hyperscaler segments don't deserve sum-of-parts re-rating inside mega-caps"** when actually they do (Azure-as-segment, AWS-as-segment).

Score: 0 = consensus correct, 1 = partial mispricing, 2 = clear narrative arbitrage.

### Q3 — Moat *(sector-specialized)*

Cloud-infra moats:

| Moat type | What it looks like in cloud-infra |
|---|---|
| **Switching costs** | Migration cost (data egress fees, app re-architecting), enterprise lock-in, multi-year contracts |
| **Efficient scale** | Multi-region footprint that smaller competitors cannot replicate; infrastructure capex levels |
| **Customer relationships** | Decades of enterprise relationships (ORCL's database installed base feeding OCI cross-sell) |
| **Technical differentiation** | Bare-metal performance (ORCL), networking architecture, specific workload optimization |
| **Capital access** | Ability to finance multi-billion capex cycles — eliminates undercapitalized competitors |

Sub-criteria:
- Number of moat types stacked
- Customer cross-sell evidence (e.g., ORCL DB customers adopting OCI)
- Multi-region coverage breadth
- RPO retention rate

### Q4 — Intensity *(universal)*

Cloud-infra intensity:
- AI training compute demand growth (FLOP/$ requirements rising)
- Enterprise cloud migration rate (still well under 50% globally)
- GenAI workload adoption per enterprise account
- Inference compute growth (often underestimated vs training)
- Sovereign / regulated workload migration to dedicated regions

Score: 0 = flat / declining, 1 = stable growth, 2 = step-change (AI training compute demand is step-change).

### Q5 — Visibility *(universal)*

Cloud-infra visibility:
- Contracted RPO disclosure (the headline metric — ORCL's RPO step-changes were the 2025 bull thesis)
- Multi-year customer contracts with frontier AI labs
- Capacity commitment disclosures (multi-year capex guidance)
- Customer concentration risk (anchor customer dependence cuts both ways)

Score: 0 = book-and-bill exposure, 1 = mixed, 2 = >3yr RPO + diversified anchor customer base.

### Q6 — Sovereignty *(universal)*

Cloud-infra sovereignty:
- Domicile + data residency offerings (sovereign cloud is increasingly mandatory)
- Regulatory positioning (CHIPS-act-adjacent, EU AI Act compliance, data residency regimes)
- Friend-shore data-center footprint
- Government customer exposure (positive when defense/intel; negative if hostile-jurisdiction)

Score: 0 = exposed to hostile-jurisdiction data flows, 1 = neutral, 2 = explicit sovereign tailwind + sovereign-cloud product offering.

### Q7 — Catalyst *(universal)*

Cloud-infra catalysts:
- Quarterly RPO disclosure (especially step-changes)
- Major customer contract announcement (frontier AI lab wins)
- Capex guidance updates (multi-year reframings)
- Margin expansion guidance (utilisation-driven)
- New region launches with anchor customer commitment
- Partnership announcements with chip suppliers (preferred allocation arrangements)

Score: 0 = none visible, 1 = within 12 months, 2 = within 90 days.

### Q8 — Technicals & Risk *(universal)*

Same as base framework. **Cloud-infra-specific idiosyncratic risk:** stocks in this sector trade with high beta to AI capex sentiment AND hyperscaler-segment-disclosure quality. Single quarterly RPO miss can produce large drawdowns. Sizing discipline matters.

---

## 4. Worked Example — ORCL (Oracle)

| Pillar | Sub-criteria notes (`hyperscaler-pure-play`) | Score |
|---|---|---|
| Q1 Bottleneck | Aggressive multi-region capacity build (2); secured multi-GW power pipeline (1-2 — still building); good NVDA allocation status (1-2); RPO step-change disclosed (>3 yrs visibility on key contracts) (2); multiple frontier-AI customers (2); differentiated networking + DB integration (2) | **2** |
| Q2 Narrative | Market still partially anchored on "legacy database vendor"; OCI re-rating in progress but partial arbitrage remains | **2** |
| Q3 Moat | Switching costs (DB installed base) + customer cross-sell + technical differentiation + capital access = multiple moats stack | **2** |
| Q4 Intensity | AI training compute demand step-change + enterprise GenAI adoption | **2** |
| Q5 Visibility | Disclosed RPO trajectory + multi-year frontier-AI contracts | **2** |
| Q6 Sovereignty | US-domiciled, growing sovereign-cloud offerings | **1** |
| Q7 Catalyst | Quarterly RPO disclosure + ongoing contract announcements | **2** |
| Q8 Technicals | Volatile post 2025 re-rating; check exhaustion / regime | **0-1** |
| **Total** | | **13-14 / 16** |

**Interpretation:** Strong pass — bordering on the highest tier (RHM.DE territory). Adapter correctly identifies ORCL as a *binding-scarcity cloud-infra play with multi-moat structure in step-change demand regime*. This validates the user's decision (in the refresh session) to retag ORCL from Software → Data-center REITs / cloud-infra. The thesis under the new tag scores meaningfully higher than it would under either Software (would score 8-10 on cyclical SaaS metrics) or AI-Infra/Semi (would score poorly on chip-bottleneck criteria).

**Honest caveat:** Q1, Q2, Q3, Q4, Q5, Q7 all scoring 2 means any single sub-criterion turning compresses the score materially. The most vulnerable sub-criteria: power-access (could become constrained), customer concentration (anchor frontier-AI customers could renegotiate), valuation reversion (Q8 captures partially). Worth re-scoring quarterly with each RPO disclosure.

---

## 5. (No second worked example — only one current holding fits this adapter)

Same spec discipline as Industrial-Electrical: no manufactured examples for un-held names. When CRWV, NBIS, EQIX, or hyperscaler-segment scoring enters the watchlist, score them then.

---

## 6. Schema Sketch

```json
{
  "id": "cloud-infra",
  "name": "Cloud-Infrastructure & Hyperscaler Capex",
  "applies_to": "stock",
  "version": "1.0",
  "scope": "Hyperscaler cloud platforms, data-center REITs with hyperscaler concentration, neoclouds. Excludes enterprise SaaS, semiconductor side, power side.",
  "sub_types": ["hyperscaler-pure-play", "hyperscaler-segment", "neocloud", "dc-reit", "sovereign-cloud"],
  "questions": [
    {
      "id": "bottleneck",
      "label": "Bottleneck (Cloud-Infra)",
      "specialized": true,
      "sub_criteria": [
        { "id": "capacity_build_pace",   "weight_by_subtype": {"hyperscaler-pure-play": 2, "hyperscaler-segment": 2, "neocloud": 2, "dc-reit": 2, "sovereign-cloud": 1} },
        { "id": "power_access",          "weight_by_subtype": {"hyperscaler-pure-play": 2, "hyperscaler-segment": 1, "neocloud": 2, "dc-reit": 2, "sovereign-cloud": 1} },
        { "id": "chip_allocation",       "weight_by_subtype": {"hyperscaler-pure-play": 2, "hyperscaler-segment": 1, "neocloud": 2, "dc-reit": 0, "sovereign-cloud": 1} },
        { "id": "rpo_depth",             "weight_by_subtype": {"hyperscaler-pure-play": 2, "hyperscaler-segment": 2, "neocloud": 2, "dc-reit": 1, "sovereign-cloud": 1} },
        { "id": "anchor_customer_wins",  "weight_by_subtype": {"hyperscaler-pure-play": 2, "hyperscaler-segment": 1, "neocloud": 2, "dc-reit": 1, "sovereign-cloud": 1} },
        { "id": "differentiation",       "weight_by_subtype": {"hyperscaler-pure-play": 2, "hyperscaler-segment": 1, "neocloud": 2, "dc-reit": 1, "sovereign-cloud": 2} }
      ]
    },
    { "id": "narrative", "specialized": false, "weight": 2 },
    {
      "id": "moat",
      "label": "Moat (Cloud-Infra)",
      "specialized": true,
      "moat_types": ["switching-costs", "efficient-scale", "customer-relationships", "technical-differentiation", "capital-access"],
      "sub_criteria": ["moat_stack_count", "cross_sell_evidence", "multi_region_coverage", "rpo_retention_rate"]
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

1. **Hyperscaler-segment scoring inside mega-caps (Azure, AWS, GCP)** — current proposal: score *segments only* not whole companies (same as RR.L multi-segment handling). *Confirm.* Alternative: only score pure-plays + neoclouds + REITs; never split mega-caps.

2. **Neocloud sub-type — speculative or in-scope?** Names like CRWV are pre-mature business models (heavy capex, customer concentration, lease-financed). Current proposal: in-scope with explicit "speculative" tag in the score display. Alternative: exclude until they have multi-year profitable track record. *Confirm.*

3. **Data-center REITs — under this adapter or under a future REIT adapter?** Current proposal: under this adapter when hyperscaler concentration >50% of revenue. *Confirm or override.*

4. **ORCL at 13-14/16 — gut check?** This is among the highest worked-example scores. Honest reflection of binding-scarcity cloud-infra in step-change AI regime, or rubric over-rewarding ORCL's specific setup?

5. **The "Software" question revisited.** The Cloud-Infra adapter exists *because* Software didn't fit ORCL. Implication: when a Software adapter is eventually written (for hypothetical future watchlist additions like MSFT-as-whole-company, CRM, ADBE), it will explicitly *exclude* cloud-infra revenue and score on SaaS-specific bottlenecks. *Note for future spec.*

6. **Sub-type granularity (5 proposed)** — sensible? *Recommendation:* keep 5; each represents a genuinely different rotation thesis.

---

## 8. Coverage check — all 24 holdings now have an adapter

After this adapter, every current holding has a coherent adapter home:

| Adapter | Holdings |
|---|---|
| AI-Infra/Semi (TBD-formalised) | NVDA, TSM, ASML, ARM, LSCC, COHR, MOD, 4063.T, RGTI |
| **Cloud-Infra (this)** | **ORCL** |
| Energy-Power (drafted) | RR.L (power leg), BE |
| Hydrocarbons (drafted) | XOM |
| Industrial-Electrical (drafted) | SU.PA |
| Defense (drafted) | RHM.DE, RR.L (defense leg) |
| Pharma (drafted) | LLY, ABBV |
| Mining-Metals (drafted) | AEM, AU, WPM, PAAS, ALB |
| GICS sub-sector only (no adapter — physical ETFs) | GLD, SLV |

**Coverage = 24/24 holdings.** Milestone achieved.

---

## 9. Next Steps

- User reviews this adapter, confirms open decisions in §7
- Batch session complete. When user returns, queue:
  - **Pharma adapter open items 12.2 → 12.5** (carried over from interrupted session)
  - **5 new adapter review queues:** Defense (6 items) → Mining (7 items) → Industrial-Electrical (6 items) → AI-Infra (7 items) → Cloud-Infra (6 items) = ~32 decisions total

---

## 10. Version History

| Version | Date | Notes |
|---|---|---|
| v1 draft | 2026-05-17 | Initial draft with 6 open items. Completes adapter coverage for all 24 current holdings. |

---

*Draft v1. Pending user review. Personal use only. Not investment advice.*
