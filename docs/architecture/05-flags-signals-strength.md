# Architecture 05: flags, signals and strength

Draft 0.3, 2026-11-20. Uses `.physa/glossary.md` terms only. Builds on 01–04. Items marked (Q) are open questions for the owner.

Everything so far leans on strength: stakes multiply by it, policy follows from it, decay lowers it, the network view draws it. This design says where its evidence comes from (flags and signals in the running product, git and CI, people) and how it is worked out. It answers the map's open question `strength-formula` with a first model.

**Strength is one of Physa's core differentiators and also highly experimental** (the owner). It starts **opt-in** (§6.0), and its weights, decay and thresholds are built to be tuned and customized per team (§6.5). Flags, signals, results and rollback do not depend on it.

**The whole flags feature is opt-in too, off by default** (the owner: "If I as a solo developer just want to YOLO everything, I don't care about going through all of that ceremony"). It comes with the product signals that need it: `physa serve`, the provider, a flag per idea, experiments, guardrails and automatic rollback. **Engineering evidence is always on, whatever else is switched on or off** (§4.1).

## 1. Constraints

- **Ceremony is opt-in** (`ceremony-opt-in`). A solo developer runs none of this; flags, product signals and strength are switched on when a team wants them.
- **Measure only what ideas claim** (`claims-only`). Never general analytics.
- **One SDK for flags and signal events** (`one-sdk`), so exposure and outcome join on the same person.
- **Own the meaning, integrate the switch** (`openfeature`): evaluation through OpenFeature; teams keep their vendor.
- **A self-hosted flag service in the core** (`own-flag-service`): instant rollback with no vendor and no hosted layer.
- **Say when it can't tell** (`cant-tell`). Noise is never shown as a result.
- **Judgment is named** (`named-judgment`), weighted below measurement.
- **Strength is measured, never set by hand** (`measured-strength`); **decay proposes, never deletes** (`decay-proposes`).
- **Source code never leaves the customer** (`agents-customer-side`); signal events carry no personal data.
- **Reading never replays history** (`no-replay`).

## 2. Where things run

The engine runs on a developer's laptop or a CI machine. A production app cannot ask a laptop whether a flag is on. So, with flags on, one part of Physa has to run next to production (Q1):

```
 production app                       next to production                 laptop / CI
┌──────────────────────┐  evaluate   ┌─────────────────────────┐  API   ┌──────────────┐
│ OpenFeature SDK      │────────────▶│ physa serve             │◀──────▶│ engine       │
│  + Physa provider    │  track      │  flags · signal events  │ token  │ strength,    │
│  (or vendor + hook)  │────────────▶│  one SQLite file        │        │ policy,      │
└──────────────────────┘             └─────────────────────────┘        │ Needs you    │
        │ OpenTelemetry (errors, latency, tagged with the flag)          └──────────────┘
        └──────────────▶ the team's own telemetry backend ───── queried for guardrails ──┘
```

- **`physa serve`** is the same Go binary, run as a server: flag evaluation and signal events, one SQLite file, deployable anywhere a container or a binary runs. The hosted layer runs it for teams that don't want to.
- **The engine drives it** through its API with a token: widening a rollout, turning a flag off for a guardrail, reading results. People do the same through the CLI.
- **It holds no code, no map and no personal data** (§5.3).

## 3. Flags

### 3.1 One flag per idea

- With flags on, when an idea is bet on, the engine creates its flag. **The flag's key is the idea's slug** (Q4). Every change in the bet that adds behaviour checks that flag.
- **Audiences**: off, internal, a percentage, everyone (glossary: Flag). "Internal" is a team setting: a list of targeting keys or an attribute rule (for example an email domain), kept in `physa serve`.
- **Percentages are deterministic**: a hash of the flag key and the person's targeting key, so a person stays in or out as the percentage widens.
- **An experiment is one flag with a variant per idea** (glossary: Experiment). The ideas share the flag; each variant belongs to its idea.

### 3.2 Who changes a flag

| Change | By | Bar |
|---|---|---|
| Widen the audience | a person, or the engine when the team's policy lets stakes at that exposure pass on their own (`two-bars`) | the exposing bar |
| Turn off | anyone, at once; the engine when a guardrail degrades (`guardrail-rollback`) | none: rollback is always allowed |
| Remove | a change built by a harness, proposed by decay (§7.3) | a sign-off on the proposal |

Every change to a flag is recorded with who and why, and turning off is a rollback (`rollback-evidence`).

### 3.3 Evaluation: OpenFeature, no SDK of our own

Physa writes **no SDK of its own** (Q2). It uses OpenFeature's:

- **Evaluation** goes through OpenFeature's standard SDKs with a Physa provider. `physa serve` speaks the **OpenFeature Remote Evaluation Protocol (OFREP)**, so the generic OFREP providers that already exist for the main languages work with it, and a Physa provider per language is a thin wrapper that adds caching and the event batching below.
- **Signal events** go through OpenFeature's **tracking API** (`track(event, context, details)`), which the Physa provider sends to `physa serve` in batches.
- **One SDK** therefore means the OpenFeature SDK the team already uses or adopts, so exposure and outcome join on the same targeting key, which is what `one-sdk` asked for.

Teams with a flag vendor (Q3):

- They keep the vendor's OpenFeature provider for evaluation.
- They add Physa's **OpenFeature hook**, which records exposure after each evaluation of a Physa-linked flag, and send `track` events to Physa alongside.
- Physa changes the vendor's flags through the vendor's API: one small driver per vendor, in the hosted layer. The open-source core drives only `physa serve`.

To verify before building: which languages have a working OFREP provider and support `track` today (Q2).

## 4. Signals

### 4.1 What a pitch names

A signal is named during shaping, before code (glossary: Signal). Three shapes cover what ideas claim:

| Shape | For | Example in a pitch |
|---|---|---|
| **Rate** | product and design ideas | `checkout_completed` per exposed person, expected **up** against the flag off |
| **Value** | product and design ideas | `time_to_first_invoice` per person, expected **down** |
| **Engineering** | architecture ideas | reverts, rework, defects or lead time on the changes behind the flag; error rate or latency from OpenTelemetry, expected **no worse** or **better** |

- A signal names its event (or engineering measure), what it is counted per (a person, a request), the direction it expects, and what it is compared with: the flag off, or the other variants of an experiment.
- **Engineering signals need no events in the app, and are always on** (`engineering-always-on`), with flags on or off. Git, PRs and CI give reverts, rework, defects and lead time (`engineering-evidence`); OpenTelemetry gives errors and latency, split by the flag through OpenTelemetry's feature flag attributes on spans, which the Physa hook sets.
- **A guardrail is a signal with "no worse" that never decays** while its capability lives (glossary: Guardrail).

### 4.2 Results, honestly

- **Rates are compared with a simple Bayesian model** (Q6): for each side, a beta distribution from exposed people and those who did the event. The result is the probability that the idea's side is better, with a range.
- **A result is clear** when that probability passes the team's threshold one way: 95% by default, set by each team or developer (Q6). Until then the result is "not enough flow to tell yet" (`cant-tell`), shown with how many more people would likely settle it.
- **Values** are compared by their medians, with a range from resampling; the same 95% rule.
- **Engineering signals** compare the idea's changes with the capability's recent changes: reverts, rework and defects per change; errors and latency per request, split by flag.
- No p-values, no significance language. One sentence a person can read: "Checkout completion is higher with the idea on (97% likely), from 1,840 people."

### 4.3 Judgment

- A named person can record an assessment on an idea: better, worse, or can't tell, with one line why (`named-judgment`).
- It counts as evidence with a smaller weight (§6.2), always shown as judgment and with the name.
- For design ideas and low-traffic products it may be most of the evidence there is. That is fine as long as it is labelled.

## 5. Events and their store

### 5.1 What an event is

```
{ event, idea, variant, key_hash, time, value? }
```

- Only events a live signal names are kept. `physa serve` drops any other `track` call and counts the drops, so a team sees tracking that no idea claims.
- `idea` and `variant` are filled by the provider from the evaluation it already did for that person.

### 5.2 Aggregated on arrival

- Events are folded into **daily counts per idea, variant and event** as they arrive. Raw events are kept 7 days for debugging, then dropped.
- Results read the daily counts, never raw events (`no-replay`).
- When an idea is established or withers, its events and counts are proposed for removal with it (`events-decay`).

### 5.3 Privacy

- The targeting key is hashed with a per-installation secret before it is stored. No names, emails, IPs or free text.
- `details` from `track` keep only a number (`value`); anything else is dropped.
- So `physa serve` holds nothing a team would need a data processing agreement for, beyond hashed keys.

## 6. Strength

### 6.0 Experimental and opt-in

- **Evidence is always recorded**, whether strength is on or not: every contribution in §6.2 is written as a fact (what happened, to which idea or capability, how much of it, when). It is small, and it is what lets strength be switched on later, or its settings tested, against real history (Q14).
- **Computing strength is opt-in** (Q13), per repo, while the model is experimental. Until it is on, nothing shows a strength number, and nothing that would follow from it happens on its own.
- **Without strength, everything else still works** (Q15):
  - stakes are worked out from exposure and how easily a change is undone, as if every capability were equally strong;
  - ideas are proposed established by the conditions in §7.1, which need no strength, and withered only by a clear result against them;
  - no decay proposals; the index and slices show facts (live flags, recent rollbacks) instead of strength;
  - the network view draws capabilities and ideas without thickness.

### 6.1 The rule

Strength follows the rule Physarum's tubes follow: **a tube thickens with the flow through it and thins at a steady rate when the flow stops** (Q8).

- Every piece of evidence is recorded once, when it happens, as a **contribution**: what it was, how much it adds (or takes away), and when.
- **Strength is the sum of all contributions, each decayed exponentially** by its age: `strength = Σ amount × ½^(age / half-life)`.
- **Stored as state, never replayed**: each idea and capability keeps a running value and the time it was last updated. A new contribution decays the running value to now and adds itself, which is exact for exponential decay. Old contribution rows expire with the store's retention (03 §4.2); the running value carries them.
- **Explainable**: the breakdown (which contributions, how much each still counts) is kept for the recent ones, so a person can always see why a tube is the thickness it is.
- Strength is never negative. An idea at zero with no flow coming in is withering.

### 6.2 Starting weights

These are starting numbers, in flow units. The team can tune them as policy; calibration (vision §7a) will propose changes from how they perform (Q9).

| Evidence | Amount |
|---|---|
| A person adds to an idea (a comment, an edit, a reaction) | +1 |
| A spike on the idea | +2 |
| A pitch written | +3 |
| A bet | +5 |
| A change landed | +1 |
| Exposure, per day the flag is on for anyone | + log₂(1 + people exposed that day) |
| A clear signal result for the idea | +20 |
| A clear signal result against it | −20 |
| Named judgment for / against | +5 / −5 |
| A slice that loaded the capability | +0.2 (capabilities) |
| A change anchored to the capability | +1 (capabilities) |
| Flag turned off as a rollback | −10 |
| A guardrail trip | −15 |
| A revert of the idea's changes | −15 |
| A decision reversed | −20 |
| A defect traced to the idea's changes | −5 |

- **Exposure is logarithmic**, so a busy product does not drown every other kind of evidence, and a small one can still grow its ideas.
- **Attention counts before code**, so an idea a PM, a designer and a developer keep returning to thickens before anything is built (`idea-is-tube`). Agents' activity counts only through what people did with it (a spike someone started, a pitch someone wrote).

### 6.3 Decay

- **Half-lives** (Q10): **14 days for ideas, 90 days for capabilities.** Appetite ignores time (03 Q16), but decay is about real use, which happens in calendar time however fast agents build.
- **Guardrails keep a capability's flow measured**: their events never decay while the capability lives, so a used capability keeps thickening and an unused one thins (`capabilities-fade`).
- A capability with no guardrail has no usage evidence, only engineering and citations. Its strength shows "usage unknown" rather than pretending (Q12).

### 6.4 Capabilities

A capability's strength is its own contributions (guardrail usage, anchored changes, slice loads) plus **the strength of the ideas established into it**, carried in when each idea is established (Q12). An established idea stops having strength of its own: it has become part of the capability.

### 6.5 Built to be tuned

The weights, decay and thresholds will be wrong at first and will differ between teams. So they are not code: they are **strength settings**, an advanced setting, and the computation is built around changing them safely (Q16).

- **One settings file, versioned.** It holds, per kind of evidence, a weight and a shape (fixed, per unit, logarithmic, capped); half-lives per idea and capability; the floor and durations of §7; the clear-result threshold of §4.2. The defaults in §6.2 and §6.3 are simply the first version.
- **Evidence and settings are separate.** Evidence facts never contain a weight. Strength is evidence passed through a settings version, and every strength value records which version produced it.
- **Changing settings recomputes**, once, from the evidence facts, as an explicit step the engine runs, never on a read (`no-replay`). The running values are then current again.
- **Preview before adopting**: `physa strength preview <settings>` shows how strengths and proposals would change against recent history (which ideas would wither, which would be established, which capabilities would thin). A team sees the effect of a change before making it.
- **Customizing means the settings, not code.** New kinds of evidence are added by Physa; teams change how much each counts and how fast it fades. If the model itself turns out wrong, a new model is a new settings version with a new shape, compared through preview with the old one.
- **The same computation everywhere.** The engine and the hosted layer run the same package over the same evidence and settings, so two people on a team see the same strength (where the settings live: Q17).

## 7. What strength proposes

With strength on (§6.0). Strength never moves anything by itself. It produces proposals a person accepts (`decay-proposes`), and they appear in Needs you of the idea's owner or the capability's steward.

### 7.1 Established

With flags off, an idea is **proposed established** once its bet's changes have landed with no revert for 14 days; a revert counts against it.

With flags on, an idea is **proposed established** (Q11) when all hold:

- its flag is on for everyone;
- its signal has a clear result for it, or a named judgment for it where the signal cannot tell;
- no rollback for 14 days.

Accepting it: the idea's strength is carried into its capability, and removing its flag is proposed (§7.3).

### 7.2 Withering

An idea is **proposed withered** when its strength has stayed under a floor (default 1 flow unit) for 14 days, or its signal has a clear result against it.

Accepting it: its lesson is written into the map as a decision with the idea as a rejected option (glossary: Idea), and removing its code and flag is proposed.

### 7.3 Removing flags and code

- A removal is a small **bet the engine proposes**, with a very small appetite: "remove the `bulk-invite` flag, keeping the on path" or "remove `bulk-invite`'s code and flag".
- A person bets on it as on any other (`people-bet`). The harness builds it; it goes through the same gates.
- A capability that keeps thinning is proposed for removal the same way, on the map update PR (`capabilities-fade`).

## 8. Where it lives

| What | Where |
|---|---|
| Flag state, audiences, signal events and daily counts | `physa serve` (one SQLite file), or the hosted layer |
| Contributions, running strength, proposals | the engine's store (03 §4) |
| Signal definitions | the pitch, in the engine's store; a guardrail's definition is a rule in the capability's map file |
| Results | computed by `physa serve` from daily counts; pulled by the engine |

Guardrails are the one part of signals that enters the map: they are promises an established capability makes, not experiments.

## 9. Settled with the owner (2026-11-20)

- **Q1–Q5** As proposed: `physa serve` next to production; no SDK of our own (OpenFeature with a thin Physa provider, OFREP and tracking; language coverage to verify); vendors kept with Physa's hook, vendor drivers in the hosted layer; one flag per idea keyed by its slug; three signal shapes.
- **Q6** Bayesian comparison, no p-values. The clear-result threshold is set by each team or developer, 95% by default (the owner: "that is for individuals/teams to decide").
- **Q7** Minimal events, untracked ones dropped and counted, raw events kept 7 days.
- **Q8** Strength as decaying contributions with a running value. The weights and decay "need to be carefully architected because it will definitely need to be tuned, and potentially customized by each team", as advanced settings (§6.5).
- **Q9–Q12** As proposed: the starting weights, half-lives of 14 and 90 days, established and withered as proposals, capability strength with "usage unknown".
- Strength is experimental and opt-in to begin with (the owner: "one of the core differentiators, and also a highly experimental feature … it should be opt-in to begin with").

- **Flags are opt-in, off by default**, with the product signals that need them. With flags off: landing is exposing (one bar); rollback starts at revert; no automatic rollback; an idea is proposed established once its bet's changes have landed with no revert for 14 days.
- **Engineering evidence is always on** (the owner: "it should be always on no matter what").
- **Ceremony is opt-in**: the map and the engine are the core; flags, product signals, strength, shaping and sign-offs are off for a solo developer; appetite and its circuit breaker stay on.

- **Q13** Strength is opt-in per repo while it is experimental.
- **Q14** Evidence is always recorded, even with strength off, and kept for a year.
- **Q15** With strength off, nothing shows a strength number: stakes from exposure and reversibility; no decay proposals; facts instead of thickness in the index, slices and network view.
- **Q16** Strength settings are one versioned file (weights with shapes, half-lives, thresholds); changing them recomputes once; `physa strength preview` before adopting.
- **Q17** The settings live in the repo only when a team changes a default, as `.physa/strength.toml`, reviewed in PRs; with no file, the defaults apply.
- **Q18** Strength's glossary entry says it is not a judgment of quality.
