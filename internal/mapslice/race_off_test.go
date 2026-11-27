//go:build !race

package mapslice

// raceEnabled is true when the tests run with -race, which slows them too much for latency budgets to mean anything.
const raceEnabled = false
