package claude

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Timohone/agentstatus/internal/session"
)

func writeTranscript(t *testing.T, root, body string) string {
	t.Helper()
	p := filepath.Join(root, "projects", "p", "t.jsonl")
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadTopicLastWins(t *testing.T) {
	root := t.TempDir()
	p := writeTranscript(t, root, `{"type":"user"}
{"type":"ai-title","aiTitle":"Erstes","sessionId":"s"}
kaputte zeile {{{
{"type":"ai-title","aiTitle":"Zweites","sessionId":"s"}
{"type":"assistant"}
`)
	if got := readTopic(p, root); got != "Zweites" {
		t.Errorf("got %q", got)
	}
}

func TestReadTopicNone(t *testing.T) {
	root := t.TempDir()
	p := writeTranscript(t, root, `{"type":"user"}`+"\n")
	if got := readTopic(p, root); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestReadTopicTailLimit(t *testing.T) {
	root := t.TempDir()
	filler := strings.Repeat(`{"type":"user","x":"`+strings.Repeat("a", 900)+`"}`+"\n", 400) // > 256 KB
	title := `{"type":"ai-title","aiTitle":"Thema"}` + "\n"
	if got := readTopic(writeTranscript(t, root, title+filler), root); got != "" {
		t.Errorf("Titel nur am Anfang: %q", got)
	}
	if got := readTopic(writeTranscript(t, root, filler+title), root); got != "Thema" {
		t.Errorf("Titel am Ende: %q", got)
	}
}

func TestReadTopicOutsideRoot(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	p := writeTranscript(t, other, `{"type":"ai-title","aiTitle":"Fremd"}`+"\n")
	if got := readTopic(p, root); got != "" {
		t.Errorf("ausserhalb: %q", got)
	}
	if got := readTopic("relativ/t.jsonl", root); got != "" {
		t.Errorf("relativ: %q", got)
	}
	// Symlink aus dem Root heraus
	link := filepath.Join(root, "link.jsonl")
	os.Symlink(p, link)
	if got := readTopic(link, root); got != "" {
		t.Errorf("symlink: %q", got)
	}
}

func TestApplyWritesTopic(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	p := writeTranscript(t, root, `{"type":"ai-title","aiTitle":"Backup check"}`+"\n")
	Apply(dir, Agent, Event{Name: "UserPromptSubmit", SessionID: "s1", TranscriptPath: p}, 5, t0)
	s, _ := session.Load(dir, Agent, "s1")
	if s.Topic != "Backup check" {
		t.Errorf("%+v", s)
	}
	// ohne Pfad bleibt das Thema erhalten
	Apply(dir, Agent, Event{Name: "Stop", SessionID: "s1"}, 5, t0)
	if s, _ = session.Load(dir, Agent, "s1"); s.Topic != "Backup check" {
		t.Errorf("verloren: %+v", s)
	}
}

func TestReadTopicFIFO(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "f.jsonl")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Skip(err)
	}
	done := make(chan string, 1)
	go func() { done <- readTopic(p, root) }()
	select {
	case got := <-done:
		if got != "" {
			t.Errorf("got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("readTopic blockiert an einem FIFO")
	}
}
