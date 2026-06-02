# Sector Adapter — Software / SaaS — v1 (Draft)

> **Status:** Thirteenth adapter under the Cross-Sector Investment Philosophy v1.1. Covers application & infrastructure software companies whose economics are recurring-revenue subscriptions (SaaS), distinct from Cloud-Infra (hyperscaler capacity) and AI-Infra-Semi (silicon). The thesis is the recurring-revenue compounding machine: retention, expansion, and operating leverage.
> **Doctrine sources:** `Cloud_Infra_Sector_Adapter_v1_1_Lock_Supplement.md` (segment-scoring + the software/cloud boundary), `AI_Infra_Semi_Sector_Adapter_v1.md`, `Cross_Sector_Investment_Philosophy_v1_1.md`, `Pharma_Sector_Adapter_v1.md` (8-Q template).
> **Calibration note:** Complete template per user request. **Uncalibrated** — no pure-SaaS name currently held (ORCL routes to Cloud-Infra as a hyperscaler-adjacent hybrid). First pure-SaaS evaluation calibrates.
> **Not investment advice. Personal use only.**

---

## 1. Scope

### In-scope
- **Application SaaS**: CRM, ERP, HCM, vertical SaaS (Salesforce, Workday, ServiceNow, Intuit, vertical names)
- **Infrastructure / dev software**: observability, security, data (Datadog, CrowdStrike, Snowflake, MongoDB, Cloudflare)
- **AI-application software**: software whose product is an AI capability sold as a subscription (distinct from the silicon/compute beneath it)
- **Platform software**: developer platforms, low-code, API-first businesses with subscription/usage economics

### Out-of-scope (different adapter)
- **Hyperscalers / mega-cap cloud** (MSFT/GOOGL/AMZN cloud segments, ORCL) → Cloud-Infra (segment-scored there)
- **Semiconductors / AI silicon** → AI-Infra-Semi
- **Data-center REITs** → Cloud-Infra (or the new REIT adapter for pure-property plays)
- **Hardware-led companies** with software attached → score by dominant economics

### The software/cloud boundary (inherited from Cloud-Infra supplement)
The line: **Cloud-Infra = selling capacity/infrastructure at hyperscale; Software/SaaS = selling an application or tool on subscription.** ORCL sits in Cloud-Infra because its thesis is now hyperscaler capacity (OCI). A Salesforce or CrowdStrike sits here because the thesis is recurring application revenue + net retention. When ambiguous, route by what the *revenue growth* is actually coming from.

### What this pays for
SaaS pays for **a recurring-revenue compounding machine**: high-retention subscriptions that expand over time (net revenue retention > 100%), with software's near-zero marginal cost producing operating leverage as the base scales. The thesis is a bet on durable retention + expansion + a path to (or existence of) real free-cash-flow margins — *not* growth at any cost.

---

## 2. Sub-types within this adapter

| Sub-type | Code | Notes |
|---|---|---|
| Application SaaS | `app-saas` | CRM/ERP/HCM/vertical; switching costs + seat expansion dominate |
| Infrastructure SaaS | `infra-saas` | Observability/security/data; usage-based + consumption expansion; Q1 + Q4 weighted |
| AI-application SaaS | `ai-saas` | AI capability sold as subscription; **anti-narrative-inversion sharp** (AI-software hype); must show real retention not just bookings |
| Platform / dev software | `platform-saas` | API-first/dev platforms; network effects + ecosystem lock-in |

Sub-type affects Q1 (what the bottleneck/category position is) and Q5 (which retention metric dominates — seat expansion vs consumption).

---

## 3. The Eight-Question Adapter (/16)
Score each pillar 0/1/2, total /16. **Pass = 6+ pillars ≥ 1, with Q3 (moat) and Q5 (visibility) emphasis; no pillar = 0.** Note: for SaaS, Q5 (revenue visibility/durability) carries unusual weight because recurring revenue *is* the thesis.

### Q1 — Bottleneck / Category Position *(sector-specialized)*
Is the company the category leader in a category that's a genuine budget priority? Sub-criteria: category leadership (clear #1/#2 vs commoditized), category as a CIO budget priority (security/AI = yes; nice-to-have = no), mission-criticality (rip-out cost), TAM expansion runway.

### Q2 — Narrative *(universal)*
AI-software, cybersecurity, data-cloud narratives — current/mature/stale. **Anti-narrative-inversion especially sharp for `ai-saas`**: penalize "we added AI" narratives without retention to back them.

### Q3 — Moat *(sector-specialized — DOMINANT)*
*Where SaaS quality scores.* Sub-criteria: switching costs / data gravity (how embedded), net revenue retention (the single best moat metric — >120% = exceptional, <100% = leaky bucket), ecosystem/integration lock-in, competitive durability (or is it one feature from being commoditized by a platform). NRR is the number that separates real SaaS moats from fragile ones.

### Q4 — Intensity *(universal)*
Rule-of-40 (growth + FCF margin), sales-efficiency (CAC payback / magic number), R&D as % of revenue, dilution from stock-comp (a real SaaS-specific drag — score it). Growth bought with unsustainable S&M or massive dilution scores low.

### Q5 — Visibility *(universal — DOMINANT for SaaS)*
*The recurring-revenue annuity.* Sub-criteria: ARR/recurring-revenue % of total, net revenue retention durability, RPO/billings backlog visibility, churn stability. This is the SaaS equivalent of aerospace's aftermarket — predictable recurring revenue is the whole point.

### Q6 — Sovereignty *(universal)*
Data-residency/regulatory exposure, customer concentration, geographic mix, platform-dependency risk (built on AWS/a single app store).

### Q7 — Catalyst *(universal)*
Inflections: path-to-profitability milestone, major product/AI launch with real attach, large customer wins, margin-expansion inflection, FCF crossover.

### Q8 — Technicals & Risk *(universal)*
Reads 9e/macro. SaaS-specific risk: valuation sensitivity to rates (long-duration assets — a rising-2Y regime hits high-multiple SaaS hard, ties to Pal), platform-disintermediation risk (a hyperscaler builds your feature), AI-commoditization risk (your product becomes a GPT wrapper).

---

## 4. Worked example
*None yet — no pure-SaaS holding.* First evaluation (a Salesforce/CrowdStrike/Datadog-class name) becomes the calibration anchor. Provisional expectation: best-in-class names with NRR>120% + Rule-of-40 cleared + category leadership should score in the 13-15/16 range; the rubric must avoid rewarding unprofitable hypergrowth (Q4 + Q5 guard against that).

## 5. VETO / kill criteria
### Universal VETOs apply.
### Software-specific
- **Net revenue retention falling below 100% and trending down** → veto check (the moat is leaking)
- **Accounting irregularity** (revenue-recognition games, billings manipulation) → veto
- **Platform disintermediation** (the hyperscaler/OS vendor builds the core product in) → re-score + veto check
- **Security breach destroying trust** (for a security/data vendor especially) → veto until resolved
- **Stock-comp dilution >X%/yr with no FCF path** → veto check (shareholder value leaking faster than it compounds)

## 6. Open decisions
1. Sub-type granularity (4) — confirm at first use; `ai-saas` may merge into the others if it proves not analytically distinct.
2. Q5 vs Q3 weighting — both flagged dominant; confirm which leads on first calibration.
3. The rates-sensitivity Q8 link to Pal/regime — confirm it reads the 9e macro state.

*Software/SaaS adapter v1 (template, uncalibrated). Authored 2026-06-01, Claude.ai. Personal use only. Not investment advice.*
