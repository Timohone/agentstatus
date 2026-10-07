package install

import (
	"bytes"
	"os"
	"path/filepath"
	"time"
)

// GrokEvents: inkl. der Grok-eigenen Ereignisse. Kein PreToolUse: die Grok-Doku nennt keine Rueckfrage-Werkzeuge.
var GrokEvents = []string{"SessionStart", "UserPromptSubmit", "PostToolUse", "Notification", "Stop", "StopCancelled", "StopFailure", "PermissionDenied", "SessionEnd"}

// GrokHooksPath: eigene Datei in ~/.grok/hooks/ (Grok liest dort alle *.json); eine fremde Datei fassen wir nie an.
func GrokHooksPath() string {
	home, _ := os.UserHomeDir()
	return GrokHooksPathFor(home)
}

func GrokHooksPathFor(home string) string {
	return filepath.Join(home, ".grok", "hooks", "agentstatus.json")
}

func PlanGrok(old []byte, command string, add bool) ([]byte, bool, error) {
	return planHooks(old, command, add, GrokEvents, nil, oursFor("claude"), "agentstatus.json")
}

// ApplyGrok schreibt oder entfernt die Datei; nach dem Uninstall bleibt keine leere Huelle zurueck.
func ApplyGrok(path, command string, add bool, confirm func(diff string) bool, now time.Time) (bool, error) {
	changed, err := applyFile(path, func(old []byte) ([]byte, bool, error) { return PlanGrok(old, command, add) }, confirm, now)
	if err == nil && changed && !add {
		if b, _ := os.ReadFile(path); bytes.Equal(bytes.TrimSpace(b), []byte("{}")) {
			err = os.Remove(path)
		}
	}
	return changed, err
}
