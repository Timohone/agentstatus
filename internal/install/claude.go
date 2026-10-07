// Package install traegt agentstatus als Hook in die Konfiguration eines Agents ein.
package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrDeclined = errors.New("aborted by user")

var ClaudeEvents = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Notification", "Stop", "SessionEnd"}

// claudeMatchers: PreToolUse feuert vor jedem Werkzeug; wir wollen nur die Rueckfrage-Dialoge.
var claudeMatchers = map[string]string{"PreToolUse": "AskUserQuestion|ExitPlanMode"}

func ClaudeSettingsPath() string {
	home, _ := os.UserHomeDir()
	return ClaudeSettingsPathFor(home, os.Getenv)
}

// ClaudeSettingsPathFor: Pfad aus Home und Umgebung, damit setup und install dieselbe Regel nutzen.
func ClaudeSettingsPathFor(home string, getenv func(string) string) string {
	if d := getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "settings.json")
	}
	return filepath.Join(home, ".claude", "settings.json")
}

func QuoteCommand(path string) string {
	// If path contains spaces or shell metacharacters, single-quote it
	if strings.ContainsAny(path, " \t\n$`\\\"';&|<>()[]{}*?!~") {
		// Escape single quotes as '\''
		escaped := strings.ReplaceAll(path, "'", "'\\''")
		return "'" + escaped + "'"
	}
	return path
}

// firstWord liefert das erste Shell-Wort; einfache (wie QuoteCommand sie setzt) und doppelte Anfuehrungszeichen werden aufgeloest.
func firstWord(c string) string {
	var b strings.Builder
	var quote rune
	for _, r := range strings.TrimSpace(c) {
		switch {
		case quote == 0 && (r == '\'' || r == '"'):
			quote = r
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == ' ' || r == '\t'):
			return b.String()
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// oursFor: unser Eintrag = erstes Wort ist ein Programm namens agentstatus und der Befehl endet auf " hook <agent>".
func oursFor(agent string) func(any) bool {
	return func(h any) bool {
		m, _ := h.(map[string]any)
		c, _ := m["command"].(string)
		return filepath.Base(firstWord(c)) == "agentstatus" && strings.HasSuffix(strings.TrimSpace(c), " hook "+agent)
	}
}

// PlanClaude: reine Funktion, alter Inhalt -> neuer Inhalt. Ungueltiges JSON wird abgelehnt, nie ueberschrieben.
func PlanClaude(settings []byte, command string, add bool) ([]byte, bool, error) {
	return planHooks(settings, command, add, ClaudeEvents, claudeMatchers, oursFor("claude"), "settings.json")
}

func planHooks(settings []byte, command string, add bool, events []string, matchers map[string]string, ours func(any) bool, fname string) ([]byte, bool, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(settings)) > 0 {
		var obj any
		decoder := json.NewDecoder(bytes.NewReader(settings))
		decoder.UseNumber()
		if err := decoder.Decode(&obj); err != nil {
			return nil, false, fmt.Errorf("%s is not valid JSON, leaving it untouched: %w", fname, err)
		}
		// Validate that top level is an object
		rootObj, ok := obj.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("unexpected structure in %s, leaving it untouched: root is not an object", fname)
		}
		root = rootObj
		// Check for trailing data
		var trailing any
		err := decoder.Decode(&trailing)
		if err != io.EOF {
			return nil, false, fmt.Errorf("%s contains more than one JSON object, leaving it untouched", fname)
		}
	}

	// Validate structure
	if len(root) > 0 {
		// Top level must be an object (which it is)
		hooks, hooksPresent := root["hooks"]
		if hooksPresent && hooks != nil {
			hooksMap, ok := hooks.(map[string]any)
			if !ok {
				return nil, false, fmt.Errorf("unexpected structure in %s, leaving it untouched: hooks is not an object", fname)
			}
			// Validate each event's value is an array or null (treat null as absent)
			for _, ev := range events {
				if evVal, evPresent := hooksMap[ev]; evPresent && evVal != nil {
					if _, ok := evVal.([]any); !ok {
						return nil, false, fmt.Errorf("unexpected structure in %s, leaving it untouched: %s is not an array", fname, ev)
					}
				}
			}
		}
	}

	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	changed := false
	for _, ev := range events {
		evVal := hooks[ev]
		// Treat null as absent
		if evVal == nil {
			evVal = []any{}
		}
		groups, _ := evVal.([]any)
		present := false
		var kept []any
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			inner, _ := gm["hooks"].([]any)
			var rest []any
			for _, h := range inner {
				if ours(h) {
					if !add || present {
						// Uninstall, oder zweiter eigener Eintrag im selben Ereignis: entfernen
						present = true
						changed = true
						continue
					}
					present = true
					// Eigener Eintrag mit altem Programmpfad: Befehl ersetzen, Gruppe (Matcher) bleibt.
					if hm, _ := h.(map[string]any); hm["command"] != command {
						hm["command"] = command
						changed = true
					}
				}
				rest = append(rest, h)
			}
			if len(rest) == 0 && len(inner) > 0 {
				continue // Gruppe bestand nur aus unserem Eintrag
			}
			// Only reassign if something was actually removed
			if len(rest) != len(inner) && gm != nil {
				gm["hooks"] = rest
			}
			kept = append(kept, g)
		}
		if add && !present {
			g := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command}}}
			if m := matchers[ev]; m != "" {
				g["matcher"] = m
			}
			kept = append(kept, g)
			changed = true
		}
		if len(kept) == 0 {
			delete(hooks, ev)
		} else {
			hooks[ev] = kept
		}
	}
	if len(hooks) == 0 {
		delete(root, "hooks")
	} else {
		root["hooks"] = hooks
	}

	// Encode with proper formatting and HTML escaping disabled
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(root); err != nil {
		return nil, false, err
	}
	// ponytail: Schluessel werden sortiert; reihenfolgetreu nur mit Token-Parser, falls sich jemand beschwert.
	return buf.Bytes(), changed, nil
}

func ApplyClaude(path, command string, add bool, confirm func(diff string) bool, now time.Time) (bool, error) {
	return applyFile(path, func(old []byte) ([]byte, bool, error) { return PlanClaude(old, command, add) }, confirm, now)
}

func applyFile(path string, plan func(old []byte) ([]byte, bool, error), confirm func(diff string) bool, now time.Time) (bool, error) {
	// Handle symlink chains: resolve iteratively up to 40 levels
	targetPath := path
	currentPath := path
	for i := 0; i < 40; i++ {
		fi, err := os.Lstat(currentPath)
		if err != nil {
			if !os.IsNotExist(err) {
				return false, err
			}
			// File doesn't exist; if it's a dangling link, currentPath is the target
			break
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			// Not a symlink; we've reached the target
			targetPath = currentPath
			break
		}
		// It's a symlink; resolve it
		link, err := os.Readlink(currentPath)
		if err != nil {
			return false, err
		}
		// Resolve relative to the directory containing the symlink
		if !filepath.IsAbs(link) {
			link = filepath.Join(filepath.Dir(currentPath), link)
		}
		currentPath = link
	}
	if targetPath == path {
		// We went through all 40 iterations; likely a symlink loop
		targetPath = currentPath
	}

	old, err := os.ReadFile(targetPath)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	out, changed, err := plan(old)
	if err != nil || !changed {
		return false, err
	}
	if !confirm(fmt.Sprintf("--- %s (before)\n%s\n+++ after\n%s", targetPath, old, out)) {
		return false, ErrDeclined
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return false, err
	}
	if len(old) > 0 {
		// Create backup with unique name if it exists
		baseBackup := targetPath + ".agentstatus-" + now.Format("20060102-150405")
		backup := baseBackup
		backupCreated := false
		for i := 0; i <= 1000; i++ {
			if i > 0 {
				backup = fmt.Sprintf("%s-%d", baseBackup, i)
			}
			f, err := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err == nil {
				if _, err := f.Write(old); err != nil {
					f.Close()
					return false, err
				}
				f.Close()
				backupCreated = true
				break
			}
			if !os.IsExist(err) {
				return false, err
			}
		}
		if !backupCreated {
			return false, fmt.Errorf("could not create backup: no free file name")
		}
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(targetPath); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp := fmt.Sprintf("%s.tmp-%d", targetPath, os.Getpid())
	if err := os.WriteFile(tmp, out, mode); err != nil {
		return false, err
	}
	// Ensure mode is exactly preserved (umask handling)
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return false, err
	}
	if err := os.Rename(tmp, targetPath); err != nil {
		os.Remove(tmp)
		return false, err
	}
	return true, nil
}

// CheckBinary lehnt temporaere Binaries ab (z. B. von `go run`): der Hook-Befehl wuerde ins Leere zeigen.
func CheckBinary(bin string) error {
	bins := []string{bin}
	if r, err := filepath.EvalSymlinks(bin); err == nil {
		bins = append(bins, r)
	}
	tmps := []string{os.TempDir()}
	if r, err := filepath.EvalSymlinks(tmps[0]); err == nil {
		tmps = append(tmps, r) // macOS: /var ist ein Link auf /private/var
	}
	for _, b := range bins {
		for _, t := range tmps {
			if rel, err := filepath.Rel(t, b); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("refusing to install a temporary binary (%s); build or install agentstatus first", bin)
			}
		}
	}
	return nil
}
