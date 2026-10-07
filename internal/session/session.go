// Package session implementiert das agentstatus-Protokoll v1: eine JSON-Datei je Session.
package session

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode"
)

type Status string

const (
	Working Status = "working"
	Waiting Status = "waiting"
	Idle    Status = "idle"
	Error   Status = "error"
)

func (s Status) Valid() bool {
	return s == Working || s == Waiting || s == Idle || s == Error
}

type Session struct {
	V       int       `json:"v"`
	Agent   string    `json:"agent"`
	ID      string    `json:"id"`
	Status  Status    `json:"status"`
	CWD     string    `json:"cwd,omitempty"`
	Title   string    `json:"title,omitempty"`
	Topic   string    `json:"topic,omitempty"`
	PID     int       `json:"pid,omitempty"`
	Since   time.Time `json:"since"`
	Updated time.Time `json:"updated"`
	Detail  string    `json:"detail,omitempty"`
	NoPID   bool      `json:"-"` // nur fuer Set: die gespeicherte pid entfernen statt behalten
}

const (
	maxDetail = 200
	maxTopic  = 120
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// ValidName: nur sichere Zeichen, nie "." oder "..", damit der Dateiname eindeutig und harmlos ist.
func ValidName(s string) bool {
	return nameRe.MatchString(s) && s != "." && s != ".."
}

func Dir() string {
	if d := os.Getenv("AGENTSTATUS_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "agentstatus", "sessions")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agentstatus", "sessions")
}

func path(dir, agent, id string) string {
	return filepath.Join(dir, agent+"@"+id+".json")
}

func Load(dir, agent, id string) (Session, bool) {
	b, err := os.ReadFile(path(dir, agent, id))
	if err != nil {
		return Session{}, false
	}
	var s Session
	if json.Unmarshal(b, &s) != nil {
		return Session{}, false
	}
	return s, true
}

// Clean macht Text fuer die Terminalausgabe harmlos: Zeilenumbruch und Tab werden zu
// einem Leerzeichen, alle anderen Steuerzeichen (ESC, BEL, DEL, C1) fallen weg.
func Clean(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, s))
}

// Set mischt in die bestehende Datei: leere Felder von in behalten den alten Wert.
func Set(dir string, in Session, now time.Time) (Session, error) {
	if !ValidName(in.Agent) || !ValidName(in.ID) {
		return Session{}, fmt.Errorf("agent and id may only contain [A-Za-z0-9._-]")
	}
	if in.PID < 0 || in.PID > math.MaxInt32 {
		return Session{}, fmt.Errorf("pid must be between 1 and %d", math.MaxInt32)
	}
	if !in.Status.Valid() {
		return Session{}, fmt.Errorf("status must be working, waiting, idle or error, not %q", in.Status)
	}
	in.Title, in.Detail, in.CWD, in.Topic = Clean(in.Title), Clean(in.Detail), Clean(in.CWD), Clean(in.Topic)
	s, ok := Load(dir, in.Agent, in.ID)
	if !ok || s.Status != in.Status || s.Since.IsZero() {
		s.Since = now
	}
	s.V, s.Agent, s.ID, s.Status, s.Updated = 1, in.Agent, in.ID, in.Status, now
	if in.CWD != "" {
		s.CWD = in.CWD
	}
	if in.Title != "" {
		s.Title = in.Title
	}
	if in.Topic != "" {
		s.Topic = in.Topic
		if r := []rune(s.Topic); len(r) > maxTopic {
			s.Topic = string(r[:maxTopic])
		}
	}
	if in.NoPID {
		s.PID = 0
	} else if in.PID != 0 {
		s.PID = in.PID
	}
	s.Detail = in.Detail // Detail gilt nur fuer den aktuellen Zustand
	if r := []rune(s.Detail); len(r) > maxDetail {
		s.Detail = string(r[:maxDetail])
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Session{}, err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return Session{}, err
	}
	final := path(dir, s.Agent, s.ID)
	tmp := fmt.Sprintf("%s.tmp-%d", final, os.Getpid())
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return Session{}, err
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return Session{}, err
	}
	return s, nil
}

func Clear(dir, agent, id string) error {
	if !ValidName(agent) || !ValidName(id) {
		return fmt.Errorf("agent and id may only contain [A-Za-z0-9._-]")
	}
	err := os.Remove(path(dir, agent, id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// SecondLine: Detail (bei waiting/error), sonst Thema, sonst bei mehrdeutigem Namen "#" + 4 Zeichen der ID.
func SecondLine(s Session, ambiguous bool) string {
	if d := Clean(s.Detail); (s.Status == Waiting || s.Status == Error) && d != "" {
		return d
	}
	if t := Clean(s.Topic); t != "" {
		return t
	}
	if ambiguous {
		r := []rune(s.ID)
		if len(r) > 4 {
			r = r[:4]
		}
		return "#" + string(r)
	}
	return ""
}
