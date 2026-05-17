# Sector Adapter — Pharma — v1 (Draft)

> **Status:** Third adapter built under the Cross-Sector Investment Philosophy v1.1. Covers branded pharmaceutical companies — innovators with patent-protected revenue streams. Separated by sub-type because the rotation thesis and bottleneck dynamics differ materially between metabolic/obesity, diversified, oncology, and rare disease.
> **Doctrine source:** `Cross_Sector_Investment_Philosophy_v1.1.md` §6
> **Not investment advice. Personal use only.**

---

## 1. Scope

### In-scope (this adapter covers)
- **Branded large-cap pharma:** LLY, ABBV, NVO, MRK, PFE, BMY, JNJ, AZN, GSK
- **Branded mid-cap pharma:** REGN, VRTX, BIIB
- **Speciality / rare disease:** ALNY, BMRN
- **Pure-play oncology:** specialty oncology biotechs with at least one commercial drug
- **Diversified biopharma:** AMGN, GILD

### Out-of-scope (different adapter)
- **Generics / biosimilars** (TEVA, SNY generics divisions) → no adapter yet; the moat is cost not patents
- **Medical devices** (MDT, ABT devices, ISRG) → future Medical Devices adapter (TBD)
- **Diagnostics / tools** (TMO, DHR, ILMN) → future Life Sciences Tools adapter (TBD)
- **Healthcare services / payers** (UNH, CI) → out of personal portfolio scope
- **Pre-commercial biotech / clinical-stage** → too speculative for this framework; needs a different binary-outcome lens

### The rotation thesis

Pharma re-rates on forcing functions that are fundamentally different from AI infra or hydrocarbons:

| AI-Infra rotation thesis | Hydrocarbons rotation thesis | **Pharma rotation thesis** |
|---|---|---|
| Demand step-change up | Supply destruction | **Demographic / disease prevalence + payer regime shift** |
| Multi-year contracted backlog | Capital-return discipline | **Patent runway + pipeline depth** |
| Customer = hyperscalers | Customer = global GDP | **Customer = payers + patients (split power)** |
| Lock-in via PPAs | Lock-in via reserves | **Lock-in via patents + regulatory exclusivity** |

The binding constraint in pharma is **not physical infrastructure**. It is **patent-protected innovation that addresses a structurally growing disease burden**. The scarcity is *efficacious, branded therapy with a defensible patent runway*. When that runway shortens or pipeline replacement fails, the equity de-rates regardless of current cash flow.

### Worked example: how this splits across the current holdings

- **LLY** → `metabolic-obesity` sub-type. GLP-1 franchise + obesity platform. Demand-driven rotation thesis.
- **ABBV** → `diversified-immunology` sub-type. Post-Humira pipeline transition (Skyrizi, Rinvoq). Visibility-driven rotation thesis.

Both fit this adapter cleanly; sub-type emphasis differs.

---

## 2. Sub-types within this adapter

Each candidate is tagged with **one** sub-type (primary). Sub-type affects which Q1 and Q3 sub-criteria are weighted heaviest:

| Sub-type | Code | Notes |
|---|---|---|
| Metabolic / obesity | `metabolic-obesity` | GLP-1 platform names; demand step-change driver |
| Diversified immunology | `diversified-immunology` | Post-Humira-style pipeline transitions; multi-product portfolios |
| Oncology | `oncology` | Commercial oncology with active pipeline; binary clinical risk |
| Rare disease / specialty | `rare-specialty` | Orphan drug protection; pricing power but small populations |
| Mega-cap diversified | `mega-diversified` | Multi-therapeutic-area, multi-blockbuster franchises (JNJ, MRK) |

---

## 3. The Eight-Question Adapter

Score each pillar **0 / 1 / 2**. Total out of 16. Strong-pass = **6+/8 pillars at score ≥1, with Q1 and Q3 ≥1.**

### Q1 — Bottleneck *(sector-specialized)*

> *Is the name sitting at a binding scarcity in pharma — patent runway, pipeline depth, regulatory exclusivity, manufacturing capacity, or addressable disease burden?*

Sub-criteria (score 0/1/2 each, average → pillar score):

| Sub-criterion | 0 | 1 | 2 |
|---|---|---|---|
| **Patent runway (years to LOE on primary asset)** | <3 yrs to LOE | 3-7 yrs | >7 yrs OR formulation/method-of-use extensions secured |
| **Pipeline NPV vs current revenue** | Pipeline NPV <30% of current rev | 30-100% | >100% — pipeline can replace current franchise |
| **Regulatory exclusivity status** | Standard expiry only | Some additional exclusivity (orphan, peds) | Multi-layer (orphan + biologic + method-of-use stacking) |
| **Biosimilar / generic threat profile** | Imminent biosimilar entry inevitable | Manageable but real | Biologic complexity high; biosimilar economics weak |
| **In-house next-gen replacement** | None visible | Mid/late-stage candidate exists | Late-stage / launched replacement with differentiated profile |
| **Addressable disease burden trajectory** | Flat / declining (disease cohort shrinking) | Stable | Rising (demographic + screening + obesity-era prevalence) |
| **Manufacturing capacity** | Single-source, supply-constrained | Adequate | Owns scarce manufacturing capacity (e.g., GLP-1 fill-finish) |

Sub-criterion weighting by sub-type emphasis applied in JSON schema (§6).

Pillar score = round(weighted avg of applicable sub-criteria).

### Q2 — Narrative *(universal)*

> *Mispriced in the wrong bucket?*

Pharma-specific narrative arbitrage patterns:
- **"Patent cliff" mispricing** — market over-prices the cliff and under-prices the pipeline transition. Classic ABBV-post-Humira setup.
- **"Mature large-cap, no growth"** when actually entering a new mechanism-of-action franchise (e.g., GLP-1 reframed metabolic disease).
- **"Pipeline-dependent biotech"** when actually a derisked late-stage asset with peer-reviewed efficacy.
- **"Payer pushback / IRA pricing"** when actually a drug where volume × discounted-price > status quo (the LLY/NVO playbook).
- **"Generics race"** when actually a biologic with complex manufacturing where biosimilars haven't materialised at expected pace.

Score: 0 = consensus correct, 1 = partial mispricing, 2 = clear narrative arbitrage.

### Q3 — Moat *(sector-specialized)*

> *Which moat type is operative, and how durable?*

Pharma moats (Dorsey / Morningstar taxonomy adapted):

| Moat type | What it looks like in pharma |
|---|---|
| **Intangibles — patents** | Composition-of-matter patents, formulation extensions, method-of-use extensions, biologic complexity |
| **Intangibles — brand / clinician trust** | First-line guideline positioning, KOL endorsement, prescribing-physician inertia |
| **Regulatory** | Orphan designation, pediatric exclusivity, biosimilar pathway barriers, FDA / EMA approvals as moat |
| **Switching costs** | Patient stickiness on chronic therapy, formulary placement, payer contracts |
| **Scale / efficient scale** | Sales force reach, manufacturing scale, R&D budget that small competitors can't match |

Sub-criteria:
- Number of moat types stacked (1, 2, or 3+)
- Patent runway weighted vs total revenue
- Switching cost evidence (compliance rates, switching studies)
- Manufacturing complexity (especially biologics)

Pillar score = 2 if multiple durable moats stack with clear evidence; 1 if single moat or stacked moats with one weak link; 0 if patent runway is the only moat and it's running out.

### Q4 — Intensity *(universal)*

> *Is demand-per-unit-of-output rising?*

Pharma-specific intensity signals:
- Diagnosed prevalence growth in the target indication (obesity, MASH, Alzheimer's, oncology incidence)
- Treatment-rate growth (% of diagnosed patients actually on therapy — under-treatment is the largest intensity opportunity)
- Geographic expansion (US-only → ex-US launches)
- Line-of-therapy expansion (second-line → first-line approval)
- Indication expansion (one approved indication → multiple)

Score: 0 = flat / declining, 1 = stable growth, 2 = step-change (e.g., GLP-1 indication expansion to cardiovascular outcomes).

### Q5 — Visibility *(universal)*

> *Take-or-pay or contracted revenue?*

Pharma-specific visibility:
- Patent runway certainty (composition-of-matter > formulation > method-of-use, in declining order of certainty)
- Long-term supply contracts with payers (uncommon but exists)
- Standing prescriptions on chronic therapy (high stickiness)
- Pipeline catalysts on calendar (Phase 3 readouts, PDUFA dates)
- Hedge against single-asset risk (% revenue from top product)

Score: 0 = single-asset concentration with near LOE, 1 = diversified or long runway, 2 = both diversified and long runway with active pipeline replacement.

### Q6 — Sovereignty *(universal)*

> *Strategic, policy-supported, friend-shored?*

Pharma-specific sovereignty:
- Domestic manufacturing footprint (IRA tailwinds, on-shoring pressure)
- Strategic indication classification (pandemic preparedness, biodefense, mental health priority designations)
- Friend-shore API supply (vs Indian/Chinese API dependency)
- US-listed / OECD-domiciled vs sanctioned-jurisdiction exposure
- IRA negotiated-price status (a *negative* sovereignty signal — being on Medicare negotiation list)

Score: 0 = high IRA negotiation exposure + foreign API dependency, 1 = mixed, 2 = US-manufactured + diversified API + not on Medicare negotiation list.

### Q7 — Catalyst *(universal)*

> *Near-term event forcing re-rating?*

Pharma-specific catalysts:
- PDUFA date / regulatory approval decision
- Phase 3 readout
- Indication expansion approval
- Patent litigation outcome
- M&A announcement (acquirer or target)
- Manufacturing capacity expansion completion
- Major label change (cardiovascular outcomes added to GLP-1, etc.)

Score: 0 = none visible, 1 = within 12 months, 2 = within 90 days.

### Q8 — Technicals & Risk *(universal)*

> *Chart clean, exhaustion / turbulence signals quiet?*

Same as base framework. No pharma-specific adjustment.

**Note on idiosyncratic pharma risk:** binary clinical events can produce gaps that no technical pattern predicts. Q8 captures *price-action* risk but not *trial-readout* risk. Trial risk lives in Q5 (visibility) and Q7 (catalyst) — when a Phase 3 readout is the catalyst, the catalyst is also the risk. Position-sizing discipline (Spec 9c Percoco layer) addresses this; the score does not.

---

## 4. Worked Example — LLY (Eli Lilly)

Illustrative scoring. **Not a recommendation.**

| Pillar | Sub-criteria notes (`metabolic-obesity`) | Score |
|---|---|---|
| Q1 Bottleneck | Tirzepatide patent runway strong (~2036) (2); pipeline NPV (orforglipron, retatrutide, donanemab) >100% of current rev (2); regulatory exclusivity stacking (2); biosimilar threat distant (2); next-gen replacement (oral GLP-1 orforglipron, triple-agonist retatrutide) launched/late-stage (2); obesity prevalence rising globally (2); manufacturing capacity = the binding scarcity in the sector (2) | **2** |
| Q2 Narrative | Market still partially anchors on "expensive valuation"; obesity TAM still being mapped. Partial arbitrage remains | **1** |
| Q3 Moat | Patents + manufacturing complexity (peptide fill-finish) + clinician brand + scale R&D budget — three+ moats stacked | **2** |
| Q4 Intensity | Obesity diagnosed prevalence + indication expansion (CV outcomes, MASH, OSA) + geographic expansion = step-change | **2** |
| Q5 Visibility | Long patent runway + diversified pipeline + supply-constrained demand (visibility is unusually high for pharma) | **2** |
| Q6 Sovereignty | US-domiciled, expanding US manufacturing (NC, IN), not on Medicare negotiation list (yet) | **2** |
| Q7 Catalyst | Quarterly earnings + ongoing label expansions; nothing imminent <90 days unless retatrutide Phase 3 reads out | **1** |
| Q8 Technicals | Recently consolidated after 2024-25 run; check exhaustion / regime quiet | **1** |
| **Total** | | **13 / 16** |

**Interpretation:** Passes strongly (6+ pillars ≥1 with both Q1 and Q3 at 2). Higher conviction than RR.L (11/16) or XOM (11/16) — reflects that LLY's bottleneck position is currently exceptional rather than merely strong. The adapter correctly identifies LLY as a *demand-step-change-driven, supply-constrained, multi-moat franchise* — not a "mature pharma."

**Honest caveat:** Q1 scored 2 across nearly all sub-criteria. The risk is that any single sub-criterion turning (e.g., manufacturing capacity catches up to demand, removing supply-constraint premium) materially compresses the bottleneck score. Worth re-scoring quarterly.

---

## 5. Worked Example — ABBV (AbbVie)

Illustrative scoring. **Not a recommendation.**

| Pillar | Sub-criteria notes (`diversified-immunology`) | Score |
|---|---|---|
| Q1 Bottleneck | Humira LOE in rear-view, biosimilars priced in (1); pipeline NPV: Skyrizi + Rinvoq trajectory replacing Humira (2); regulatory exclusivity on Skyrizi/Rinvoq solid (2); biosimilar threat low for in-line newer biologics (2); next-gen replacement (cedirogant + immuno pipeline) building (1-2); immunology disease burden rising (1); manufacturing adequate (1) | **2** |
| Q2 Narrative | Market partly still under-pricing the Skyrizi/Rinvoq transition vs Humira-cliff fear. Classic narrative arbitrage | **2** |
| Q3 Moat | Patents + clinician inertia in derm/rheum + scale sales force = multiple moats, durable | **2** |
| Q4 Intensity | Immunology indication expansion (atopic derm, UC, Crohn's, etc.) + line-of-therapy expansion | **2** |
| Q5 Visibility | Diversified across immunology, oncology, neuroscience, aesthetics; Skyrizi/Rinvoq runway secured | **2** |
| Q6 Sovereignty | US-domiciled, established US manufacturing; some IRA exposure on certain assets | **1** |
| Q7 Catalyst | Regular quarterly Skyrizi/Rinvoq beats are the catalyst rhythm; no single 90-day binary event | **1** |
| Q8 Technicals | Steady uptrend; check exhaustion / regime quiet | **1** |
| **Total** | | **13 / 16** |

**Interpretation:** Passes strongly (6+ pillars ≥1, Q1 and Q3 ≥1). Same total as LLY but a different *shape* — ABBV's strength is in pipeline-replacement execution (Q2 narrative + Q5 visibility), while LLY's is in demand step-change (Q1 + Q4). Both score 13 but for different reasons, which is exactly what a well-calibrated sector adapter should produce.

**Cross-adapter sanity check:**
- RR.L (Energy-Power) = 11/16
- XOM (Hydrocarbons) = 11/16
- **LLY (Pharma) = 13/16**
- **ABBV (Pharma) = 13/16**

Pharma scoring higher reflects that the patent + pipeline structure in branded pharma genuinely *is* a more durable moat than energy / hydrocarbons in this cycle. The framework is not biased — it's revealing real differences in moat durability across sectors. If RR.L deserves to score *similarly* to LLY, the rubric is mis-calibrated. If LLY genuinely deserves a higher conviction score than RR.L right now, the rubric is working as intended.

---

## 6. Schema Sketch (for future spec)

```json
{
  "id": "pharma",
  "name": "Pharma",
  "applies_to": "stock",
  "version": "1.0",
  "scope": "Branded pharmaceutical companies — innovators with patent-protected revenue. Excludes generics, devices, diagnostics, and services.",
  "sub_types": ["metabolic-obesity", "diversified-immunology", "oncology", "rare-specialty", "mega-diversified"],
  "questions": [
    {
      "id": "bottleneck",
      "label": "Bottleneck (Pharma)",
      "specialized": true,
      "sub_criteria": [
        { "id": "patent_runway",       "weight_by_subtype": {"metabolic-obesity": 2, "diversified-immunology": 2, "oncology": 2, "rare-specialty": 2, "mega-diversified": 1} },
        { "id": "pipeline_npv",        "weight_by_subtype": {"metabolic-obesity": 2, "diversified-immunology": 2, "oncology": 2, "rare-specialty": 1, "mega-diversified": 2} },
        { "id": "regulatory_exclusivity", "weight_by_subtype": {"metabolic-obesity": 1, "diversified-immunology": 2, "oncology": 1, "rare-specialty": 2, "mega-diversified": 1} },
        { "id": "biosimilar_threat",   "weight_by_subtype": {"metabolic-obesity": 1, "diversified-immunology": 2, "oncology": 1, "rare-specialty": 1, "mega-diversified": 2} },
        { "id": "next_gen_replacement","weight_by_subtype": {"metabolic-obesity": 2, "diversified-immunology": 2, "oncology": 2, "rare-specialty": 1, "mega-diversified": 1} },
        { "id": "disease_burden",      "weight_by_subtype": {"metabolic-obesity": 2, "diversified-immunology": 1, "oncology": 2, "rare-specialty": 0, "mega-diversified": 1} },
        { "id": "manufacturing_capacity","weight_by_subtype": {"metabolic-obesity": 2, "diversified-immunology": 1, "oncology": 1, "rare-specialty": 2, "mega-diversified": 1} }
      ]
    },
    { "id": "narrative", "specialized": false, "weight": 2 },
    {
      "id": "moat",
      "label": "Moat (Pharma)",
      "specialized": true,
      "moat_types": ["intangibles-patents", "intangibles-brand", "regulatory", "switching", "scale"],
      "sub_criteria": ["moat_stack_count", "runway_weighted_revenue", "switching_evidence", "manufacturing_complexity"]
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

1. **Pre-commercial / clinical-stage biotech exclusion** — currently out of scope (§1). Confirm: any pre-commercial names (RGTI-style speculative biotech bets) get tagged as Healthcare GICS top-level only, not under this adapter. *Recommendation:* confirm exclusion. Pre-commercial biotech needs a different framework (binary-outcome / option-value model), not the 8-Q screen.

2. **Sub-type granularity (5 proposed)** — too granular? Too sparse? Could collapse `oncology` and `rare-specialty` since both are exclusivity-heavy + smaller patient populations. *Recommendation:* keep 5. The Q1 weighting differences (disease burden score 2 for oncology, 0 for rare-specialty) genuinely differ.

3. **LLY worked example: 13/16 — gut check?** Higher than the prior two adapters' worked examples. Is LLY *genuinely* a higher-conviction setup than RR.L (Energy-Power) and XOM (Hydrocarbons), or is the Pharma rubric too lenient?

4. **ABBV worked example: 13/16 — gut check?** Same calibration question. Should ABBV score *equal to* LLY or below it?

5. **Manufacturing capacity as Q1 sub-criterion** — controversial inclusion. Manufacturing scarcity is *the* GLP-1 bottleneck right now (the entire bull thesis), but for most pharma names manufacturing isn't a binding constraint. The sub-type weighting handles this (manufacturing weight=2 for metabolic-obesity, =1 elsewhere) but reasonable people could argue manufacturing belongs elsewhere or not at all. *Recommendation:* keep as Q1 sub-criterion with sub-type weighting. Removing it would understate the LLY bull thesis.

---

## 8. Next Steps

- User reviews this adapter, confirms open decisions in §7
- After Pharma locked → next adapter candidates: **Defense** (RHM.DE + RR.L defense leg) or **Mining/Metals** (GLD/AEM/AU/WPM/SLV/PAAS — 6 holdings, biggest cluster after AI infra)
- After 4-5 adapters drafted: write Spec 9i — Adapter Scoring Engine (the actual UI for running scores against named tickers)

---

## 9. Version History

| Version | Date | Notes |
|---|---|---|
| v1 draft | 2026-05-17 | Initial draft with 5 open items |

---

*Draft v1. Pending user review. Personal use only. Not investment advice.*
