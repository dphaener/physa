package skillhook

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ours is Physa's entry as Write indents it inside SessionStart.
const ours = `{
        "hooks": [
          {
            "type": "command",
            "command": "physa skill session",
            "timeout": 5
          }
        ]
      }`

// settings returns a path to settings.json in a fresh temp .claude
// directory, holding content unless content is "".
func settings(t *testing.T, content string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func wantState(t *testing.T, path string, want State) {
	t.Helper()
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != want {
		t.Errorf("Read = %v, want %v", got, want)
	}
}

func TestStateString(t *testing.T) {
	for st, want := range map[State]string{Missing: "missing", Current: "current", Stale: "stale", Other: "other"} {
		if got := st.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", st, got, want)
		}
	}
}

func TestReadStates(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    State
	}{
		{"empty object", `{}`, Missing},
		{"no SessionStart", `{"hooks":{"PreToolUse":[]}}`, Missing},
		{"other entries only", `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo hi"}]}]}}`, Missing},
		{"current", `{"hooks":{"SessionStart":[` + entry + `]}}`, Current},
		{"current, spaced", "{ \"hooks\" : { \"SessionStart\" : [ {\"hooks\": [ {\"type\": \"command\", \"command\": \"physa skill session\", \"timeout\": 5} ] } ] } }\n", Current},
		{"other timeout", `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"physa skill session","timeout":9}]}]}}`, Stale},
		{"added matcher", `{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"physa skill session","timeout":5}]}]}}`, Stale},
		{"two entries", `{"hooks":{"SessionStart":[` + entry + `,` + entry + `]}}`, Stale},
		{"two hooks in entry", `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"physa skill session","timeout":5},{"type":"command","command":"x"}]}]}}`, Missing},
		{"invalid JSON", `{"hooks":`, Other},
		{"empty file", ` `, Other},
		{"trailing data", `{} {}`, Other},
		{"top level array", `[]`, Other},
		{"hooks is a string", `{"hooks":"x"}`, Other},
		{"hooks is null", `{"hooks":null}`, Other},
		{"SessionStart is an object", `{"hooks":{"SessionStart":{}}}`, Other},
		{"hooks repeated", `{"hooks":{},"hooks":{}}`, Other},
		{"SessionStart repeated", `{"hooks":{"SessionStart":[],"SessionStart":[]}}`, Other},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantState(t, settings(t, c.content), c.want)
		})
	}
}

func TestReadMissingFile(t *testing.T) {
	wantState(t, settings(t, ""), Missing)
	wantState(t, filepath.Join(t.TempDir(), "no", ".claude", "settings.json"), Missing)
}

func TestWriteMissingFile(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "settings.json")
	if err := Write(path); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"hooks\": {\n    \"SessionStart\": [\n      " + ours + "\n    ]\n  }\n}\n"
	if got := readFile(t, path); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", fi.Mode().Perm())
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o755 {
		t.Errorf("dir mode = %v, want 0755", di.Mode().Perm())
	}
	wantState(t, path, Current)
}

func TestWriteEmptyObject(t *testing.T) {
	path := settings(t, "{}\n")
	if err := Write(path); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"hooks\": {\n    \"SessionStart\": [\n      " + ours + "\n    ]\n  }\n}\n"
	if got := readFile(t, path); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
}

// others is a settings file with other keys around hooks, another hook event
// and another SessionStart entry, in an order no sort would give.
const others = `{
  "model": "opus",
  "permissions": {"allow": ["Bash(go test:*)"], "deny": []},
  "hooks": {
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "guard"}]}],
    "SessionStart": [{"hooks": [{"type": "command", "command": "echo hello"}]}],
    "Stop": []
  },
  "alwaysThinkingEnabled": true
}
`

// othersWith is others, as Write indents it, with Physa's entry after the
// other SessionStart entry.
const othersWith = `{
  "model": "opus",
  "permissions": {
    "allow": [
      "Bash(go test:*)"
    ],
    "deny": []
  },
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "guard"
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo hello"
          }
        ]
      },
      ` + ours + `
    ],
    "Stop": []
  },
  "alwaysThinkingEnabled": true
}
`

func TestWriteKeepsOtherKeysAndHooks(t *testing.T) {
	path := settings(t, others)
	if err := Write(path); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != othersWith {
		t.Errorf("file =\n%s\nwant\n%s", got, othersWith)
	}
	wantState(t, path, Current)
}

func TestWriteAddsSessionStartAtEndOfHooks(t *testing.T) {
	path := settings(t, `{"hooks":{"Stop":[]},"z":1}`)
	if err := Write(path); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"hooks\": {\n    \"Stop\": [],\n    \"SessionStart\": [\n      " + ours + "\n    ]\n  },\n  \"z\": 1\n}\n"
	if got := readFile(t, path); got != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
}

func TestWriteTwiceGivesOneEntry(t *testing.T) {
	path := settings(t, others)
	if err := Write(path); err != nil {
		t.Fatal(err)
	}
	first := readFile(t, path)
	if err := Write(path); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != first {
		t.Errorf("second Write changed the file:\n%s", got)
	}
	if n := strings.Count(first, Command); n != 1 {
		t.Errorf("%d entries running %q, want 1", n, Command)
	}
}

func TestWriteOnCurrentLeavesBytes(t *testing.T) {
	content := "{\"hooks\":{\"SessionStart\":[" + entry + "]},   \"x\":1}"
	path := settings(t, content)
	if err := Write(path); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != content {
		t.Errorf("file = %q, want it untouched", got)
	}
}

func TestWriteReplacesStaleInPlace(t *testing.T) {
	path := settings(t, `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"a"}]},{"matcher":"startup","hooks":[{"type":"command","command":"physa skill session","timeout":30}]},{"hooks":[{"type":"command","command":"b"}]}]}}`)
	wantState(t, path, Stale)
	if err := Write(path); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	a, p, b := strings.Index(got, `"a"`), strings.Index(got, Command), strings.Index(got, `"b"`)
	if a < 0 || p < 0 || b < 0 || a >= p || p >= b {
		t.Errorf("entry not replaced in place:\n%s", got)
	}
	if strings.Contains(got, "matcher") || strings.Contains(got, "30") {
		t.Errorf("stale entry kept:\n%s", got)
	}
	wantState(t, path, Current)
}

func TestWriteCollapsesDuplicate(t *testing.T) {
	path := settings(t, `{"hooks":{"SessionStart":[`+entry+`,{"hooks":[{"type":"command","command":"a"}]},`+entry+`]}}`)
	wantState(t, path, Stale)
	if err := Write(path); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	if n := strings.Count(got, Command); n != 1 {
		t.Errorf("%d entries, want 1:\n%s", n, got)
	}
	if strings.Index(got, Command) > strings.Index(got, `"a"`) {
		t.Errorf("kept entry should be the first one's place:\n%s", got)
	}
	wantState(t, path, Current)
}

func TestWriteKeepsNumbersAndUnicode(t *testing.T) {
	path := settings(t, `{"n":1e3,"f":1.50,"s":"é","e":"\u00e9 <&>","\u00e9":0}`)
	if err := Write(path); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	for _, want := range []string{`"n": 1e3`, `"f": 1.50`, `"s": "é"`, `"e": "\u00e9 <&>"`, `"\u00e9": 0`} {
		if !strings.Contains(got, want) {
			t.Errorf("file lacks %s:\n%s", want, got)
		}
	}
}

func TestWriteKeepsMode(t *testing.T) {
	path := settings(t, `{}`)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d files, want only settings.json", len(entries))
	}
}

func TestRemoveLeavesOnlyOthers(t *testing.T) {
	path := settings(t, othersWith)
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	if strings.Contains(got, Command) {
		t.Errorf("entry kept:\n%s", got)
	}
	// Writing the original and indenting it gives what Remove must leave:
	// the others, in their order, with only whitespace changed.
	orig := settings(t, others)
	if err := Write(orig); err != nil {
		t.Fatal(err)
	}
	if err := Remove(orig); err != nil {
		t.Fatal(err)
	}
	if again := readFile(t, orig); again != got {
		t.Errorf("remove after write =\n%s\nwant\n%s", again, got)
	}
	for _, want := range []string{`"model": "opus"`, `"command": "guard"`, `"command": "echo hello"`, `"Stop": []`, `"alwaysThinkingEnabled": true`} {
		if !strings.Contains(got, want) {
			t.Errorf("file lacks %s:\n%s", want, got)
		}
	}
	wantState(t, path, Missing)
}

func TestRemoveDropsEmptiedKeys(t *testing.T) {
	path := settings(t, `{"hooks":{"SessionStart":[`+entry+`,`+entry+`]}}`)
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "{}\n" {
		t.Errorf("file = %q, want {}", got)
	}
	path = settings(t, `{"a":1,"hooks":{"Stop":[],"SessionStart":[`+entry+`]}}`)
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": 1,\n  \"hooks\": {\n    \"Stop\": []\n  }\n}\n"
	if got := readFile(t, path); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestRemoveMissingLeavesBytes(t *testing.T) {
	content := `{"hooks":{"SessionStart":[]},   "x":1}`
	path := settings(t, content)
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != content {
		t.Errorf("file = %q, want it untouched", got)
	}
	none := filepath.Join(t.TempDir(), ".claude", "settings.json")
	if err := Remove(none); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(none); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Remove created %s", none)
	}
}

func TestInvalidJSONRefused(t *testing.T) {
	for _, content := range []string{`{"hooks":`, `{"hooks":"x"}`, `{"hooks":{"SessionStart":{}}}`} {
		path := settings(t, content)
		wantState(t, path, Other)
		if err := Write(path); !errors.Is(err, ErrUnsafe) {
			t.Errorf("Write over %s = %v, want ErrUnsafe", content, err)
		}
		if err := Remove(path); !errors.Is(err, ErrUnsafe) {
			t.Errorf("Remove over %s = %v, want ErrUnsafe", content, err)
		}
		if got := readFile(t, path); got != content {
			t.Errorf("file = %q, want %q unchanged", got, content)
		}
	}
}

func TestSymlinkRefused(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.json")
	content := `{"hooks":{"SessionStart":[` + entry + `]}}`
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	wantState(t, path, Other)
	if err := Write(path); !errors.Is(err, ErrUnsafe) {
		t.Errorf("Write = %v, want ErrUnsafe", err)
	}
	if err := Remove(path); !errors.Is(err, ErrUnsafe) {
		t.Errorf("Remove = %v, want ErrUnsafe", err)
	}
	if got := readFile(t, target); got != content {
		t.Errorf("target = %q, want it unchanged", got)
	}
	if fi, err := os.Lstat(path); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("symlink replaced: %v", err)
	}
}

func TestDirectoryRefused(t *testing.T) {
	path := settings(t, "")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	wantState(t, path, Other)
	if err := Write(path); !errors.Is(err, ErrUnsafe) {
		t.Errorf("Write = %v, want ErrUnsafe", err)
	}
}
