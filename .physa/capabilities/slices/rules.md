# Slices · Rules

## Contents

- [Agents choose, the CLI assembles](#agents-choose-the-cli-assembles)
- [Same request, same slice](#same-request-same-slice)
- [Product rules and promises are never cut](#product-rules-and-promises-are-never-cut)
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

### Product rules and promises are never cut
`never-cut` · from docs/architecture/01-map-and-slice.md
When a slice is over its budget, product.md's decisions and questions go first, then rejected options, terms, questions, reasons and decisions. Product rules, promises and rules stay.
Because: they are what an agent most needs; if they alone don't fit, the map is too big.

### Fast from day one
`fifty-ms` · from docs/architecture/01-map-and-slice.md
`physa map index` and `physa map slice` answer in under 50 ms on a generated map of 100 capabilities, enforced by a test.
Because: Rhiza only got its speed target after the pain.
