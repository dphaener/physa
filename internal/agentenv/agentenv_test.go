package agentenv

import "testing"

// fakeEnv returns a getenv that reads only from env.
func fakeEnv(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name      string
		env       map[string]string
		wantAgent string
		wantOK    bool
	}{
		{"none set", nil, "", false},
		{"CLAUDECODE set", map[string]string{"CLAUDECODE": "1"}, "Claude Code", true},
		{"CLAUDE_CODE_SESSION_ID set", map[string]string{"CLAUDE_CODE_SESSION_ID": "abc"}, "Claude Code", true},
		{"both set", map[string]string{"CLAUDECODE": "1", "CLAUDE_CODE_SESSION_ID": "abc"}, "Claude Code", true},
		{"CLAUDECODE empty", map[string]string{"CLAUDECODE": ""}, "", false},
		{"CLAUDE_CODE_SESSION_ID empty", map[string]string{"CLAUDE_CODE_SESSION_ID": ""}, "", false},
		{"unrelated variables", map[string]string{"PHYSA_AGENT": "claude-code"}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			agent, ok := Detect(fakeEnv(c.env))
			if agent != c.wantAgent || ok != c.wantOK {
				t.Errorf("Detect = (%q, %v), want (%q, %v)", agent, ok, c.wantAgent, c.wantOK)
			}
		})
	}
}
