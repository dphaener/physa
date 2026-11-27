# Go architecture · Rules

## Contents

- [A package has one purpose and is named for what it provides](#a-package-has-one-purpose-and-is-named-for-what-it-provides)
- [Packages are grouped by what depends on what, not by layer](#packages-are-grouped-by-what-depends-on-what-not-by-layer)
- [Only what outside programs need is public, and it exposes no internal type](#only-what-outside-programs-need-is-public-and-it-exposes-no-internal-type)
- [Interfaces are small and declared by the package that uses them](#interfaces-are-small-and-declared-by-the-package-that-uses-them)
- [Implementations return concrete types](#implementations-return-concrete-types)
- [An exported struct embeds a type only when it is that type](#an-exported-struct-embeds-a-type-only-when-it-is-that-type)
- [Errors are returned, only a programmer's mistake panics, and only main exits](#errors-are-returned-only-a-programmers-mistake-panics-and-only-main-exits)
- [An error says what failed, and its shape follows how callers match it](#an-error-says-what-failed-and-its-shape-follows-how-callers-match-it)
- [Errors are matched with errors.Is and errors.As, never by their text](#errors-are-matched-with-errorsis-and-errorsas-never-by-their-text)
- [Each error is handled once, and logged only where it is handled](#each-error-is-handled-once-and-logged-only-where-it-is-handled)
- [Every goroutine has an owner that waits for it](#every-goroutine-has-an-owner-that-waits-for-it)
- [A context flows from the command down and is never stored](#a-context-flows-from-the-command-down-and-is-never-stored)
- [What varies is passed in, never read from package state](#what-varies-is-passed-in-never-read-from-package-state)
- [Tests pass their fakes in and never swap package variables](#tests-pass-their-fakes-in-and-never-swap-package-variables)
- [A table test is for cases that share one logic](#a-table-test-is-for-cases-that-share-one-logic)
- [Tests use real implementations and small hand-written fakes](#tests-use-real-implementations-and-small-hand-written-fakes)
- [A failing test says what was wrong with which input](#a-failing-test-says-what-was-wrong-with-which-input)
- [Tests live in the package they test](#tests-live-in-the-package-they-test)
- [Tests never depend on the wall clock](#tests-never-depend-on-the-wall-clock)
- [Every exported name is documented](#every-exported-name-is-documented)
- [An API is hard to misuse](#an-api-is-hard-to-misuse)
- [A marshalled struct names every field it writes](#a-marshalled-struct-names-every-field-it-writes)

## Entries

### A package has one purpose and is named for what it provides
`one-purpose-package` · from docs/research/go-architecture.md
A package's name says what it provides, never a grab-bag such as util, common or helpers. A package that grows too large is split along its purposes, never at a line count.
Because: a reader finds code by what it does, and a package with one purpose changes without dragging unrelated code along.

### Packages are grouped by what depends on what, not by layer
`group-by-dependency` · from docs/research/go-architecture.md
Code that changes together lives together. Packages are not cut into layers or by kind of thing, such as models, handlers or types.
Because: a change to one behaviour then touches one package, not one in every layer.

### Only what outside programs need is public, and it exposes no internal type
`public-surface` · from docs/research/go-architecture.md
Everything a program outside the repo does not need stays internal. A public package never shows an internal type in its API: the client carries its own wire types and converts at its edge.
Because: an outside program can read an internal type but cannot name or build it, and each one exposed freezes a package that should change freely.

### Interfaces are small and declared by the package that uses them
`consumer-interfaces` · from docs/research/go-architecture.md
A package that needs a behaviour declares an interface of the one to few methods it calls. The three shared ports, version control, the forge and the agent adapter, are the exception, kept by decision.
Because: a small interface is cheap to fake and to satisfy, and its user can change it without touching anyone else.

### Implementations return concrete types
`return-concrete` · from docs/research/go-architecture.md
Constructors return concrete types, except a port's, which returns the port. No interface is added before its second use, or only to allow a fake. Compile-time checks that a type satisfies an interface go only on adapters.
Because: a concrete type can gain methods without breaking anyone, and an interface added early fixes a shape before it is known.

### An exported struct embeds a type only when it is that type
`embed-only-is-a` · from docs/research/go-architecture.md
Embedding makes every method of the embedded type part of the struct's API, so an exported struct embeds only what it truly is, as a decorator is. Otherwise the type is a named field.
Because: an embedded type's methods become a promise the struct must keep, and taking them back later breaks callers.

### Errors are returned, only a programmer's mistake panics, and only main exits
`errors-returned` · from docs/research/go-architecture.md
Failure travels as a returned error. A panic marks a programmer's mistake, or a setup that cannot proceed. Only the program's main function exits the process; everything below it returns.
Because: a returned error can be handled, wrapped and tested, while a panic or an exit deep down skips cleanup and takes the choice from the caller.

### An error says what failed, and its shape follows how callers match it
`error-shape` · from docs/research/go-architecture.md
Each error adds what was being done when it failed, wraps its cause when callers may need it and hides it when they must not. An error no caller matches is plain; one callers match is a sentinel; one that carries data is a type.
Because: an exposed cause becomes part of the API, so only what callers need is exposed, and a message with its context is read once, not traced.

### Errors are matched with errors.Is and errors.As, never by their text
`match-errors` · from docs/research/go-architecture.md
Code that tests an error's kind uses errors.Is for a sentinel and errors.As for a type, so wrapped errors still match. It never compares or searches an error's message.
Because: messages are reworded freely, and matching on text breaks the first time someone improves one.

### Each error is handled once, and logged only where it is handled
`handle-once` · from docs/research/go-architecture.md
Code either handles an error or returns it, never both: an error is not logged and then returned. Library code returns errors and leaves logging to the code that handles them.
Because: an error logged at every level repeats one failure many times, and only the code that decides knows whether it matters.

### Every goroutine has an owner that waits for it
`goroutine-owner` · from docs/research/go-architecture.md
Whoever starts a goroutine knows how it stops and waits for it to finish. APIs are synchronous and leave concurrency to the caller. Channels are unbuffered or hold one value, unless the code documents why a larger size is right.
Because: a goroutine nobody waits for leaks and races with shutdown, and a caller can make a synchronous API concurrent but not the reverse.

### A context flows from the command down and is never stored
`context-down` · from docs/research/go-architecture.md
Every call that blocks or does I/O takes a context as its first parameter, passed down from the command. A fresh background context is made only at the top, and no struct keeps a context.
Because: a cancel at the top, such as Ctrl-C, then stops every phase cleanly and lets its partial work be recorded.

### What varies is passed in, never read from package state
`pass-what-varies` · from docs/research/go-architecture.md
No package variable changes after start, and no package has an init function. Flags, environment and the logger are read at the command and passed down; nothing below it uses the global logger.
Because: code whose inputs are its parameters can be read, tested and run twice at once, while package state ties every caller to every other.

### Tests pass their fakes in and never swap package variables
`tests-inject-fakes` · from docs/research/go-architecture.md
A test hands the code under test its fakes through parameters or fields. It never reassigns a package variable to swap one in, so tests can run in parallel.
Because: a swapped package variable is shared by every test at once, which rules out parallel tests and lets one test leak into the next.

### A table test is for cases that share one logic
`table-tests-share-logic` · from docs/research/go-architecture.md
A table holds cases that run the same steps on different inputs. Cases that need their own setup, or branches inside the loop, get their own test.
Because: a table full of flags and branches hides what each case checks and is harder to change than separate tests.

### Tests use real implementations and small hand-written fakes
`real-and-hand-fakes` · from docs/research/go-architecture.md
Tests use the real thing where it is cheap, such as real git in temporary repos, and otherwise small fakes written by hand beside the test. No mock generators, and no test doubles exported for other packages.
Because: real implementations catch what mocks assume away, and hand-written fakes stay as small as their test needs.

### A failing test says what was wrong with which input
`useful-failures` · from docs/research/go-architecture.md
A failure message names the input, what came back and what was wanted. Tests use t.Error to keep going while later checks still tell something, mark helpers with t.Helper, and use no assertion libraries.
Because: a failure that explains itself is fixed without rerunning it under a debugger.

### Tests live in the package they test
`tests-in-package` · from docs/research/go-architecture.md
A test belongs to the package it tests, never to a separate test package. This is the one stance Physa holds on test packages.
Because: tests can then reach the unexported seams they pass fakes through, and one stance keeps every package's tests alike.

### Tests never depend on the wall clock
`no-wall-clock` · from docs/research/go-architecture.md
Tests never sleep to wait for something, and the default run has no timing thresholds. A test waits on a signal, or polls with a deadline. Speed is measured by benchmarks.
Because: a timing threshold passes on a quiet laptop and fails under the race detector or on a loaded runner.

### Every exported name is documented
`document-exports` · from docs/research/go-architecture.md
Every exported type, function, method, constant and variable has a doc that starts with its name and says what it promises.
Because: the doc tells a reader what they may rely on without reading the code behind it.

### An API is hard to misuse
`hard-to-misuse` · from docs/research/go-architecture.md
A function takes no run of same-typed parameters a caller could swap, and nil never carries a meaning of its own. Past a few parameters, an option struct replaces the list.
Because: a call that cannot be written wrong needs no review to catch it, and an option struct grows without breaking callers.

### A marshalled struct names every field it writes
`explicit-tags` · from docs/research/go-architecture.md
Every field of a struct written as JSON or any other format carries an explicit tag.
Because: the serialized form is a contract with stored data and other programs, and renaming a Go field must never change it silently.
