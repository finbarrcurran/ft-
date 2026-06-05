# FT — SC-04: Earnings-Triggered Thesis-Update Prompt Template (v1)

> **From:** Claude.ai · **Date:** 2026-06-05 · **Baseline:** v1.38.0.
> **Type:** Methodology / prompt template (authoring artifact — not a Claude Code build item).
> **Discipline:** no-steering (note #7); empirical-anchor mandate; cite notes by exact #+name (note #15); human-in-the-loop — **never auto-lock**.
> **Personal use only. Not investment advice.**

---

## §0 — What this is, and when it fires

A reusable prompt that, when a holding reports earnings, produces a **score-blind redline DRAFT** updating that holding's locked thesis. It re-tests only the pillars the print touches, checks every documented live trigger, and flags anything it can't verify. It **drafts**; you decide whether to re-score/re-lock.

- **Trigger:** a holding reports earnings (quarterly, per the scoring-log governance cadence: *"thesis scores → quarterly on company earnings cadence + immediate on invalidation trigger"*), **or** a documented invalidation trigger fires off-cycle.
- **Frameworks:** 8-Q operating-stock (/16) primary; **Asset-Hedge (/8) supported via the same structure** — the template reads the prior thesis's own pillar set, so it adapts automatically. **Crypto is out of scope** (no earnings event).
- **Output:** a DRAFT. The tally/band/pass-gate are **assembled downstream by you/Claude at stage 4**, not by the model.
- **To start a run:** paste the kickoff prompt in **§8** into a new chat (it embeds the process, so it works even if this file isn't attached).

---

## §1 — The load-bearing design: the no-steering split (note #7)

A naïve "show the model the current scores and ask it to revise them" prompt **violates note #7** — showing prior scores anchors the model into confirming them, exactly what the WBA floor-anchor test was built to prevent. So the template splits what the model sees:

- **The model SEES** the prior thesis's *narrative* — reasoning, claims, citations, sub-criteria, documented triggers, kill-switches. It needs these to judge "did this print confirm, strengthen, weaken, or break each claim?"
- **The model does NOT see** the prior numeric pillar scores, the tally, the Final Score, the calibration band, or the score-history table. It scores the affected pillars **blind** from the earnings evidence.
- **The redline diff (locked score → blind score) is assembled OUTSIDE the model** — by you/Claude at stage 4. The model literally cannot compute the diff because it was never shown the locked side. That is the correct, faithful behaviour.

**Score-redaction list** (strip these from the prior thesis before it goes into the prompt — done at prompt-assembly, §3):
- Every `— Score: <n> / <max>` pillar header → replace with the pillar heading only.
- The `Tally:` line · the `> **Final Score:**` line · the `Calibration ladder band:` line.
- `Section 14 — Score history` (the whole table).
- Any inline "scored 2 because…" phrasings that state the number → keep the *reasoning*, remove the *number*.

Keep everything else verbatim: Action recommendation, multi-segment framing, per-pillar reasoning prose, citations, catalysts, risks, kill-switches, post-lock action items.

---

## §2 — The 4-stage workflow wrapper (note #14)

| Stage | Owner | Action |
|---|---|---|
| (pre) | Claude.ai | Assemble the prompt per §3 (score-redacted prior thesis + primary earnings sources + live note list + extracted triggers) |
| **1** | Gemini 3.1 Pro | Produce the blind redline draft from the §4 template |
| **2** | Claude.ai | **Mandatory empirical audit** (note #14) — verify every anchor against primary sources; flag fabrications, errors, omissions |
| **3** | *optional* | Gemini corrective re-pass — **skip it if the re-pass introduces new errors** (the CCJ stage-3 failure; Gemini fabricates more under correction pressure). When in doubt, replace stage 3 with a Claude-assembled lock |
| **4** | Claude.ai + you | Assemble the redline: combine blind re-tests with the carried (unchanged) locked scores → new tally → pass-gate per the framework → band. **You decide whether to re-lock. Never auto-lock.** |

Default engine is **Gemini 3.1 Pro** (note #13: Perplexity unsuitable for framework scoring; Flash underperforms on adherence). A Claude-direct variant is a trivial derivative of the same template if you want a second read.

---

## §3 — Prompt-assembly checklist (Claude.ai, before stage 1)

- [ ] **(A)** Build the **score-redacted** prior thesis (§1 redaction list applied).
- [ ] **(B)** Gather **primary** earnings sources only: press release, 8-K exhibit 99.1, 10-Q/10-K if filed, earnings-call transcript. (Note: secondary aggregators carry materially different figures — primary IR/SEC only.)
- [ ] **(C)** Paste the **live methodology-note list**, each by exact number + name (e.g. note #1 … note #16+). The model may cite **only** from this list — this is the guard against invented-note fabrication (seen on SLV/SU.PA).
- [ ] **(D)** Extract the prior thesis's **documented triggers** verbatim into one block: upgrade-triggers, kill-switches (e.g. JNJ §11), and post-lock action items (e.g. JNJ post-lock items 1–5).

---

## §4 — THE TEMPLATE (copy-paste; fill the `{{…}}` slots)

```
ROLE: You are producing a DRAFT redline update to an existing locked investment
thesis, triggered by a fresh earnings report. You re-test only the pillars the
print touches, you score them BLIND, and you do not lock anything. A human
assembles the final redline.

INPUTS
A) PRIOR THESIS (SCORE-REDACTED): the full reasoning, claims, citations,
   sub-criteria, documented triggers and kill-switches of the current locked
   thesis — with ALL numeric pillar scores, the tally, the final score, the
   band, and the score-history table REMOVED. You see the argument, not the verdict.
   ---
   {{PRIOR_THESIS_SCORE_REDACTED}}
   ---
B) NEW EARNINGS EVIDENCE (primary sources only): press release / 8-K 99.1 /
   10-Q / earnings-call transcript for the just-reported period. Treat ONLY
   these — and data explicitly inside them — as verifiable.
   ---
   {{NEW_EARNINGS_SOURCES}}
   ---
C) LIVE METHODOLOGY NOTES (cite ONLY from this list, by exact number + name;
   inventing a note number or name is a critical failure):
   ---
   {{LIVE_NOTE_LIST}}
   ---
D) DOCUMENTED TRIGGERS to check (verbatim from the prior thesis):
   ---
   {{PRIOR_TRIGGERS_KILLSWITCHES_POSTLOCK_ITEMS}}
   ---

TASK
1. For each pillar where (B) gives NEW evidence, re-test it BLIND: arrive at a
   score using the prior thesis's OWN pillar definitions/sub-criteria and the
   earnings evidence, WITHOUT reference to what the pillar previously scored
   (you have not been shown it). Pillars with no new evidence are NOT re-tested.
2. For every item in (D), state the print's effect: FIRED / NOT FIRED /
   STILL PENDING / CANNOT VERIFY.
3. In a dedicated block, flag anything the print should plausibly contain but
   you could NOT confirm from (B).

HARD RULES (anti-fabrication)
- Every numeric claim MUST carry a primary-source citation from (B). A figure
  you cannot cite from (B) goes in the CANNOT-VERIFY block — never in the body.
- No fabricated prices, betas, or market data. If it isn't in (B), it's CANNOT-VERIFY.
- Cite methodology notes ONLY from (C), by exact number + name.
- Label any company figure marked preliminary/unaudited as PROVISIONAL.
- You are NOT shown prior scores and you do NOT request them. Scoring blind is the point.
- Do NOT compute a tally, band, or pass-gate — those require the carried (non-
  re-tested) scores you were deliberately not given. The human assembles them.

OUTPUT (return exactly this structure)

## Pillar re-tests
For each re-tested pillar:
- <Pillar ID — name (as in the prior thesis)>
  - New anchor: <specific figure/event from (B) + citation>
  - Effect on prior claim: CONFIRM | STRENGTHEN | WEAKEN | BREAK — <one line>
  - Blind score: <n> / <max> — <reasoning grounded in the prior sub-criteria>
  - Notes applied: <note #N: exact name>, … (or "none directly binding")
  - Citation: <primary source>

## Pillars not re-tested
- <Pillar ID — name>: no new evidence in this print.

## Triggers checked
- <trigger text> → FIRED | NOT FIRED | STILL PENDING | CANNOT VERIFY — <one line + citation if FIRED>

## Could-not-verify — flagged
- <item>: <what would be needed to confirm it> (NO estimate, NO inference)

## Proposed update — DRAFT, NOT APPLIED
- Blind re-tested pillar scores: <re-tested pillars only>
- Tally / band / pass-gate: NOT COMPUTED HERE (require the carried scores,
  deliberately withheld). The human assembles them at stage 4.
- Redline direction: the CONFIRM/STRENGTHEN/WEAKEN/BREAK flags above tell the
  human where each re-tested pillar moves; the human applies the numeric delta
  against the locked scores.

END OF OUTPUT.
```

---

## §5 — Stage-4 assembly (Claude.ai + you, after the model returns)

1. Take the blind re-tested scores; pair each with the carried locked score for that pillar (which the model never saw).
2. Build the new tally: blind re-tests + unchanged carried pillars.
3. Run the **pass-gate as defined by the prior thesis's framework** (8-Q vs Asset-Hedge gates differ — note #5 Asset-Hedge gate exception applies for hedges).
4. Assign the band off the calibration ladder.
5. Produce the redline diff (locked → proposed) as the score-history table's next row, and decide: hold, re-score, or re-lock (`v<N+1>`). **Human call.** A re-lock follows the locked-thesis MD format spec exactly (title em-dashes, `> ` metadata, `Status: Locked — <date>`, standalone `> **Final Score:**`).

---

## §6 — Why the anti-fabrication clauses are non-optional

Baked into §4 because of documented failure modes, not as boilerplate: Gemini **fabricates more severely under correction pressure** than in initial drafts (CCJ stage-3); it has invented **non-existent methodology-note citations** with fabricated names (SLV, SU.PA) — hence "cite only from (C)"; and it has hallucinated **prices/market data** (the pattern flagged in JNJ §13 item 15) — hence "no fabricated prices; CANNOT-VERIFY instead." The empirical-anchor mandate is the spine: a figure without a primary-source citation does not enter the body.

---

## §7 — Reference case: JNJ (live trigger this template is built to catch)

JNJ v1 carries a documented **live Q5 upgrade-trigger**: *if the talc settlement crystallises at the Q2 print (July 15), Q5 candidate-upgrades from 1 → 2.* That is exactly a (D) trigger. When JNJ reports Q2:
- (A) = JNJ v1 score-redacted; (B) = JNJ Q2 press release + 8-K + 10-Q + call; (C) = live note list (#1–#16+); (D) = JNJ §11 kill-switches + the 5 post-lock action items (talc status, segment income-before-tax PROVISIONAL verification, IMAAVY PDUFA, MedTech CV growth, Ottava).
- The model re-tests Q5 (and any other pillar the print moves) **blind**, reports the talc trigger as FIRED / STILL PENDING / CANNOT VERIFY, and flags the segment income-before-tax figures if the 10-Q lines aren't in (B). You assemble the redline and decide on `JNJ v2`.

---

## §8 — Kickoff prompt (paste to open the update chat)

When a holding reports, fill the three slots, attach the files listed, and paste the block below into a new chat. It embeds the process, so it runs standalone even if this file isn't attached. **Where this is filed:** this section *is* the canonical home — do not copy the prompt into a separate file (a second copy drifts from the process above). The "remember to run it" hook lives in each thesis's post-lock action items, which already name the trigger date.

**Attach:** the current locked thesis (`{{TICKER}}_v{{N}}_locked.md`) · this SC-04 file · `_scoring_log.md` (for the live note list) · the primary earnings sources (press release / 8-K 99.1 / 10-Q / call transcript) or their links.

```
You are my framework partner for FT (personal investment research; not investment
advice). {{TICKER}} has reported {{PERIOD}} (released {{REPORT_DATE}}). Its locked
thesis needs a SCORE-BLIND redline update per the SC-04 process. Run SC-04, in order:

1. ASSEMBLE (you): build the SCORE-REDACTED version of the attached locked thesis —
   strip every pillar score, the tally, the Final Score line, the band, and the
   score-history table; keep all reasoning, citations, triggers, kill-switches
   (note #7, no-steering). Extract the thesis's documented triggers + kill-switches
   + post-lock action items into one trigger-check block. Pull the live methodology-
   note list (exact #+name) from the scoring log.
2. STAGE 1 — Gemini draft: produce the stage-1 prompt for me to run in Gemini 3.1 Pro
   (re-test only the pillars this print touches, BLIND; check every trigger; flag
   can't-verify). Hand me the prompt; I'll run it and paste Gemini's output back.
3. STAGE 2 — audit (you): verify every anchor in Gemini's draft against the primary
   sources; flag fabrications/errors/omissions (note #14).
4. STAGE 4 — assemble (you + me): pair the blind re-tests with the carried locked
   scores, recompute the tally + pass-gate (use the note #5 gate if Asset-Hedge),
   assign the band, produce the redline (locked → proposed). I decide whether to
   re-lock as v{{N+1}}. Never auto-lock.

Skip stage 3 unless a corrective Gemini re-pass is clearly warranted — and abandon it
if it introduces new errors (the CCJ pattern). Before you start: confirm which files
you have in scope, then produce the score-redacted thesis for my check before building
the stage-1 prompt.
```

---

## Snags / flags
- **S-04a.** The redaction (§1) is the whole feature — if prior scores leak into (A), note #7 is violated and the re-test is just anchored confirmation. Sanity-check the redacted thesis before stage 1.
- **S-04b.** The model **cannot** and **must not** output a tally/band/direction-vs-prior — it doesn't have the carried scores. If a draft returns a full tally, the redaction failed or the model inferred prior scores; reject and re-assemble.
- **S-04c.** Asset-Hedge (/8) reuses this unchanged, but the stage-4 pass-gate is the **note #5 exception** (Q1–Q3 ≥1; Q4 informational), not the 8-Q gate. Don't apply the wrong gate.
- **S-04d.** A Claude-direct variant is fine as a second read, but stage 2 (audit) still applies to it — no single-model self-lock.

---

*Template authored 2026-06-05, Claude.ai. v1 — refine after first live run (JNJ Q2, July 15). Personal use only. Not investment advice.*
