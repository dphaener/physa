package jsonblock

import "testing"

func TestLast(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		want   string
		wantOK bool
	}{
		{"no fence", "just words\n{\"a\":1}", "", false},
		{"one fence", "x\n```json\n{\"a\":1}\n```\ny", `{"a":1}`, true},
		{"two fences last wins", "```json\n1\n```\ntext\n```json\n2\n```", "2", true},
		{"unclosed last fence", "```json\n1\n```\n```json\n2\n", "1", true},
		{"only an unclosed fence", "```json\n1\n", "", false},
		{"non-json fence ignored", "```json\n1\n```\n```go\n2\n```", "1", true},
		{"only a non-json fence", "```go\n2\n```", "", false},
		{"four backticks", "````json\n```\n{\"a\":1}\n````", "```\n{\"a\":1}", true},
		{"three backticks do not close four", "````json\n1\n```\n2\n````", "1\n```\n2", true},
		{"crlf", "```json\r\n{\"a\":1}\r\n```\r\n", `{"a":1}`, true},
		{"json case-insensitive", "```JSON\n1\n```", "1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Last(tt.text)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("Last() = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
