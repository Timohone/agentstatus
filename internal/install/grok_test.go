package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const grokCmd = "/usr/local/bin/agentstatus hook claude"

func grokFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), ".grok", "hooks", "agentstatus.json")
}

func TestGrokHooksPathUsesHome(t *testing.T) {
	t.Setenv("HOME", "/h")
	if p := GrokHooksPath(); p != "/h/.grok/hooks/agentstatus.json" {
		t.Fatal(p)
	}
}

func TestGrokInstallCycle(t *testing.T) {
	p := grokFile(t)
	if ch, err := ApplyGrok(p, grokCmd, true, yes, t0); !ch || err != nil {
		t.Fatal(ch, err)
	}
	b, _ := os.ReadFile(p)
	for _, ev := range GrokEvents {
		if !strings.Contains(mustJSON(hooks(t, b)[ev]), grokCmd) {
			t.Errorf("%s fehlt", ev)
		}
	}
	for _, ev := range []string{"StopCancelled", "StopFailure", "PermissionDenied"} {
		if hooks(t, b)[ev] == nil {
			t.Errorf("Grok-Ereignis %s fehlt", ev)
		}
	}
	if hooks(t, b)["PreToolUse"] != nil {
		t.Error("PreToolUse darf nicht installiert werden (Grok kennt keine Rueckfrage-Werkzeuge)")
	}
	if ch, _ := ApplyGrok(p, grokCmd, true, yes, t0); ch {
		t.Fatal("zweites install muss nichts tun")
	}
}

func TestGrokPathUpdateKeepsBackup(t *testing.T) {
	p := grokFile(t)
	ApplyGrok(p, "/old/agentstatus hook claude", true, yes, t0)
	if ch, err := ApplyGrok(p, grokCmd, true, yes, t0); !ch || err != nil {
		t.Fatal(ch, err)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "/old/") || !strings.Contains(string(b), grokCmd) {
		t.Fatalf("Pfad nicht aktualisiert: %s", b)
	}
	if bk, _ := filepath.Glob(p + ".agentstatus-*"); len(bk) != 1 {
		t.Fatalf("Sicherung fehlt: %v", bk)
	}
}

func TestGrokDeclinedWritesNothing(t *testing.T) {
	p := grokFile(t)
	if _, err := ApplyGrok(p, grokCmd, true, func(string) bool { return false }, t0); err != ErrDeclined {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("Datei darf nicht entstehen")
	}
}

func TestGrokUninstallRemovesOnlyOurFile(t *testing.T) {
	p := grokFile(t)
	other := filepath.Join(filepath.Dir(p), "fremd.json")
	ApplyGrok(p, grokCmd, true, yes, t0)
	os.WriteFile(other, []byte(`{"hooks":{}}`), 0o600)
	if ch, err := ApplyGrok(p, grokCmd, false, yes, t0); !ch || err != nil {
		t.Fatal(ch, err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("agentstatus.json muss weg sein")
	}
	if b, _ := os.ReadFile(other); string(b) != `{"hooks":{}}` {
		t.Fatal("fremde Datei veraendert")
	}
	if ch, err := ApplyGrok(p, grokCmd, false, yes, t0); ch || err != nil {
		t.Fatal("uninstall ohne Datei: nichts zu tun", ch, err)
	}
}

func TestGrokUninstallKeepsForeignEntriesInOurFile(t *testing.T) {
	p := grokFile(t)
	ApplyGrok(p, grokCmd, true, yes, t0)
	b, _ := os.ReadFile(p)
	b = []byte(strings.Replace(string(b), `"hooks": {`, `"hooks": {"Foo": [{"hooks":[{"type":"command","command":"x.sh"}]}],`, 1))
	os.WriteFile(p, b, 0o600)
	ApplyGrok(p, grokCmd, false, yes, t0)
	if out, _ := os.ReadFile(p); !strings.Contains(string(out), "x.sh") || strings.Contains(string(out), "agentstatus") {
		t.Fatalf("%s", out)
	}
}
