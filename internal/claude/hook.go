// Package claude bildet die Hook-Ereignisse von Claude Code auf das Protokoll ab.
package claude

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/Timohone/agentstatus/internal/session"
)

const Agent = "claude-code"

// Event deckt Claudes Nutzlast und Groks Zusatzfelder (camelCase, snake_case-Ereignisnamen) ab.
type Event struct {
	Name             string          `json:"hook_event_name"`
	NameGrok         string          `json:"hookEventName"`
	SessionID        string          `json:"session_id"`
	SessionIDGrok    string          `json:"sessionId"`
	CWD              string          `json:"cwd"`
	TranscriptPath   string          `json:"transcript_path"`
	Message          string          `json:"message"`
	NotificationType string          `json:"notification_type"`
	Source           string          `json:"source"`
	ToolName         string          `json:"tool_name"`
	LastMessage      string          `json:"last_assistant_message"` // Stop: letzte Antwort, falls Claude sie liefert
	Error            json.RawMessage `json:"error"`                  // StopFailure (Grok): Fehlertyp; nur ein String zaehlt
}

type Action struct {
	Skip, Clear bool
	Status      session.Status
	Detail      string
}

var grokNames = map[string]string{
	"session_start": "SessionStart", "stop": "Stop", "user_prompt_submit": "UserPromptSubmit",
	"post_tool_use": "PostToolUse", "session_end": "SessionEnd", "notification": "Notification",
	"stop_cancelled": "StopCancelled", "stop_failure": "StopFailure", "permission_denied": "PermissionDenied",
}

// normalized fuellt leere Claude-Felder aus den Grok-Feldern; snake_case-Namen werden PascalCase.
func (ev Event) normalized() Event {
	if ev.SessionID == "" {
		ev.SessionID = ev.SessionIDGrok
	}
	if ev.Name == "" {
		ev.Name = ev.NameGrok
	}
	if n, ok := grokNames[ev.Name]; ok {
		ev.Name = n
	}
	return ev
}

func errorText(raw json.RawMessage) string {
	var t string
	if json.Unmarshal(raw, &t) != nil {
		return ""
	}
	return t
}

// DetectAgent: Grok ruft die Claude-Hooks per Kompatibilitaet auf. Der Prozessname entscheidet
// (comm ist gekuerzt, daher Praefix); nur bei leerem oder fremdem Namen (node, bun) gilt GROK_SESSION_ID.
func DetectAgent(procName string, getenv func(string) string) string {
	n := strings.ToLower(procName)
	switch {
	case strings.HasPrefix(n, "grok"):
		return "grok"
	case strings.HasPrefix(n, "claude"):
		return Agent
	case getenv("GROK_SESSION_ID") != "":
		return "grok"
	}
	return Agent
}

func Map(ev Event) Action {
	ev = ev.normalized()
	if !session.ValidName(ev.SessionID) {
		return Action{Skip: true}
	}
	switch ev.Name {
	case "SessionStart":
		if ev.Source == "compact" {
			return Action{Skip: true} // Kompaktierung mitten im Zug: der Status bleibt
		}
		return Action{Status: session.Idle}
	case "Stop":
		if endsWithQuestion(ev.LastMessage) {
			return Action{Status: session.Waiting, Detail: "Claude asked you something"}
		}
		return Action{Status: session.Idle}
	case "StopCancelled":
		return Action{Status: session.Idle}
	case "PreToolUse":
		// nur die Rueckfrage-Dialoge: sie loesen weder Notification noch Stop aus
		switch ev.ToolName {
		case "AskUserQuestion":
			return Action{Status: session.Waiting, Detail: "Claude asks you a question"}
		case "ExitPlanMode":
			return Action{Status: session.Waiting, Detail: "Claude waits for plan approval"}
		}
	case "StopFailure":
		return Action{Status: session.Error, Detail: errorText(ev.Error)}
	case "UserPromptSubmit", "PostToolUse", "PermissionDenied":
		// nach einer Freigabe/Ablehnung arbeitet der Agent weiter; sonst stuende "waiting" bis zum Stop
		return Action{Status: session.Working}
	case "SessionEnd":
		return Action{Clear: true}
	case "Notification":
		switch ev.NotificationType {
		case "permission_prompt", "elicitation_dialog", "elicitation_url_dialog", "agent_needs_input", "":
			return Action{Status: session.Waiting, Detail: ev.Message}
		case "idle_prompt", "task_complete":
			return Action{Status: session.Idle}
		}
	}
	return Action{Skip: true}
}

// Apply: pid 0 heisst "keine pid" (Hintergrunddienst) und entfernt eine alte pid aus der Datei.
func Apply(dir, agent string, ev Event, pid int, now time.Time) error {
	ev = ev.normalized()
	if ev.Name == "Stop" && ev.LastMessage == "" {
		ev.LastMessage = lastAssistantText(ev.TranscriptPath, configRoot())
	}
	a := Map(ev)
	switch {
	case a.Skip:
		return nil
	case a.Clear:
		return session.Clear(dir, agent, ev.SessionID)
	}
	_, err := session.Set(dir, session.Session{Agent: agent, ID: ev.SessionID, Status: a.Status,
		Detail: a.Detail, CWD: ev.CWD, Topic: readTopic(ev.TranscriptPath, configRoot()), PID: pid, NoPID: pid == 0}, now)
	return err
}

// endsWithQuestion ist eine Heuristik: endet die letzte Antwort (ohne Markdown-Zeichen und Klammern
// am Ende) auf "?", wartet Claude vermutlich auf eine Antwort.
func endsWithQuestion(s string) bool {
	return strings.HasSuffix(strings.TrimRight(s, " \t\r\n*_`)"), "?")
}
