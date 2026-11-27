---
promise: Physa's Go code stays easy to change: each iteration builds on the last without being held back by earlier choices.
stewards: [dphaener]
---

# Go architecture

Go architecture covers how Physa's Go code is shaped: package boundaries, interfaces, errors, concurrency, context, state, tests and the public API. Its rules come from the Go community's own guides, each citing them through docs/research/go-architecture.md; Physa's deliberate departures are its decisions. Rules a tool checks live in CI, not here. Its entries arrive with the pursuit go-architecture.
