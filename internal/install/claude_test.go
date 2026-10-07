package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const cmd = "/opt/homebrew/bin/agentstatus hook claude"

var t0 = time.Date(2026, 10, 6, 18, 0, 0, 0, time.UTC)

func hooks(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	h, _ := m["hooks"].(map[string]any)
	return h
}

func TestPlanAddsKeepsExisting(t *testing.T) {
	in := []byte(`{"model":"opus","hooks":{"Stop":[{"hooks":[{"type":"command","command":"say fertig"}]}]}}`)
	out, changed, err := PlanClaude(in, cmd, true)
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	if !strings.Contains(string(out), `"model": "opus"`) || !strings.Contains(string(out), "say fertig") {
		t.Fatalf("Bestehendes verloren:\n%s", out)
	}
	h := hooks(t, out)
	for _, ev := range ClaudeEvents {
		if !strings.Contains(mustJSON(h[ev]), cmd) {
			t.Errorf("%s fehlt", ev)
		}
	}
	if n := len(h["Stop"].([]any)); n != 2 {
		t.Errorf("Stop: %d Gruppen, erwartet 2 (fremde + eigene)", n)
	}
}

func TestInstallIdempotent(t *testing.T) {
	once, _, _ := PlanClaude([]byte(`{}`), cmd, true)
	twice, changed, err := PlanClaude(once, cmd, true)
	if err != nil || changed || string(once) != string(twice) {
		t.Fatalf("zweites install hat geaendert: %v %v", changed, err)
	}
}

func TestUninstallExact(t *testing.T) {
	orig := []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"say fertig"}]}]},"theme":"dark"}`)
	added, _, _ := PlanClaude(orig, cmd, true)
	removed, changed, err := PlanClaude(added, cmd, false)
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	var a, b any
	json.Unmarshal(orig, &a)
	json.Unmarshal(removed, &b)
	if mustJSON(a) != mustJSON(b) {
		t.Fatalf("nicht exakt zurueck:\n%s\nvs\n%s", orig, removed)
	}
}

func TestInstallRefusesInvalidJSON(t *testing.T) {
	if _, _, err := PlanClaude([]byte(`{"hooks": // Kommentar`), cmd, true); err == nil {
		t.Fatal("Fehler erwartet")
	}
}

func TestInstallCreatesMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "neu", "settings.json")
	changed, err := ApplyClaude(p, cmd, true, func(string) bool { return true }, t0)
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), cmd) {
		t.Fatalf("%s", b)
	}
}

func TestInstallFollowsSymlink(t *testing.T) {
	d := t.TempDir()
	real := filepath.Join(d, "repo-settings.json")
	os.WriteFile(real, []byte(`{"theme":"dark"}`), 0o644)
	link := filepath.Join(d, "settings.json")
	os.Symlink(real, link)
	if _, err := ApplyClaude(link, cmd, true, func(string) bool { return true }, t0); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Lstat(link)
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("Symlink wurde durch eine Datei ersetzt")
	}
	b, _ := os.ReadFile(real)
	if !strings.Contains(string(b), cmd) {
		t.Fatal("Ziel des Links nicht geschrieben")
	}
	backups, _ := filepath.Glob(filepath.Join(d, "repo-settings.json.agentstatus-*"))
	if len(backups) != 1 {
		t.Fatalf("Sicherung fehlt: %v", backups)
	}
}

func TestInstallDeclinedWritesNothing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(p, []byte(`{}`), 0o644)
	changed, err := ApplyClaude(p, cmd, true, func(string) bool { return false }, t0)
	b, _ := os.ReadFile(p)
	if err != ErrDeclined || changed || string(b) != "{}" {
		t.Fatalf("expected ErrDeclined, got %v %v %s", err, changed, b)
	}
}

func TestDanglingSymlink(t *testing.T) {
	d := t.TempDir()
	link := filepath.Join(d, "settings.json")
	target := filepath.Join(d, "real", "settings.json")
	os.Symlink(target, link)
	changed, err := ApplyClaude(link, cmd, true, func(string) bool { return true }, t0)
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	fi, _ := os.Lstat(link)
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("Symlink wurde durch eine Datei ersetzt")
	}
	b, _ := os.ReadFile(target)
	if !strings.Contains(string(b), cmd) {
		t.Fatal("Ziel des Links nicht geschrieben")
	}
}

func TestForeignGroupSurvivesInstall(t *testing.T) {
	// Foreign group {"matcher":"x"} (no hooks key) should not get hooks added
	in := []byte(`{"hooks":{"Stop":[{"matcher":"x"}]}}`)
	out, _, _ := PlanClaude(in, cmd, true)
	// Parse and verify the foreign group is unchanged
	var outObj any
	json.Unmarshal(out, &outObj)
	outHooks := outObj.(map[string]any)["hooks"].(map[string]any)["Stop"].([]any)
	// Find the foreign group in the output (the first one should be our agentstatus entry)
	var foreignGroup map[string]any
	for _, g := range outHooks {
		gm := g.(map[string]any)
		if _, hasHooks := gm["hooks"]; !hasHooks {
			foreignGroup = gm
			break
		}
	}
	if foreignGroup == nil || !strings.Contains(mustJSON(foreignGroup), "matcher") {
		t.Fatalf("Foreign group not preserved:\nin: %s\nout: %s", in, out)
	}
	// Verify {"hooks":[]} stays as {"hooks":[]}
	in2 := []byte(`{"hooks":{"Stop":[{"matcher":"x","hooks":[]}]}}`)
	out2, _, _ := PlanClaude(in2, cmd, true)
	if !strings.Contains(string(out2), `"hooks": []`) {
		t.Fatalf("Empty hooks array was mutated:\nout: %s", out2)
	}
}

func TestForeignGroupSurvivesUninstall(t *testing.T) {
	// Add, then remove; should return to original state
	orig := []byte(`{"hooks":{"Stop":[{"matcher":"x"}]}}`)
	added, _, _ := PlanClaude(orig, cmd, true)
	removed, _, _ := PlanClaude(added, cmd, false)
	var a, b any
	json.Unmarshal(orig, &a)
	json.Unmarshal(removed, &b)
	if mustJSON(a) != mustJSON(b) {
		t.Fatalf("Foreign group not restored:\norig: %s\nfinal: %s", orig, removed)
	}
}

func TestInvalidTopLevelStructure(t *testing.T) {
	tests := [][]byte{
		[]byte(`null`),
		[]byte(`[]`),
		[]byte(`{"hooks":[]}`),
		[]byte(`{"hooks":{"Stop":{}}}`),
	}
	for _, in := range tests {
		_, _, err := PlanClaude(in, cmd, true)
		if err == nil {
			t.Errorf("expected error for %s", in)
		}
	}
}

func TestSpecialCharsInCommand(t *testing.T) {
	specialCmd := "/tmp/my dir/agentstatus"
	quotedCmd := QuoteCommand(specialCmd) + " hook claude"
	out, _, _ := PlanClaude([]byte(`{}`), quotedCmd, true)
	// Verify the command is quoted in output
	if !strings.Contains(string(out), "my dir") {
		t.Fatalf("Special chars lost in output: %s", out)
	}
	// Verify ours() can still recognize it
	h := map[string]any{"command": quotedCmd}
	if !oursFor("claude")(h) {
		t.Fatalf("oursFor didn't recognize quoted command: %s", quotedCmd)
	}
}

func TestLargeNumberPrecision(t *testing.T) {
	in := []byte(`{"number":12345678901234567890}`)
	out, _, _ := PlanClaude(in, cmd, true)
	if !strings.Contains(string(out), "12345678901234567890") {
		t.Fatalf("Number precision lost: %s", out)
	}
}

func TestBackupUniqueness(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(p, []byte(`{"a":1}`), 0o644)
	ApplyClaude(p, cmd, true, func(string) bool { return true }, t0)
	ApplyClaude(p, cmd, false, func(string) bool { return true }, t0)
	backups, _ := filepath.Glob(filepath.Join(filepath.Dir(p), "settings.json.agentstatus-*"))
	if len(backups) != 2 {
		t.Fatalf("expected 2 backups, got %d: %v", len(backups), backups)
	}
	b0, _ := os.ReadFile(backups[0])
	if !strings.Contains(string(b0), `"a"`) {
		t.Fatalf("First backup doesn't contain original: %s", b0)
	}
}

func TestPreserveFileMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(p, []byte(`{}`), 0o644)
	// Explicitly set mode to 0o664 before ApplyClaude
	os.Chmod(p, 0o664)
	ApplyClaude(p, cmd, true, func(string) bool { return true }, t0)
	fiAfter, _ := os.Stat(p)
	modeAfter := fiAfter.Mode().Perm()
	if modeAfter != 0o664 {
		t.Fatalf("mode not preserved: expected 0o664, got %o", modeAfter)
	}
}

func TestHTMLNotEscaped(t *testing.T) {
	// Foreign command with special chars should not be HTML-escaped
	in := []byte(`{"hooks":{"Stop":[{"command":"a && b <x>"}]}}`)
	out, _, _ := PlanClaude(in, cmd, true)
	if !strings.Contains(string(out), "a && b <x>") {
		t.Fatalf("HTML chars were escaped:\n%s", out)
	}
}

func TestSymlinkChain(t *testing.T) {
	d := t.TempDir()
	link1 := filepath.Join(d, "settings.json")
	link2 := filepath.Join(d, "link2.json")
	target := filepath.Join(d, "real.json")
	os.Symlink("link2.json", link1)
	os.Symlink("real.json", link2)
	_, err := ApplyClaude(link1, cmd, true, func(string) bool { return true }, t0)
	if err != nil {
		t.Fatal(err)
	}
	// Both symlinks should still be symlinks
	fi1, _ := os.Lstat(link1)
	fi2, _ := os.Lstat(link2)
	if fi1.Mode()&os.ModeSymlink == 0 || fi2.Mode()&os.ModeSymlink == 0 {
		t.Fatal("Symlink was replaced with regular file")
	}
	// Target should have the hook
	b, _ := os.ReadFile(target)
	if !strings.Contains(string(b), cmd) {
		t.Fatal("Target not written")
	}
}

func TestRelativeDanglingSymlink(t *testing.T) {
	d := t.TempDir()
	link := filepath.Join(d, "settings.json")
	// Create a dangling symlink with relative target
	os.Symlink("sub/real.json", link)
	_, err := ApplyClaude(link, cmd, true, func(string) bool { return true }, t0)
	if err != nil {
		t.Fatal(err)
	}
	// Link should still be a symlink
	fi, _ := os.Lstat(link)
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("Symlink was replaced")
	}
	// Target should exist
	target := filepath.Join(d, "sub", "real.json")
	b, _ := os.ReadFile(target)
	if !strings.Contains(string(b), cmd) {
		t.Fatal("Target not created")
	}
}

func TestSymlinkLoop(t *testing.T) {
	d := t.TempDir()
	link1 := filepath.Join(d, "link1.json")
	link2 := filepath.Join(d, "link2.json")
	os.Symlink("link2.json", link1)
	os.Symlink("link1.json", link2)
	_, err := ApplyClaude(link1, cmd, true, func(string) bool { return true }, t0)
	// Should get an error trying to read the loop
	if err == nil {
		t.Fatal("Expected error for symlink loop")
	}
}

func TestTrailingData(t *testing.T) {
	in := []byte(`{} garbage`)
	_, _, err := PlanClaude(in, cmd, true)
	if err == nil || !strings.Contains(err.Error(), "more than one JSON object") {
		t.Fatalf("Expected error for trailing data, got: %v", err)
	}
}

func TestNullHooks(t *testing.T) {
	// "hooks": null should be treated as absent
	in := []byte(`{"hooks":null}`)
	out, _, err := PlanClaude(in, cmd, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), cmd) {
		t.Fatalf("Hook not added when hooks was null: %s", out)
	}
}

func TestNullEvent(t *testing.T) {
	// "Stop": null should be treated as absent
	in := []byte(`{"hooks":{"Stop":null}}`)
	out, _, err := PlanClaude(in, cmd, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), cmd) {
		t.Fatalf("Hook not added when event was null: %s", out)
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestCheckBinaryRefusesTemp(t *testing.T) {
	if err := CheckBinary(filepath.Join(os.TempDir(), "go-build123", "agentstatus")); err == nil {
		t.Error("temporary binary must be refused")
	}
	if err := CheckBinary("/usr/local/bin/agentstatus"); err != nil {
		t.Error(err)
	}
}

func TestPreToolUseHasMatcher(t *testing.T) {
	out, _, err := PlanClaude(nil, cmd, true)
	if err != nil {
		t.Fatal(err)
	}
	g := hooks(t, out)["PreToolUse"].([]any)[0].(map[string]any)
	if g["matcher"] != "AskUserQuestion|ExitPlanMode" {
		t.Fatalf("matcher: %v", g)
	}
	if hooks(t, out)["Stop"].([]any)[0].(map[string]any)["matcher"] != nil {
		t.Error("Stop darf keinen Matcher tragen")
	}
	again, changed, _ := PlanClaude(out, cmd, true)
	if changed || string(again) != string(out) {
		t.Error("nicht idempotent")
	}
	gone, _, _ := PlanClaude(out, cmd, false)
	if strings.Contains(string(gone), "agentstatus") || strings.Contains(string(gone), "matcher") {
		t.Errorf("Rueckweg unvollstaendig:\n%s", gone)
	}
}

func countOurs(t *testing.T, b []byte, ev, agent string) int {
	t.Helper()
	n := 0
	for _, g := range hooks(t, b)[ev].([]any) {
		for _, h := range g.(map[string]any)["hooks"].([]any) {
			if oursFor(agent)(h) {
				n++
			}
		}
	}
	return n
}

func TestInstallRewritesOldPath(t *testing.T) {
	old := "/Users/x/go/bin/agentstatus hook claude"
	seed, _, _ := PlanClaude(nil, old, true)
	out, changed, err := PlanClaude(seed, cmd, true)
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	if strings.Contains(string(out), "go/bin") {
		t.Fatalf("alter Pfad noch da:\n%s", out)
	}
	for _, ev := range ClaudeEvents {
		if n := countOurs(t, out, ev, "claude"); n != 1 || !strings.Contains(mustJSON(hooks(t, out)[ev]), cmd) {
			t.Errorf("%s: %d eigene Eintraege", ev, n)
		}
	}
	g := hooks(t, out)["PreToolUse"].([]any)[0].(map[string]any)
	if g["matcher"] != "AskUserQuestion|ExitPlanMode" {
		t.Errorf("Matcher verloren: %v", g)
	}
	if _, changed, _ := PlanClaude(out, cmd, true); changed {
		t.Error("zweiter Lauf hat geaendert")
	}
	gone, _, _ := PlanClaude(seed, cmd, false) // Uninstall entfernt auch den alten Pfad
	if strings.Contains(string(gone), "agentstatus") {
		t.Errorf("Uninstall unvollstaendig:\n%s", gone)
	}
}

func TestInstallLeavesForeignPathAlone(t *testing.T) {
	in := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/usr/bin/other hook claude"}]}]}}`
	out, _, err := PlanClaude([]byte(in), cmd, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "/usr/bin/other hook claude") || countOurs(t, out, "Stop", "claude") != 1 {
		t.Fatalf("fremder Eintrag veraendert:\n%s", out)
	}
}

func TestInstallCollapsesDuplicates(t *testing.T) {
	in := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/a/agentstatus hook claude"},{"type":"command","command":"/b/agentstatus hook claude"}]}]}}`
	out, changed, err := PlanClaude([]byte(in), cmd, true)
	if err != nil || !changed || countOurs(t, out, "Stop", "claude") != 1 {
		t.Fatalf("%v %v\n%s", changed, err, out)
	}
}

func TestForeignHooksNamedLikeUsAreNotOurs(t *testing.T) {
	for _, c := range []string{"~/bin/agentstatus-notify.sh hook claude", "npx agentstatus-x hook claude"} {
		in := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"` + c + `"}]}]}}`
		out, _, err := PlanClaude([]byte(in), cmd, true)
		if err != nil || !strings.Contains(string(out), c) || countOurs(t, out, "Stop", "claude") != 1 {
			t.Fatalf("%s angefasst: %v\n%s", c, err, out)
		}
		gone, _, _ := PlanClaude(out, cmd, false)
		if !strings.Contains(string(gone), c) {
			t.Fatalf("uninstall entfernte %s:\n%s", c, gone)
		}
	}
}

func TestQuotedPathIsOursAndRewritten(t *testing.T) {
	old := QuoteCommand("/Users/John Doe/.local/bin/agentstatus") + " hook claude"
	if !oursFor("claude")(map[string]any{"command": old}) {
		t.Fatal("gequoteter Pfad nicht erkannt")
	}
	seed, _, _ := PlanClaude(nil, old, true)
	out, changed, err := PlanClaude(seed, cmd, true)
	if err != nil || !changed || strings.Contains(string(out), "John Doe") || countOurs(t, out, "Stop", "claude") != 1 {
		t.Fatalf("%v %v\n%s", changed, err, out)
	}
}

func TestDedupeKeepsForeignHookInSameGroup(t *testing.T) {
	in := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/a/agentstatus hook claude"},{"type":"command","command":"/usr/bin/other"},{"type":"command","command":"/b/agentstatus hook claude"}]}]}}`
	out, changed, err := PlanClaude([]byte(in), cmd, true)
	if err != nil || !changed || countOurs(t, out, "Stop", "claude") != 1 || !strings.Contains(string(out), "/usr/bin/other") {
		t.Fatalf("%v %v\n%s", changed, err, out)
	}
}

func TestDoubleQuotedPathIsOurs(t *testing.T) {
	if !oursFor("claude")(map[string]any{"command": `"/Users/John Doe/bin/agentstatus" hook claude`}) {
		t.Fatal("doppelt gequoteter Pfad nicht erkannt")
	}
}
