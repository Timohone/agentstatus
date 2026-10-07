package session

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Entry struct {
	Session
	Stale bool `json:"stale"`
}

const (
	staleAfter  = 24 * time.Hour
	expireAfter = 7 * 24 * time.Hour
)

// Parse akzeptiert nur, was das Protokoll v1 verlangt; alles andere wird uebersprungen.
func Parse(b []byte) (Session, bool) {
	var s Session
	if json.Unmarshal(b, &s) != nil {
		return Session{}, false
	}
	if s.V != 1 || !ValidName(s.Agent) || !ValidName(s.ID) || !s.Status.Valid() || s.Updated.IsZero() {
		return Session{}, false
	}
	if s.PID < 0 || s.PID > math.MaxInt32 {
		return Session{}, false
	}
	if s.Since.IsZero() {
		s.Since = s.Updated
	}
	return s, true
}

func rank(e Entry) int {
	if e.Stale {
		return 4
	}
	switch e.Status {
	case Waiting:
		return 0
	case Working:
		return 1
	case Error:
		return 2
	}
	return 3
}

// List liest alle Sessions und raeumt auf. boot ist der Systemstart (Zeitnull = unbekannt):
// eine Session mit pid, die vor dem Start zuletzt aktualisiert wurde, ist ein Geist (pid-Wiederverwendung).
func List(dir string, now time.Time, alive func(int) bool, boot time.Time) ([]Entry, error) {
	files, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, f := range files {
		name := f.Name()
		if f.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		full := filepath.Join(dir, name)
		b, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		s, ok := Parse(b)
		if !ok {
			continue
		}
		age := now.Sub(s.Updated)
		if (s.PID > 0 && (!alive(s.PID) || (!boot.IsZero() && s.Updated.Before(boot)))) || (s.PID == 0 && age > expireAfter) {
			os.Remove(full)
			continue
		}
		out = append(out, Entry{Session: s, Stale: s.PID == 0 && age > staleAfter})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := rank(out[i]), rank(out[j]); ri != rj {
			return ri < rj
		}
		return out[i].Since.Before(out[j].Since)
	})
	return out, nil
}
