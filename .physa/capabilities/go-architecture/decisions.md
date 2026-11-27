# Go architecture · Decisions

## Contents

- [The 800-line limit stays, and a split follows a package's purpose](#the-800-line-limit-stays-and-a-split-follows-a-packages-purpose)
- [Version control, the forge and the agent adapter stay shared ports](#version-control-the-forge-and-the-agent-adapter-stay-shared-ports)
- [The map never imports the engine](#the-map-never-imports-the-engine)
- [Tools check what they can, and the map holds the rest](#tools-check-what-they-can-and-the-map-holds-the-rest)
- [The small rules are fixed as nearby code is touched](#the-small-rules-are-fixed-as-nearby-code-is-touched)

## Entries

### The 800-line limit stays, and a split follows a package's purpose
`line-limit-stays` · decided by dphaener, accepting a proposal · 2026-11-27 · from docs/research/go-architecture.md
Every package stays under 800 non-blank lines of non-test Go. A package that outgrows the limit is split along its purposes, never at a line count.
Because: the limit has kept packages readable, though the community's guides set no size limit; splits made by count left recovery in four pieces and one package a helper eleven others import.
Rejected:
- Dropping the limit: the community sets none, but the limit stops god-packages.
- Splitting by line count: it cuts one purpose into pieces and turns packages into helpers.

### Version control, the forge and the agent adapter stay shared ports
`shared-ports` · decided by dphaener, accepting a proposal · 2026-11-27 · from docs/research/go-architecture.md
These three are each one shared interface with adapters beneath it, used by every consumer, instead of each consumer declaring its own. They are the exception to small interfaces declared by their users.
Because: a port with adapters beneath it is a recognised school, Ben Johnson's, and it keeps the promise that nothing reads version control, the forge or an agent except through it.
Rejected:
- Each consumer declaring its own interface for these too: the promise of one way in would rest on every consumer's care.
- A root package of domain interfaces: it puts every contract in one package that everything imports.

### The map never imports the engine
`map-never-imports-engine` · decided by dphaener, accepting a proposal · 2026-11-27 · from docs/research/go-architecture.md
No package on the map's side, including the foundations both use, imports a package on the engine's side; the engine may import the map's. Codecheck enforces it.
Because: the map is a product usable alone, in one binary with the engine.
Rejected:
- Dependencies that only point one way, which is all the guides ask: the map's side could then reach into the engine whenever no cycle formed.

### Tools check what they can, and the map holds the rest
`tools-check-first` · decided by dphaener, accepting a proposal · 2026-11-27 · from docs/research/go-architecture.md
Staticcheck and a small golangci-lint set (errcheck with fmt.Fprint and deferred Close excluded, errorlint, unused) join vet, the race detector and codecheck in CI. The map keeps only rules a tool cannot check.
Because: a tool enforces its rules on every change without anyone reading them, and the map's rules stay few enough to read in full.
Rejected:
- The broad golangci-lint set: it gave 3,015 findings, mostly noise.
- The parallel-test linter: no test can run in parallel until the swapped package variables are gone.
- The external-test-package linter: it contradicts tests living in the package they test.

### The small rules are fixed as nearby code is touched
`small-rules-as-touched` · decided by dphaener, accepting a proposal · 2026-11-27 · from docs/research/go-architecture.md
Six small rules are fixed in code a change already touches, never in a sweep: package names say what they provide, exported structs embed only what they are, no struct keeps a context, the logger is passed in, no error is discarded silently, and no dead code stays.
Because: no sweep PR; the fixes ride with the changes that touch the code, so each is small and reviewed beside its context.
Rejected:
- A sweep change: one large diff across every package, hard to review and to revert, for fixes that change no behaviour.
