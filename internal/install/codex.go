package install

import (
	"os"
	"path/filepath"
	"time"
)

var CodexEvents = []string{"SessionStart", "UserPromptSubmit", "PostToolUse", "PermissionRequest", "Stop", "Interrupt", "SessionEnd"}

func CodexHooksPath() string {
	home, _ := os.UserHomeDir()
	return CodexHooksPathFor(home, os.Getenv)
}

func CodexHooksPathFor(home string, getenv func(string) string) string {
	if d := getenv("CODEX_HOME"); d != "" {
		return filepath.Join(d, "hooks.json")
	}
	return filepath.Join(home, ".codex", "hooks.json")
}

func PlanCodex(old []byte, command string, add bool) ([]byte, bool, error) {
	return planHooks(old, command, add, CodexEvents, nil, oursFor("codex"), "hooks.json")
}

func ApplyCodex(path, command string, add bool, confirm func(diff string) bool, now time.Time) (bool, error) {
	return applyFile(path, func(old []byte) ([]byte, bool, error) { return PlanCodex(old, command, add) }, confirm, now)
}
