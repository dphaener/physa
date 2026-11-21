# Physa · Rules

## Contents

- [Never write "ADR"](#never-write-adr)
- [Agents run on the customer's side](#agents-run-on-the-customers-side)
- [Source code never leaves the customer](#source-code-never-leaves-the-customer)
- [Nothing changes silently](#nothing-changes-silently)
- [Every claim cites its source](#every-claim-cites-its-source)
- [Opinionated about mechanisms, not policy](#opinionated-about-mechanisms-not-policy)
- [No internal IDs on screens](#no-internal-ids-on-screens)

## Entries

### Never write "ADR"
`no-adr` · from docs/vision.md
Never use "ADR" or "architecture decision record", in code, docs, commits or conversation. Say "decision".
Because: the owner rejects the term; one word for one thing.

### Agents run on the customer's side
`agents-customer-side` · from docs/vision.md
Physa never runs an agent of its own. Every agent step uses the customer's agent and keys.
Because: it keeps trust simple and costs low, and teams bring their own agents.

### Source code never leaves the customer
`source-stays` · from docs/vision.md
The hosted layer receives the map, PR metadata, records and signal events, never source.
Because: developer trust comes first, and code is the thing they guard most.

### Nothing changes silently
`no-silent-change` · from docs/vision.md
Physa proposes; people accept. Every change to the map, a flag or a policy arrives as something a person can accept or refuse.
Because: a tool that edits behind people's backs loses their trust.

### Every claim cites its source
`cite-sources` · from docs/vision.md
Every rule and decision names the change or document it comes from.
Because: people can check it, and stale knowledge can be found.

### Opinionated about mechanisms, not policy
`teams-set-policy` · from docs/vision.md
Physa decides how things work. Teams decide who may bet on what, how strict each level of stakes is, and how much agents may do.
Because: teams work differently, and the owner wants the core opinionated but the edges flexible.

### No internal IDs on screens
`no-ids-on-screens` · from docs/research/rhiza-v1-lessons.md
No surface a person sees shows internal identifiers.
Because: "it's nonsense to a user" (the owner, about Rhiza).
