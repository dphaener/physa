# Go architecture: sources and candidate rules

Research for the go-architecture capability, 2026-11-27, done by Myxo for the owner, who settled every decision ("yes to all"). The rules come from the Go community's guides below; the assessment of Physa's code against them graded it B.


The community guidance behind `candidates.md`, read on 2026-11-27 with a generic client. Each entry says who owns it and how much weight it carries. The ranking is mine, not the sources'.

Authority, highest first:

1. **Go team, official.** Published on go.dev by the Go project. This is as close to "the standard" as Go gets.
2. **Large-org style guides.** Google's and Uber's guides. Many engineers maintain them and much code follows them, but they set house style for those companies' codebases. They are not Go-wide rules.
3. **Respected individuals.** Talks and essays by well-known Go people. Influential, but one person's view.

## Official (Go team)

### Effective Go
- URL: https://go.dev/doc/effective_go
- Owner: the Go project (go.dev).
- Authority: official, and foundational, but dated. The page says it was written for Go's 2009 release and is not actively updated: it covers neither modules nor generics. I used it for interfaces, errors, panics, concurrency and `init`.

### Go Code Review Comments
- URL: https://go.dev/wiki/CodeReviewComments
- Owner: the Go project wiki. The Go team curates it as a supplement to Effective Go.
- Authority: official, and the most-cited list of review rules. It is short and direct, and its rules on interfaces, contexts and goroutine lifetimes carry the most weight of any source here.

### Organizing a Go module
- URL: https://go.dev/doc/modules/layout
- Owner: the Go project (go.dev docs).
- Authority: official, and the only official word on directory layout (`cmd/`, `internal/`). It is deliberately thin: it describes conventions and doesn't prescribe a package structure.

### Data Race Detector
- URL: https://go.dev/doc/articles/race_detector
- Owner: the Go project.
- Authority: official. I used it only for `go test -race`.

### Go Proverbs
- URL: https://go-proverbs.github.io/
- Owner: the site collects Rob Pike's Gopherfest SV 2015 talk. Pike is a co-creator of Go, but the site is a community transcription, not go.dev.
- Authority: high in spirit, low in precision. They are slogans, not rules. Several of them anchor rules here ("The bigger the interface, the weaker the abstraction", "Errors are values", "Don't panic").

## Large-org style guides

### Google Go Style Guide (Guide, Decisions, Best Practices)
- URL: https://google.github.io/styleguide/go/ (with `/decisions` and `/best-practices`)
- Owner: Google, open source in the google/styleguide repo. The page doesn't name an individual owner.
- Authority: high, the most thorough guide there is. The index gives each document a different weight: the Style Guide is "normative" and "canonical", Style Decisions is normative but not canonical, and Best Practices is neither ("These best practices are not canonical, but Go programmers at Google are encouraged to use them where possible"). I used Decisions for contexts, goroutines, errors and tests, and Best Practices for global state, interfaces, error wrapping and logging.
- Caveat: the two long pages were truncated in reading. I read each in two passes, but a few sections (flags in libraries, `t.Fatal` from goroutines) didn't come through, so no rule here cites them.

### Uber Go Style Guide
- URL: https://github.com/uber-go/guide/blob/master/style.md
- Owner: Uber, maintained in the uber-go/guide repo.
- Authority: high in industry and widely copied. It is more prescriptive than Google's, and some rules are Uber-specific (it recommends its own `go.uber.org/atomic` and uses testify in its examples). I read it whole from the raw file. I used it for error types, handling errors once, goroutine lifetimes, globals, `init`, exits, embedding, channel sizes, test tables and its linter list.

## Individuals

### Dave Cheney, Practical Go (QCon China 2019 workshop notes)
- URL: https://dave.cheney.net/practical-go/presentations/qcon-china.html
- Owner: Dave Cheney, a long-time Go contributor and speaker.
- Authority: influential and opinionated. It disagrees with the others in places (it prefers fewer, larger packages, and internal tests), and I note those spots in the candidates.

### Ben Johnson, Standard Package Layout (2016)
- URL: https://www.gobeyond.dev/standard-package-layout/ (the old gobeyond.io address no longer resolves, and the Medium original returns 403). I also used the layout summary in his worked example, https://github.com/benbjohnson/wtf (README).
- Owner: Ben Johnson, author of BoltDB and Litestream.
- Authority: an influential essay on application layout. Its four tenets are one school of thought, not consensus: the root package holds domain types and interfaces, subpackages are grouped by dependency, there is a shared mock package, and main wires it all. It partly contradicts Code Review Comments on where interfaces live and on mocks.

## Not used

### golang-standards/project-layout
- URL: https://github.com/golang-standards/project-layout
- Why not: despite the name, it is not a Go standard. It's a community repo with no tie to the Go team. Russ Cox, then Go's tech lead, said so in its issue tracker (issue #117), and the official layout page above now covers the same ground. Its `pkg/`, `api/`, `web/` and similar folders are one group's habits, and Go doesn't need most of them. Citing it would launder an opinion as a standard. (I'm citing the Cox issue from memory and didn't fetch it again.)

### Linters as sources
Where `candidates.md` names a tool, I'm the source: go vet, staticcheck and golangci-lint's linters are named from their documentation as I know it, and I checked them by running them on Physa (see `assessment.md`). Uber's guide recommends a linter base set of errcheck, goimports, revive, govet and staticcheck, run through golangci-lint.


There are 40 candidates, architecture-level only. gofmt and the linters own formatting and naming nits, so they aren't here. Each candidate has a one-line rule, the source's reason in its own words, a tool that can check it if one exists, and both sides where sources disagree. Quotes are short and verbatim from the pages listed in `sources.md`. Where a rule or a reason is my inference rather than a source's, the entry says so (**Myxo's**).

These are candidates for the owner to accept, change or drop. None is settled.

Short names for sources: **EG** Effective Go, **CRC** Code Review Comments, **Layout** Organizing a Go module, **Race** the race detector article, **Proverbs** Go Proverbs, **G-Dec** and **G-BP** Google Decisions and Best Practices, **Uber** Uber guide, **Cheney** Practical Go, **BJ** Ben Johnson.

---

## Packages and boundaries

### P1. Name a package for what it provides, never a grab-bag name
- Why: "Avoid meaningless package names like util, common, misc, api, types, and interfaces." (CRC). "Name your package for what it provides, not what it contains." (Cheney)
- Sources: CRC https://go.dev/wiki/CodeReviewComments#package-names; G-BP "Util packages" https://google.github.io/styleguide/go/best-practices; Cheney §4.1.
- Tool: partly. revive's `var-naming` flags some meaningless package names; a name that hides a grab-bag needs a reader.

### P2. A package has one cohesive job; split on cohesion, not on size
- Why: "prefer multiple packages, each focused on a single aspect, to a single monolithic package." (Cheney). "Larger packages or commands may benefit from splitting off some functionality into supporting packages." (Layout)
- Sources: Cheney §4.2; Layout https://go.dev/doc/modules/layout.
- Tool: no.
- **Disagreement.** Cheney, in the same talk: "The advice I find myself repeating is to prefer fewer, larger packages." Ben Johnson calls one monolithic package fine for small apps but says beyond about 10K lines "it gets extremely difficult to navigate the code". Nobody official sets a size limit. A line budget, like Physa's 800, is a house rule, not a community one.

### P3. Group by what depends on what, not by layer or by kind
- Why: grouping by layer, Rails-style, "only works if you have one-way dependencies but many times your application is not that simple." (BJ)
- Source: BJ, "Group subpackages by dependency", https://www.gobeyond.dev/standard-package-layout/.
- Tool: no.

### P4. Everything not meant for other modules lives under `internal/`
- Why: "It's recommended to keep packages in `internal` as much as possible." Then "we're free to refactor its API and generally move things around without breaking external users." (Layout)
- Source: Layout.
- Tool: the compiler enforces `internal/` visibility; which packages *should* be public needs a reader.

### P5. A public package never exposes types from `internal/`
- Why (**Myxo's**, from Layout's reasoning): an outside module can't import `internal/`, so it can read an exported field of an internal type but can't name, build or declare that type. The public API then quietly depends on code Layout says you're "free to refactor".
- Tool: no standard linter. A small custom check can do it: walk the public package's exported API and flag any `internal/` type.

### P6. Dependencies point one way: foundations import nothing above them, and `main` wires the pieces
- Why: "The root package should not depend on any other package in your application!" and "Main package ties together dependencies" (BJ). His subpackages "communicate solely through our common domain language."
- Sources: BJ tenets 1 and 4; Uber "Exit in Main" puts process control in `main` alone.
- Tool: golangci-lint `depguard` (import allow/deny lists), or a custom checker like Physa's `tools/codecheck`.

### P7. Every package has a doc comment, adjacent to the package clause, saying what it is for
- Why: "Package comments, like all comments to be presented by godoc, must appear adjacent to the package clause" (CRC). "Documentation is for users." (Proverbs)
- Source: CRC "Package Comments".
- Tool: staticcheck `ST1000` (not on by default), revive `package-comments`.

## Interfaces

### I1. Define an interface in the package that consumes it, not the one that implements it
- Why: "Go interfaces generally belong in the package that uses values of the interface type, not the package that implements those values." (CRC). "The consumer of the interface should define it" (G-Dec).
- Sources: CRC "Interfaces"; G-BP "Interface ownership and visibility".
- Tool: no.
- **Disagreement.** Ben Johnson puts domain interfaces in the root package as shared contracts ("a `wtf.DialService` interface... and SQLite does the actual implementation"). Effective Go allows a constructor to return an interface when "a type exists only to implement an interface". So a shared port interface in a neutral package is a recognised school. It is not the default.

### I2. Return concrete types; don't add an interface before a second use exists, and never just to mock
- Why: "The implementing package should return concrete (usually pointer or struct) types: that way, new methods can be added to implementations without requiring extensive refactoring." "Do not define interfaces before they are used" (CRC).
- Source: CRC "Interfaces"; G-BP "Avoid unnecessary interfaces".
- Tool: partly. golangci-lint `ireturn` flags functions that return interfaces.

### I3. Keep interfaces small: one to a few methods
- Why: "The bigger the interface, the weaker the abstraction." (Proverbs). "Interfaces with only one or two methods are common in Go code" (EG). "The larger the interface, the harder it is to implement" (G-BP).
- Tool: golangci-lint `interfacebloat` (default limit 10 methods).

### I4. Check interface compliance at compile time only where nothing else would
- Why: "Verify interface compliance at compile time where appropriate." (Uber)
- **Disagreement, mild.** Effective Go: "Don't do this for every type that satisfies an interface, though... such declarations are only used when there are no static conversions already present". Uber wants it more often than Effective Go does.
- Tool: no.

### I5. Don't embed types in exported structs unless the struct truly *is* the embedded thing
- Why: "These embedded types leak implementation details, inhibit type evolution, and obscure documentation." (Uber)
- Source: Uber "Avoid Embedding Types in Public Structs". Effective Go presents embedding neutrally, as "a simple convenience".
- Tool: no.

## Errors

### E1. Return errors; panic only on programmer error or a setup that cannot proceed
- Why: "real library functions should avoid `panic`" with one counterexample, "during initialization" (EG). "Don't use panic for normal error handling." (CRC). "Panic/recover is not an error handling strategy." (Uber)
- Sources: EG "Panic"; CRC "Don't Panic"; G-Dec "Don't panic", which allows `MustXYZ` for setup.
- Tool: partly. golangci-lint `forbidigo` set to `panic` flags every call for review.

### E2. Never discard an error silently
- Why: "this is terrible practice. Always check error returns" (EG). "Do not discard errors using `_` variables." (CRC)
- Tool: golangci-lint `errcheck` (and go vet for a few cases). Teams usually exclude `fmt.Fprint*` and deferred `Close`.

### E3. Wrap with context; `%w` when callers may need the cause, `%v` when they must not
- Why: "Use `%w` if the caller should have access to the underlying error. Use `%v` to obfuscate the underlying error." "Return the original error as-is if there is no additional context to add." (Uber). Google: "the message should encode the action that failed".
- Sources: Uber "Error Wrapping"; G-BP "Adding information to errors".
- Tool: golangci-lint `errorlint` flags a non-wrapping verb on an error (which is right only if you didn't mean `%v`), and `wrapcheck` flags errors returned unwrapped across package lines. wrapcheck is noisy.

### E4. Choose the error's shape by whether callers match it
- Why: Uber's table: no matching, use `errors.New` or `fmt.Errorf`; matching a static message, use a top-level `var` with `errors.New`; matching with data, use a custom type. Google: "A sentinel error is an error that serves as a primary categorization of a failure."
- Sources: Uber "Error Types"; G-BP "Error handling".
- Tool: golangci-lint `errname` checks names (`ErrX` and `XError`). `err113` pushes toward sentinels and is noisy.

### E5. Match errors with `errors.Is` and `errors.As`, never by their text
- Why: "Do not attempt to distinguish errors based on their string form." (G-BP)
- Tool: golangci-lint `errorlint` catches `==` and type assertions on errors. No linter catches `strings.Contains(err.Error(), ...)`.

### E6. Handle each error once: decide, or return it, but don't log it and return it too
- Why: "it should typically handle each error only once." (Uber). "If you return an error, it's usually better not to log it yourself but rather let the caller handle it." (G-BP). "Handling an error means inspecting the error value, and making a single decision." (Cheney)
- Tool: no.

### E7. Only `main` exits the process
- Why: "Call one of `os.Exit` or `log.Fatal*` only in `main()`. All other functions should return errors to signal failure." (Uber). "Program initialization errors (such as bad flags and configuration) should be propagated upward to `main`" (G-BP).
- Tool: revive `deep-exit`.

## Concurrency

### C1. Every goroutine has an owner, a known exit, and someone who waits for it
- Why: "When you spawn goroutines, make it clear when - or whether - they exit." "If that just isn't feasible, document when and why the goroutines exit." (CRC). "Given a goroutine spawned by the system, there must be a way to wait for the goroutine to exit." (Uber). "Never start a goroutine without knowning when it will stop" [sic] (Cheney).
- Sources: CRC "Goroutine Lifetimes"; G-Dec "Goroutine lifetimes"; Uber "Don't fire-and-forget goroutines"; Cheney §8.3. Uber adds "`init()` functions should not spawn goroutines."
- Tool: no static check. `go.uber.org/goleak` catches leaks in tests.

### C2. Prefer synchronous APIs, and leave concurrency to the caller
- Why: "Prefer synchronous functions... over asynchronous ones." "Synchronous functions keep goroutines localized within a call" (CRC). "If your function starts a goroutine you must provide the caller with a way to explicitly stop that goroutine." (Cheney)
- Tool: no.

### C3. Channels are unbuffered or size one; any other size is justified in a comment
- Why: "Channels should usually have a size of one or be unbuffered... Any other size must be subject to a high level of scrutiny." (Uber)
- Tool: no.

### C4. State shared across goroutines has one guard, and CI runs the race detector
- Why: "Do not communicate by sharing memory; instead, share memory by communicating." (EG), and "Channels orchestrate; mutexes serialize." (Proverbs). "run your tests using the race detector (`go test -race`). The race detector only finds races that happen at runtime" (Race).
- Tool: `go test -race`; go vet `copylocks` catches copied mutexes.

## Context

### X1. Thread `ctx` as the first parameter through every call that blocks or does I/O; `context.Background()` belongs at the top
- Why: "Go programs pass Contexts explicitly along the entire function call chain". "The default case is to pass a Context; only use context.Background() directly if you have a good reason why the alternative is a mistake." (CRC)
- Sources: CRC "Contexts"; G-Dec "Contexts".
- Tool: golangci-lint `contextcheck` (a function that makes a fresh context while one is available) and `noctx` (`exec.Command`, `db.Query` and the like without a context).

### X2. Don't store a context in a struct
- Why: "Don't add a Context member to a struct type; instead add a ctx parameter to each method on that type that needs to pass it along." (CRC). "Do not add a context member to a struct type." (G-Dec)
- **Nuance.** CRC makes one exception, for methods "whose signature must match an interface in the standard library or in a third party library". **Myxo's** note: a short-lived per-operation struct that never outlives the call (a run's private "runner") breaks the letter of the rule but carries little of the risk, which is a stored context outliving its request.
- Tool: golangci-lint `containedctx`.

## State and configuration

### S1. No mutable package-level state; inject what varies. Avoid `init()`.
- Why: "Avoid mutating global variables, instead opting for dependency injection." "Avoid `init()` where possible." (Uber). "Libraries should not force their clients to use APIs that rely on global state." (G-BP). "Mutable global state introduces tight coupling between independent parts of your program" (Cheney)
- Tool: golangci-lint `gochecknoglobals` (flags every package var, including read-only tables, so it's noisy) and `gochecknoinits`.

### S2. Don't make tests swap package variables to fake a dependency
- Why: one of Google's litmus tests for bad global state: "Users of the API are tempted to swap or replace global state for testing purposes", and "Independent test cases interact with each other through global state." (G-BP)
- Tool: no.

### S3. Read configuration (flags, environment) at the command boundary and pass it down
- Why: "Program initialization errors (such as bad flags and configuration) should be propagated upward to `main`" and a package-level default instance "must only be used by binary build targets, not libraries" (G-BP). **Myxo's** extension: reading `os.Getenv` deep in a package is the same hidden global input.
- Tool: partly. golangci-lint `forbidigo` set to `os.Getenv` allows it only in chosen packages.

## Testing

### T1. Use table-driven tests when many cases share one logic, and not when cases branch
- Why: "Use table-driven tests when many different test cases can be tested using similar testing logic." (G-Dec). "Table tests should **NOT** be used whenever there needs to be complex or conditional logic inside subtests" (Uber).
- Tool: no.

### T2. Test against real implementations and small hand-written fakes, not generated mocks
- Why: "design the API so that it can be tested using the public API of the real implementation." "Instead return a concrete type and let the consumer mock the producer implementation." (CRC). "Do not export a test double implementation of an interface from an API that consumes it." (G-BP)
- **Disagreement.** Ben Johnson: "Use a shared mock subpackage", hand-written mocks shared across packages. Google allows a `footest` helper package beside the real one ("append the word `test` to the original package name").
- Tool: no. `depguard` can ban mock-generator libraries.

### T3. Failures say what was wrong with what input; keep going with `t.Error`; mark helpers with `t.Helper`; no assertion libraries
- Why: "Tests should fail with helpful messages saying what was wrong, with what inputs" (CRC). "Calling `t.Fatal` is appropriate when some piece of test setup fails" (G-BP). "call `t.Helper` to attribute failures in the test helper to the line where the helper is called." "Do not create "assertion libraries" as helpers for testing." (G-Dec)
- **Disagreement.** Uber's own examples use testify (`require.NoError`, `assert.Equal`). Google forbids assertion libraries.
- Tool: golangci-lint `thelper` (helpers call `t.Helper`); `depguard` for assertion libraries.

### T4. Pick one test-package stance and keep it
- Why: "I recommend using internal tests when writing unit tests for your package." (Cheney). Google: "Tests may be defined in the same package as the code being tested" but "It is not always appropriate or even possible".
- **Disagreement.** No source requires `_test` (external) packages. golangci-lint's `testpackage` linter enforces them, which only makes sense if you adopt that stance.
- Tool: `testpackage` (external stance only).

### T5. Tests are independent of each other, so they could run in parallel
- Why (**Myxo's**, from G-BP's litmus test "Independent test cases interact with each other through global state"): tests that share globals can't run with `t.Parallel`, and they hide the coupling S2 warns about.
- Tool: golangci-lint `paralleltest` and `tparallel`. Both are opinionated and only fit a codebase that has chosen parallel tests.

### T6. Tests don't depend on wall-clock time: no sleeps to synchronise, no timing thresholds in the default run
- Why (**Myxo's**): a sleep encodes a guess about scheduling, and a timing threshold measures the machine, not the code. Both fail under load, on CI runners and under `-race`. Wait on a signal, or poll with a deadline. Measure speed with `Benchmark` functions (`go test -bench`). No fetched source states this rule, though it follows from CRC's "make it clear when... they exit".
- Tool: no (a grep for `time.Sleep` and `time.Since` in tests finds candidates).

## API surface

### A1. Every exported name has a doc comment
- Why: "All top-level, exported names should have doc comments, as should non-trivial unexported type or function declarations." (CRC)
- Tool: revive `exported`; staticcheck `ST1020`, `ST1021` and `ST1022` (not on by default).

### A2. Design APIs that are hard to misuse: no runs of same-typed parameters, no meaningful `nil`
- Why: "APIs should be easy to use and hard to misuse." "APIs with multiple parameters of the same type are hard to use correctly." "Don't mix nil and non nil-able parameters in the same function signature." (Cheney)
- Tool: no.

### A3. Structs that are marshalled carry explicit field tags
- Why: "Any struct field that is marshaled into JSON, YAML, or other formats... Changes to the structure of the serialized form--including field names--break this contract." (Uber)
- Tool: golangci-lint `musttag`.

### A4. Keep dead exported and unexported code out
- Why: "Every exported type increases the cognitive load for the reader." (G-BP). **Myxo's** extension: unused code is a lie about what the package does.
- Tool: staticcheck `U1000`, golangci-lint `unused`.

## Logging

### L1. Log where an error is handled, never where it's returned; libraries return errors
- Why: "If you return an error, it's usually better not to log it yourself but rather let the caller handle it." "Use `log.Error` sparingly." (G-BP)
- Tool: no.

### L2. A long-running process logs through an injected logger, not the `log` package's global
- Why (**Myxo's**, from S1): the global logger is package-level mutable state, and tests can't capture or silence it without swapping it (S2). The standard library's `log/slog` gives a passable `*slog.Logger` with levels and structure.
- Tool: `depguard` or `forbidigo` on `log.Print*` outside `main`.

---

## Left out on purpose

- Formatting, import order, naming case, receiver names, error-string capitalisation: gofmt, goimports, revive and staticcheck `ST1005` own these.
- `go.uber.org/atomic`: Uber-specific, and `sync/atomic`'s typed values (Go 1.19+) cover it.
- Functional options versus option structs: a constructor style, not architecture. Physa's sessions use plain option structs, which is fine by both Google and Uber.
- golang-standards/project-layout: see `sources.md`.
