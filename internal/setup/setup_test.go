package setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct {
	env   Env
	home  string
	calls [][]string
	paths map[string]string // cli name -> path
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{home: t.TempDir(), paths: map[string]string{}}
	f.env = Env{
		Home:       f.home,
		Getenv:     func(string) string { return "" },
		Executable: "/opt/test/agentstatus",
		AppDir:     filepath.Join(f.home, "NoApp.app"),
		Check:      func(string) error { return nil },
		Exec:       func(n string, a ...string) error { f.calls = append(f.calls, append([]string{n}, a...)); return nil },
	}
	f.env.LookPath = func(n string) (string, error) {
		if p, ok := f.paths[n]; ok {
			return p, nil
		}
		return "", errors.New("not found")
	}
	f.paths["agentstatus"] = "/opt/homebrew/bin/agentstatus"
	return f
}

func (f *fixture) mkdir(t *testing.T, rel string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(f.home, rel), 0o755); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) write(t *testing.T, rel, content string) {
	t.Helper()
	f.mkdir(t, filepath.Dir(rel))
	if err := os.WriteFile(filepath.Join(f.home, rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) run(t *testing.T, opts Options, in string) string {
	t.Helper()
	var out bytes.Buffer
	if err := Run(f.env, opts, IO{In: strings.NewReader(in), Out: &out}); err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	return out.String()
}

func (f *fixture) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(f.home, rel))
	return err == nil
}

func (f *fixture) read(rel string) string {
	b, _ := os.ReadFile(filepath.Join(f.home, rel))
	return string(b)
}

func norm(t *testing.T, s string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, s)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func TestDetect(t *testing.T) {
	f := newFixture(t)
	f.mkdir(t, ".claude")
	f.paths["codex"] = "/usr/local/bin/codex" // nur im PATH, kein ~/.codex
	got := map[string]Agent{}
	for _, a := range Detect(f.env) {
		got[a.Name] = a
	}
	if !got["claude"].Found || !got["codex"].Found || got["grok"].Found {
		t.Fatalf("detect: %+v", got)
	}
	if got["claude"].Connected {
		t.Fatal("claude must not be connected yet")
	}
}

func TestNoAgentWritesNothing(t *testing.T) {
	f := newFixture(t)
	out := f.run(t, Options{Yes: true}, "")
	if !strings.Contains(out, "nothing written") {
		t.Fatalf("out: %s", out)
	}
	for _, d := range []string{".claude", ".codex", ".grok", ".local"} {
		if f.exists(d) {
			t.Fatalf("%s must not exist", d)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("calls: %v", f.calls)
	}
}

func TestYesConnectsAllFound(t *testing.T) {
	f := newFixture(t)
	f.mkdir(t, ".claude")
	f.mkdir(t, ".codex")
	out := f.run(t, Options{Yes: true}, "")
	if !strings.Contains(f.read(".claude/settings.json"), "/opt/homebrew/bin/agentstatus hook claude") {
		t.Fatal("claude not connected")
	}
	if !strings.Contains(f.read(".codex/hooks.json"), "/opt/homebrew/bin/agentstatus hook codex") {
		t.Fatal("codex not connected")
	}
	if f.exists(".grok") {
		t.Fatal("grok dir created although not found")
	}
	if !strings.Contains(out, "/hooks") {
		t.Fatalf("codex trust hint missing: %s", out)
	}
	if !strings.Contains(out, "Grok (experimental)") {
		t.Fatalf("grok row missing: %s", out)
	}
}

func TestAskPerAgent(t *testing.T) {
	f := newFixture(t)
	f.mkdir(t, ".claude")
	f.mkdir(t, ".codex")
	out := f.run(t, Options{}, "n\n\n") // Claude nein, Codex Enter = ja
	if !strings.Contains(out, "Connect Claude Code? [Y/n]") {
		t.Fatalf("no question: %s", out)
	}
	if f.exists(".claude/settings.json") {
		t.Fatal("claude written despite n")
	}
	if !f.exists(".codex/hooks.json") {
		t.Fatal("codex not written")
	}
}

func TestEOFMeansNo(t *testing.T) {
	f := newFixture(t)
	f.mkdir(t, ".claude")
	f.run(t, Options{}, "")
	if f.exists(".claude/settings.json") {
		t.Fatal("written without answer")
	}
}

func TestOnly(t *testing.T) {
	f := newFixture(t)
	f.mkdir(t, ".claude")
	f.mkdir(t, ".codex")
	f.run(t, Options{Yes: true, Only: []string{"codex"}}, "")
	if f.exists(".claude/settings.json") || !f.exists(".codex/hooks.json") {
		t.Fatal("--only codex not respected")
	}
	var out bytes.Buffer
	if Run(f.env, Options{Only: []string{"nope"}}, IO{In: strings.NewReader(""), Out: &out}) == nil {
		t.Fatal("unknown --only must fail")
	}
}

func TestAlreadyConnectedNoQuestion(t *testing.T) {
	f := newFixture(t)
	f.mkdir(t, ".claude")
	f.run(t, Options{Yes: true}, "")
	out := f.run(t, Options{}, "") // keine Eingabe: eine Frage wuerde nein ergeben, aber sie darf gar nicht kommen
	if !strings.Contains(out, "already connected") || strings.Contains(out, "[Y/n]") {
		t.Fatalf("out: %s", out)
	}
}

func TestOldPathIsUpdated(t *testing.T) {
	f := newFixture(t)
	f.write(t, ".claude/settings.json", `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/Users/x/go/bin/agentstatus hook claude"}]}]}}`)
	f.run(t, Options{Yes: true}, "")
	s := f.read(".claude/settings.json")
	if strings.Contains(s, "go/bin") || strings.Count(s, "hook claude\"") != strings.Count(s, "/opt/homebrew/bin/agentstatus hook claude") {
		t.Fatalf("not updated cleanly: %s", s)
	}
}

func TestRemoveRestoresOriginal(t *testing.T) {
	f := newFixture(t)
	claude := `{"theme":"dark","hooks":{"Stop":[{"hooks":[{"type":"command","command":"/usr/bin/other hook claude"}]}]}}`
	codex := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/usr/bin/mine"}]}]}}`
	f.write(t, ".claude/settings.json", claude)
	f.write(t, ".codex/hooks.json", codex)
	f.mkdir(t, ".grok")
	f.run(t, Options{Yes: true}, "")
	if !f.exists(".grok/hooks/agentstatus.json") {
		t.Fatal("grok file missing after setup")
	}
	out := f.run(t, Options{Yes: true, Remove: true}, "")
	if norm(t, f.read(".claude/settings.json")) != norm(t, claude) {
		t.Fatalf("claude differs: %s", f.read(".claude/settings.json"))
	}
	if norm(t, f.read(".codex/hooks.json")) != norm(t, codex) {
		t.Fatalf("codex differs: %s", f.read(".codex/hooks.json"))
	}
	if f.exists(".grok/hooks/agentstatus.json") {
		t.Fatal("grok file remains")
	}
	if !strings.Contains(out, "disconnected") {
		t.Fatalf("out: %s", out)
	}
}

func TestPanelLoginAndRemove(t *testing.T) {
	f := newFixture(t)
	f.mkdir(t, ".claude")
	f.mkdir(t, "App.app")
	f.env.AppDir = filepath.Join(f.home, "App.app")
	f.run(t, Options{Yes: true}, "")
	want := [][]string{
		{"defaults", "write", "com.timohone.agentstatus", "launchAtLoginRequested", "-bool", "true"},
		{"open", "-a", "AgentStatus"},
	}
	if len(f.calls) != 2 || strings.Join(f.calls[0], " ") != strings.Join(want[0], " ") || strings.Join(f.calls[1], " ") != strings.Join(want[1], " ") {
		t.Fatalf("calls: %v", f.calls)
	}
	f.calls = nil
	f.run(t, Options{Yes: true, Remove: true}, "")
	if len(f.calls) != 2 || f.calls[0][5] != "false" || strings.Join(f.calls[1], " ") != "open -a AgentStatus" {
		t.Fatalf("remove calls: %v", f.calls)
	}
	f.calls = nil
	f.run(t, Options{Yes: true, Only: []string{"claude"}}, "")
	if len(f.calls) != 0 {
		t.Fatalf("--only must not touch the panel: %v", f.calls)
	}
}

func TestLinkOfferedFromBundle(t *testing.T) {
	f := newFixture(t)
	delete(f.paths, "agentstatus")
	f.mkdir(t, ".claude")
	exe := filepath.Join(f.home, "Apps", "AgentStatus.app", "Contents", "MacOS", "agentstatus")
	f.env.Executable = exe
	out := f.run(t, Options{}, "y\ny\n") // Link ja, Claude ja
	link := filepath.Join(f.home, ".local", "bin", "agentstatus")
	if tgt, err := os.Readlink(link); err != nil || tgt != exe {
		t.Fatalf("link: %v %q", err, tgt)
	}
	if !strings.Contains(out, "not in your PATH") {
		t.Fatalf("PATH hint missing: %s", out)
	}
	if !strings.Contains(f.read(".claude/settings.json"), link+" hook claude") {
		t.Fatalf("hook should use the stable link: %s", f.read(".claude/settings.json"))
	}
	f.run(t, Options{Yes: true, Remove: true}, "")
	if _, err := os.Lstat(link); err == nil {
		t.Fatal("link not removed")
	}
}

func TestLinkDeclinedUsesBundlePath(t *testing.T) {
	f := newFixture(t)
	delete(f.paths, "agentstatus")
	f.mkdir(t, ".claude")
	exe := filepath.Join(f.home, "AgentStatus.app", "Contents", "MacOS", "agentstatus")
	f.env.Executable = exe
	f.run(t, Options{}, "n\ny\n")
	if f.exists(".local") {
		t.Fatal("link dir created despite n")
	}
	if !strings.Contains(f.read(".claude/settings.json"), exe+" hook claude") {
		t.Fatal("bundle path expected")
	}
}

func TestTempBinaryRejected(t *testing.T) {
	f := newFixture(t)
	f.env.Check = nil // echte Pruefung
	f.paths["agentstatus"] = filepath.Join(os.TempDir(), "agentstatus")
	f.mkdir(t, ".claude")
	var out bytes.Buffer
	if Run(f.env, Options{Yes: true}, IO{In: strings.NewReader(""), Out: &out}) == nil {
		t.Fatal("temp binary must be refused")
	}
	if f.exists(".claude/settings.json") {
		t.Fatal("written despite refusal")
	}
}

func bundleFixture(t *testing.T) (*fixture, string, string) {
	f := newFixture(t)
	delete(f.paths, "agentstatus")
	f.mkdir(t, ".claude")
	f.mkdir(t, ".codex")
	exe := filepath.Join(f.home, "AgentStatus.app", "Contents", "MacOS", "agentstatus")
	f.env.Executable = exe
	return f, exe, filepath.Join(f.home, ".local", "bin", "agentstatus")
}

func TestRemoveKeepsLinkWhenDisconnectDeclined(t *testing.T) {
	f, _, link := bundleFixture(t)
	f.run(t, Options{Yes: true}, "")
	f.run(t, Options{Remove: true}, "y\nn\n") // Claude ab, Codex bleibt
	if _, err := os.Lstat(link); err != nil {
		t.Fatal("link removed although Codex hook remains")
	}
	f.run(t, Options{Remove: true, Yes: true}, "")
	if _, err := os.Lstat(link); err == nil {
		t.Fatal("link should go once nothing is connected")
	}
}

func TestRemoveWithNothingConnectedStillCleansUp(t *testing.T) {
	f, exe, link := bundleFixture(t)
	os.MkdirAll(filepath.Dir(link), 0o755)
	os.Symlink(exe, link)
	f.mkdir(t, "App.app")
	f.env.AppDir = filepath.Join(f.home, "App.app")
	f.run(t, Options{Remove: true, Yes: true}, "")
	if _, err := os.Lstat(link); err == nil {
		t.Fatal("link remains")
	}
	if len(f.calls) != 2 || f.calls[0][5] != "false" || f.calls[1][0] != "open" {
		t.Fatalf("calls: %v", f.calls)
	}
}

func TestRemoveHintWhenAppUnreachable(t *testing.T) {
	f := newFixture(t)
	f.mkdir(t, ".claude")
	f.mkdir(t, "App.app")
	f.env.AppDir = filepath.Join(f.home, "App.app")
	f.env.Exec = func(n string, a ...string) error {
		if n == "open" {
			return errors.New("no app")
		}
		return nil
	}
	out := f.run(t, Options{Yes: true, Remove: true}, "")
	if !strings.Contains(out, "turn off Launch at Login") || strings.Contains(out, "switched off.") {
		t.Fatal(out)
	}
}

func TestRemoveSwitchesLoginOffMessage(t *testing.T) {
	f := newFixture(t)
	f.mkdir(t, "App.app")
	f.env.AppDir = filepath.Join(f.home, "App.app")
	out := f.run(t, Options{Yes: true, Remove: true}, "")
	if !strings.Contains(out, "Launch at Login switched off.") {
		t.Fatal(out)
	}
}

func TestHooksWillUseLine(t *testing.T) {
	f := newFixture(t)
	f.mkdir(t, ".claude")
	out := f.run(t, Options{Yes: true}, "")
	if !strings.Contains(out, "Hooks will use: /opt/homebrew/bin/agentstatus") {
		t.Fatal(out)
	}
}

func TestAppDirFor(t *testing.T) {
	if got := AppDirFor("/Users/x/Applications/AgentStatus.app/Contents/Helpers/agentstatus", "/Applications/AgentStatus.app"); got != "/Users/x/Applications/AgentStatus.app" {
		t.Fatal(got)
	}
	d := t.TempDir()
	second := filepath.Join(d, "AgentStatus.app")
	os.MkdirAll(second, 0o755)
	if got := AppDirFor("/opt/homebrew/bin/agentstatus", filepath.Join(d, "none.app"), second); got != second {
		t.Fatal(got)
	}
	if got := AppDirFor("/opt/homebrew/bin/agentstatus", "/nope/a.app", "/nope/b.app"); got != "/nope/a.app" {
		t.Fatal(got)
	}
}

func TestLoginNoAnswerWritesNothing(t *testing.T) {
	f := newFixture(t)
	f.mkdir(t, ".claude")
	f.mkdir(t, "App.app")
	f.env.AppDir = filepath.Join(f.home, "App.app")
	f.run(t, Options{}, "y\n") // Claude ja, Login-Frage unbeantwortet
	if len(f.calls) != 1 || f.calls[0][0] != "open" {
		t.Fatalf("calls: %v", f.calls)
	}
	f.calls = nil
	f.run(t, Options{}, "n\n") // schon verbunden: nur die Login-Frage, ausdrueckliches Nein
	if len(f.calls) != 2 || f.calls[0][5] != "false" {
		t.Fatalf("calls: %v", f.calls)
	}
}

func TestOnlyAbsentAgentMessage(t *testing.T) {
	f := newFixture(t)
	f.mkdir(t, ".claude")
	out := f.run(t, Options{Yes: true, Only: []string{"codex"}}, "")
	if !strings.Contains(out, "Codex not found") || strings.Contains(out, "No supported") {
		t.Fatal(out)
	}
	if f.exists(".codex") {
		t.Fatal("codex dir created")
	}
}

func TestEnvDirsHonoured(t *testing.T) {
	f := newFixture(t)
	cc, ch := filepath.Join(f.home, "cc"), filepath.Join(f.home, "ch")
	f.mkdir(t, "cc")
	f.mkdir(t, "ch")
	f.env.Getenv = func(k string) string { return map[string]string{"CLAUDE_CONFIG_DIR": cc, "CODEX_HOME": ch}[k] }
	f.run(t, Options{Yes: true}, "")
	if !f.exists("cc/settings.json") || !f.exists("ch/hooks.json") || f.exists(".claude") || f.exists(".codex") {
		t.Fatal("env dirs not used")
	}
}

func TestForeignLinkLeftAlone(t *testing.T) {
	for _, mk := range []string{"file", "link"} {
		f, exe, link := bundleFixture(t)
		os.MkdirAll(filepath.Dir(link), 0o755)
		if mk == "file" {
			os.WriteFile(link, []byte("x"), 0o755)
		} else {
			os.Symlink("/usr/bin/true", link)
		}
		out := f.run(t, Options{Yes: true}, "")
		if !strings.Contains(out, "not ours") || strings.Contains(out, "Create a link") {
			t.Fatalf("%s: %s", mk, out)
		}
		if !strings.Contains(f.read(".claude/settings.json"), exe+" hook claude") {
			t.Fatalf("%s: bundle path expected", mk)
		}
		f.run(t, Options{Yes: true, Remove: true}, "")
		if _, err := os.Lstat(link); err != nil {
			t.Fatalf("%s: foreign link removed", mk)
		}
	}
}

func TestLinkToOtherBundleIsOurs(t *testing.T) {
	f, _, link := bundleFixture(t)
	os.MkdirAll(filepath.Dir(link), 0o755)
	other := "/Applications/AgentStatus.app/Contents/Helpers/agentstatus"
	os.Symlink(other, link)
	out := f.run(t, Options{Yes: true}, "")
	if !strings.Contains(out, "already links to "+other+"; left as is") {
		t.Fatal(out)
	}
	if !strings.Contains(f.read(".claude/settings.json"), link+" hook claude") {
		t.Fatal("link path expected")
	}
}

func TestTempAppNoLink(t *testing.T) {
	f, _, _ := bundleFixture(t)
	f.env.Check = func(string) error { return errors.New("temporary") }
	var out bytes.Buffer
	if Run(f.env, Options{Yes: true}, IO{In: strings.NewReader(""), Out: &out}) == nil || f.exists(".local") {
		t.Fatal("link created for temp app")
	}
}
