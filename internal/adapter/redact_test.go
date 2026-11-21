package adapter

import (
	"strings"
	"testing"
)

// Every key below is made up, in the shape of the real thing.
func TestRedactKeyShapes(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"anthropic", "key sk-ant-api03-AbCdEf0123456789_xyz-QRS here", "key [redacted] here"},
		{"openai", "use sk-proj1234567890abcdefghij now", "use [redacted] now"},
		{"github classic", "ghp_0123456789abcdefghijABCDEFGHIJklmnop", "[redacted]"},
		{"github oauth", "gho_0123456789abcdefghijABCDEFGHIJklmnop.", "[redacted]."},
		{"github user", "(ghu_0123456789abcdefghijABCDEFGHIJklmnop)", "([redacted])"},
		{"github server", "ghs_0123456789abcdefghijABCDEFGHIJklmnop", "[redacted]"},
		{"github refresh", "ghr_0123456789abcdefghijABCDEFGHIJklmnop", "[redacted]"},
		{"github fine-grained", "github_pat_11ABCDEFG0123456789_abcdefghijklmnopqrstuvwxyz", "[redacted]"},
		{"slack bot", "xoxb-1234567890-0987654321-AbCdEfGhIjKl", "[redacted]"},
		{"slack user", "xoxp-1234567890-abcdef", "[redacted]"},
		{"aws access", "AKIAIOSFODNN7EXAMPLE in env", "[redacted] in env"},
		{"aws session", "ASIAIOSFODNN7EXAMPLE", "[redacted]"},
		{"google", "AIzaSyA-0123456789abcdefghijklmnopqrstu", "[redacted]"},
		{"gitlab", "glpat-0123456789abcdefghij", "[redacted]"},
		{"jwt", "jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U ok", "jwt [redacted] ok"},
		{"bearer", "Authorization: Bearer abc123.DEF456-ghi789", "Authorization: Bearer [redacted]"},
		{"bearer lower", "bearer 0123456789abcdef", "bearer [redacted]"},
		{"password assignment", "password=hunter2x9", "password=[redacted]"},
		{"token colon", "token: 7f9c2ba4e88f827d", "token: [redacted]"},
		{"secret upper", "SECRET = s3cr3t-value!", "SECRET = [redacted]!"},
		{"api_key quoted", `"api_key": "abcd-efgh-ijkl"`, `"api_key": "[redacted]"`},
		{"apikey", "apikey=Zm9vYmFyYmF6", "apikey=[redacted]"},
		{"api-key", "api-key:qwerty12345,", "api-key:[redacted],"},
		{"passwd sentence end", "the passwd: correct-horse.", "the passwd: [redacted]."},
		{"assignment of a prefixed key", "token=sk-ant-api03-AbCdEf0123456789", "token=[redacted]"},
		{"two in one text", "sk-ant-api03-AbCdEf0123456789 and password=hunter2x9", "[redacted] and password=[redacted]"},
	}
	for _, c := range cases {
		if got := Redact(c.in); got != c.want {
			t.Errorf("%s: Redact(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestRedactPEMBlockWhole(t *testing.T) {
	block := "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEAu1SU1LfVLPHCozMxH2Mo\n4lgOEePzNm0tRgeLezV6ffAt0gunVTLw7onLRnrq0\n-----END RSA PRIVATE KEY-----"
	in := "here it is:\n" + block + "\nthat was it"
	if got, want := Redact(in), "here it is:\n[redacted]\nthat was it"; got != want {
		t.Errorf("Redact = %q, want %q", got, want)
	}
	open := "key:\n-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQ\nand more"
	if got, want := Redact(open), "key:\n[redacted]"; got != want {
		t.Errorf("unterminated: Redact = %q, want %q", got, want)
	}
	public := "-----BEGIN PUBLIC KEY-----\nMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A\n-----END PUBLIC KEY-----"
	if got := Redact(public); got != public {
		t.Errorf("public key changed: %q", got)
	}
}

func TestRedactLeavesCommitIDsAndUUIDs(t *testing.T) {
	keep := []string{
		"10aa3d3c4f5e6a7b8c9d0e1f2a3b4c5d6e7f8a9b",
		"commit 10aa3d3c4f5e6a7b8c9d0e1f2a3b4c5d6e7f8a9b landed",
		"11111111-1111-4111-8111-111111111111",
		"session deebad1c-5f2e-4b7a-9c3d-0e1f2a3b4c5d ran",
		"task-notification-abcdefghijklmnopqrstu",
		"risk-assessment-of-the-whole-plan",
		"the secret: we keep it",
		"token: the one",
		"password protected",
		"a token: placeholder.",
		"Bearer tokens expire",
		"ask-the-person-before-anything-irreversible",
	}
	for _, s := range keep {
		if got := Redact(s); got != s {
			t.Errorf("Redact(%q) = %q, want it unchanged", s, got)
		}
	}
}

func TestRedactLongInput(t *testing.T) {
	in := strings.Repeat("sk-a password: token= Bearer of -----BEGIN ", 50000)
	if got := Redact(in); got != in {
		t.Errorf("long input changed (%d bytes in, %d out)", len(in), len(got))
	}
}
