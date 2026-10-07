package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Timohone/agentstatus/internal/session"
)

func TestHookIgnoresBadInput(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENTSTATUS_DIR", dir)
	for _, in := range []string{"", "kein json", `{"hook_event_name":"Erfunden","session_id":"s"}`, `{"hook_event_name":"Stop"}`} {
		r, w, _ := os.Pipe()
		w.WriteString(in)
		w.Close()
		old := os.Stdin
		os.Stdin = r
		code := dispatch([]string{"hook", "claude"})
		os.Stdin = old
		if code != 0 {
			t.Errorf("Eingabe %q: Exit %d", in, code)
		}
	}
	if code := dispatch([]string{"hook", "unbekannt"}); code != 0 {
		t.Errorf("unbekannter Agent: Exit %d", code)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	for _, f := range files {
		if strings.HasSuffix(f, ".json") {
			t.Fatalf("nichts darf geschrieben sein: %v", files)
		}
	}
}

func TestHookEndToEnd(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENTSTATUS_DIR", dir)
	r, w, _ := os.Pipe()
	w.WriteString(`{"hook_event_name":"UserPromptSubmit","session_id":"e2e","prompt":"Hallo"}`)
	w.Close()
	old := os.Stdin
	os.Stdin = r
	code := dispatch([]string{"hook", "claude"})
	os.Stdin = old
	s, ok := session.Load(dir, "claude-code", "e2e")
	if code != 0 || !ok || s.Status != session.Working || s.Title != "" || s.PID <= 0 {
		t.Fatalf("code %d ok %v %+v", code, ok, s)
	}
}

func TestSetRejectsNegativePid(t *testing.T) {
	t.Setenv("AGENTSTATUS_DIR", t.TempDir())
	if code := dispatch([]string{"set", "--agent", "a", "--id", "x", "--status", "idle", "--pid", "-3"}); code != 1 {
		t.Fatalf("Exit %d", code)
	}
}

func TestHookHugePayload(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENTSTATUS_DIR", dir)
	r, w, _ := os.Pipe()
	go func() {
		w.WriteString(`{"hook_event_name":"UserPromptSubmit","session_id":"big","prompt":"` + strings.Repeat("x", 2<<20) + `"}`)
		w.Close()
	}()
	old := os.Stdin
	os.Stdin = r
	code := dispatch([]string{"hook", "claude"})
	os.Stdin = old
	if _, ok := session.Load(dir, "claude-code", "big"); code != 0 || !ok {
		t.Fatalf("code %d ok %v", code, ok)
	}
}

func TestInstallFlagAfterAgent(t *testing.T) {
	configDir := t.TempDir()
	settingsPath := filepath.Join(configDir, "settings.json")

	// Create empty settings.json
	if err := os.WriteFile(settingsPath, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	// Set binary path to avoid temp dir check
	oldBinaryPath := testBinaryPath
	testBinaryPath = "/usr/local/bin/agentstatus"
	t.Cleanup(func() { testBinaryPath = oldBinaryPath })

	// Test install with --yes after agent name
	code := dispatch([]string{"install", "claude", "--yes"})
	if code != 0 {
		t.Fatalf("install with --yes after agent: exit code %d", code)
	}

	// Verify hook was added to settings
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hook claude") {
		t.Fatalf("hook not found in settings: %s", data)
	}

	// Test uninstall with --yes after agent name
	code = dispatch([]string{"uninstall", "claude", "--yes"})
	if code != 0 {
		t.Fatalf("uninstall with --yes after agent: exit code %d", code)
	}

	// Verify hook was removed from settings
	data, err = os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "hook claude") {
		t.Fatalf("hook not removed from settings: %s", data)
	}
}

func TestHookCodex(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENTSTATUS_DIR", dir)
	feed := func(in string) int {
		r, w, _ := os.Pipe()
		w.WriteString(in)
		w.Close()
		old := os.Stdin
		os.Stdin = r
		defer func() { os.Stdin = old }()
		return dispatch([]string{"hook", "codex"})
	}
	for _, bad := range []string{"", "kein json", `{"hook_event_name":"Erfunden","session_id":"s"}`} {
		if feed(bad) != 0 {
			t.Errorf("Eingabe %q: Exit != 0", bad)
		}
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "*.json")); len(files) != 0 {
		t.Fatalf("nichts darf geschrieben sein: %v", files)
	}
	if feed(`{"hook_event_name":"PermissionRequest","session_id":"cx","tool_name":"Bash"}`) != 0 {
		t.Fatal("Exit != 0")
	}
	if s, ok := session.Load(dir, "codex", "cx"); !ok || s.Status != session.Waiting || s.PID <= 0 {
		t.Fatalf("%v %+v", ok, s)
	}
}

func TestInstallCodex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	old := testBinaryPath
	testBinaryPath = "/usr/local/bin/agentstatus"
	t.Cleanup(func() { testBinaryPath = old })
	if dispatch([]string{"install", "codex", "--yes"}) != 0 {
		t.Fatal("install")
	}
	b, _ := os.ReadFile(filepath.Join(home, "hooks.json"))
	if !strings.Contains(string(b), "hook codex") {
		t.Fatalf("%s", b)
	}
	if dispatch([]string{"uninstall", "--yes", "codex"}) != 0 {
		t.Fatal("uninstall")
	}
	b, _ = os.ReadFile(filepath.Join(home, "hooks.json"))
	if strings.Contains(string(b), "agentstatus") {
		t.Fatalf("%s", b)
	}
}

func TestInstallCodexWithoutCodex(t *testing.T) {
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("PATH", t.TempDir())
	old := testBinaryPath
	testBinaryPath = "/usr/local/bin/agentstatus"
	t.Cleanup(func() { testBinaryPath = old })
	if dispatch([]string{"install", "codex", "--yes"}) == 0 {
		t.Fatal("muss ablehnen")
	}
	if _, err := os.Stat(os.Getenv("CODEX_HOME")); err == nil {
		t.Fatal("nichts darf geschrieben sein")
	}
}

func TestInstallGrok(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".grok"), 0o755)
	old := testBinaryPath
	testBinaryPath = "/usr/local/bin/agentstatus"
	t.Cleanup(func() { testBinaryPath = old })
	f := filepath.Join(home, ".grok", "hooks", "agentstatus.json")
	if dispatch([]string{"install", "grok", "--yes"}) != 0 {
		t.Fatal("install")
	}
	if b, _ := os.ReadFile(f); !strings.Contains(string(b), "agentstatus hook claude") || !strings.Contains(string(b), "StopCancelled") {
		t.Fatalf("%s", b)
	}
	if dispatch([]string{"uninstall", "--yes", "grok"}) != 0 {
		t.Fatal("uninstall")
	}
	if _, err := os.Stat(f); err == nil {
		t.Fatal("Datei muss weg sein")
	}
}

func TestInstallGrokWithoutGrok(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	old := testBinaryPath
	testBinaryPath = "/usr/local/bin/agentstatus"
	t.Cleanup(func() { testBinaryPath = old })
	if dispatch([]string{"install", "grok", "--yes"}) == 0 {
		t.Fatal("muss ablehnen")
	}
	if _, err := os.Stat(filepath.Join(home, ".grok")); err == nil {
		t.Fatal("~/.grok darf nicht entstehen")
	}
}

func hookWith(t *testing.T, agent, in string) int {
	t.Helper()
	r, w, _ := os.Pipe()
	go func() { w.WriteString(in); w.Close() }()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	return dispatch([]string{"hook", agent})
}

func TestDebugLog(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sessions")
	t.Setenv("AGENTSTATUS_DIR", dir)
	log := filepath.Join(root, "hook-log.jsonl")

	hookWith(t, "claude", `{"hook_event_name":"Stop","session_id":"d1"}`)
	if _, err := os.Stat(log); err == nil {
		t.Fatal("Log ohne debug on angelegt")
	}
	if dispatch([]string{"debug", "on"}) != 0 {
		t.Fatal("debug on")
	}
	if fi, err := os.Stat(log); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", fi, err)
	}
	hookWith(t, "codex", `{"hook_event_name":"Stop","session_id":"d2"}`)
	hookWith(t, "claude", `kein json`)
	b, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("Zeilen: %q", b)
	}
	var l struct {
		Time     string          `json:"time"`
		AgentArg string          `json:"agent_arg"`
		Detected string          `json:"detected"`
		PidRec   *int            `json:"pid_recorded"`
		Payload  json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &l); err != nil || l.Time == "" || l.AgentArg != "codex" ||
		l.Detected != "codex" || l.PidRec == nil || !strings.Contains(string(l.Payload), `"session_id":"d2"`) {
		t.Fatalf("%v %+v", err, l)
	}
	l.Payload = nil
	json.Unmarshal([]byte(lines[1]), &l)
	if string(l.Payload) != `"kein json"` {
		t.Fatalf("payload %s", l.Payload)
	}
	if dispatch([]string{"debug", "status"}) != 0 {
		t.Fatal("status")
	}
	if dispatch([]string{"debug", "off"}) != 0 {
		t.Fatal("off")
	}
	if _, err := os.Stat(log); err == nil {
		t.Fatal("Log nach off noch da")
	}
}

func TestDebugLogSizeLimit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENTSTATUS_DIR", filepath.Join(root, "sessions"))
	log := filepath.Join(root, "hook-log.jsonl")
	os.WriteFile(log, make([]byte, maxHookLog), 0o600)
	if hookWith(t, "claude", `{"hook_event_name":"Stop","session_id":"d3"}`) != 0 {
		t.Fatal("Exit")
	}
	if fi, _ := os.Stat(log); fi.Size() != maxHookLog {
		t.Fatalf("Groesse %d", fi.Size())
	}
}

func TestListNamesSecondLine(t *testing.T) {
	mk := func(id, topic string) session.Entry {
		return session.Entry{Session: session.Session{ID: id, Status: session.Idle, CWD: "/p/assistant", Topic: topic}}
	}
	none := func(string) bool { return false }
	names, got := listNames([]session.Entry{mk("aaaa1", "Backup"), mk("bbbb2", ""), mk("cccc3", strings.Repeat("x", 60))}, "/h", none)
	want := []string{"Backup", "#bbbb", strings.Repeat("x", 49) + "…"}
	for i := range want {
		if got[i] != want[i] || names[i] != "assistant" {
			t.Errorf("%d: %q %q, erwartet %q", i, names[i], got[i], want[i])
		}
	}
	if _, got := listNames([]session.Entry{mk("aaaa1", "")}, "/h", none); got[0] != "" {
		t.Errorf("%q", got[0])
	}
}

func TestVersion(t *testing.T) {
	for _, a := range []string{"--version", "version"} {
		r, w, _ := os.Pipe()
		old := os.Stdout
		os.Stdout = w
		code := dispatch([]string{a})
		w.Close()
		os.Stdout = old
		out, _ := io.ReadAll(r)
		if code != 0 || string(out) != "agentstatus dev\n" {
			t.Errorf("%s: code=%d out=%q", a, code, out)
		}
	}
}
