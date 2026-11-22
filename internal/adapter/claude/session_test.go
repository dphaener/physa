package claude

import "testing"

// fakeEnv returns a getenv that reads only from env.
func fakeEnv(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func TestInSession(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"none set", nil, false},
		{"CLAUDECODE set", map[string]string{"CLAUDECODE": "1"}, true},
		{"CLAUDE_CODE_SESSION_ID set", map[string]string{"CLAUDE_CODE_SESSION_ID": "abc"}, true},
		{"CLAUDECODE empty", map[string]string{"CLAUDECODE": ""}, false},
		{"CLAUDE_CODE_SESSION_ID empty", map[string]string{"CLAUDE_CODE_SESSION_ID": ""}, false},
		{"other variables only", map[string]string{"PHYSA_AGENT": "x"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := InSession(fakeEnv(c.env)); got != c.want {
				t.Errorf("InSession = %v, want %v", got, c.want)
			}
		})
	}
}
