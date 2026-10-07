package codex

import (
	"encoding/json"
	"os"
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
		"session_start.json":                   {Status: session.Idle},
		"session_start_compact.json":           {Skip: true},
		"user_prompt_submit.json":              {Status: session.Working},
		"post_tool_use.json":                   {Status: session.Working},
		"permission_request.json":              {Status: session.Waiting, Detail: "Delete x"},
		"permission_request_noinput.json":      {Status: session.Waiting, Detail: "apply_patch"},
		"permission_request_input_string.json": {Status: session.Waiting, Detail: "mcp__x"},
		"permission_request_input_array.json":  {Status: session.Waiting, Detail: "mcp__x"},
		"permission_request_input_nodesc.json": {Status: session.Waiting, Detail: "Bash"},
		"stop.json":                            {Status: session.Idle},
		"interrupt.json":                       {Status: session.Idle},
		"session_end.json":                     {Clear: true},
		"pre_tool_use.json":                    {Skip: true},
	}
	for file, want := range cases {
		if got := Map(fixture(t, file)); got != want {
			t.Errorf("%s: %+v, erwartet %+v", file, got, want)
		}
	}
}

func TestMapInvalidID(t *testing.T) {
	if !Map(Event{Name: "Stop", SessionID: "../x"}).Skip {
		t.Error("ungueltige ID muss uebersprungen werden")
	}
}

func TestApplyLifecycle(t *testing.T) {
	dir := t.TempDir()
	if err := Apply(dir, Agent, fixture(t, "permission_request.json"), 42, t0); err != nil {
		t.Fatal(err)
	}
	s, ok := session.Load(dir, "codex", "c1")
	if !ok || s.Status != session.Waiting || s.PID != 42 || s.CWD != "/tmp/p" || s.Title != "" {
		t.Fatalf("%v %+v", ok, s)
	}
	Apply(dir, Agent, fixture(t, "interrupt.json"), 42, t0)
	if s, _ := session.Load(dir, "codex", "c1"); s.Status != session.Idle {
		t.Fatalf("%+v", s)
	}
	Apply(dir, Agent, fixture(t, "session_end.json"), 42, t0)
	if _, ok := session.Load(dir, "codex", "c1"); ok {
		t.Fatal("Datei muss weg sein")
	}
}

func TestApplyWithoutPIDDropsOldPID(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	ev := Event{Name: "UserPromptSubmit", SessionID: "dp"}
	Apply(dir, Agent, ev, 4242, now)
	Apply(dir, Agent, ev, 0, now)
	if s, _ := session.Load(dir, Agent, "dp"); s.PID != 0 {
		t.Fatalf("pid %d", s.PID)
	}
}
