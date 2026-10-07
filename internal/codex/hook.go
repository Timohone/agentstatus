// Package codex bildet die Hook-Ereignisse der Codex CLI auf das Protokoll ab (laut Doku, nicht am echten Lauf belegt).
package codex

import (
	"encoding/json"
	"time"

	"github.com/Timohone/agentstatus/internal/session"
)

const Agent = "codex"

type Event struct {
	Name      string          `json:"hook_event_name"`
	SessionID string          `json:"session_id"`
	CWD       string          `json:"cwd"`
	Source    string          `json:"source"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

// description liefert tool_input.description, nur wenn tool_input ein Objekt mit String-Feld ist.
func (e Event) description() string {
	var o struct {
		Description string `json:"description"`
	}
	if json.Unmarshal(e.ToolInput, &o) != nil {
		return ""
	}
	return o.Description
}

type Action struct {
	Skip, Clear bool
	Status      session.Status
	Detail      string
}

func Map(ev Event) Action {
	if !session.ValidName(ev.SessionID) {
		return Action{Skip: true}
	}
	switch ev.Name {
	case "SessionStart":
		if ev.Source == "compact" {
			return Action{Skip: true} // Kompaktierung mitten im Zug: der Status bleibt
		}
		return Action{Status: session.Idle}
	case "Stop", "Interrupt":
		return Action{Status: session.Idle}
	case "UserPromptSubmit", "PostToolUse":
		return Action{Status: session.Working}
	case "PermissionRequest":
		d := ev.description()
		if d == "" {
			d = ev.ToolName
		}
		return Action{Status: session.Waiting, Detail: d}
	case "SessionEnd":
		return Action{Clear: true}
	}
	return Action{Skip: true}
}

// Apply: pid 0 heisst "keine pid" (Hintergrunddienst) und entfernt eine alte pid aus der Datei.
func Apply(dir, agent string, ev Event, pid int, now time.Time) error {
	a := Map(ev)
	switch {
	case a.Skip:
		return nil
	case a.Clear:
		return session.Clear(dir, agent, ev.SessionID)
	}
	_, err := session.Set(dir, session.Session{Agent: agent, ID: ev.SessionID, Status: a.Status,
		Detail: a.Detail, CWD: ev.CWD, PID: pid, NoPID: pid == 0}, now)
	return err
}
