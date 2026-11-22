package claude

// InSession reports whether the environment getenv reads marks the process as
// running inside a Claude Code session: CLAUDECODE or CLAUDE_CODE_SESSION_ID
// set to a non-empty value. The caller passes the environment's lookup, so the
// answer can be tested without the real environment.
func InSession(getenv func(string) string) bool {
	return getenv("CLAUDECODE") != "" || getenv("CLAUDE_CODE_SESSION_ID") != ""
}
