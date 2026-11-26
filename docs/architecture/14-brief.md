# Architecture 14: the brief

Draft 0.1, 2026-11-26; proposed by Myxo for the owner to review. Settled so far: design comes before pursuing, teams bring their own design workflow, what it hands Physa is a brief, Brief is a glossary term, and the plan is optional in it. The rest is open (§8). Uses `.physa/glossary.md` terms only. Serves the owner's idea `design-brief`.

The owner, on where design sits: "design before pursue. We may abandon an idea during design." Then: "design may include spiking/grilling/etc. And the outcome of that, if pursuit is decided, should flow into pursue. The tricky part is that teams will bring their own design workflow and the output needs to be constrained in some form that can flow into pursue."

Today design happens outside Physa. A team works out an idea in chat, docs or PRs, makes its calls, then a person pursues and types the signal again, and the calls reach the map only if someone writes them in. Physa sees the idea and the pursuit, and nothing of what decided between them.

## 1. The line

Design stays beyond the line (vision §5): how a team designs, with spikes, grilling, documents or none, is its own workflow, and Physa prescribes none of it. What Physa defines is what a design hands it: a **brief**, in one shape whatever the workflow. A harness is the precedent: teams bring any workflow of agents, and the engine sees only the kinds of role and findings in one shape (vision §9).

The brief is not a pitch. Pitches were dropped (`pursue-against-signal`) because they prescribed how a team thinks, in seven parts before a bet. A brief requires only what a pursuit already requires, the signal, so a pursuit still needs nothing else. Everything else in it is optional, and is there because Physa acts on it.

## 2. What a brief holds

| Part | Required | Shape | What Physa does with it |
|---|---|---|---|
| Signal | yes | the four parts a pursuit needs: claim, direction, window, resolver | pursue takes it (§4) |
| Appetite | no | tokens, as `physa pursue --appetite` | pursue takes it |
| Decisions | no | the map's decision entries: heading, slug, the choice, `Because:`, who decided, `Rejected:` | held with the idea, and proposed to the map when the changes that make them true land (§5) |
| Questions | no | the map's question entries | the same as decisions |
| Changes | no | the plan's changes: name, capability, what, acceptance, must not, after | pursue sets them as the plan's first revision |

Decisions and questions use the map's own entry format, so they move into the map unchanged and `physa check` can check them in the brief as it checks them in the map. A decision in a brief needs who decided, as one in the map does; an agent's call made on a person's behalf says so, as it does everywhere else.

A brief belongs to its idea, in the engine's store, never in the map (`ideas-not-in-map`). An idea has at most one current brief; a new one is the next revision, and the history is kept, as with plans.

## 3. Recording a brief

`physa idea brief <idea> --from <file>|-` records the brief as the idea's next revision. It refuses one that fails its checks and lists every problem, as `physa pursuit plan set` does. Agents may record a brief: it decides nothing on its own. `physa idea show` shows the current brief, and `--history` the revisions.

A team's design workflow writes the brief however it likes (an agent at the end of a grilling session, a script reading a design doc) and records it with this command or the engine's API. When the idea belongs to someone else, its person sees in Needs you that a brief is ready.

## 4. Pursuing from a brief

`physa pursue <idea>` on an idea with a brief shows the brief's signal and appetite, and asks the person to confirm or edit them before it starts. When the brief has changes, the pursuit starts with them as its plan's first revision. The person still pursues: an agent's session is still refused (`people-pursue`). The brief only means they confirm instead of retype.

An idea without a brief is pursued as today.

## 5. Decisions reaching the map

A brief's decisions and questions are not knowledge until code makes them true: the map holds only what is true (vision §4). They wait with the idea.

When a change of the pursuit lands, learning (08) reads the brief and proposes the decisions and questions that change made true, in the capability the change names, anchored to it. A decision about a part not yet built waits for the change that builds it. A steward accepts or refuses each proposal, as with every learning proposal. This is a new learning signal, read from the store, like the bet's pitch was before the reset (08 §3):

| Signal | From | Proposes |
|---|---|---|
| A landed change of a pursuit whose idea has a brief | the store | the brief's decisions and questions that change made true |

Which decisions a change made true is the learning agent's judgment, from the change's plan entry and its diff's capability; Physa assembles, the agent judges.

## 6. When design abandons an idea

An idea abandoned during design keeps its brief. Nothing goes to the map: no change made it true. When the idea withers, its lesson stays as vision §4 says, a decision with the idea as a rejected option, and the brief is where that lesson's reason comes from.

## 7. The glossary and the vision

Brief joins the glossary:

> **Brief**: What a team's design work hands Physa for an idea, in one shape whatever the workflow: the signal it would be pursued against, and optionally an appetite, the decisions made and the questions still open, and the planned changes. A pursuit started from a brief takes its signal, and its changes as the plan.
> _Avoid_: pitch, spec, PRD, design doc

Pursuit's entry ends "How the team got from idea to pursuit is its own workflow, not Physa's"; it gains "; what that workflow hands Physa is a brief".

The vision changes in three places: §5 says the write-up before a pursuit is workflow a team may add, and what it hands Physa is a brief; §9's "check briefs built by the engine" become "check prompts", so the word has one meaning; and the flow from idea to landed change gains the brief.

## 8. Questions for the owner

Each has a recommended answer, worded so "yes" accepts it.

1. **Only the signal is required**, and the rest is optional, so a pursuit still needs only a signal. Yes?
2. **Decisions and questions use the map's entry format**, checked by `physa check` in the brief. Yes?
3. **`physa idea brief <idea> --from`**, agents allowed, revisions kept, refused with every problem listed. Yes?
4. **Pursuing from a brief** shows its signal to confirm or edit, and sets its changes as the plan. Yes?
5. **Decisions reach the map through learning** when the changes that make them true land, as proposals a steward accepts. Yes?
6. **This idea's signal**: claim, ideas that are designed reach their pursuit and the map without anyone retyping them; for, pursuits started from a brief, and decisions in the map anchored to changes that came from one; against, pursuits typed by hand after design, or brief decisions that never reach the map; window 2027-03-01; resolver, Darin, reading the store and the map's decisions. Yes?
