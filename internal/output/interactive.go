package output

import "os"

// Interactive says whether a person is at the terminal for this command: both
// input and output are terminals, CI is unset, and no agent is running it. It
// is the one place every idea and bet command decides between a person at a
// terminal (pickers, styles, confirms) and a pipe, CI or an agent (plain text
// or --json, errors instead of questions). getenv reads CI from the
// command's environment.
func Interactive(in, out *os.File, getenv func(string) string, agent bool) bool {
	return interactive(IsTerminal(in), IsTerminal(out), getenv("CI"), agent)
}

// interactive is Interactive with the environment and terminal checks passed
// in, so the rules can be tested without a terminal.
func interactive(inTTY, outTTY bool, ci string, agent bool) bool {
	return inTTY && outTTY && ci == "" && !agent
}
