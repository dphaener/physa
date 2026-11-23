# Architecture 10: appetite and cost

Draft 0.3, 2026-11-23; Q1–Q11 settled. Uses `.physa/glossary.md` terms only. Builds on 03 §10 and §10a, 02 §2.7 and 08 §7, and would replace them where they differ. Items marked (Q) are open questions for the owner.

An appetite is how much a team is willing to spend on a pitch. Today it is a count of tokens, and Physa counts them in several places that do not agree with each other or with what the spend costs. Two bets on 2026-11-22 and 2026-11-23 showed this:

- **The count isn't the cost.** The build of `llm-init-discovery` used 24.1M tokens counting cached input, or 1.6M without it. Cache reads are billed, but at a small share of the input price (5% on Claude Opus 5.5), writing to the cache costs more than plain input, output costs several times more, and the build used four models at different prices. Neither figure is what it cost.
- **Fixed caps inside a phase fail on large repos.** Discovery's cap of 100,000 tokens stopped it on nebari (6,108 files) before it named a single source, while 1.5M of the bet's 2m appetite went unspent. Propose's fixed input budget of 100,000 had cut 89% of nebari's docs the day before, with 79% of that appetite unspent.
- **Not every spend reaches the bet.** The engine can't start sessions yet (step 6), so the build's tokens and the acceptance run's tokens reach the bet only if someone reports them by hand.

## 1. Settled with the owner (2026-11-23)

- **Q1 An appetite is weighted tokens, shown with a cost estimate.** Each kind of token counts by its price relative to input, so a cache read counts for a small share of a token and output for several. The owner chose this "with a cost estimate (this is forward thinking to the UI where users can see something while setting an appetite)".
- **Q2 Everything done for the bet counts against it**: every session working on it, the agents those sessions start, its checks, reviews and fixes, and the runs that check its signal.
- **Q3 Phases share the one appetite, with no fixed caps.** A phase inside a run, like discovery, propose or recover in init, gets a share of the run's appetite, and what it leaves unspent passes on. A bigger appetite buys a deeper run.
- **Q4 A subscription counts the same as API pricing.** The appetite means the same on any plan; a plan's usage limits are a separate concern.
- **Q5 Prices come from the adapter, cached.** Each agent's adapter reports what its sessions cost, and Physa keeps the prices it works out from that, the newest it learned, so it can weigh spend and show a cost estimate before a run starts. Three details settled the same day:
  - **"Dollars as the measure."** The adapter reports each session's cost in dollars and its tokens by kind (uncached input, cache reads, 5-minute and 1-hour cache writes, output), per model. Physa ships no price table.
  - **"The default model's input."** One weighted token is worth the input price of the adapter's default model, so a session's weighted tokens are its dollars divided by that price.
  - **"Assume the published ratios."** Physa keeps a small table of the ratios between token kinds per model family and backs a model's input price out of a session's dollars: input price = dollars / (uncached input + cache reads × the read ratio + 5-minute cache writes × 1.25 + 1-hour cache writes × 2 + output × 5). The read ratio is 0.05 for Claude Opus 5.5 and Claude Sonnet 5.5, 0.025 for Claude Fable 5.x and Claude Mythos 5.x, and 0.1 for any other model. Each model's input price is cached machine-wide, in `spend.json` in Physa's settings directory, once worked out, newest wins.
- **Q6 Init is a special case: up front about its cost, never bounded.** The owner: "I think init is a special case. We should be up front about it but I don't think it should be bounded at all or we won't get good results." Init shows what the run is expected to cost before it spends, and has no appetite and no circuit breaker. This reopens `init-appetite` ("Init shows its budget first and stops at it") and the refusal #60 built, and replaces 02 Q4 for init.
- **Q7 Spend is shown in weighted tokens first, with the cost estimate beside it**, for example "2.1M tokens (about $28)", before a bet or run, while it runs and after, in the CLI and the views. The appetite's own unit leads; the dollars, from the adapter's cached prices, are labelled an estimate.
- **Q10 Init shows its estimate after gathering and waits for a yes.** Once gather and discovery have run, init estimates the whole run in weighted tokens and dollars and asks to go on, on a terminal; off one it needs an explicit flag to go on without asking. The person sees the cost before the large spend, at the price of stopping a run nobody is watching.
- **Q9 A model with no price yet counts at raw tokens, with no cost shown.** Until its adapter reports a price, every token of that model's sessions counts at weight 1 and no dollar figure is given for it; the figure is weighed once a price arrives.
- **Q10a Init estimates recover from a measured ratio and warns, never stops, when it runs over.** The estimate is the sources' size times a ratio Physa measures from past init runs (on nebari's run 9, recovery spent about 12.5 weighted tokens for each estimated token of its sources, §3), shown as a range. A run that passes the top of the range says so in its output and goes on, because init is unbounded (Q6).
- **Q11 Learning keeps a default appetite; a spike's is always set by its person.** Learning runs unattended, so it stays bounded, with a default tuned from its own runs. A spike has no default: the person sets it when it starts. (The owner prefers the word "experiment" to "spike"; the glossary is unchanged until they settle it.)
- **Q8 Opting out of an appetite removes the circuit breaker, never the count.** A bet or run with no appetite has no 80% warning and no breaker, and its spend is still recorded and shown, so evidence and learning keep their data. The owner first raised it: "I think maybe appetite can be opt out as well." This changes the vision's "appetite … always on" (vision §11, 05) to "spend always counted".

Already settled elsewhere, and kept unless a question below reopens it:

- **Appetite is a budget, never an estimate**, set by a person when they bet (glossary).
- **At 80% the person who made the bet is told; at 100% the circuit breaker stops it** and the bet goes back to shaping. It is never extended automatically (03 Q12, `appetite-tokens`).
- **Time plays no part** (03 Q16).
- **A spike has its own very small appetite** (03 Q18).
- **Learning spends under an appetite** shown before it starts (08). Init did too (02 Q4) until Q6.
- **Budgets are tuned from run data** (`measure-before-budget`).

## 2. Where Physa counts spend today

| Where | What is counted | The limit |
|---|---|---|
| A bet (03 §10a) | tokens a session reports through `bet.spend` | the bet's appetite; 80% tells, 100% stops |
| `physa init` (02 §2.7) | weighted tokens from each session's dollars, raw columns kept | none (Q6); an estimate after gathering, and a yes before going on (Q10, Q10a) |
| Discovery (02 §2.2) | the same | none, though it spends before the yes |
| Propose (02 §2.3) | the same | none; its sources read whole (#60) |
| Recover (02 §2.4) | the same | none; at most three sessions at once |
| Learning (08 §7) | the same | `--appetite` per run |
| The reviewer (02 §3) | prints its tokens | none |

## 3. What init cost on a real repo

Init on nebari (6,108 files, 100 PRs, 490 changes) on 2026-11-23, with nothing cut (run 7, the code merged in #59–#62): about 6.77M tokens counted raw (discovery 74,135; propose 682,461 in, 6,608 out; recover 5,876,053 in, 303,714 of them cached, 133,092 out). At Claude Opus 5.5's list prices ($4 a million input, $0.20 cached, $20 output) this was first put at roughly $28: about $25 of uncached input, mostly recover's 18 sessions, and $3 of output. That counted every uncached input token at 1×, and most of them were not plain input: Claude Code writes its prompt cache with a 1-hour lifetime, at twice the input price, and init's sessions are one turn, so nearly all of their input is written to the cache (run 8's propose session wrote 716,749 tokens, all 1-hour, and read 649). With those writes at 2× the run cost nearer twice $28. Init's one-turn sessions pay Claude Code's 1-hour cache-write premium on prompts that are never read again, which is about half of what init costs. The owner chose to record this, not change it (2026-11-23). The default appetite of 300,000 would have refused before proposing.

Run 9 (2026-11-23) measured it in weighted tokens: 6.67M raw tokens weighed 14M (about $56): discovery 32k, propose 1.4M and recover 12.5M. Its estimate had priced propose's prompt and the sources × a raw ratio as plain input and said "about 3.7M to 9.6M tokens (about $15 to $38)". The estimate now works in weighted terms, measured, never assumed: each finished run records recovery's weighted tokens ÷ gather's estimated sources (run 9: 12.5M ÷ 998k ≈ 12.5) and propose's weighted ÷ raw tokens (run 9: 1.4M ÷ 682,510 ≈ 2.05), and until two runs are recorded init starts from those, with ×0.5 to ×1.5 around 12.5 (Q10a). The new ratios are kept under a new key in `spend.json`; those recorded in raw tokens before are ignored, and dropped when the cache is next saved.

## 4. Open questions

None. What follows from §1 to change: `appetite-tokens` and 03 §10 (weighted tokens, the opt-out), `init-appetite`, 02 §2.7 and #60's refusal (Q6, Q10, Q10a), the glossary's Appetite (weighted tokens) and Circuit breaker (opting out), and the vision's "appetite … always on" (Q8).

Done so far:

- **Weighing spend (Q1, Q5, Q7, Q9).** The adapter reports each session's tokens by kind and dollars per model, and the `spend` package backs out and caches input prices, weighs sessions and shows "2.1M tokens (about $28)".
- **Init (Q6, Q10, Q10a).** Init has no appetite, no circuit breaker and no `--appetite`, in a full run and in each phase alone, and #60's refusal and reservations are gone. A full run estimates the rest of the run after gathering, from a ratio it records after each run, and asks before going on, or needs `--yes` off a terminal; it warns once when it passes the estimate. `init-appetite` and `init-no-cut` now say so, and 02 §2.2–§2.7 with them.

Still to change: `appetite-tokens` and 03 §10, the glossary's Appetite and Circuit breaker (the owner's to change), the vision's "appetite … always on", and learning's spend line (Q11 keeps its appetite and breaker).
