# Slices · Rules

## Contents

- [Agents choose, the CLI assembles](#agents-choose-the-cli-assembles)
- [Same request, same slice](#same-request-same-slice)
- [Promises, overviews, rules and headings are always in a slice](#promises-overviews-rules-and-headings-are-always-in-a-slice)
- [Fast from day one](#fast-from-day-one)

## Entries

### Agents choose, the CLI assembles
`agent-chooses` · from docs/architecture/01-map-and-slice.md
The agent reads the index and names the capabilities it needs. The CLI assembles them and never matches or ranks.
Because: the agent knows its task; matching text to capabilities is flaky, and embeddings are heavy.

### Same request, same slice
`deterministic-slice` · from docs/architecture/01-map-and-slice.md
Everything in a slice except its current-state section depends only on the map and the request.
Because: agents and people must be able to trust and reproduce what was loaded.

### Promises, overviews, rules and headings are always in a slice
`never-cut` · from docs/architecture/09-capability-folders.md
Product rules, promises, overviews, rules, and the headings of decisions and questions are in every slice; the agent chooses the rest, and nothing is cut by size.
Because: they are what an agent most needs, and which decisions and questions bear on a task is the agent's judgment, not a size limit's.

### Fast from day one
`fifty-ms` · from docs/architecture/01-map-and-slice.md
`physa map index` and `physa map slice` answer in under 50 ms on a generated map of 100 capabilities, enforced by a test.
Because: Rhiza only got its speed target after the pain.
