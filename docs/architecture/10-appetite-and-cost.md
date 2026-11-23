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
- **Q5 Prices come from the adapter, cached.** Each agent's adapter reports the prices of the models its sessions use, and Physa keeps the last it was told, so it can weigh spend and show a cost estimate before a run starts.
- **Q6 Init is a special case: up front about its cost, never bounded.** The owner: "I think init is a special case. We should be up front about it but I don't think it should be bounded at all or we won't get good results." Init shows what the run is expected to cost before it spends, and has no appetite and no circuit breaker. This reopens `init-appetite` ("Init shows its budget first and stops at it") and the refusal #60 built, and replaces 02 Q4 for init.
- **Q7 Spend is shown in weighted tokens first, with the cost estimate beside it**, for example "2.1M tokens (about $28)", before a bet or run, while it runs and after, in the CLI and the views. The appetite's own unit leads; the dollars, from the adapter's cached prices, are labelled an estimate.
- **Q10 Init shows its estimate after gathering and waits for a yes.** Once gather and discovery have run, init estimates the whole run in weighted tokens and dollars and asks to go on, on a terminal; off one it needs an explicit flag to go on without asking. The person sees the cost before the large spend, at the price of stopping a run nobody is watching.
- **Q9 A model with no price yet counts at raw tokens, with no cost shown.** Until its adapter reports a price, every token of that model's sessions counts at weight 1 and no dollar figure is given for it; the figure is weighed once a price arrives.
- **Q10a Init estimates recover from a measured ratio and warns, never stops, when it runs over.** The estimate is the sources' size times a ratio Physa measures from past init runs (on nebari, recover read about 8 times its sources), shown as a range. A run that passes the top of the range says so in its output and goes on, because init is unbounded (Q6).
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
| `physa init` (02 §2.7) | input, cached input and output, all at full weight | `--appetite`, default 300,000; a phase that does not fit what is left is not started, and init names the appetite it needs |
| Discovery (02 §2.2) | the same | a quarter of the appetite (100,000 with `--gather` alone) |
| Propose (02 §2.3) | the same | what discovery leaves; its sources read whole (#60) |
| Recover (02 §2.4) | the same | what is left after both; every capability's first turn reserved before any retry (#60) |
| Learning (08 §7) | the same | `--appetite` per run |
| The reviewer (02 §3) | prints its tokens | none |

## 3. What init cost on a real repo

Init on nebari (6,108 files, 100 PRs, 490 changes) on 2026-11-23, with nothing cut (run 7, the code merged in #59–#62): about 6.77M tokens counted raw (discovery 74,135; propose 682,461 in, 6,608 out; recover 5,876,053 in, 303,714 of them cached, 133,092 out). At Claude Opus 5.5's list prices ($4 a million input, $0.20 cached, $20 output) that is roughly $28: about $25 of uncached input, mostly recover's 18 sessions, and $3 of output. The default appetite of 300,000 would have refused before proposing.

## 4. Open questions

None. What follows from §1 to change: `appetite-tokens` and 03 §10 (weighted tokens, the opt-out), `init-appetite`, 02 §2.7 and #60's refusal (Q6, Q10, Q10a), the glossary's Appetite (weighted tokens) and Circuit breaker (opting out), and the vision's "appetite … always on" (Q8).
