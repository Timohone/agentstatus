package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func yes(string) bool { return true }

func TestCodexInstallCycle(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hooks.json")
	orig := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"other.sh"}]}]}}`
	os.WriteFile(p, []byte(orig), 0o600)
	cmd := "/usr/local/bin/agentstatus hook codex"
	if ch, err := ApplyCodex(p, cmd, true, yes, time.Now()); !ch || err != nil {
		t.Fatal(ch, err)
	}
	b, _ := os.ReadFile(p)
	for _, ev := range CodexEvents {
		if !strings.Contains(string(b), `"`+ev+`"`) {
			t.Errorf("%s fehlt", ev)
		}
	}
	if !strings.Contains(string(b), "other.sh") {
		t.Fatal("fremder Hook verloren")
	}
	if ch, _ := ApplyCodex(p, cmd, true, yes, time.Now()); ch {
		t.Fatal("zweites install muss nichts tun")
	}
	if ch, err := ApplyCodex(p, cmd, false, yes, time.Now()); !ch || err != nil {
		t.Fatal(ch, err)
	}
	b, _ = os.ReadFile(p)
	if strings.Contains(string(b), "agentstatus") || !strings.Contains(string(b), "other.sh") {
		t.Fatalf("Rueckweg unsauber: %s", b)
	}
	if hooks(t, b)["Stop"] == nil || len(hooks(t, b)) != 1 {
		t.Fatalf("%s", b)
	}
}

func TestCodexIgnoresClaudeEntry(t *testing.T) {
	out, ch, err := PlanCodex([]byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/x/agentstatus hook claude"}]}]}}`), "/x/agentstatus hook codex", false)
	if err != nil || ch || !strings.Contains(string(out), "hook claude") {
		t.Fatal(ch, err, string(out))
	}
}

func TestCodexInvalidJSONAndSymlink(t *testing.T) {
	d := t.TempDir()
	bad := filepath.Join(d, "bad.json")
	os.WriteFile(bad, []byte("{nope"), 0o600)
	if _, err := ApplyCodex(bad, "a hook codex", true, yes, time.Now()); err == nil || !strings.Contains(err.Error(), "hooks.json") {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(bad); string(b) != "{nope" {
		t.Fatal("veraendert")
	}
	real := filepath.Join(d, "real.json")
	os.WriteFile(real, []byte("{}"), 0o600)
	link := filepath.Join(d, "hooks.json")
	os.Symlink(real, link)
	if _, err := ApplyCodex(link, "a hook codex", true, yes, time.Now()); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("Symlink ersetzt")
	}
	if b, _ := os.ReadFile(real); !strings.Contains(string(b), "PermissionRequest") {
		t.Fatal("Ziel nicht beschrieben")
	}
}

func TestCodexHooksPath(t *testing.T) {
	t.Setenv("CODEX_HOME", "/x/ch")
	if CodexHooksPath() != "/x/ch/hooks.json" {
		t.Fatal(CodexHooksPath())
	}
}

func TestCodexRewritesOldPath(t *testing.T) {
	newCmd := "/opt/homebrew/bin/agentstatus hook codex"
	seed, _, _ := PlanCodex(nil, "/Users/x/go/bin/agentstatus hook codex", true)
	foreign := strings.Replace(string(seed), `"hooks": {`, `"hooks": {"Extra": [{"hooks":[{"type":"command","command":"/usr/bin/other hook codex"}]}],`, 1)
	out, changed, err := PlanCodex([]byte(foreign), newCmd, true)
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	if strings.Contains(string(out), "go/bin") || !strings.Contains(string(out), "/usr/bin/other hook codex") {
		t.Fatalf("%s", out)
	}
	for _, ev := range CodexEvents {
		if n := countOurs(t, out, ev, "codex"); n != 1 {
			t.Errorf("%s: %d", ev, n)
		}
	}
	if _, changed, _ := PlanCodex(out, newCmd, true); changed {
		t.Error("zweiter Lauf hat geaendert")
	}
}
