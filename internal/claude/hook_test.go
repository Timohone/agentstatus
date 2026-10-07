package claude

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Timohone/agentstatus/internal/session"
)

var t0 = time.Date(2026, 10, 6, 18, 0, 0, 0, time.UTC)

func fixture(t *testing.T, name string) Event {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var ev Event
	if err := json.Unmarshal(b, &ev); err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestMapFixtures(t *testing.T) {
	cases := map[string]Action{
		"session_start.json":                {Status: session.Idle},
		"user_prompt_submit.json":           {Status: session.Working},
		"post_tool_use.json":                {Status: session.Working},
		"notification_permission.json":      {Status: session.Waiting},
		"notification_idle.json":            {Status: session.Idle},
		"stop.json":                         {Status: session.Idle},
		"session_end.json":                  {Clear: true},
		"grok_session_start.json":           {Status: session.Idle},
		"grok_user_prompt_submit.json":      {Status: session.Working},
		"grok_stop_cancelled.json":          {Status: session.Idle},
		"grok_notification_permission.json": {Status: session.Waiting},
	}
	for file, want := range cases {
		got := Map(fixture(t, file))
		if got.Skip != want.Skip || got.Clear != want.Clear || got.Status != want.Status {
			t.Errorf("%s: %+v, erwartet %+v", file, got, want)
		}
	}
}

func TestMapDetail(t *testing.T) {
	w := Map(Event{Name: "Notification", SessionID: "s", NotificationType: "permission_prompt", Message: "Claude needs your permission to use Bash"})
	if w.Status != session.Waiting || w.Detail != "Claude needs your permission to use Bash" {
		t.Errorf("%+v", w)
	}
}

func TestMapGrokEvents(t *testing.T) {
	cases := map[string]Event{
		"idle":    {NameGrok: "stop_cancelled", SessionIDGrok: "g"},
		"working": {NameGrok: "permission_denied", SessionIDGrok: "g"},
		"error":   {NameGrok: "stop_failure", SessionIDGrok: "g", Error: json.RawMessage(`"rate_limit"`)},
	}
	for want, ev := range cases {
		if a := Map(ev); a.Skip || string(a.Status) != want {
			t.Errorf("%s: %+v", want, a)
		}
	}
	if a := Map(Event{Name: "StopFailure", SessionID: "g", Error: json.RawMessage(`"rate_limit"`)}); a.Detail != "rate_limit" {
		t.Errorf("detail %+v", a)
	}
	if a := Map(Event{Name: "Notification", SessionID: "g", NotificationType: "task_complete"}); a.Status != session.Idle {
		t.Errorf("task_complete %+v", a)
	}
	// snake_case und PascalCase fuehren zum selben Ergebnis; PascalCase aus Claude gewinnt
	if a := Map(Event{Name: "Stop", NameGrok: "stop", SessionID: "x"}); a.Status != session.Idle {
		t.Errorf("%+v", a)
	}
}

func TestErrorFieldLenient(t *testing.T) {
	var ev Event
	if err := json.Unmarshal([]byte(`{"sessionId":"g","hookEventName":"stop_failure","error":{"type":"rate_limit"}}`), &ev); err != nil {
		t.Fatal(err)
	}
	if a := Map(ev); a.Status != session.Error || a.Detail != "" {
		t.Errorf("%+v", a)
	}
}

func TestDetectAgent(t *testing.T) {
	none := func(string) string { return "" }
	grokEnv := func(k string) string {
		if k == "GROK_SESSION_ID" {
			return "01a1"
		}
		return ""
	}
	for _, c := range []struct {
		name string
		env  func(string) string
		want string
	}{
		{"grok-1.0.46-mac", none, "grok"},
		{"claude", none, "claude-code"},
		{"node", grokEnv, "grok"},
		{"claude", grokEnv, "claude-code"},
		{"", none, "claude-code"},
	} {
		if got := DetectAgent(c.name, c.env); got != c.want {
			t.Errorf("%q: %s, erwartet %s", c.name, got, c.want)
		}
	}
}

func TestMapNewWaitingTypes(t *testing.T) {
	for _, nt := range []string{"elicitation_url_dialog", "agent_needs_input"} {
		if a := Map(Event{Name: "Notification", SessionID: "s", NotificationType: nt}); a.Skip || a.Status != session.Waiting {
			t.Errorf("%s: %+v", nt, a)
		}
	}
}

func TestMapUnknownSkipped(t *testing.T) {
	for _, ev := range []Event{
		{Name: "SubagentStop", SessionID: "s"},
		{Name: "Notification", SessionID: "s", NotificationType: "auth_success"},
		{Name: "", SessionID: "s"},
		{Name: "Stop", SessionID: ""},
		{Name: "Stop", SessionID: "../boese"},
	} {
		if !Map(ev).Skip {
			t.Errorf("haette uebersprungen werden muessen: %+v", ev)
		}
	}
}

func TestApplyWritesNoTitleAndAgent(t *testing.T) {
	dir := t.TempDir()
	Apply(dir, "grok", fixture(t, "grok_user_prompt_submit.json"), 42, t0)
	s, ok := session.Load(dir, "grok", "01a112cd-0000-7000-8000-000000000001")
	if !ok || s.Title != "" || s.PID != 42 || s.CWD != "/tmp/probe" || s.Status != session.Working {
		t.Fatalf("%+v", s)
	}
	Apply(dir, "grok", Event{NameGrok: "session_end", SessionIDGrok: "01a112cd-0000-7000-8000-000000000001"}, 42, t0)
	if _, ok := session.Load(dir, "grok", "01a112cd-0000-7000-8000-000000000001"); ok {
		t.Fatal("SessionEnd muss die Datei entfernen")
	}
}

func TestSessionStartCompactSkipped(t *testing.T) {
	if !Map(Event{Name: "SessionStart", SessionID: "s", Source: "compact"}).Skip {
		t.Error("compact muss uebersprungen werden")
	}
	if Map(Event{Name: "SessionStart", SessionID: "s", Source: "startup"}).Skip {
		t.Error("startup darf nicht uebersprungen werden")
	}
}

func TestMapPreToolUse(t *testing.T) {
	cases := []struct {
		tool string
		want Action
	}{
		{"AskUserQuestion", Action{Status: session.Waiting, Detail: "Claude asks you a question"}},
		{"ExitPlanMode", Action{Status: session.Waiting, Detail: "Claude waits for plan approval"}},
		{"Bash", Action{Skip: true}},
	}
	for _, c := range cases {
		got := Map(Event{Name: "PreToolUse", SessionID: "s1", ToolName: c.tool})
		if got != c.want {
			t.Errorf("%s: %+v", c.tool, got)
		}
	}
}

func TestMapStopQuestion(t *testing.T) {
	cases := map[string]session.Status{
		"Soll ich weitermachen?":     session.Waiting,
		"Soll ich weitermachen?**\n": session.Waiting,
		"Fertig.":                    session.Idle,
		"":                           session.Idle,
	}
	for msg, want := range cases {
		got := Map(Event{Name: "Stop", SessionID: "s1", LastMessage: msg})
		if got.Status != want {
			t.Errorf("%q: %v", msg, got.Status)
		}
	}
	if got := Map(Event{Name: "Stop", SessionID: "s1", LastMessage: "Ok (oder?)"}); got.Status != session.Waiting || got.Detail != "Claude asked you something" {
		t.Errorf("%+v", got)
	}
}

func tx(t *testing.T, lines ...string) (string, string) {
	root := t.TempDir()
	return writeTranscript(t, root, strings.Join(lines, "\n")+"\n"), root
}

func TestLastAssistantText(t *testing.T) {
	text := `{"type":"assistant","message":{"content":[{"type":"text","text":"Erst\nSoll ich?**"}]}}`
	tool := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash"}]}}`
	user := `{"type":"user","message":{"content":"hallo"}}`
	p, root := tx(t, user, text, user)
	if got := lastAssistantText(p, root); !strings.HasSuffix(got, "Soll ich?**") {
		t.Errorf("%q", got)
	}
	p, root = tx(t, text, tool)
	if got := lastAssistantText(p, root); got != "" {
		t.Errorf("Werkzeug zuletzt: %q", got)
	}
	if got := lastAssistantText("", root); got != "" {
		t.Error("kein Pfad")
	}
}

func TestApplyStopFromTranscript(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	p := writeTranscript(t, root, `{"type":"assistant","message":{"content":[{"type":"text","text":"Passt das so?**"}]}}`+"\n")
	dir := t.TempDir()
	if err := Apply(dir, Agent, Event{Name: "Stop", SessionID: "s1", TranscriptPath: p}, 0, t0); err != nil {
		t.Fatal(err)
	}
	s, _ := session.Load(dir, Agent, "s1")
	if s.Status != session.Waiting {
		t.Errorf("%+v", s)
	}
	if err := Apply(dir, Agent, Event{Name: "Stop", SessionID: "s2"}, 0, t0); err != nil {
		t.Fatal(err)
	}
	s, _ = session.Load(dir, Agent, "s2")
	if s.Status != session.Idle {
		t.Errorf("%+v", s)
	}
}
